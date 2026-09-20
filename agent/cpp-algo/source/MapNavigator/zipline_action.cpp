#include "zipline_action.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstring>
#include <optional>
#include <string>
#include <variant>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "position_provider.h"
#include "semantic_helpers.h"
#include "zipline_ride_machine.h"

#include "../utils.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

// 跑一个 pipeline 节点, 回答这一趟里点名的那个节点认没认出来。识别不中的节点不会进这一趟的
// 节点表, 所以「表里有且 completed」等于提示确实在屏幕上、动作也确实发了出去。
bool RunNodeAndReportHit(MaaContext* context, const char* entry, const char* node, const std::string& pipeline_override)
{
    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        return false;
    }
    const MaaTaskId task_id = MaaContextRunTask(context, entry, pipeline_override.c_str());
    if (task_id == MaaInvalidId) {
        LogWarn << "Zipline subtask failed to dispatch." << VAR(entry);
        return false;
    }

    ScopedStringBuffer entry_name;
    MaaSize node_count = 0;
    MaaStatus status = MaaStatus_Invalid;
    if (entry_name.Get() == nullptr || !MaaTaskerGetTaskDetail(tasker, task_id, entry_name.Get(), nullptr, &node_count, &status)
        || node_count == 0) {
        return false;
    }
    std::vector<MaaNodeId> node_ids(node_count);
    if (!MaaTaskerGetTaskDetail(tasker, task_id, entry_name.Get(), node_ids.data(), &node_count, &status)) {
        return false;
    }

    for (const MaaNodeId node_id : node_ids) {
        ScopedStringBuffer node_name;
        MaaRecoId reco_id = 0;
        MaaActId action_id = 0;
        MaaBool completed = 0;
        if (node_name.Get() == nullptr || !MaaTaskerGetNodeDetail(tasker, node_id, node_name.Get(), &reco_id, &action_id, &completed)) {
            continue;
        }
        const char* raw = MaaStringBufferGet(node_name.Get());
        if (raw != nullptr && std::strcmp(raw, node) == 0) {
            return completed != 0;
        }
    }
    return false;
}

// 出口的 next 截断掉, 子任务跑到那儿就返回引擎; roi、动作、按键一律留在 pipeline 里。
std::string BuildMountOverride()
{
    json::object exit_node;
    exit_node["next"] = json::array {};
    json::object root;
    root[kZiplineMountExitNode] = std::move(exit_node);
    return json::value(std::move(root)).dumps();
}

// 认出架子的交互提示才按下去。认不出就是这根架子不在跟前, 这一趟不该有任何按键发出去。
bool PressMountPrompt(MaaContext* context)
{
    return RunNodeAndReportHit(context, kZiplineMountEntryNode, kZiplineMountRecognitionNode, BuildMountOverride());
}

std::string BuildPitchResetOverride(int units)
{
    json::object param;
    param["dx"] = 0;
    param["dy"] = units;
    json::object reset_node;
    reset_node["custom_action_param"] = std::move(param);
    json::object root;
    root[kZiplinePitchResetNode] = std::move(reset_node);
    return json::value(std::move(root)).dumps();
}

// 阶段机的眼睛: 每拍定位一次, 把这一跳可能落到的架子都交给定位器当搜索先验。落地那一帧是
// 冷启动, 挂对了落在第一个, 挂错了落在其中一个, 空响没滑走就还在上索点, 位置由匹配分决定
class RuntimeZiplineObserver : public IZiplineObserver
{
public:
    explicit RuntimeZiplineObserver(const Context& ctx)
        : ctx_(ctx)
    {
    }

    ZiplineObservation Observe(const std::vector<ZiplineNodeRef>& hint_nodes) override
    {
        ZiplineObservation obs;
        obs.at = std::chrono::steady_clock::now();
        std::vector<maplocator::SearchHint> hints;
        const std::string zone = ctx_.position->zone_id;
        if (!zone.empty()) {
            for (const ZiplineNodeRef& node : hint_nodes) {
                hints.push_back(
                    maplocator::SearchHint { .zone_id = zone, .x = node.x, .y = node.y, .radius = kZiplineLandingHintRadiusWu });
            }
        }
        if (ctx_.position_provider->Capture(ctx_.position, false, {}, hints)) {
            obs.fix = *ctx_.position;
        }
        return obs;
    }

    // 两个信号按代价从低到高求值: 右上角按钮在架上收起, 故地面判据命中即判定角色在地面; 未命中时
    // 再读底部操作引导, 命中「离开滑索架」的片段才判定已上架
    MountVerdict CheckMounted() override
    {
        if (ctx_.maa_context == nullptr) {
            return MountVerdict::Unclear;
        }
        if (RunNodeAndReportHit(ctx_.maa_context, kZiplineOnGroundEntryNode, kZiplineOnGroundNode, "{}")) {
            return MountVerdict::OnGround;
        }
        const bool hint = RunNodeAndReportHit(ctx_.maa_context, kZiplineOnTowerHintEntryNode, kZiplineOnTowerHintNode, "{}");
        return hint ? MountVerdict::OnTower : MountVerdict::Unclear;
    }

    void ResetTracking() override { ctx_.position_provider->ResetTracking(); }

private:
    const Context& ctx_;
};

// 阶段机的手。每个动作同步发出、发完即回, 转没转到位由后面的观测说了算
class RuntimeZiplineActuator : public IZiplineActuator
{
public:
    explicit RuntimeZiplineActuator(const Context& ctx)
        : ctx_(ctx)
    {
    }

    // 俯仰没有可读反馈, 只能先把镜头拉到天空方向的硬限位, 再把这个已知位置记作最大俯仰。
    // 独立 Pipeline 节点通过相对鼠标移动承载实际输入, 不会像 Swipe 那样带一次左键按下/抬起。
    bool ResetPitchToMaximum() override
    {
        if (ctx_.maa_context == nullptr) {
            LogWarn << "Zipline aim: no pipeline context to reset the pitch.";
            return false;
        }
        const double reset_delta_deg =
            kZiplinePitchMaximumElevationDeg + kZiplinePitchMaximumDepressionDeg + kZiplinePitchResetOvershootDeg;
        const int units = static_cast<int>(std::lround(-reset_delta_deg * ctx_.action_wrapper->DefaultPitchUnitsPerDegree()));
        if (units == 0
            || !RunNodeAndReportHit(ctx_.maa_context, kZiplinePitchResetNode, kZiplinePitchResetNode, BuildPitchResetOverride(units))) {
            LogWarn << "Zipline aim: the pitch reset task did not complete." << VAR(kZiplinePitchResetNode) << VAR(units);
            return false;
        }
        return true;
    }

    // 一次只发后端一个批次, 剩下的角度由阶段机拿下一拍的朝向读数重算
    std::optional<double> TurnYaw(double delta_deg) override
    {
        const double cap = ctx_.action_wrapper->SteeringProfile().max_batch_delta_deg;
        const double step = std::clamp(delta_deg, -cap, cap);
        if (!TurnToHeadingOnce(ctx_, step)) {
            return std::nullopt;
        }
        return step;
    }

    bool TurnPitch(double delta_deg) override
    {
        // 屏幕坐标里 dy 向下为正, 抬头看上坡要往上拉, 所以取负
        const int units = static_cast<int>(std::lround(-delta_deg * ctx_.action_wrapper->DefaultPitchUnitsPerDegree()));
        return units == 0 || ctx_.action_wrapper->SendViewDeltaSync(0, units);
    }

    bool PressMount() override { return PressMountPrompt(ctx_.maa_context); }

    // 起滑就是对着瞄好的方向按一下左键
    void FireLaunch() override
    {
        ctx_.motion_controller->SetForwardState(false);
        ctx_.action_wrapper->ClickMouseLeftSync();
    }

    void Dismount() override
    {
        ctx_.action_wrapper->MouseRightDownSync(kZiplineDismountHoldMs);
        ctx_.action_wrapper->MouseRightUpSync(0);
    }

    void Wait(int32_t ms) override { utils::SleepFor(ms); }

private:
    const Context& ctx_;
};

// 滑索的每一条异常出口都从这里走。索是捷径不是必经之路，捷径走不成就丢掉剩余链并重新接回
// 后续路线；不能在人挂在索上或者卡在架子边上时直接结束，否则角色只会原地不动到超时。
// stay_on_tower 时人留在架子上等重规划: 新路线要是仍从这根架子起滑, 就省一次上索
Result DropChainAndRecover(const Context& ctx, const char* reason, const char* detail, bool stay_on_tower)
{
    Result result;
    StopMotionAndCommitment(ctx);
    if (!stay_on_tower) {
        LeaveZiplineTower(ctx);
    }

    // 还没走完的接近段全是走廊上的普通点，链的头一跳就跟在它们后面。先碰到别的语义点就说明
    // 前面根本没有链——最后一跳出事时就是这样，剩下的路本来就是走路，一个点都不该丢。
    const std::vector<Waypoint>& path = ctx.session->current_path();
    size_t hop = ctx.session->current_node_idx();
    while (hop < path.size() && path[hop].IsContinuousRun()) {
        ++hop;
    }
    // 整条链一起丢。留下任何一跳，重规划都会把人送回索边再试一次，而刚失败的正是这条索。
    size_t dropped = 0;
    while (hop + dropped < path.size() && path[hop + dropped].action == ActionType::ZIPLINE) {
        ++dropped;
    }
    if (dropped != 0) {
        ctx.session->SkipPastWaypoint(hop + dropped - 1, reason);
    }

    LogWarn << "Action: ZIPLINE given up, recovering from a fresh position." << VAR(reason) << VAR(detail) << VAR(dropped)
            << VAR(stay_on_tower) << VAR(ctx.position->x) << VAR(ctx.position->y);

    ctx.runtime_state->zipline_approach.Reset();
    ctx.runtime_state->OnWaypointAdvance();
    ctx.runtime_state->route.Reset();
    ctx.position_provider->ResetTracking();
    ctx.session->ResetProgress();
    // 剩下的路是照着「从落点出发」规划的，人却可能仍在索这一头。先丢掉滑行期间的跟踪状态，
    // 等连续新定位重新贴回 navmesh，再从剩余路线里找第一个实际可达的接入点。OnWaypointAdvance
    // 会清掉恢复状态和重规划标志，所以两者只能压在它后面。
    ctx.runtime_state->zipline_recovery.Begin(std::chrono::steady_clock::now());
    ctx.runtime_state->dynamic_replan_requested = true;

    SelectPhaseForCurrentWaypoint(ctx, reason);
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

// 一跳到了。航点只在这里推进; 落在中继架上就直接开下一跳, 链尾才下索
Result FinishHop(const Context& ctx, const HopCompleted& done)
{
    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;

    ctx.session->NoteCanonicalFinalGoalConsumed(ctx.session->CurrentAbsoluteNodeIndex(), done.at, "zipline_ride_complete");
    ctx.session->AdvanceToNextWaypoint(ActionType::ZIPLINE, "zipline_ride_complete");
    ctx.runtime_state->OnWaypointAdvance();
    LogInfo << "Action: ZIPLINE ride landed." << VAR(done.at.x) << VAR(done.at.y) << VAR(done.still_on_tower);
    if (!done.at.zone_id.empty()) {
        ctx.session->UpdateCurrentZone(done.at.zone_id);
    }
    ctx.session->ResetProgress();
    ctx.runtime_state->ResetNavigationAssistState();
    ctx.runtime_state->route.Reset();
    // 滑行本身就是一次实打实的位移，落点还是连着几帧稳定定位确认过的，起步闸没有再拦一次的
    // 道理。链上的下一跳尤其：上索点就在脚下，拦住就等于要求人先走开再走回来
    ctx.runtime_state->route.startup_anchor_pos = done.at;
    ctx.runtime_state->route.startup_anchor_initialized = true;
    ctx.runtime_state->route.startup_motion_confirmed = true;
    ctx.position_provider->ResetTracking();

    if (!ctx.session->HasCurrentWaypoint()) {
        LeaveZiplineTower(ctx);
        ctx.session->NoteRouteTailConsumed(done.at, "route_tail_consumed");
        return result;
    }
    if (done.still_on_tower) {
        if (CurrentHopStartsUnderfoot(ctx)) {
            return StartZiplineHop(ctx, ctx.session->CurrentWaypoint(), 0.0);
        }
        // 规划说续跳, 路线却没接上同一根架子: 下来走
        LeaveZiplineTower(ctx);
    }
    SelectPhaseForCurrentWaypoint(ctx, "zipline_ride_complete");
    return result;
}

} // namespace

// 这个站位上没出提示。面板给的是离身位最近的那台设备, 原地重按拿到的还是同一个答案, 所以改瞄
// 计划里的下一个站位, 走过去的这一路提示预筛照样开着, 先冒出来就先按下去。站位全试过还不出提示
// 就把这根架子记成上不去, 退索走路
Result AdvanceMountSpot(const Context& ctx, const Waypoint& waypoint, const char* reason)
{
    ZiplineApproachState& approach = ctx.runtime_state->zipline_approach;
    const size_t spot_count = waypoint.zipline_hop ? waypoint.zipline_hop->mount_spots.size() : 0;
    const size_t cursor = waypoint.zipline_hop ? approach.MountSpotCursor(waypoint.zipline_hop->mount) : 0;
    if (cursor + 1 >= spot_count) {
        if (waypoint.zipline_hop) {
            ctx.runtime_state->zipline_ride.MarkMountUnreachable(*waypoint.zipline_hop);
        }
        return AbandonZipline(ctx, "zipline_prompt_missing", "no mount prompt from any stand point at this tower");
    }

    approach.spot_index = cursor + 1;
    approach.press_missed = true;
    const ZiplineMountSpot& spot = waypoint.zipline_hop->mount_spots[approach.spot_index];
    const bool retargeted = ctx.session->RetargetCurrentWaypoint(spot.x, spot.y, reason);
    // 顶在设备上走不动也算这个站位试过了。硬时钟不归零, 下一拍就会把剩下的站位一口气烧光
    ctx.session->ResetHardProgress();
    LogWarn << "Action: ZIPLINE no mount prompt here; walking to the next stand point." << VAR(reason) << VAR(retargeted)
            << VAR(approach.spot_index) << VAR(spot_count) << VAR(spot.x) << VAR(spot.y);
    ctx.runtime_state->route.Reset();
    ctx.runtime_state->route.startup_anchor_pos = *ctx.position;
    ctx.runtime_state->route.startup_anchor_initialized = true;
    ctx.runtime_state->route.startup_motion_confirmed = true;
    ctx.position_provider->ResetTracking();
    SelectPhaseForCurrentWaypoint(ctx, reason);

    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

bool CurrentHopStartsUnderfoot(const Context& ctx)
{
    const std::optional<ZiplineNodeRef> underfoot = ctx.runtime_state->zipline_ride.TowerUnderfoot();
    if (!underfoot || !ctx.session->HasCurrentWaypoint()) {
        return false;
    }
    const Waypoint& next = ctx.session->CurrentWaypoint();
    return next.action == ActionType::ZIPLINE && next.zipline_hop && next.zipline_hop->mount.SameTower(*underfoot);
}

Result AbandonZipline(const Context& ctx, const char* reason, const char* detail)
{
    return DropChainAndRecover(ctx, reason, detail, /*stay_on_tower=*/false);
}

void LeaveZiplineTower(const Context& ctx)
{
    ZiplineRideMachine& ride = ctx.runtime_state->zipline_ride;
    const bool was_on_tower = ride.OnTower();
    RuntimeZiplineActuator actuator(ctx);
    ride.Dismount(actuator);
    // 刚下来的那几百毫秒移动指令还会被架子吃掉
    if (was_on_tower) {
        utils::SleepFor(kZiplineLaunchSettleMs);
    }
}

Result StartZiplineHop(const Context& ctx, const Waypoint& waypoint, double actual_distance)
{
    Result result;
    StopMotionAndCommitment(ctx);

    // 这一跳的计划是规划器算出来写进点里的，手写路线写不出来。缺了就没有可对准的方向，
    // 与其对着 (0,0) 转镜头再瞎按一下，不如当这根索不存在
    if (!waypoint.zipline_hop) {
        return AbandonZipline(ctx, "zipline_hop_missing", "waypoint carries no hop plan");
    }

    // 脚下这根不是这一跳的上索架时先下来: 留在上面瞄, 发射的是别的架子上的索, 滑到哪都不算数,
    // 还会往账本里记一笔本来没问题的索
    ZiplineRideMachine& ride = ctx.runtime_state->zipline_ride;
    const std::optional<ZiplineNodeRef> underfoot = ride.TowerUnderfoot();
    if (underfoot && !waypoint.zipline_hop->mount.SameTower(*underfoot)) {
        LogWarn << "Standing on a tower this hop does not start from; stepping down first." << VAR(underfoot->x) << VAR(underfoot->y)
                << VAR(waypoint.zipline_hop->mount.x) << VAR(waypoint.zipline_hop->mount.y);
        LeaveZiplineTower(ctx);
    }

    // 链首要先站上架子; 中途落下来人已经站在下一根上, 直接接着瞄就行
    if (!ride.OnTower()) {
        if (ctx.maa_context == nullptr) {
            return AbandonZipline(ctx, "zipline_no_context", "no pipeline context to recognize the mount prompt");
        }
        if (!PressMountPrompt(ctx.maa_context)) {
            // 预筛叫停的这一次人还没走到, 认不出就是那个图标不属于滑索架: 当没发生, 接着走
            if (ctx.runtime_state->semantic.zipline_prompt_probe) {
                LogInfo << "Zipline mount pre-filter did not hold up; keeping the approach." << VAR(actual_distance);
                return result;
            }
            // 认不出都得靠挪身位解决: 人差一点点没走到跟前、站位猜的方向上没有设备模型、或者架子
            // 边上那根供电桩把面板占着。交回导航换下一个站位, 预筛一路开着, 提示先冒出来就先按下去
            LogInfo << "No mount prompt at this stand point; moving on to the next one." << VAR(actual_distance);
            return AdvanceMountSpot(ctx, waypoint, "zipline_mount_no_prompt");
        }
        // 此处只负责发出上索按键, 是否已上架由阶段机的 Mounting 段判定; 判定落定前不瞄准也不发射
        ctx.runtime_state->zipline_approach.press_missed = false;
    }

    ride.Begin(*waypoint.zipline_hop);
    LogInfo << "Action: ZIPLINE hop started." << VAR(waypoint.zipline_hop->landing.x) << VAR(waypoint.zipline_hop->landing.y)
            << VAR(waypoint.zipline_hop->planned_elevation_deg) << VAR(waypoint.zipline_hop->chain_continues) << VAR(actual_distance);
    // 航点等落地再推进: 起滑那一刻人还在上索点, 这条链就算已经是路线的尾巴也不能在这里收工
    ctx.session->UpdatePhase(NaviPhase::WaitZipline, "zipline_hop_started");
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

// 滑行中的每一拍都交给阶段机, 这里只把它的出口事件接回导航
Result TickZiplineRide(const Context& ctx)
{
    Result result;
    ctx.motion_controller->SetForwardState(false);

    ZiplineRideMachine& ride = ctx.runtime_state->zipline_ride;
    if (ride.stage() == ZiplineStage::Idle) {
        return AbandonZipline(ctx, "zipline_ride_idle", "waiting on a ride that is not running");
    }
    RuntimeZiplineObserver observer(ctx);
    RuntimeZiplineActuator actuator(ctx);
    const StageResult outcome = ride.Tick(observer, actuator);

    if (const auto* done = std::get_if<HopCompleted>(&outcome)) {
        return FinishHop(ctx, *done);
    }
    if (const auto* abandoned = std::get_if<ChainAbandoned>(&outcome)) {
        return AbandonZipline(ctx, abandoned->reason, "the ride machine gave this chain up");
    }
    if (const auto* replan = std::get_if<ReplanRequested>(&outcome)) {
        return DropChainAndRecover(
            ctx,
            "zipline_replan_requested",
            replan->still_on_tower ? "waiting on the tower for a new route" : "this hop is dead, re-routing from the ground",
            replan->still_on_tower);
    }
    if (std::get_if<NeedsReposition>(&outcome) != nullptr) {
        return AdvanceMountSpot(ctx, ctx.session->CurrentWaypoint(), "zipline_mount_unmounted");
    }
    result.stay_in_current_tick = true;
    utils::SleepFor(kZiplineRideRetryIntervalMs);
    return result;
}

} // namespace semantic_nodes

} // namespace mapnavigator
