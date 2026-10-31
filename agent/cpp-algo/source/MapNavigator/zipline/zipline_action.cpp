#include "zipline_action.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <optional>
#include <string>
#include <variant>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "../semantic/heading_turn.h"
#include "../semantic/semantic_helpers.h"
#include "../sensing/maa_task.h"
#include "../sensing/position_provider.h"
#include "zipline_ride_machine.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

// 识别不中的节点不进本趟节点表, 表里有它就说明提示在屏幕上且动作已发出
bool run_node_and_report_hit(MaaContext* context, const char* entry, const char* node, const std::string& pipeline_override)
{
    return RunTaskAndCheckNode(context, entry, node, pipeline_override).value_or(false);
}

std::string build_mount_override()
{
    json::object exit_node;
    exit_node["next"] = json::array {};
    json::object root;
    root[kZiplineMountExitNode] = std::move(exit_node);
    return json::value(std::move(root)).dumps();
}

bool press_mount_prompt(MaaContext* context)
{
    return run_node_and_report_hit(context, kZiplineMountEntryNode, kZiplineMountRecognitionNode, build_mount_override());
}

std::string build_pitch_reset_override(int units)
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

class RuntimeZiplineObserver : public IZiplineObserver
{
public:
    explicit RuntimeZiplineObserver(const Context& ctx)
        : ctx_(ctx)
    {
    }

    ZiplineObservation observe(const std::vector<ZiplineNodeRef>& hint_nodes, bool force_global_search) override
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
        if (ctx_.position_provider->capture(ctx_.position, force_global_search, {}, hints)) {
            obs.fix = *ctx_.position;
        }
        return obs;
    }

    // 右上角按钮上架后会收起, 所以命中地面判据即在地面, 否则再看底部引导
    MountVerdict checkMounted() override
    {
        if (ctx_.maa_context == nullptr) {
            return MountVerdict::Unclear;
        }
        if (run_node_and_report_hit(ctx_.maa_context, kZiplineOnGroundEntryNode, kZiplineOnGroundNode, "{}")) {
            return MountVerdict::OnGround;
        }
        const bool hint = run_node_and_report_hit(ctx_.maa_context, kZiplineOnTowerHintEntryNode, kZiplineOnTowerHintNode, "{}");
        return hint ? MountVerdict::OnTower : MountVerdict::Unclear;
    }

    void resetTracking() override { ctx_.position_provider->resetTracking(); }

private:
    const Context& ctx_;
};

class RuntimeZiplineActuator : public IZiplineActuator
{
public:
    explicit RuntimeZiplineActuator(const Context& ctx)
        : ctx_(ctx)
    {
    }

    // 俯仰无读数, 只能拉到天空方向的硬限位当已知位置; 走 pipeline 节点是因为 Swipe 会带一次左键
    bool resetPitchToMaximum() override
    {
        const double reset_delta_deg =
            kZiplinePitchMaximumElevationDeg + kZiplinePitchMaximumDepressionDeg + kZiplinePitchResetOvershootDeg;
        const int units = static_cast<int>(std::lround(-reset_delta_deg * ctx_.action_wrapper->defaultPitchUnitsPerDegree()));
        if (ctx_.action_wrapper->usesTouchBackend()) {
            return units != 0 && ctx_.action_wrapper->sendViewDeltaSync(0, units);
        }
        if (ctx_.maa_context == nullptr) {
            LogWarn << "Zipline aim: no pipeline context to reset the pitch.";
            return false;
        }
        if (units == 0
            || !run_node_and_report_hit(
                ctx_.maa_context,
                kZiplinePitchResetNode,
                kZiplinePitchResetNode,
                build_pitch_reset_override(units))) {
            LogWarn << "Zipline aim: the pitch reset task did not complete." << VAR(kZiplinePitchResetNode) << VAR(units);
            return false;
        }
        return true;
    }

    std::optional<double> turnYaw(double delta_deg) override
    {
        const double cap = ctx_.action_wrapper->steeringProfile().max_batch_delta_deg;
        const double step = std::clamp(delta_deg, -cap, cap);
        if (!TurnToHeadingOnce(ctx_, step)) {
            return std::nullopt;
        }
        return step;
    }

    bool turnPitch(double delta_deg) override
    {
        // 屏幕 dy 向下为正, 抬头要取负
        const int units = static_cast<int>(std::lround(-delta_deg * ctx_.action_wrapper->defaultPitchUnitsPerDegree()));
        return units == 0 || ctx_.action_wrapper->sendViewDeltaSync(0, units);
    }

    bool pressMount() override { return press_mount_prompt(ctx_.maa_context); }

    void fireLaunch() override
    {
        ctx_.motion_controller->setForwardState(false);
        ctx_.action_wrapper->triggerZiplineLaunchSync();
    }

    void dismount() override { ctx_.action_wrapper->triggerZiplineDismountSync(kZiplineDismountHoldMs); }

    void wait(int32_t ms) override { SleepFor(ms); }

private:
    const Context& ctx_;
};

size_t skip_continuous_run(const std::vector<Waypoint>& path, size_t index)
{
    while (index < path.size() && path[index].isContinuousRun()) {
        ++index;
    }
    return index;
}

Result drop_chain_and_recover(const Context& ctx, const char* reason, const char* detail, bool stay_on_tower)
{
    Result result;
    StopMotionAndCommitment(ctx);
    if (!stay_on_tower) {
        LeaveZiplineTower(ctx);
    }

    const std::vector<Waypoint>& path = ctx.session->currentPath();
    const size_t hop = skip_continuous_run(path, ctx.session->currentNodeIdx());
    // 整条链一起丢, 留一跳重规划就会把人送回刚失败的索
    size_t dropped = 0;
    while (hop + dropped < path.size() && path[hop + dropped].action == ActionType::ZIPLINE) {
        ++dropped;
    }
    if (dropped != 0) {
        ctx.session->skipPastWaypoint(hop + dropped - 1, reason);
    }
    if (stay_on_tower) {
        ctx.session->noteStandingTower(ctx.runtime_state->zipline_ride.towerUnderfoot());
    }

    LogWarn << "Action: ZIPLINE given up, recovering from a fresh position." << VAR(reason) << VAR(detail) << VAR(dropped)
            << VAR(stay_on_tower) << VAR(ctx.position->x) << VAR(ctx.position->y);

    ctx.runtime_state->zipline_approach.reset();
    ctx.runtime_state->onWaypointAdvance();
    ctx.runtime_state->route.reset();
    ctx.position_provider->resetTracking();
    ctx.session->resetProgress();
    // onWaypointAdvance 会清掉恢复状态和重规划标志, 这两行必须在它后面
    ctx.runtime_state->zipline_recovery.begin(std::chrono::steady_clock::now());
    ctx.runtime_state->dynamic_replan_requested = true;

    SelectPhaseForCurrentWaypoint(ctx, reason);
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

Result finish_hop(const Context& ctx, const HopCompleted& done)
{
    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;

    ctx.session->noteCanonicalFinalGoalConsumed(ctx.session->currentAbsoluteNodeIndex(), done.at, "zipline_ride_complete");
    ctx.session->advanceToNextWaypoint(ActionType::ZIPLINE, "zipline_ride_complete");
    ctx.runtime_state->onWaypointAdvance();
    LogInfo << "Action: ZIPLINE ride landed." << VAR(done.at.x) << VAR(done.at.y) << VAR(done.still_on_tower);
    if (!done.at.zone_id.empty()) {
        ctx.session->updateCurrentZone(done.at.zone_id);
    }
    ctx.session->resetProgress();
    ctx.runtime_state->resetNavigationAssistState();
    ctx.runtime_state->route.reset();
    // 起步确认直接置位, 下一跳的上索点就在脚下, 拦住就得先走开再走回来
    ctx.runtime_state->route.confirmStartupAt(done.at);
    ctx.position_provider->resetTracking();

    if (!ctx.session->hasCurrentWaypoint()) {
        LeaveZiplineTower(ctx);
        ctx.session->noteRouteTailConsumed(done.at, "route_tail_consumed");
        return result;
    }
    if (done.still_on_tower) {
        if (SkipToHopUnderfoot(ctx, "zipline_ride_complete")) {
            return StartZiplineHop(ctx, ctx.session->currentWaypoint(), 0.0);
        }
        LeaveZiplineTower(ctx);
    }
    ctx.runtime_state->dynamic_replan_requested = true;
    SelectPhaseForCurrentWaypoint(ctx, "zipline_ride_complete");
    return result;
}

} // namespace

// 面板只给离身位最近的设备, 原地重按还是同一个答案, 所以换下一个站位
Result AdvanceMountSpot(const Context& ctx, const Waypoint& waypoint, const char* reason)
{
    constexpr const char* kGiveUpReason = "zipline_prompt_missing";
    constexpr const char* kGiveUpDetail = "no mount prompt from any stand point at this tower";
    if (!waypoint.zipline_hop) {
        return AbandonZipline(ctx, kGiveUpReason, kGiveUpDetail);
    }
    const ZiplineHopPlan& plan = *waypoint.zipline_hop;
    ZiplineApproachState& approach = ctx.runtime_state->zipline_approach;
    const size_t spot_count = plan.mount_spots.size();
    const size_t cursor = approach.mountSpotCursor(plan.mount);
    if (cursor + 1 >= spot_count) {
        ctx.runtime_state->zipline_ride.markMountUnreachable(plan);
        return AbandonZipline(ctx, kGiveUpReason, kGiveUpDetail);
    }

    approach.spot_index = cursor + 1;
    approach.press_missed = true;
    const ZiplineMountSpot& spot = plan.mount_spots[approach.spot_index];
    const bool retargeted = ctx.session->retargetCurrentWaypoint(spot.x, spot.y, reason);
    // 硬时钟不归零的话, 下一拍就会把剩下的站位一口气烧光
    ctx.session->resetHardProgress();
    LogWarn << "Action: ZIPLINE no mount prompt here; walking to the next stand point." << VAR(reason) << VAR(retargeted)
            << VAR(approach.spot_index) << VAR(spot_count) << VAR(spot.x) << VAR(spot.y);
    ctx.runtime_state->route.reset();
    ctx.runtime_state->route.confirmStartupAt(*ctx.position);
    ctx.position_provider->resetTracking();
    SelectPhaseForCurrentWaypoint(ctx, reason);

    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

bool SkipToHopUnderfoot(const Context& ctx, const char* reason)
{
    const std::optional<ZiplineNodeRef> underfoot = ctx.runtime_state->zipline_ride.towerUnderfoot();
    if (!underfoot) {
        return false;
    }
    const std::vector<Waypoint>& path = ctx.session->currentPath();
    const size_t current = ctx.session->currentNodeIdx();
    const size_t hop = skip_continuous_run(path, current);
    if (hop >= path.size() || path[hop].action != ActionType::ZIPLINE || !path[hop].zipline_hop
        || !path[hop].zipline_hop->mount.sameTower(*underfoot)) {
        return false;
    }
    if (hop > current) {
        LogInfo << "Next hop leaves from the tower underfoot; skipping the walk to its stand point." << VAR(reason) << VAR(current)
                << VAR(hop) << VAR(underfoot->x) << VAR(underfoot->y);
        ctx.session->skipPastWaypoint(hop - 1, reason);
    }
    return true;
}

Result AbandonZipline(const Context& ctx, const char* reason, const char* detail)
{
    return drop_chain_and_recover(ctx, reason, detail, /*stay_on_tower=*/false);
}

void LeaveZiplineTower(const Context& ctx)
{
    ZiplineRideMachine& ride = ctx.runtime_state->zipline_ride;
    const bool was_on_tower = ride.onTower();
    RuntimeZiplineActuator actuator(ctx);
    ride.dismount(actuator);
    // 刚下来的那几百毫秒移动指令还会被架子吃掉
    if (was_on_tower) {
        SleepFor(kZiplineLaunchSettleMs);
    }
}

Result StartZiplineHop(const Context& ctx, const Waypoint& waypoint, double actual_distance)
{
    Result result;
    StopMotionAndCommitment(ctx);

    if (!waypoint.zipline_hop) {
        return AbandonZipline(ctx, "zipline_hop_missing", "waypoint carries no hop plan");
    }

    // 站在别的架子上瞄, 发出去的是那根架子的索, 还会把好索记成坏的
    ZiplineRideMachine& ride = ctx.runtime_state->zipline_ride;
    const std::optional<ZiplineNodeRef> underfoot = ride.towerUnderfoot();
    if (underfoot && !waypoint.zipline_hop->mount.sameTower(*underfoot)) {
        LogWarn << "Standing on a tower this hop does not start from; stepping down first." << VAR(underfoot->x) << VAR(underfoot->y)
                << VAR(waypoint.zipline_hop->mount.x) << VAR(waypoint.zipline_hop->mount.y);
        LeaveZiplineTower(ctx);
    }

    if (!ride.onTower()) {
        if (ctx.maa_context == nullptr) {
            return AbandonZipline(ctx, "zipline_no_context", "no pipeline context to recognize the mount prompt");
        }
        if (!press_mount_prompt(ctx.maa_context)) {
            // 预筛提前叫停时认不出, 说明那个图标不属于滑索架, 接着走
            if (ctx.runtime_state->semantic.zipline_prompt_probe) {
                LogInfo << "Zipline mount pre-filter did not hold up; keeping the approach." << VAR(actual_distance);
                return result;
            }
            LogInfo << "No mount prompt at this stand point; moving on to the next one." << VAR(actual_distance);
            return AdvanceMountSpot(ctx, waypoint, "zipline_mount_no_prompt");
        }
        ctx.runtime_state->zipline_approach.press_missed = false;
    }

    ride.begin(*waypoint.zipline_hop);
    LogInfo << "Action: ZIPLINE hop started." << VAR(waypoint.zipline_hop->landing.x) << VAR(waypoint.zipline_hop->landing.y)
            << VAR(waypoint.zipline_hop->planned_elevation_deg) << VAR(waypoint.zipline_hop->chain_continues) << VAR(actual_distance);
    // 航点等落地再推进, 起滑时人还在上索点, 链尾也不能在这里收工
    ctx.session->updatePhase(NaviPhase::WaitZipline, "zipline_hop_started");
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

Result TickZiplineRide(const Context& ctx)
{
    Result result;
    ctx.motion_controller->setForwardState(false);

    ZiplineRideMachine& ride = ctx.runtime_state->zipline_ride;
    if (ride.stage() == ZiplineStage::Idle) {
        return AbandonZipline(ctx, "zipline_ride_idle", "waiting on a ride that is not running");
    }
    RuntimeZiplineObserver observer(ctx);
    RuntimeZiplineActuator actuator(ctx);
    const StageResult outcome = ride.tick(observer, actuator);

    if (const auto* done = std::get_if<HopCompleted>(&outcome)) {
        return finish_hop(ctx, *done);
    }
    if (const auto* abandoned = std::get_if<ChainAbandoned>(&outcome)) {
        return AbandonZipline(ctx, abandoned->reason, "the ride machine gave this chain up");
    }
    if (const auto* replan = std::get_if<ReplanRequested>(&outcome)) {
        return drop_chain_and_recover(
            ctx,
            "zipline_replan_requested",
            replan->still_on_tower ? "waiting on the tower for a new route" : "this hop is dead, re-routing from the ground",
            replan->still_on_tower);
    }
    if (std::get_if<NeedsReposition>(&outcome) != nullptr) {
        return AdvanceMountSpot(ctx, ctx.session->currentWaypoint(), "zipline_mount_unmounted");
    }
    result.stay_in_current_tick = true;
    SleepFor(kZiplineRideRetryIntervalMs);
    return result;
}

} // namespace semantic_nodes

} // namespace mapnavigator
