#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <thread>

#include <MaaUtils/Logger.h>

#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "position_provider.h"
#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{


void StopMotionAndCommitment(const Context& ctx)
{
    ctx.motion_controller->SetForwardState(false);
}

void SelectPhaseForCurrentWaypoint(const Context& ctx, const char* reason)
{
    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        return;
    }
    ctx.session->UpdatePhase(NaviPhase::Navigate, reason);
}

void ClearHeldZoneCandidate(NavigationRuntimeState* runtime_state)
{
    runtime_state->semantic.held_zone_candidate.clear();
    runtime_state->semantic.held_zone_hits = 0;
}

bool AcceptHeldZoneCandidate(const Context& ctx, const std::string& zone_id)
{
    if (zone_id.empty()) {
        ClearHeldZoneCandidate(ctx.runtime_state);
        return false;
    }

    if (!ctx.position_provider->LastCaptureWasHeld()) {
        ctx.runtime_state->semantic.held_zone_candidate = zone_id;
        ctx.runtime_state->semantic.held_zone_hits = 1;
        return true;
    }

    if (ctx.runtime_state->semantic.held_zone_candidate == zone_id) {
        ++ctx.runtime_state->semantic.held_zone_hits;
    }
    else {
        ctx.runtime_state->semantic.held_zone_candidate = zone_id;
        ctx.runtime_state->semantic.held_zone_hits = 1;
    }

    return ctx.runtime_state->semantic.held_zone_hits >= kZoneConfirmStableFrames;
}

void ConsumeMatchedZoneNodes(const Context& ctx)
{
    while (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().IsZoneDeclaration()) {
        const std::string& zone_id = ctx.session->CurrentWaypoint().zone_id;
        if (!zone_id.empty() && zone_id != ctx.session->current_zone_id()) {
            break;
        }
        ctx.session->AdvanceToNextWaypoint(ActionType::ZONE, "zone_declaration_consumed");
        ctx.runtime_state->OnWaypointAdvance();
    }
}

size_t FindFutureZoneDeclaration(const Context& ctx, const std::string& zone_id)
{
    const std::vector<Waypoint>& path = ctx.session->current_path();
    for (size_t index = ctx.session->current_node_idx(); index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        if (waypoint.IsZoneDeclaration() && !waypoint.zone_id.empty() && waypoint.zone_id == zone_id) {
            return index;
        }
    }
    return std::numeric_limits<size_t>::max();
}

Result FinalizePortalTransitZone(const Context& ctx, const std::string& zone_id, size_t matched_zone_index)
{
    if (matched_zone_index > ctx.session->current_node_idx()) {
        ctx.session->SkipPastWaypoint(matched_zone_index - 1, "portal_zone_fast_forward");
    }
    ctx.session->UpdateCurrentZone(zone_id);
    ctx.session->ResetProgress();
    ctx.runtime_state->OnWaypointAdvance();
    ClearHeldZoneCandidate(ctx.runtime_state);
    ConsumeMatchedZoneNodes(ctx);
    StopMotionAndCommitment(ctx);
    ctx.position_provider->ResetTracking();
    ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix = false;
    ctx.runtime_state->semantic.portal_transit_needs_reacquire = true;
    LogInfo << "Portal transit accepted zone transition." << VAR(zone_id) << VAR(matched_zone_index);

    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;
    result.changed_zone = true;
    return result;
}

Result TickPortalTransit(const Context& ctx)
{
    Result result;
    if (!ctx.runtime_state->semantic.portal_transit_active) {
        return result;
    }

    const auto now = std::chrono::steady_clock::now();
    const auto waited_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(now - ctx.runtime_state->semantic.portal_transit_started).count();
    if (waited_ms > kZoneConfirmTimeoutMs) {
        result.request_failure = true;
        result.failure_reason = "portal_transit_timeout";
        result.failure_log_message = "PORTAL transit timed out before a valid zone landing was confirmed.";
        return result;
    }

    if (ctx.runtime_state->semantic.portal_transit_needs_reacquire) {
        if (!ctx.position_provider->Capture(ctx.position, false, ctx.session->current_zone_id())) {
            result.stay_in_current_tick = true;
            utils::SleepFor(kZoneConfirmRetryIntervalMs);
            return result;
        }
        if (ctx.position_provider->LastCaptureWasHeld() || ctx.position->zone_id != ctx.session->current_zone_id()) {
            result.stay_in_current_tick = true;
            utils::SleepFor(kZoneConfirmRetryIntervalMs);
            return result;
        }

        ctx.runtime_state->semantic.portal_transit_active = false;
        ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix = false;
        ctx.runtime_state->semantic.portal_transit_needs_reacquire = false;
        ctx.runtime_state->semantic.portal_transit_started = {};
        ClearHeldZoneCandidate(ctx.runtime_state);
        LogInfo << "Portal transit landing confirmed." << VAR(ctx.position->zone_id);
        result.consumed = true;
        result.stay_in_current_tick = true;
        result.changed_zone = true;
        return result;
    }

    if (ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix) {
        ctx.motion_controller->SetForwardState(true);
    }

    NaviPosition candidate;
    if (!ctx.position_provider->Capture(&candidate, true, {})) {
        result.stay_in_current_tick = true;
        utils::SleepFor(kZoneConfirmRetryIntervalMs);
        return result;
    }

    *ctx.position = candidate;
    if (candidate.zone_id.empty() || candidate.zone_id == ctx.session->current_zone_id()) {
        result.stay_in_current_tick = true;
        utils::SleepFor(kZoneConfirmRetryIntervalMs);
        return result;
    }

    const size_t matched_zone_index = FindFutureZoneDeclaration(ctx, candidate.zone_id);
    if (matched_zone_index == std::numeric_limits<size_t>::max()) {
        StopMotionAndCommitment(ctx);
        result.stay_in_current_tick = true;
        utils::SleepFor(kZoneConfirmRetryIntervalMs);
        return result;
    }

    if (!AcceptHeldZoneCandidate(ctx, candidate.zone_id)) {
        StopMotionAndCommitment(ctx);
        ctx.position_provider->ResetTracking();
        result.stay_in_current_tick = true;
        utils::SleepFor(kZoneConfirmRetryIntervalMs);
        return result;
    }

    *ctx.position = candidate;
    return FinalizePortalTransitZone(ctx, candidate.zone_id, matched_zone_index);
}

Result TickTransferWaitImpl(const Context& ctx)
{
    Result result;
    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.runtime_state->semantic.transfer_wait_started = {};
        ctx.runtime_state->semantic.transfer_anchor_pos = {};
        ctx.runtime_state->semantic.transfer_stable_hits = 0;
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    ctx.motion_controller->SetForwardState(false);

    const auto now = std::chrono::steady_clock::now();
    if (ctx.runtime_state->semantic.transfer_wait_started.time_since_epoch().count() == 0) {
        ctx.runtime_state->semantic.transfer_wait_started = now;
    }

    if (!ctx.position_provider->Capture(ctx.position, false, {})) {
        if (std::chrono::duration_cast<std::chrono::milliseconds>(now - ctx.runtime_state->semantic.transfer_wait_started).count()
            > kRelocationWaitTimeoutMs) {
            result.request_failure = true;
            result.failure_reason = "transfer_wait_timeout";
            result.failure_log_message = "TRANSFER wait timed out before capture stabilized.";
            return result;
        }
        result.stay_in_current_tick = true;
        utils::SleepFor(kRelocationRetryIntervalMs);
        return result;
    }

    const int64_t waited_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(now - ctx.runtime_state->semantic.transfer_wait_started).count();
    if (ctx.position_provider->LastCaptureWasHeld()) {
        ctx.runtime_state->semantic.transfer_stable_hits = 0;
        if (waited_ms > kRelocationWaitTimeoutMs) {
            result.request_failure = true;
            result.failure_reason = "transfer_wait_timeout";
            result.failure_log_message = "TRANSFER wait timed out while locator fix stayed held.";
            return result;
        }
        result.stay_in_current_tick = true;
        utils::SleepFor(kRelocationRetryIntervalMs);
        return result;
    }

    const double moved_from_anchor = std::hypot(
        ctx.position->x - ctx.runtime_state->semantic.transfer_anchor_pos.x,
        ctx.position->y - ctx.runtime_state->semantic.transfer_anchor_pos.y);
    const bool movement_observed = ctx.position->zone_id != ctx.runtime_state->semantic.transfer_anchor_pos.zone_id
                                   || moved_from_anchor >= kRelocationResumeMinDistance;
    if (!movement_observed) {
        ctx.runtime_state->semantic.transfer_stable_hits = 0;
        if (waited_ms > kRelocationWaitTimeoutMs) {
            result.request_failure = true;
            result.failure_reason = "transfer_wait_timeout";
            result.failure_log_message = "TRANSFER wait timed out without external movement.";
            return result;
        }
        result.stay_in_current_tick = true;
        utils::SleepFor(kRelocationRetryIntervalMs);
        return result;
    }

    ++ctx.runtime_state->semantic.transfer_stable_hits;
    if (ctx.runtime_state->semantic.transfer_stable_hits < kRelocationStableFixes) {
        result.stay_in_current_tick = true;
        utils::SleepFor(kRelocationRetryIntervalMs);
        return result;
    }

    ctx.session->UpdateCurrentZone(ctx.position->zone_id);
    ctx.session->ResetProgress();
    ctx.runtime_state->ResetNavigationAssistState();
    ctx.runtime_state->semantic.transfer_wait_started = {};
    ctx.runtime_state->semantic.transfer_anchor_pos = {};
    ctx.runtime_state->semantic.transfer_stable_hits = 0;

    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    SelectPhaseForCurrentWaypoint(ctx, "transfer_wait_complete");
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

Result ConsumeHeadingNodesImpl(const Context& ctx)
{
    Result result;
    bool consumed = false;
    while (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().IsHeadingOnly()) {
        const Waypoint heading_node = ctx.session->CurrentWaypoint();
        double target_heading = std::fmod(heading_node.heading_angle, 360.0);
        if (target_heading < 0.0) {
            target_heading += 360.0;
        }
        const double required_turn = NaviMath::NormalizeAngle(target_heading - ctx.runtime_state->pose.estimated_heading);

        ctx.motion_controller->SetForwardState(false);
        utils::SleepFor(kStopWaitMs);

        const TurnCommandResult turn_result = ctx.motion_controller->ApplySteering(required_turn);
        if (!turn_result.issued) {
            result.consumed = consumed;
            result.stay_in_current_tick = consumed;
            return result;
        }

        ctx.runtime_state->pose.estimated_heading = NaviMath::NormalizeAngle(ctx.runtime_state->pose.estimated_heading + turn_result.issued_delta_degrees);

        const double remaining_turn = NaviMath::NormalizeAngle(target_heading - ctx.runtime_state->pose.estimated_heading);
        if (std::abs(remaining_turn) > 1.0) {
            result.consumed = consumed;
            result.stay_in_current_tick = true;
            return result;
        }

        ctx.session->AdvanceToNextWaypoint(ActionType::HEADING, "heading_consumed");
        ctx.session->ResetProgress();
        ctx.runtime_state->OnWaypointAdvance();
        consumed = true;

        if (ctx.session->HasCurrentWaypoint()) {
            ctx.motion_controller->SetForwardState(true);
        }
        else {
            ctx.action_wrapper->PulseForwardSync(kPostHeadingForwardPulseMs);
        }
    }

    result.consumed = consumed;
    result.stay_in_current_tick = consumed;
    return result;
}

} // namespace

Result TickSemanticFlow(const Context& ctx, NaviPhase phase)
{
    if (phase == NaviPhase::WaitTransfer) {
        return TickTransferWaitImpl(ctx);
    }
    if (ctx.runtime_state->semantic.portal_transit_active) {
        return TickPortalTransit(ctx);
    }
    return {};
}

Result ConsumeInlineSemantics(const Context& ctx)
{
    Result result;

    ConsumeMatchedZoneNodes(ctx);
    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    Result heading_result = ConsumeHeadingNodesImpl(ctx);
    if (heading_result.consumed) {
        ConsumeMatchedZoneNodes(ctx);
        return heading_result;
    }

    if (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().IsZoneDeclaration()) {
        ctx.motion_controller->SetForwardState(true);
        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    return result;
}

Result HandleArrivalSemantic(const Context& ctx, const Waypoint& waypoint, double actual_distance)
{
    Result result;
    const size_t arrived_absolute_node_idx = ctx.session->CurrentAbsoluteNodeIndex();

    if (waypoint.RequiresStrictArrival() && ctx.motion_controller->IsMoving()) {
        StopMotionAndCommitment(ctx);
        utils::SleepFor(kStopWaitMs);
    }

    if (waypoint.action == ActionType::TRANSFER) {
        StopMotionAndCommitment(ctx);
        ctx.session->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *ctx.position, "transfer_wait_started");
        ctx.session->AdvanceToNextWaypoint(ActionType::TRANSFER, "transfer_wait_started");
        ctx.runtime_state->OnWaypointAdvance();
        ctx.runtime_state->semantic.transfer_anchor_pos = *ctx.position;
        ctx.runtime_state->semantic.transfer_wait_started = std::chrono::steady_clock::now();
        ctx.runtime_state->semantic.transfer_stable_hits = 0;
        ctx.position_provider->ResetTracking();
        LogInfo << "Action: TRANSFER reached." << VAR(actual_distance);

        if (!ctx.session->HasCurrentWaypoint()) {
            ctx.runtime_state->semantic.transfer_wait_started = {};
            ctx.runtime_state->semantic.transfer_anchor_pos = {};
            ctx.runtime_state->semantic.transfer_stable_hits = 0;
            ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        }
        else {
            ctx.session->UpdatePhase(NaviPhase::WaitTransfer, "transfer_wait_started");
        }

        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    if (waypoint.action == ActionType::PORTAL) {
        ctx.session->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *ctx.position, "portal_entered");
        ctx.session->AdvanceToNextWaypoint(ActionType::PORTAL, "portal_entered");
        ctx.runtime_state->OnWaypointAdvance();
        ctx.runtime_state->semantic.portal_transit_active = true;
        ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix = true;
        ctx.runtime_state->semantic.portal_transit_needs_reacquire = false;
        ctx.runtime_state->semantic.portal_transit_started = std::chrono::steady_clock::now();
        ClearHeldZoneCandidate(ctx.runtime_state);
        ctx.position_provider->ResetTracking();
        ctx.motion_controller->SetForwardState(true);
        LogInfo << "Action: PORTAL entered transit flow." << VAR(actual_distance);

        if (!ctx.session->HasCurrentWaypoint()) {
            ctx.runtime_state->semantic.portal_transit_active = false;
            ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix = false;
            ctx.runtime_state->semantic.portal_transit_needs_reacquire = false;
            ctx.runtime_state->semantic.portal_transit_started = {};
            ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        }
        else {
            SelectPhaseForCurrentWaypoint(ctx, "portal_entered");
        }

        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    return result;
}

} // namespace semantic_nodes

} // namespace mapnavigator
