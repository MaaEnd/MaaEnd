#include "semantic_nodes.h"

#include <chrono>
#include <cmath>
#include <optional>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "../process_state.h"
#include "../sensing/position_provider.h"
#include "../state_machine/navigation_state_machine.h"
#include "../zipline/zipline_action.h"
#include "async_prompt_action.h"
#include "find_action.h"
#include "heading_turn.h"
#include "jump_pad_action.h"
#include "semantic_helpers.h"
#include "transit_wait.h"
#include "trigger_action.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

Result arrive_transfer(Navigator* nav, const std::optional<size_t>& node_idx, double actual_distance)
{
    HoldAndArmCameraAlign(nav);
    NoteCanonicalFinalGoalConsumed(nav->session, node_idx, *nav->position, "transfer_wait_started");
    NoteInteraction(nav->session);
    LeaveCurrentWaypoint(nav->session, ActionType::TRANSFER, "transfer_wait_started");
    ResetOnWaypointAdvance(&nav->runtime_state);
    const auto now = std::chrono::steady_clock::now();
    const bool waits = HasCurrentWaypoint(*nav->session);
    if (waits) {
        BeginTransferWait(&nav->runtime_state.semantic.transfer, *nav->position, now);
    }
    else {
        ClearTransferWait(&nav->runtime_state.semantic.transfer);
    }
    ResetSensorTracking(nav->position_provider);
    LogInfo << "Action: TRANSFER reached." << VAR(actual_distance);

    if (waits) {
        UpdatePhase(nav->session, NaviPhase::WaitTransfer, "transfer_wait_started");
    }
    else {
        ConcludeRouteTail(nav->session, *nav->position, "route_tail_consumed");
    }
    return Handled();
}

Result arrive_portal(Navigator* nav, const std::optional<size_t>& node_idx, double actual_distance)
{
    NoteCanonicalFinalGoalConsumed(nav->session, node_idx, *nav->position, "portal_entered");
    NoteInteraction(nav->session);
    LeaveCurrentWaypoint(nav->session, ActionType::PORTAL, "portal_entered");
    ResetOnWaypointAdvance(&nav->runtime_state);
    const auto now = std::chrono::steady_clock::now();
    if (HasCurrentWaypoint(*nav->session)) {
        BeginPortalTransit(&nav->runtime_state.semantic.portal, now);
    }
    else {
        EndPortalTransit(&nav->runtime_state.semantic.portal);
    }
    ResetSensorTracking(nav->position_provider);
    MoveForward(nav->motion_controller);
    LogInfo << "Action: PORTAL entered transit flow." << VAR(actual_distance);
    SelectPhaseOrFinishRoute(nav, "portal_entered");
    return Handled();
}

Result arrive_dig(Navigator* nav, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance)
{
    HoldAndArmCameraAlign(nav);
    if (nav->maa_context == nullptr) {
        LogError << "Action: DIG triggered but maa_context is null." << VAR(actual_distance);
        return Failure("dig_context_missing", "MaaContext is null when dispatching dig subtask.");
    }

    LogInfo << "Action: DIG triggered, dispatching subtask." << VAR(kDefaultDigEntry) << VAR(actual_distance);
    const MaaTaskId sub_id = MaaContextRunTask(nav->maa_context, kDefaultDigEntry, kDigPipelineOverride);
    if (sub_id == MaaInvalidId) {
        LogError << "Action: DIG subtask failed to dispatch." << VAR(kDefaultDigEntry) << VAR(actual_distance);
        return Failure("dig_dispatch_failed", "MaaContextRunTask returned MaaInvalidId for dig subtask.");
    }

    LogInfo << "Action: DIG subtask returned." << VAR(sub_id);
    SleepFor(kDigPostSleepMs);
    RestartRouteTracking(&nav->runtime_state.route);
    CompleteArrival(nav, waypoint, node_idx, "dig_completed");
    return Handled();
}

Result walk_on_after_prompt_miss(Navigator* nav, const std::optional<size_t>& node_idx, double actual_distance)
{
    InteractApproachState& approach = nav->runtime_state.interact_approach;
    approach.prompt_missed_node = node_idx;
    approach.missed_at = std::chrono::steady_clock::now();
    // 起步按已确认算, 否则要先挪够起步距离才放行到点判定
    RestartRouteTracking(&nav->runtime_state.route);
    ConfirmStartupAt(&nav->runtime_state.route, *nav->position);
    ResetHardProgress(nav->session);
    LogInfo << "Action: INTERACT prompt missed, walking on onto the point." << VAR(actual_distance) << VAR(*node_idx);
    return { .consumed = true };
}

Result arrive_async_interact(Navigator* nav, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance)
{
    HoldAndArmCameraAlign(nav);
    LogInfo << "Action: INTERACT reached, running the authoritative recognition." << VAR(actual_distance)
            << VAR(waypoint.interact_text.size()) << VAR(waypoint.interact_scan) << VAR(waypoint.interact_rec);
    const bool recognized = RunPromptSubtask(nav->maa_context, kInteractPromptSpec, &waypoint.interact_text, PromptRunModeOf(waypoint));
    if (!recognized && waypoint.has_position && node_idx && !PromptMissedAt(nav->runtime_state.interact_approach, node_idx)
        && actual_distance > kStrictSettleAcceptBandWu) {
        return walk_on_after_prompt_miss(nav, node_idx, actual_distance);
    }
    RestartRouteTracking(&nav->runtime_state.route);
    if (!recognized && waypoint.interact_rec) {
        LogWarn << "Action: INTERACT prompt still missing in rec mode." << VAR(actual_distance) << VAR(waypoint.interact_text_node);
        return Failure("interact_prompt_missing", "Interact prompt not recognized on the point.");
    }
    if (!waypoint.rope_ride) {
        CompleteArrival(nav, waypoint, node_idx, "async_interact_completed");
        return Handled();
    }

    const RopeRideInfo ride = *waypoint.rope_ride;
    if (!recognized) {
        LogWarn << "Action: rope prompt missing at the bottom." << VAR(actual_distance) << VAR(ride.uid);
        return { .consumed = true, .rope_unavailable = ride.uid };
    }
    CompleteArrival(nav, waypoint, node_idx, "rope_interact_pressed");
    return StartRopeRide(nav, ride);
}

Result arrive_key_interact(Navigator* nav, const Waypoint& waypoint, const std::optional<size_t>& node_idx)
{
    // 绳底没有提示文字就确认不了坐没坐上, 当这根用不了
    if (waypoint.rope_ride) {
        return { .consumed = true, .rope_unavailable = waypoint.rope_ride->uid };
    }
    // rec 模式到这里说明文本没解析出来, 不能按交互键
    if (IsRecInteract(waypoint)) {
        LogInfo << "Action: INTERACT in rec mode, skipping the key press." << VAR(waypoint.interact_text_node);
    }
    else {
        ArmCameraAlign(&nav->runtime_state);
        HoldPosition(nav->motion_controller);
        ExecuteInteract(nav->input_backend);
    }
    CompleteArrival(nav, waypoint, node_idx, "waypoint_action_completed");
    return Handled();
}

} // namespace

Result TickWaitPhase(Navigator* nav, NaviPhase phase)
{
    switch (phase) {
    case NaviPhase::WaitTransfer:
        return TickTransferWait(nav);
    case NaviPhase::WaitZipline:
        return TickZiplineRide(nav);
    case NaviPhase::WaitFind:
        return TickFindTarget(nav);
    case NaviPhase::WaitJumpPad:
        return TickJumpPad(nav);
    case NaviPhase::WaitRope:
        return TickRopeRide(nav);
    case NaviPhase::Bootstrap:
    case NaviPhase::Navigate:
    case NaviPhase::Finished:
    case NaviPhase::Failed:
        break;
    }
    return {};
}

Result ConsumeInlineSemantics(Navigator* nav)
{
    ConsumeMatchedZoneNodes(nav);
    if (!HasCurrentWaypoint(*nav->session)) {
        ConcludeRouteTail(nav->session, *nav->position, "route_tail_consumed");
        return Handled();
    }

    const Result heading_result = ConsumeHeadingNodes(nav);
    if (heading_result.consumed) {
        ConsumeMatchedZoneNodes(nav);
        return heading_result;
    }

    const Result find_result = ConsumeFindNodes(nav);
    if (find_result.consumed) {
        return find_result;
    }

    if (HasCurrentWaypoint(*nav->session) && IsZoneDeclaration(CurrentWaypoint(*nav->session))) {
        MoveForward(nav->motion_controller);
        return Handled();
    }
    return {};
}

Result StartRopeRide(Navigator* nav, const RopeRideInfo& ride)
{
    HoldAndArmCameraAlign(nav);
    BeginRopeRide(
        &nav->runtime_state.semantic.rope,
        static_cast<int64_t>(std::ceil(ride.ride_s * 1000.0)),
        std::chrono::steady_clock::now());
    ResetSensorTracking(nav->position_provider);
    LogInfo << "Rope ride started." << VAR(ride.uid) << VAR(ride.ride_s) << VAR(ride.top_height);
    if (!HasCurrentWaypoint(*nav->session) || !CurrentWaypoint(*nav->session).rope_top) {
        LogWarn << "Rope ride started without a top landing as the next point." << VAR(ride.uid);
    }
    UpdatePhase(nav->session, NaviPhase::WaitRope, "rope_ride_started");
    return Handled();
}

Result HandleArrival(Navigator* nav, const Waypoint& waypoint, double actual_distance)
{
    const std::optional<size_t> node_idx = CurrentAbsoluteNodeIndex(*nav->session);
    if (RequiresStrictArrival(waypoint) && nav->motion_controller->is_moving) {
        HoldAndArmCameraAlign(nav);
        SleepFor(kStopWaitMs);
    }

    switch (waypoint.action) {
    case ActionType::TRANSFER:
        return arrive_transfer(nav, node_idx, actual_distance);
    case ActionType::JUMP_PAD:
        return ArriveJumpPad(nav, waypoint, node_idx, actual_distance);
    case ActionType::PORTAL:
        return arrive_portal(nav, node_idx, actual_distance);
    case ActionType::ZIPLINE:
        return StartZiplineHop(nav, waypoint, actual_distance);
    case ActionType::DIG:
        return arrive_dig(nav, waypoint, node_idx, actual_distance);
    case ActionType::FIND:
        return ArriveFind(nav, waypoint, actual_distance);
    case ActionType::TRIGGER:
        return ArriveTrigger(nav, waypoint, actual_distance);
    case ActionType::INTERACT:
        if (IsAsyncInteract(waypoint) && nav->maa_context != nullptr) {
            return arrive_async_interact(nav, waypoint, node_idx, actual_distance);
        }
        return arrive_key_interact(nav, waypoint, node_idx);
    case ActionType::SPRINT:
        ExecuteSprint(nav->motion_controller, &nav->process->walk);
        break;
    case ActionType::JUMP:
        ArmCameraAlign(&nav->runtime_state);
        HoldPosition(nav->motion_controller);
        ExecuteJump(nav->input_backend);
        break;
    case ActionType::FIGHT:
        ArmCameraAlign(&nav->runtime_state);
        HoldPosition(nav->motion_controller);
        ExecuteFight(nav->input_backend);
        break;
    // HEADING/ZONE 没坐标, NAVMESH 展开后换成规划点, 实际到不了这里
    case ActionType::RUN:
    case ActionType::COLLECT:
    case ActionType::HEADING:
    case ActionType::NAVMESH:
    case ActionType::ZONE:
        break;
    }
    CompleteArrival(nav, waypoint, node_idx, "waypoint_action_completed");
    return Handled();
}

} // namespace semantic_nodes

} // namespace mapnavigator
