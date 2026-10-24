#include "semantic_nodes.h"

#include <chrono>
#include <cmath>
#include <optional>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "../sensing/position_provider.h"
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

Result ArriveTransfer(const Context& ctx, const std::optional<size_t>& node_idx, double actual_distance)
{
    StopMotionAndCommitment(ctx);
    ctx.session->NoteCanonicalFinalGoalConsumed(node_idx, *ctx.position, "transfer_wait_started");
    ctx.session->AdvanceToNextWaypoint(ActionType::TRANSFER, "transfer_wait_started");
    ctx.runtime_state->OnWaypointAdvance();
    const auto now = std::chrono::steady_clock::now();
    const bool waits = ctx.session->HasCurrentWaypoint();
    ctx.runtime_state->semantic.transfer = waits ? TransferWaitState { .started = now, .anchor = *ctx.position } : TransferWaitState {};
    ctx.position_provider->ResetTracking();
    LogInfo << "Action: TRANSFER reached." << VAR(actual_distance);

    if (waits) {
        ctx.session->UpdatePhase(NaviPhase::WaitTransfer, "transfer_wait_started");
    }
    else {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
    }
    return Handled();
}

Result ArrivePortal(const Context& ctx, const std::optional<size_t>& node_idx, double actual_distance)
{
    ctx.session->NoteCanonicalFinalGoalConsumed(node_idx, *ctx.position, "portal_entered");
    ctx.session->AdvanceToNextWaypoint(ActionType::PORTAL, "portal_entered");
    ctx.runtime_state->OnWaypointAdvance();
    const auto now = std::chrono::steady_clock::now();
    ctx.runtime_state->semantic.portal = ctx.session->HasCurrentWaypoint()
                                             ? PortalTransitState { .phase = PortalTransitPhase::AwaitZone, .started = now }
                                             : PortalTransitState {};
    ctx.position_provider->ResetTracking();
    ctx.motion_controller->SetForwardState(true);
    LogInfo << "Action: PORTAL entered transit flow." << VAR(actual_distance);
    SelectPhaseForCurrentWaypoint(ctx, "portal_entered");
    return Handled();
}

Result ArriveDig(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance)
{
    StopMotionAndCommitment(ctx);
    if (ctx.maa_context == nullptr) {
        LogError << "Action: DIG triggered but maa_context is null." << VAR(actual_distance);
        return Failure("dig_context_missing", "MaaContext is null when dispatching dig subtask.");
    }

    LogInfo << "Action: DIG triggered, dispatching subtask." << VAR(kDefaultDigEntry) << VAR(actual_distance);
    const MaaTaskId sub_id = MaaContextRunTask(ctx.maa_context, kDefaultDigEntry, kDigPipelineOverride);
    if (sub_id == MaaInvalidId) {
        LogError << "Action: DIG subtask failed to dispatch." << VAR(kDefaultDigEntry) << VAR(actual_distance);
        return Failure("dig_dispatch_failed", "MaaContextRunTask returned MaaInvalidId for dig subtask.");
    }

    LogInfo << "Action: DIG subtask returned." << VAR(sub_id);
    SleepFor(kDigPostSleepMs);
    ctx.runtime_state->route.Reset();
    return CompleteArrival(ctx, waypoint, node_idx, "dig_completed");
}

Result WalkOnAfterPromptMiss(const Context& ctx, const std::optional<size_t>& node_idx, double actual_distance)
{
    InteractApproachState& approach = ctx.runtime_state->interact_approach;
    approach.prompt_missed_node = node_idx;
    approach.missed_at = std::chrono::steady_clock::now();
    // 起步按已确认算, 否则要先挪够起步距离才放行到点判定
    ctx.runtime_state->route.Reset();
    ctx.runtime_state->route.ConfirmStartupAt(*ctx.position);
    ctx.session->ResetHardProgress();
    LogInfo << "Action: INTERACT prompt missed, walking on onto the point." << VAR(actual_distance) << VAR(*node_idx);
    return { .consumed = true };
}

Result ArriveAsyncInteract(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance)
{
    StopMotionAndCommitment(ctx);
    LogInfo << "Action: INTERACT reached, running the authoritative recognition." << VAR(actual_distance)
            << VAR(waypoint.interact_text.size()) << VAR(waypoint.interact_scan) << VAR(waypoint.interact_rec);
    const bool recognized = RunPromptSubtask(ctx.maa_context, kInteractPromptSpec, &waypoint.interact_text, waypoint.interact_rec);
    if (!recognized && waypoint.HasPosition() && node_idx && !ctx.runtime_state->interact_approach.PromptMissedAt(node_idx)
        && actual_distance > kStrictSettleAcceptBandWu) {
        return WalkOnAfterPromptMiss(ctx, node_idx, actual_distance);
    }
    ctx.runtime_state->route.Reset();
    if (!recognized && waypoint.interact_rec) {
        LogWarn << "Action: INTERACT prompt still missing in rec mode." << VAR(actual_distance) << VAR(waypoint.interact_text_node);
        return Failure("interact_prompt_missing", "Interact prompt not recognized on the point.");
    }
    if (!waypoint.rope_ride) {
        return CompleteArrival(ctx, waypoint, node_idx, "async_interact_completed");
    }

    const RopeRideInfo ride = *waypoint.rope_ride;
    if (!recognized) {
        LogWarn << "Action: rope prompt missing at the bottom." << VAR(actual_distance) << VAR(ride.uid);
        return { .consumed = true, .rope_unavailable = ride.uid };
    }
    CompleteArrival(ctx, waypoint, node_idx, "rope_interact_pressed");
    return StartRopeRide(ctx, ride);
}

Result ArriveKeyInteract(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx)
{
    // 绳底没有提示文字就确认不了坐没坐上, 当这根用不了
    if (waypoint.rope_ride) {
        return { .consumed = true, .rope_unavailable = waypoint.rope_ride->uid };
    }
    // rec 模式到这里说明文本没解析出来, 不能按交互键
    if (waypoint.IsRecInteract()) {
        LogInfo << "Action: INTERACT in rec mode, skipping the key press." << VAR(waypoint.interact_text_node);
    }
    else {
        ctx.runtime_state->ArmCameraAlign();
        ctx.action_executor->Interact();
    }
    return CompleteArrival(ctx, waypoint, node_idx, "waypoint_action_completed");
}

} // namespace

Result TickWaitPhase(const Context& ctx, NaviPhase phase)
{
    switch (phase) {
    case NaviPhase::WaitTransfer:
        return TickTransferWait(ctx);
    case NaviPhase::WaitZipline:
        return TickZiplineRide(ctx);
    case NaviPhase::WaitFind:
        return TickFindTarget(ctx);
    case NaviPhase::WaitJumpPad:
        return TickJumpPad(ctx);
    case NaviPhase::WaitRope:
        return TickRopeRide(ctx);
    case NaviPhase::Bootstrap:
    case NaviPhase::Navigate:
    case NaviPhase::Finished:
    case NaviPhase::Failed:
        break;
    }
    return {};
}

Result ConsumeInlineSemantics(const Context& ctx)
{
    ConsumeMatchedZoneNodes(ctx);
    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        return Handled();
    }

    const Result heading_result = ConsumeHeadingNodes(ctx);
    if (heading_result.consumed) {
        ConsumeMatchedZoneNodes(ctx);
        return heading_result;
    }

    const Result find_result = ConsumeFindNodes(ctx);
    if (find_result.consumed) {
        return find_result;
    }

    if (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().IsZoneDeclaration()) {
        ctx.motion_controller->SetForwardState(true);
        return Handled();
    }
    return {};
}

Result StartRopeRide(const Context& ctx, const RopeRideInfo& ride)
{
    StopMotionAndCommitment(ctx);
    ctx.runtime_state->semantic.rope = {
        .started = std::chrono::steady_clock::now(),
        .ride_ms = static_cast<int64_t>(std::ceil(ride.ride_s * 1000.0)),
    };
    ctx.position_provider->ResetTracking();
    LogInfo << "Rope ride started." << VAR(ride.uid) << VAR(ride.ride_s) << VAR(ride.top_height);
    if (!ctx.session->HasCurrentWaypoint() || !ctx.session->CurrentWaypoint().rope_top) {
        LogWarn << "Rope ride started without a top landing as the next point." << VAR(ride.uid);
    }
    ctx.session->UpdatePhase(NaviPhase::WaitRope, "rope_ride_started");
    return Handled();
}

Result HandleArrival(const Context& ctx, const Waypoint& waypoint, double actual_distance)
{
    const std::optional<size_t> node_idx = ctx.session->CurrentAbsoluteNodeIndex();
    if (waypoint.RequiresStrictArrival() && ctx.motion_controller->IsMoving()) {
        StopMotionAndCommitment(ctx);
        SleepFor(kStopWaitMs);
    }

    switch (waypoint.action) {
    case ActionType::TRANSFER:
        return ArriveTransfer(ctx, node_idx, actual_distance);
    case ActionType::JUMP_PAD:
        return ArriveJumpPad(ctx, waypoint, node_idx, actual_distance);
    case ActionType::PORTAL:
        return ArrivePortal(ctx, node_idx, actual_distance);
    case ActionType::ZIPLINE:
        return StartZiplineHop(ctx, waypoint, actual_distance);
    case ActionType::DIG:
        return ArriveDig(ctx, waypoint, node_idx, actual_distance);
    case ActionType::FIND:
        return ArriveFind(ctx, waypoint, actual_distance);
    case ActionType::TRIGGER:
        return ArriveTrigger(ctx, waypoint, actual_distance);
    case ActionType::INTERACT:
        if (waypoint.IsAsyncInteract() && ctx.maa_context != nullptr) {
            return ArriveAsyncInteract(ctx, waypoint, node_idx, actual_distance);
        }
        return ArriveKeyInteract(ctx, waypoint, node_idx);
    case ActionType::SPRINT:
        ctx.action_executor->Sprint();
        break;
    case ActionType::JUMP:
        ctx.runtime_state->ArmCameraAlign();
        ctx.action_executor->Jump();
        break;
    case ActionType::FIGHT:
        ctx.runtime_state->ArmCameraAlign();
        ctx.action_executor->Fight();
        break;
    // HEADING/ZONE 没坐标, NAVMESH 展开后换成规划点, 实际到不了这里
    case ActionType::RUN:
    case ActionType::COLLECT:
    case ActionType::HEADING:
    case ActionType::NAVMESH:
    case ActionType::ZONE:
        break;
    }
    return CompleteArrival(ctx, waypoint, node_idx, "waypoint_action_completed");
}

} // namespace semantic_nodes

} // namespace mapnavigator
