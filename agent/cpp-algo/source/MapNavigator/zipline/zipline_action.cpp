#include "zipline_action.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <optional>
#include <string>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../Backend/backend.h"
#include "../control/camera_pitch.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_time.h"
#include "../process_state.h"
#include "../semantic/heading_turn.h"
#include "../semantic/semantic_helpers.h"
#include "../sensing/maa_task.h"
#include "../sensing/position_provider.h"
#include "../state_machine/navigation_state_machine.h"
#include "zipline_ride_machine.h"

namespace mapnavigator
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

std::vector<maplocator::SearchHint> landing_hints(Navigator* nav, const std::vector<ZiplineNodeRef>& hint_nodes)
{
    std::vector<maplocator::SearchHint> hints;
    const std::string zone = nav->position->zone_id;
    if (!zone.empty()) {
        for (const ZiplineNodeRef& node : hint_nodes) {
            hints.push_back(maplocator::SearchHint { .zone_id = zone, .x = node.x, .y = node.y, .radius = kZiplineLandingHintRadiusWu });
        }
    }
    return hints;
}

} // namespace

struct ZiplineIo
{
    Navigator* nav;
};

ZiplineObservation ZiplineIoObserveTracked(ZiplineIo* io, const std::vector<ZiplineNodeRef>& hint_nodes)
{
    ZiplineObservation obs;
    obs.at = std::chrono::steady_clock::now();
    const std::vector<maplocator::SearchHint> hints = landing_hints(io->nav, hint_nodes);
    const bool captured = CaptureTracked(CapturePartsOf(*io->nav), io->nav->position, {}, hints);
    if (captured) {
        obs.fix = *io->nav->position;
    }
    return obs;
}

ZiplineObservation ZiplineIoObserveGlobal(ZiplineIo* io, const std::vector<ZiplineNodeRef>& hint_nodes)
{
    ZiplineObservation obs;
    obs.at = std::chrono::steady_clock::now();
    const std::vector<maplocator::SearchHint> hints = landing_hints(io->nav, hint_nodes);
    const bool captured = CaptureGlobal(CapturePartsOf(*io->nav), io->nav->position, {}, hints);
    if (captured) {
        obs.fix = *io->nav->position;
    }
    return obs;
}

// 右上角按钮上架后会收起, 所以命中地面判据即在地面, 否则再看底部引导
MountVerdict ZiplineIoCheckMounted(ZiplineIo* io)
{
    if (io->nav->maa_context == nullptr) {
        return MountVerdict::Unclear;
    }
    if (run_node_and_report_hit(io->nav->maa_context, kZiplineOnGroundEntryNode, kZiplineOnGroundNode, "{}")) {
        return MountVerdict::OnGround;
    }
    const bool hint = run_node_and_report_hit(io->nav->maa_context, kZiplineOnTowerHintEntryNode, kZiplineOnTowerHintNode, "{}");
    return hint ? MountVerdict::OnTower : MountVerdict::Unclear;
}

void ZiplineIoResetTracking(ZiplineIo* io)
{
    ResetSensorTracking(io->nav->position_provider);
}

// 俯仰无读数, 只能拉到天空方向的硬限位当已知位置; 走 pipeline 节点是因为 Swipe 会带一次左键
bool ZiplineIoResetPitchToMaximum(ZiplineIo* io)
{
    const int units = static_cast<int>(
        std::lround(-kZiplinePitchResetRangeScale * PitchRangeUnits(*io->nav->input_backend, io->nav->process->sensitivity)));
    if (UsesTouchBackend(*io->nav->input_backend)) {
        if (units == 0) {
            return false;
        }
        return SendViewDeltaSync(io->nav->input_backend, 0, units);
    }
    if (io->nav->maa_context == nullptr) {
        LogWarn << "Zipline aim: no pipeline context to reset the pitch.";
        return false;
    }
    bool completed = false;
    if (units != 0) {
        completed = run_node_and_report_hit(
            io->nav->maa_context,
            kZiplinePitchResetNode,
            kZiplinePitchResetNode,
            build_pitch_reset_override(units));
    }
    if (!completed) {
        LogWarn << "Zipline aim: the pitch reset task did not complete." << VAR(kZiplinePitchResetNode) << VAR(units);
        return false;
    }
    return true;
}

std::optional<double> ZiplineIoTurnYaw(ZiplineIo* io, double delta_deg)
{
    const double cap = SteeringProfileOf(*io->nav->input_backend).max_batch_delta_deg;
    const double step = std::clamp(delta_deg, -cap, cap);
    if (!semantic_nodes::TurnToHeadingOnce(io->nav, step)) {
        return std::nullopt;
    }
    return step;
}

bool ZiplineIoLowerPitchTo(ZiplineIo* io, double elevation_deg)
{
    const double fraction = CameraVerticalFractionForElevation(elevation_deg);
    const int units = static_cast<int>(std::lround(fraction * PitchRangeUnits(*io->nav->input_backend, io->nav->process->sensitivity)));
    if (units == 0) {
        return true;
    }
    return SendViewDeltaSync(io->nav->input_backend, 0, units);
}

bool ZiplineIoPressMount(ZiplineIo* io)
{
    return press_mount_prompt(io->nav->maa_context);
}

void ZiplineIoFireLaunch(ZiplineIo* io)
{
    HoldPosition(io->nav->motion_controller);
    TriggerZiplineLaunchSync(io->nav->input_backend);
}

void ZiplineIoDismount(ZiplineIo* io)
{
    TriggerZiplineDismountSync(io->nav->input_backend, kZiplineDismountHoldMs);
}

void ZiplineIoWait(ZiplineIo*, int32_t ms)
{
    SleepFor(ms);
}

namespace semantic_nodes
{

namespace
{

size_t skip_continuous_run(const std::vector<Waypoint>& path, size_t index)
{
    while (index < path.size() && IsContinuousRun(path[index])) {
        ++index;
    }
    return index;
}

// 整条链一起丢, 留一跳重规划就会把人送回刚失败的索
size_t drop_zipline_chain(Navigator* nav, const char* reason)
{
    const std::vector<Waypoint>& path = nav->session->current_path;
    const size_t hop = skip_continuous_run(path, nav->session->current_node_idx);
    size_t dropped = 0;
    while (hop + dropped < path.size() && path[hop + dropped].action == ActionType::ZIPLINE) {
        ++dropped;
    }
    if (dropped != 0) {
        NoteInteraction(nav->session);
        LeaveWaypointsThrough(nav->session, hop + dropped - 1, reason);
    }
    return dropped;
}

Result restart_after_dropped_chain(Navigator* nav, const char* reason)
{
    Result result;
    nav->runtime_state.zipline_approach = {};
    ResetOnWaypointAdvance(&nav->runtime_state);
    RestartRouteTracking(&nav->runtime_state.route);
    ResetSensorTracking(nav->position_provider);
    ResetProgress(nav->session);
    // ResetOnWaypointAdvance 会清掉恢复状态和重规划标志, 这两行必须在它后面
    BeginZiplineRecovery(&nav->runtime_state.zipline_recovery, std::chrono::steady_clock::now());
    RequestDynamicReplan(&nav->runtime_state);

    SelectPhaseOrFinishRoute(nav, reason);
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

Result abandon_chain_from_ground(Navigator* nav, const char* reason, const char* detail)
{
    HoldAndArmCameraAlign(nav);
    LeaveZiplineTower(nav);
    const size_t dropped = drop_zipline_chain(nav, reason);
    LogWarn << "Action: ZIPLINE given up, recovering from a fresh position." << VAR(reason) << VAR(detail) << VAR(dropped)
            << VAR_AS(stay_on_tower, false) << VAR(nav->position->x) << VAR(nav->position->y);
    return restart_after_dropped_chain(nav, reason);
}

Result abandon_chain_on_tower(Navigator* nav, const char* reason, const char* detail)
{
    HoldAndArmCameraAlign(nav);
    const size_t dropped = drop_zipline_chain(nav, reason);
    NoteStandingTower(nav->session, ZiplineTowerUnderfoot(nav->runtime_state.zipline_ride));
    LogWarn << "Action: ZIPLINE given up, recovering from a fresh position." << VAR(reason) << VAR(detail) << VAR(dropped)
            << VAR_AS(stay_on_tower, true) << VAR(nav->position->x) << VAR(nav->position->y);
    return restart_after_dropped_chain(nav, reason);
}

Result finish_hop(Navigator* nav, const HopCompleted& done)
{
    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;

    NoteCanonicalFinalGoalConsumed(nav->session, CurrentAbsoluteNodeIndex(*nav->session), done.at, "zipline_ride_complete");
    NoteInteraction(nav->session);
    LeaveCurrentWaypoint(nav->session, ActionType::ZIPLINE, "zipline_ride_complete");
    ResetOnWaypointAdvance(&nav->runtime_state);
    LogInfo << "Action: ZIPLINE ride landed." << VAR(done.at.x) << VAR(done.at.y) << VAR(done.still_on_tower);
    if (!done.at.zone_id.empty()) {
        UpdateCurrentZone(nav->session, done.at.zone_id);
    }
    ResetProgress(nav->session);
    ResetNavigationAssistState(&nav->runtime_state);
    RestartRouteTracking(&nav->runtime_state.route);
    // 起步确认直接置位, 下一跳的上索点就在脚下, 拦住就得先走开再走回来
    ConfirmStartupAt(&nav->runtime_state.route, done.at);
    ResetSensorTracking(nav->position_provider);

    if (!HasCurrentWaypoint(*nav->session)) {
        LeaveZiplineTower(nav);
        ConcludeRouteTail(nav->session, done.at, "route_tail_consumed");
        return result;
    }
    if (done.still_on_tower) {
        if (SkipToHopUnderfoot(nav, "zipline_ride_complete")) {
            return StartZiplineHop(nav, CurrentWaypoint(*nav->session), 0.0);
        }
        LeaveZiplineTower(nav);
    }
    RequestDynamicReplan(&nav->runtime_state);
    SelectPhaseOrFinishRoute(nav, "zipline_ride_complete");
    return result;
}

} // namespace

// 面板只给离身位最近的设备, 原地重按还是同一个答案, 所以换下一个站位
Result AdvanceMountSpot(Navigator* nav, const Waypoint& waypoint, const char* reason)
{
    constexpr const char* kGiveUpReason = "zipline_prompt_missing";
    constexpr const char* kGiveUpDetail = "no mount prompt from any stand point at this tower";
    if (!waypoint.zipline_hop) {
        return AbandonZipline(nav, kGiveUpReason, kGiveUpDetail);
    }
    const ZiplineHopPlan& plan = *waypoint.zipline_hop;
    ZiplineApproachState& approach = nav->runtime_state.zipline_approach;
    const size_t spot_count = plan.mount_spots.size();
    FollowMountTower(&approach, plan.mount);
    const size_t cursor = approach.spot_index;
    if (cursor + 1 >= spot_count) {
        MarkZiplineMountUnreachable(&nav->runtime_state.zipline_ride, plan);
        return AbandonZipline(nav, kGiveUpReason, kGiveUpDetail);
    }

    approach.spot_index = cursor + 1;
    approach.press_missed = true;
    const ZiplineMountSpot& spot = plan.mount_spots[approach.spot_index];
    const bool retargeted = RetargetCurrentWaypoint(nav->session, spot.x, spot.y, reason);
    if (retargeted) {
        // 旧坐标上攒的最好距离会让无进展判据误以为人在倒退
        ResetProgress(nav->session);
    }
    // 硬时钟不归零的话, 下一拍就会把剩下的站位一口气烧光
    ResetHardProgress(nav->session);
    LogWarn << "Action: ZIPLINE no mount prompt here; walking to the next stand point." << VAR(reason) << VAR(retargeted)
            << VAR(approach.spot_index) << VAR(spot_count) << VAR(spot.x) << VAR(spot.y);
    RestartRouteTracking(&nav->runtime_state.route);
    ConfirmStartupAt(&nav->runtime_state.route, *nav->position);
    ResetSensorTracking(nav->position_provider);
    SelectPhaseOrFinishRoute(nav, reason);

    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

bool SkipToHopUnderfoot(Navigator* nav, const char* reason)
{
    const std::optional<ZiplineNodeRef> underfoot = ZiplineTowerUnderfoot(nav->runtime_state.zipline_ride);
    if (!underfoot) {
        return false;
    }
    const std::vector<Waypoint>& path = nav->session->current_path;
    const size_t current = nav->session->current_node_idx;
    const size_t hop = skip_continuous_run(path, current);
    if (hop >= path.size() || path[hop].action != ActionType::ZIPLINE || !path[hop].zipline_hop
        || !SameTower(path[hop].zipline_hop->mount, *underfoot)) {
        return false;
    }
    if (hop > current) {
        LogInfo << "Next hop leaves from the tower underfoot; skipping the walk to its stand point." << VAR(reason) << VAR(current)
                << VAR(hop) << VAR(underfoot->x) << VAR(underfoot->y);
        NoteInteraction(nav->session);
        LeaveWaypointsThrough(nav->session, hop - 1, reason);
    }
    return true;
}

Result AbandonZipline(Navigator* nav, const char* reason, const char* detail)
{
    return abandon_chain_from_ground(nav, reason, detail);
}

bool ScreenShowsZiplineTower(Navigator* nav)
{
    ZiplineIo io { nav };
    return ZiplineIoCheckMounted(&io) == MountVerdict::OnTower;
}

void StepOffZiplineTower(Navigator* nav)
{
    ZiplineIo io { nav };
    ZiplineIoDismount(&io);
    // 刚下来的那几百毫秒移动指令还会被架子吃掉
    SleepFor(kZiplineLaunchSettleMs);
}

void LeaveZiplineTower(Navigator* nav)
{
    ZiplineRide* ride = &nav->runtime_state.zipline_ride;
    if (ZiplineOnTower(*ride)) {
        StepOffZiplineTower(nav);
    }
    RecordDismissedHop(ride);
    ResetZiplineHop(ride);
}

Result StartZiplineHop(Navigator* nav, const Waypoint& waypoint, double actual_distance)
{
    Result result;
    HoldAndArmCameraAlign(nav);

    if (!waypoint.zipline_hop) {
        return AbandonZipline(nav, "zipline_hop_missing", "waypoint carries no hop plan");
    }

    // 站在别的架子上瞄, 发出去的是那根架子的索, 还会把好索记成坏的
    ZiplineRide* ride = &nav->runtime_state.zipline_ride;
    const std::optional<ZiplineNodeRef> underfoot = ZiplineTowerUnderfoot(*ride);
    if (underfoot && !SameTower(waypoint.zipline_hop->mount, *underfoot)) {
        LogWarn << "Standing on a tower this hop does not start from; stepping down first." << VAR(underfoot->x) << VAR(underfoot->y)
                << VAR(waypoint.zipline_hop->mount.x) << VAR(waypoint.zipline_hop->mount.y);
        LeaveZiplineTower(nav);
    }

    if (!ZiplineOnTower(*ride)) {
        if (nav->maa_context == nullptr) {
            return AbandonZipline(nav, "zipline_no_context", "no pipeline context to recognize the mount prompt");
        }
        if (!press_mount_prompt(nav->maa_context)) {
            // 预筛提前叫停时认不出, 说明那个图标不属于滑索架, 接着走
            if (nav->runtime_state.semantic.zipline_prompt_probe) {
                LogInfo << "Zipline mount pre-filter did not hold up; keeping the approach." << VAR(actual_distance);
                return result;
            }
            LogInfo << "No mount prompt at this stand point; moving on to the next one." << VAR(actual_distance);
            return AdvanceMountSpot(nav, waypoint, "zipline_mount_no_prompt");
        }
        nav->runtime_state.zipline_approach.press_missed = false;
    }

    const auto hop_started_at = std::chrono::steady_clock::now();
    if (!ZiplineHopResumable(*ride, *waypoint.zipline_hop)) {
        CloseOpenHop(ride, hop_started_at);
    }
    BeginZiplineHop(ride, *waypoint.zipline_hop, hop_started_at);
    LogInfo << "Action: ZIPLINE hop started." << VAR(waypoint.zipline_hop->landing.x) << VAR(waypoint.zipline_hop->landing.y)
            << VAR(waypoint.zipline_hop->planned_elevation_deg) << VAR(waypoint.zipline_hop->chain_continues) << VAR(actual_distance);
    // 航点等落地再推进, 起滑时人还在上索点, 链尾也不能在这里收工
    UpdatePhase(nav->session, NaviPhase::WaitZipline, "zipline_hop_started");
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

Result TickZiplineRide(Navigator* nav)
{
    Result result;
    HoldPosition(nav->motion_controller);

    ZiplineRide* ride = &nav->runtime_state.zipline_ride;
    if (ride->stage == ZiplineStage::Idle) {
        return AbandonZipline(nav, "zipline_ride_idle", "waiting on a ride that is not running");
    }
    ZiplineIo io { nav };
    const StageResult outcome = TickZiplineHop(ride, &io);

    if (outcome.kind == StageResultKind::HopCompleted) {
        return finish_hop(nav, outcome.hop_completed);
    }
    if (outcome.kind == StageResultKind::ChainAbandoned) {
        return AbandonZipline(nav, outcome.chain_abandoned.reason, "the ride machine gave this chain up");
    }
    if (outcome.kind == StageResultKind::ReplanRequested) {
        if (outcome.replan_requested.still_on_tower) {
            return abandon_chain_on_tower(nav, "zipline_replan_requested", "waiting on the tower for a new route");
        }
        return abandon_chain_from_ground(nav, "zipline_replan_requested", "this hop is dead, re-routing from the ground");
    }
    if (outcome.kind == StageResultKind::NeedsReposition) {
        return AdvanceMountSpot(nav, CurrentWaypoint(*nav->session), "zipline_mount_unmounted");
    }
    result.stay_in_current_tick = true;
    SleepFor(kZiplineRideRetryIntervalMs);
    return result;
}

} // namespace semantic_nodes

} // namespace mapnavigator
