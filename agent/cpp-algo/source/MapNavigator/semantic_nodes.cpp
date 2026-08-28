#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <thread>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "action_wrapper.h"
#include "async_prompt_action.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "position_provider.h"
#include "semantic_helpers.h"
#include "semantic_nodes.h"
#include "zipline_action.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

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
            ctx.action_wrapper->PulseForwardSync(kPostHeadingForwardPulseMs);
            ctx.motion_controller->SetForwardState(false);
        }
        else if (!CommitHeadingTurn(ctx, heading_delta)) {
            result.request_failure = true;
            result.failure_reason = "heading_turn_failed";
            result.failure_log_message = "HEADING node failed to issue view turn.";
            return result;
        }

        // Closed-loop: confirm the turn landed and redo a swallowed view-drag (accept within wide band).
        achieved_heading = VerifyAndCorrectHeading(ctx, target_heading, start_heading);

        LogInfo << "Heading-only node completed." << VAR(target_heading) << VAR(start_heading) << VAR(heading_delta)
                << VAR(achieved_heading);
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

// One usable fix, skipping frames the locator held or blacked out. Reads taken while walking lag behind, and the
// residual is judged from one of them on purpose: at a walk the lag is small, and standing still to re-measure is
// exactly what this replaced. False when nothing usable comes back within the frame budget.
bool CaptureCleanFix(const Context& ctx, NaviPosition* out_pos)
{
    for (int frame = 0; frame < kStrictSettleFixMaxFrames; ++frame) {
        if (frame > 0) {
            utils::SleepFor(kStrictSettleFixIntervalMs);
        }
        if (!ctx.position_provider->Capture(ctx.position, false, ctx.session->current_zone_id())
            || ctx.position_provider->LastCaptureWasHeld() || ctx.position_provider->LastCaptureWasBlackScreen()) {
            continue;
        }
        *out_pos = *ctx.position;
        return true;
    }
    return false;
}

template <typename CanCaptureFrame>
bool CaptureStableHeadingImpl(const Context& ctx, double* out_heading, const CanCaptureFrame& can_capture_frame)
{
    std::optional<double> previous;
    for (int frame = 0; can_capture_frame(frame); ++frame) {
        if (frame > 0) {
            utils::SleepFor(kHeadingStableReadIntervalMs);
        }
        if (!ctx.position_provider->Capture(ctx.position, false, ctx.session->current_zone_id())
            || ctx.position_provider->LastCaptureWasHeld()) {
            continue;
        }
        const double current = NaviMath::NormalizeAngle(ctx.position->angle);
        if (previous && std::abs(NaviMath::NormalizeAngle(current - *previous)) <= kHeadingStableReadToleranceDeg) {
            *out_heading = current;
            return true;
        }
        previous = current;
    }
    return false;
}

} // namespace

bool TurnToHeadingOnce(const Context& ctx, double heading_delta)
{
    if (std::abs(heading_delta) <= 1.0) {
        return true;
    }

    const SteeringTransportProfile profile = ctx.action_wrapper->SteeringProfile();
    const int step_count = static_cast<int>(std::ceil(std::abs(heading_delta) / profile.max_batch_delta_deg));
    const double step_deg = heading_delta / step_count;
    const int step_interval_ms = std::max<int>(kHeadingTurnStepIntervalMs, profile.min_send_interval_ms);
    LogInfo << "Heading-only node turn." << VAR(heading_delta) << VAR(step_count) << VAR(step_deg);
    for (int step = 0; step < step_count; ++step) {
        int units = static_cast<int>(std::lround(step_deg * ctx.action_wrapper->DefaultTurnUnitsPerDegree()));
        if (units == 0) {
            units = step_deg > 0.0 ? 1 : -1;
        }
        if (!ctx.action_wrapper->SendViewDeltaSync(units, 0)) {
            return false;
        }
        if (step + 1 < step_count) {
            utils::SleepFor(step_interval_ms);
        }
    }
    return true;
}

bool CaptureStableHeading(const Context& ctx, double* out_heading)
{
    return CaptureStableHeadingImpl(ctx, out_heading, [](int frame) { return frame < kHeadingStableReadMaxFrames; });
}

bool CaptureStableHeadingUntil(const Context& ctx, double* out_heading, std::chrono::steady_clock::time_point deadline)
{
    const auto interval = std::chrono::milliseconds(kHeadingStableReadIntervalMs);
    return CaptureStableHeadingImpl(ctx, out_heading, [&](int frame) {
        const auto now = std::chrono::steady_clock::now();
        return frame == 0 ? now < deadline : now + interval <= deadline;
    });
}

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

bool CommitHeadingTurn(const Context& ctx, double heading_delta)
{
    if (!TurnToHeadingOnce(ctx, heading_delta)) {
        return false;
    }
    utils::SleepFor(kWaitAfterFirstTurnMs);
    ctx.action_wrapper->PulseForwardSync(kPostHeadingForwardPulseMs);
    ctx.motion_controller->SetForwardState(false);
    utils::SleepFor(kWaitAfterFirstTurnMs);
    return true;
}

double VerifyAndCorrectHeading(const Context& ctx, double target_heading, double fallback_heading)
{
    double achieved = fallback_heading;
    for (int correction = 0; correction <= kHeadingVerifyMaxRetries; ++correction) {
        double stable_heading = 0.0;
        if (!CaptureStableHeading(ctx, &stable_heading)) {
            LogWarn << "Heading verify skipped: no stable locator fix." << VAR(target_heading) << VAR(achieved);
            return achieved;
        }
        achieved = stable_heading;
        const double residual = NaviMath::NormalizeAngle(target_heading - achieved);
        if (std::abs(residual) <= kHeadingAcceptToleranceDeg) {
            return achieved;
        }
        if (correction == kHeadingVerifyMaxRetries) {
            LogWarn << "Heading retries exhausted, accepting." << VAR(target_heading) << VAR(achieved) << VAR(residual);
            return achieved;
        }
        LogInfo << "Heading off after turn, re-issuing." << VAR(target_heading) << VAR(achieved) << VAR(residual) << VAR(correction);
        if (!CommitHeadingTurn(ctx, residual)) {
            return achieved;
        }
    }
    return achieved;
}

bool SettleAtStrictGoal(const Context& ctx, const Waypoint& waypoint)
{
    const auto started = std::chrono::steady_clock::now();
    // 全程按着前进键: 松手再转镜头只有镜头会动, 角色朝向不变, 迈出去的那步还是走老方向。按着转才跟
    // 主循环的操舵是同一回事, 走路态本身就慢, 也没有需要先刹掉的滑行
    ctx.motion_controller->SetForwardState(true);

    // The first leg measured is whatever the approach covered since the tick's fix, each later one is the step this
    // loop walked. Either way its direction is where the character was pointing, which the minimap arrow can report
    // flipped, and it costs no probe step. Its length doubles as the calibration that sizes the next step.
    NaviPosition step_from = *ctx.position;
    int step_ms = 0;
    std::optional<double> heading;
    double wu_per_ms = 0.0;
    int stalled_steps = 0;

    for (int correction = 0; correction <= kStrictSettleMaxCorrections; ++correction) {
        NaviPosition fix {};
        if (!CaptureCleanFix(ctx, &fix)) {
            StopMotionAndCommitment(ctx);
            LogWarn << "Strict arrival settle gave up: no locator fix." << VAR(correction);
            return false;
        }

        const double travelled = std::hypot(fix.x - step_from.x, fix.y - step_from.y);
        if (travelled >= kStrictSettleStalledStepWu) {
            heading = NaviMath::CalcTargetRotation(step_from.x, step_from.y, fix.x, fix.y);
            stalled_steps = 0;
            if (step_ms > 0) {
                wu_per_ms = travelled / static_cast<double>(step_ms);
            }
        }
        else if (step_ms > 0) {
            ++stalled_steps;
        }

        const double residual = std::hypot(waypoint.x - fix.x, waypoint.y - fix.y);
        if (residual <= kStrictSettleAcceptBandWu) {
            StopMotionAndCommitment(ctx);
            LogInfo << "Strict arrival verified." << VAR(residual) << VAR(correction) << VAR(fix.x) << VAR(fix.y);
            return true;
        }

        const int64_t elapsed_ms =
            std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - started).count();
        if (correction == kStrictSettleMaxCorrections || stalled_steps >= kStrictSettleStalledSteps
            || elapsed_ms >= kStrictSettleBudgetMs) {
            StopMotionAndCommitment(ctx);
            LogWarn << "Strict arrival settle gave up, accepting on band." << VAR(residual) << VAR(correction) << VAR(stalled_steps)
                    << VAR(elapsed_ms) << VAR(fix.x) << VAR(fix.y);
            return false;
        }

        const double bearing = NaviMath::CalcTargetRotation(fix.x, fix.y, waypoint.x, waypoint.y);
        const double from_heading = heading ? *heading : NaviMath::NormalizeHeading(fix.angle);
        // Sized by what is left, floored at the stationary latch: a shorter step cannot be told apart from not having
        // moved, so it would also destroy the only test for a step that is being blocked.
        const double step_wu = std::max(residual, kStrictSettleMinStepWu);
        const int step_hold_ms =
            wu_per_ms > 0.0 ? std::clamp(static_cast<int>(std::lround(step_wu / wu_per_ms)), kStrictSettleMinStepMs, kStrictSettleMaxStepMs)
                            : kStrictSettleStepMs;

        LogInfo << "Strict arrival correcting." << VAR(residual) << VAR(bearing) << VAR(from_heading) << VAR(step_wu) << VAR(step_hold_ms)
                << VAR(correction);
        const auto step_started = std::chrono::steady_clock::now();
        if (!TurnToHeadingOnce(ctx, NaviMath::CalcDeltaRotation(from_heading, bearing))) {
            StopMotionAndCommitment(ctx);
            LogWarn << "Strict arrival settle gave up: view delta rejected." << VAR(residual) << VAR(correction);
            return false;
        }
        heading = bearing;
        utils::SleepFor(step_hold_ms);

        // 转身那阵子人也在走, 所以这一步有多长要按真实经过的时间算, 拿 sleep 的长度会把速度估高
        step_from = fix;
        step_ms = static_cast<int>(
            std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - step_started).count());
    }
    StopMotionAndCommitment(ctx);
    return false;
}

Result TickSemanticFlow(const Context& ctx, NaviPhase phase)
{
    if (phase == NaviPhase::WaitTransfer) {
        return TickTransferWaitImpl(ctx);
    }
    if (phase == NaviPhase::WaitZipline) {
        return TickZiplineRide(ctx);
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

    if (waypoint.action == ActionType::ZIPLINE) {
        return StartZiplineHop(ctx, waypoint, actual_distance, arrived_absolute_node_idx);
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

    // Fallback once the point is reached: run the same authoritative subtask the walking detector would. No prompt
    // means there is nothing to interact with here, so the route advances anyway rather than failing.
    if (waypoint.IsAsyncInteract() && ctx.maa_context != nullptr) {
        StopMotionAndCommitment(ctx);

        LogInfo << "Action: INTERACT reached, running the authoritative recognition." << VAR(actual_distance)
                << VAR(waypoint.interact_text.size()) << VAR(waypoint.interact_scan) << VAR(waypoint.interact_rec);
        RunPromptSubtask(ctx.maa_context, kInteractPromptSpec, &waypoint.interact_text, waypoint.interact_rec);

        ctx.session->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *ctx.position, "async_interact_completed");
        ctx.session->AdvanceToNextWaypoint(waypoint.action, "async_interact_completed");
        ctx.runtime_state->OnWaypointAdvance();
        ctx.runtime_state->route.Reset();

        if (!ctx.session->HasCurrentWaypoint()) {
            ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        }
        else {
            SelectPhaseForCurrentWaypoint(ctx, "async_interact_completed");
        }

        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    return result;
}

} // namespace semantic_nodes

} // namespace mapnavigator
