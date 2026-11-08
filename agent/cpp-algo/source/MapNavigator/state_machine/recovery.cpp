#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <string>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../planning/navmesh_queries.h"
#include "../semantic/semantic_nodes.h"
#include "../sensing/position_provider.h"
#include "../zipline/zipline_action.h"
#include "internal.h"

namespace mapnavigator
{

namespace
{

bool inside_push_through_disc(const std::vector<VirtualNoGoDisc>& discs, const NaviPosition& position)
{
    return std::any_of(discs.begin(), discs.end(), [&](const VirtualNoGoDisc& disc) {
        return disc.push_through && disc.zone_id == position.zone_id
               && std::hypot(disc.x - position.x, disc.y - position.y) <= disc.radius + kVirtualNoGoStandoff + kVirtualNoGoRadiusStep;
    });
}

bool far_or_strict_target(double effective_progress, const Waypoint& waypoint)
{
    return effective_progress > kNoProgressMinDistance || RequiresStrictArrival(waypoint);
}

std::optional<DynamicAnchor> resolve_recovery_anchor(const NaviParam& param, Session* session, const NaviPosition& position)
{
    if (std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(param, session, position)) {
        return anchor;
    }
    if (!HasCurrentWaypoint(*session)) {
        return std::nullopt;
    }
    const Waypoint& current_waypoint = CurrentWaypoint(*session);
    const std::optional<size_t> canonical_index = CanonicalIndexAtCurrent(*session);
    if (!current_waypoint.has_position || !canonical_index) {
        return std::nullopt;
    }
    return DynamicAnchor { *canonical_index, current_waypoint };
}

} // namespace

// cross_track 不是有限值说明投影没算出来, 这时不启动看门狗
TickStep NavigationStateMachine::checkOffRouteWedge(const NavigateTick& tick)
{
    const RouteMeasurement& route = tick.route;
    const bool watched =
        session_->phase == NaviPhase::Navigate && IsContinuousRun(tick.waypoint) && !route.on_route && std::isfinite(route.cross_track);
    if (!watched) {
        runtime_state_.offroute = {};
        return TickStep::Next;
    }
    if (const int64_t blind_ms = ResumeWedge(&runtime_state_.offroute, tick.now); blind_ms > 0) {
        LogInfo << "Off-route wedge clock resumed after a blind stretch." << VAR(blind_ms) << VAR(route.progress_distance)
                << VAR(runtime_state_.offroute.best_distance);
    }
    switch (StepWedge(&runtime_state_.offroute, route.progress_distance, tick.now)) {
    case WedgeAction::None:
        return TickStep::Next;
    case WedgeAction::Fail:
        return EndTickWith(failNavigation(
            "offroute_wedge_timeout",
            "Off-route with no route progress past the wedge timeout; terminating so the pipeline can retry.",
            route.progress_distance,
            NormalizeAngle(route.route_heading - tick.fix.current_heading),
            tick.stalled_ms));
    case WedgeAction::Replan: {
        const int64_t wedge_ms = ElapsedMs(runtime_state_.offroute.since, tick.now);
        LogWarn << "Off-route wedge detected; replanning from current position." << VAR(wedge_ms) << VAR(route.waypoint_distance)
                << VAR(route.cross_track) << VAR_AS(session_->current_node_idx(), session_->current_node_idx);
        handleDynamicReplanRequest("offroute_wedge");
        return TickStep::EndTick;
    }
    }
    return TickStep::Next;
}

TickStep NavigationStateMachine::runObstacleRecovery(const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteMeasurement& route = tick.route;
    const bool forward_hold_futile = runtime_state_.flow.futile_forward_reasserts >= kForwardHoldFutileReassertsBeforeRecovery;
    const bool pinned_without_own_fallback = forward_hold_futile && tick.stalled_ms >= kObstacleRecoveryMinTriggerMs
                                             && !StopsOnPromptDetection(waypoint) && waypoint.action != ActionType::ZIPLINE;
    const bool far_or_strict = far_or_strict_target(tick.effective_progress, waypoint);
    const bool stalled =
        session_->phase == NaviPhase::Navigate && (tick.stalled_ms >= kObstacleRecoveryMinTriggerMs || forward_hold_futile);
    if (!stalled) {
        runtime_state_.recovery_escalation.blocked_reason.clear();
        return TickStep::Next;
    }
    if (!far_or_strict && !pinned_without_own_fallback && HardStalledMs(*session_, tick.now) < kNearTargetRecoveryBudgetMs) {
        noteRecoveryBlocked("awaiting_arrival");
        return TickStep::Next;
    }
    const std::optional<DynamicAnchor> anchor = resolve_recovery_anchor(param_, session_, *position_);
    if (!anchor) {
        noteRecoveryBlocked("no_anchor");
        return TickStep::Next;
    }
    DynamicRecoveryState& recovery = runtime_state_.recovery;
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    if (!recovery.active || recovery.anchor_index != anchor->first) {
        RestartDynamicRecovery(&recovery, *position_, anchor->first, tick.now);
    }
    if (escalation.anchor_index != anchor->first) {
        const bool fought_another_anchor = escalation.anchor_index != std::numeric_limits<size_t>::max();
        RetargetRecoveryEscalation(&escalation, anchor->first);
        if (fought_another_anchor) {
            ForgetDeviceObservation(&device_recovery_);
        }
    }

    // 用硬进度时钟计时, 每次小脱困都会重置 recovery.started_at
    const int64_t recovery_elapsed_ms = HardStalledMs(*session_, tick.now);
    const int64_t recovery_timeout_ms =
        far_or_strict ? kDynamicRecoveryTotalTimeoutMs : kNearTargetRecoveryBudgetMs + kDynamicRecoveryTotalTimeoutMs;
    if (recovery_elapsed_ms > recovery_timeout_ms) {
        if (waypoint.action == ActionType::ZIPLINE) {
            LogWarn << "Recovery timed out at a zipline tower; dropping the chain and walking." << VAR(recovery_elapsed_ms)
                    << VAR(waypoint.x) << VAR(waypoint.y) << VAR(position_->x) << VAR(position_->y);
            semantic_nodes::AbandonZipline(semanticContext(), "zipline_recovery_timeout", "stuck at the tower after recovery ran out");
            return TickStep::EndTick;
        }
        return EndTickWith(failNavigation(
            "dynamic_recovery_timeout",
            "Dynamic recovery timeout reached and navigation was terminated.",
            route.progress_distance,
            NormalizeAngle(route.route_heading - tick.fix.current_heading),
            tick.stalled_ms));
    }

    const bool retry_cooling_down = recovery.last_replan_at.time_since_epoch().count() > 0
                                    && std::chrono::duration_cast<std::chrono::milliseconds>(tick.now - recovery.last_replan_at).count()
                                           < kDynamicRecoveryRetryIntervalMs;
    if (retry_cooling_down) {
        return TickStep::Next;
    }
    recovery.last_replan_at = tick.now;
    return EndTickWith(climbRecoveryLadder(tick));
}

void NavigationStateMachine::noteRecoveryBlocked(const char* reason)
{
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    if (escalation.blocked_reason == reason) {
        return;
    }
    escalation.blocked_reason = reason;
    LogWarn << "Recovery blocked." << VAR(reason) << VAR(position_->x) << VAR(position_->y) << VAR(position_->zone_id);
    NoteFailure(&failure_trail_, std::chrono::steady_clock::now(), std::string("recovery blocked ") + reason);
}

bool NavigationStateMachine::climbRecoveryLadder(const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteMeasurement& route = tick.route;
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    const DeviceRemovalOutcome device_outcome = TryRemoveDevice(
        &device_recovery_,
        route,
        waypoint,
        escalation.device_attempt_count < kRecoveryDeviceAttempts && far_or_strict_target(tick.effective_progress, waypoint));
    if (device_outcome != DeviceRemovalOutcome::NotAttempted) {
        ++escalation.device_attempt_count;
        NoteFailure(
            &failure_trail_,
            std::chrono::steady_clock::now(),
            device_outcome == DeviceRemovalOutcome::Escaped ? "recovery device escaped" : "recovery device stuck");
    }
    if (device_outcome == DeviceRemovalOutcome::Escaped) {
        return resumeAfterEscape("recovery_device_move_escape");
    }
    if (device_outcome == DeviceRemovalOutcome::NeedsFreshFix) {
        return true;
    }
    if (device_outcome == DeviceRemovalOutcome::UnknownUi) {
        const auto cleanup_started_at = std::chrono::steady_clock::now();
        PauseWedge(&runtime_state_.offroute, cleanup_started_at);
        if (!returnToWorld()) {
            return true;
        }
        ExcludeFromStallClocks(session_, std::chrono::steady_clock::now() - cleanup_started_at);
        UpdateCurrentZone(session_, position_->zone_id);
    }

    if (escalation.jump_attempt_count >= kRecoveryJumpAttemptsBeforeDetour) {
        return escalatePastJump(tick);
    }
    ++escalation.jump_attempt_count;
    const NaviPosition jump_start = *position_;
    LogInfo << "Dynamic recovery jump pulse issued." << VAR(escalation.jump_attempt_count) << VAR(escalation.detour_attempt_count);
    NoteFailure(&failure_trail_, std::chrono::steady_clock::now(), "recovery jump");
    ArmSteeringQuietPeriod(motion_controller_);
    ApplyLocalAction(motion_controller_, LocalDriverAction::JumpForward);
    SleepFor(kActionJumpSettleMs);
    HoldPosition(motion_controller_);
    if (!captureCurrentPosition(false) || position_provider_->last_capture_was_black_screen || !position_->valid) {
        LogWarn << "Dynamic recovery waiting for post-jump local tracking fix." << VAR_AS(stalled_ms, tick.stalled_ms)
                << VAR(escalation.jump_attempt_count);
        SleepFor(kTargetTickMs);
        return true;
    }

    const bool jump_zone_changed = ZonesDifferInGeometry(param_, jump_start.zone_id, position_->zone_id);
    const double jump_displacement = std::hypot(position_->x - jump_start.x, position_->y - jump_start.y);
    const double jump_waypoint_distance = std::hypot(waypoint.x - position_->x, waypoint.y - position_->y);
    const bool jump_made_progress = jump_waypoint_distance + kNoProgressDistanceEpsilon < route.waypoint_distance;
    const bool jump_moved_forward = jump_displacement >= kDynamicRecoveryResetDistance * 0.5 && jump_made_progress;
    if (jump_zone_changed || jump_displacement >= kDynamicRecoveryResetDistance || jump_moved_forward) {
        LogInfo << "Dynamic recovery jump escaped obstacle." << VAR(jump_zone_changed) << VAR(jump_displacement) << VAR(jump_moved_forward);
        return resumeAfterEscape("recovery_jump_escape");
    }
    return escalatePastJump(tick);
}

bool NavigationStateMachine::escalatePastJump(const NavigateTick& tick)
{
    const RouteMeasurement& route = tick.route;
    DynamicRecoveryState& recovery = runtime_state_.recovery;
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    const std::optional<DynamicAnchor> post_jump_anchor = resolve_recovery_anchor(param_, session_, *position_);
    if (!post_jump_anchor) {
        LogWarn << "Dynamic recovery skipped: no future anchor after post-jump local tracking." << VAR(position_->x) << VAR(position_->y)
                << VAR(position_->zone_id);
        SleepFor(kTargetTickMs);
        return true;
    }
    if (post_jump_anchor->first != recovery.anchor_index) {
        RestartDynamicRecovery(&recovery, *position_, post_jump_anchor->first, tick.now);
        recovery.last_replan_at = tick.now;
    }
    if (post_jump_anchor->first != escalation.anchor_index) {
        RetargetRecoveryEscalation(&escalation, post_jump_anchor->first);
        escalation.jump_attempt_count = 1;
    }

    if (escalation.jump_attempt_count < kRecoveryJumpAttemptsBeforeDetour) {
        SleepFor(kTargetTickMs);
        return true;
    }
    const double stuck_heading = EffectiveRouteHeading(tick.nav_run_result, route);
    if (far_or_strict_target(tick.effective_progress, tick.waypoint) && !inside_push_through_disc(runtime_state_.virtual_no_go, *position_)
        && escalation.detour_attempt_count < kRecoveryDetourAttemptsBeforeUnstick) {
        ++escalation.detour_attempt_count;
        if (tryVirtualNoGoReplan("recovery_navmesh_detour", post_jump_anchor->first, post_jump_anchor->second, *position_, stuck_heading)) {
            selectPhaseForCurrentWaypoint("recovery_navmesh_detour");
            return true;
        }
        LogWarn << "Dynamic recovery detour attempt failed; switching to physical unstick." << VAR(escalation.detour_attempt_count)
                << VAR(escalation.jump_attempt_count) << VAR(post_jump_anchor->first) << VAR(route.progress_distance)
                << VAR_AS(stalled_ms, tick.stalled_ms);
        NoteFailure(&failure_trail_, std::chrono::steady_clock::now(), "recovery detour failed");
    }
    if (executePhysicalUnstick(stuck_heading)) {
        return true;
    }
    SleepFor(kTargetTickMs);
    return true;
}

bool NavigationStateMachine::executePhysicalUnstick(double stuck_heading)
{
    LateralBypassState& unstick = runtime_state_.bypass;
    if (unstick.active && std::hypot(position_->x - unstick.origin.x, position_->y - unstick.origin.y) > kUnstickResetDistanceM) {
        unstick = {};
    }
    if (!unstick.active) {
        BeginLateralBypass(&unstick, *position_);
    }

    double distance = kUnstickMinDistanceM;
    const std::optional<navmesh::WorldPoint> target = PlanUnstickTarget(param_, *position_, stuck_heading, unstick.count, &distance);
    if (!target) {
        ++unstick.count;
        LogWarn << "Physical unstick: no on-mesh escape bearing found." << VAR(stuck_heading) << VAR(unstick.count);
        NoteFailure(&failure_trail_, std::chrono::steady_clock::now(), "recovery unstick no bearing");
        return false;
    }

    const double target_heading = CalcTargetRotation(position_->x, position_->y, target->x, target->y);
    const double heading_delta = CalcDeltaRotation(position_->angle, target_heading);
    HoldPosition(motion_controller_);
    SleepFor(kStopWaitMs);
    int units = static_cast<int>(std::lround(heading_delta * TurnUnitsPerDegree(*input_backend_)));
    if (units == 0) {
        units = heading_delta > 0.0 ? 1 : -1;
    }
    SendViewDeltaSync(input_backend_, units, 0);

    const NaviPosition step_start = *position_;
    double moved = 0.0;
    for (int pulse = 0; pulse < kUnstickMaxPulses; ++pulse) {
        PulseForwardSync(input_backend_, kUnstickPulseMs);
        if (!captureCurrentPosition(false) || position_provider_->last_capture_was_black_screen || !position_->valid) {
            break;
        }
        moved = std::hypot(position_->x - step_start.x, position_->y - step_start.y);
        if (moved >= distance * kUnstickSuccessFraction) {
            break;
        }
    }
    HoldPosition(motion_controller_);
    ++unstick.count;
    const bool dislodged = moved >= distance * kUnstickSuccessFraction;
    LogInfo << "Physical unstick step executed." << VAR(distance) << VAR(moved) << VAR(dislodged) << VAR(unstick.count)
            << VAR(target_heading) << VAR(target->x) << VAR(target->y);
    NoteFailure(&failure_trail_, std::chrono::steady_clock::now(), dislodged ? "recovery unstick dislodged" : "recovery unstick stuck");
    if (dislodged) {
        runtime_state_.recovery_escalation.jump_attempt_count = 0;
    }

    // 禁区以位移前的位置为原点, 障碍在那里的前方
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(param_, session_, *position_);
    const bool replanned =
        (anchor && tryVirtualNoGoReplan("recovery_unstick_replan", anchor->first, anchor->second, step_start, stuck_heading))
        || tryApplyDynamicOverlayToNextAnchor("recovery_unstick_replan");
    if (replanned) {
        ResetProgress(session_);
        selectPhaseForCurrentWaypoint("recovery_unstick_replan");
        return true;
    }
    ResetRouteTracking(&runtime_state_.route);
    runtime_state_.dynamic_replan_requested = false;
    runtime_state_.nav_run_dirty = true;
    ResetProgress(session_);
    selectPhaseForCurrentWaypoint("recovery_physical_unstick");
    return true;
}

} // namespace mapnavigator
