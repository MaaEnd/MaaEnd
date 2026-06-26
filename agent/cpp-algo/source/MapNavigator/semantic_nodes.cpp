#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <thread>

#include <MaaFramework/MaaAPI.h>
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

bool TurnToHeadingOnce(const Context& ctx, double heading_delta)
{
    if (std::abs(heading_delta) <= 1.0) {
        return true;
    }

    int units = static_cast<int>(std::lround(heading_delta * ctx.action_wrapper->DefaultTurnUnitsPerDegree()));
    if (units == 0) {
        units = heading_delta > 0.0 ? 1 : -1;
    }

    LogInfo << "Heading-only node turn." << VAR(heading_delta) << VAR(units);
    return ctx.action_wrapper->SendViewDeltaSync(units, 0);
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
        ctx.runtime_state->dynamic_replan_requested = true;
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

double VerifyAndCorrectHeading(const Context& ctx, double target_heading, double fallback_heading)
{
    double achieved = fallback_heading;
    for (int correction = 0; correction <= kHeadingVerifyMaxRetries; ++correction) {
        if (!ctx.position_provider->Capture(ctx.position, false, ctx.session->current_zone_id())
            || ctx.position_provider->LastCaptureWasHeld()) {
            LogWarn << "Heading verify skipped: no fresh locator fix." << VAR(target_heading) << VAR(achieved);
            return achieved;
        }
        achieved = NaviMath::NormalizeAngle(ctx.position->angle);
        const double residual = NaviMath::NormalizeAngle(target_heading - achieved);
        if (std::abs(residual) <= kHeadingAcceptToleranceDeg) {
            return achieved;
        }
        if (correction == kHeadingVerifyMaxRetries) {
            LogWarn << "Heading retries exhausted, accepting." << VAR(target_heading) << VAR(achieved)
                    << VAR(residual);
            return achieved;
        }
        LogInfo << "Heading off after turn, re-issuing." << VAR(target_heading) << VAR(achieved)
                << VAR(residual) << VAR(correction);
        if (!TurnToHeadingOnce(ctx, residual)) {
            return achieved;
        }
        ctx.action_wrapper->PulseForwardSync(kPostHeadingForwardPulseMs);
        ctx.motion_controller->SetForwardState(false);
    }
    return achieved;
}

Result ConsumeHeadingNodesImpl(const Context& ctx)
{
    Result result;
    bool consumed = false;
    while (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().IsHeadingOnly()) {
        const Waypoint heading_node = ctx.session->CurrentWaypoint();
        double target_heading = 0.0;
        if (heading_node.heading_uses_target) {
            target_heading = NaviMath::CalcTargetRotation(ctx.position->x, ctx.position->y, heading_node.x, heading_node.y);
        }
        else {
            target_heading = std::fmod(heading_node.heading_angle, 360.0);
            if (target_heading < 0.0) {
                target_heading += 360.0;
            }
        }

        const double start_heading = NaviMath::NormalizeAngle(ctx.position->angle);
        const double heading_delta = NaviMath::NormalizeAngle(target_heading - start_heading);

        ctx.motion_controller->SetForwardState(false);
        utils::SleepFor(kStopWaitMs);

        double achieved_heading = start_heading;
        if (std::abs(heading_delta) <= 1.0) {
            LogInfo << "Heading-only node already aligned." << VAR(target_heading) << VAR(start_heading);
        }
        else if (!TurnToHeadingOnce(ctx, heading_delta)) {
            result.request_failure = true;
            result.failure_reason = "heading_turn_failed";
            result.failure_log_message = "HEADING node failed to issue view turn.";
            return result;
        }

        ctx.action_wrapper->PulseForwardSync(kPostHeadingForwardPulseMs);
        ctx.motion_controller->SetForwardState(false);

        // Closed-loop: confirm the turn landed and redo a swallowed view-drag (accept within wide band).
        achieved_heading = VerifyAndCorrectHeading(ctx, target_heading, start_heading);

        LogInfo << "Heading-only node completed." << VAR(target_heading) << VAR(start_heading)
                << VAR(heading_delta) << VAR(achieved_heading);
        ctx.session->AdvanceToNextWaypoint(ActionType::HEADING, "heading_consumed");
        ctx.session->ResetProgress();
        ctx.runtime_state->OnWaypointAdvance();
        consumed = true;

        if (!ctx.session->HasCurrentWaypoint()) {
            ctx.session->NoteRouteTailConsumed(*ctx.position, "heading_route_consumed");
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
    const std::optional<size_t> arrived_absolute_node_idx = ctx.session->CurrentAbsoluteNodeIndex();

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

    if (waypoint.action == ActionType::DIG) {
        StopMotionAndCommitment(ctx);

        if (ctx.maa_context == nullptr) {
            LogError << "Action: DIG triggered but maa_context is null." << VAR(actual_distance);
            result.request_failure = true;
            result.failure_reason = "dig_context_missing";
            result.failure_log_message = "MaaContext is null when dispatching dig subtask.";
            return result;
        }

        LogInfo << "Action: DIG triggered, dispatching subtask." << VAR(kDefaultDigEntry) << VAR(actual_distance);
        const MaaTaskId sub_id = MaaContextRunTask(ctx.maa_context, kDefaultDigEntry, kDigPipelineOverride);
        if (sub_id == MaaInvalidId) {
            LogError << "Action: DIG subtask failed to dispatch." << VAR(kDefaultDigEntry) << VAR(actual_distance);
            result.request_failure = true;
            result.failure_reason = "dig_dispatch_failed";
            result.failure_log_message = "MaaContextRunTask returned MaaInvalidId for dig subtask.";
            return result;
        }

        LogInfo << "Action: DIG subtask returned." << VAR(sub_id);
        utils::SleepFor(kDigPostSleepMs);

        ctx.session->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *ctx.position, "dig_completed");
        ctx.session->AdvanceToNextWaypoint(waypoint.action, "dig_completed");
        ctx.runtime_state->OnWaypointAdvance();
        ctx.runtime_state->route.Reset();

        if (!ctx.session->HasCurrentWaypoint()) {
            ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        }
        else {
            SelectPhaseForCurrentWaypoint(ctx, "dig_completed");
        }

        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    return result;
}

} // namespace semantic_nodes

} // namespace mapnavigator
