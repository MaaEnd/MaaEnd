#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../planning/navmesh_path_expander.h"
#include "../semantic/semantic_nodes.h"
#include "../sensing/position_provider.h"
#include "../zipline/zipline_action.h"
#include "internal.h"

namespace mapnavigator
{

namespace
{

bool InsidePushThroughDisc(const std::vector<VirtualNoGoDisc>& discs, const NaviPosition& position)
{
    return std::any_of(discs.begin(), discs.end(), [&](const VirtualNoGoDisc& disc) {
        return disc.push_through && disc.zone_id == position.zone_id
               && std::hypot(disc.x - position.x, disc.y - position.y) <= disc.radius + kVirtualNoGoStandoff + kVirtualNoGoRadiusStep;
    });
}

} // namespace

// cross_track 不是有限值说明投影没算出来, 这时不启动看门狗
TickStep NavigationStateMachine::CheckOffRouteWedge(const NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    const bool watched = session_->phase() == NaviPhase::Navigate && tick.waypoint.IsContinuousRun() && !route.on_route
                         && std::isfinite(route.cross_track) && !runtime_state_.cross_tier_escape.active;
    if (!watched) {
        runtime_state_.offroute.Reset();
        return TickStep::Next;
    }
    OffRouteWedgeState& wedge = runtime_state_.offroute;
    if (const int64_t blind_ms = wedge.ResumeAt(tick.now); blind_ms > 0) {
        LogInfo << "Off-route wedge clock resumed after a blind stretch." << VAR(blind_ms) << VAR(route.progress_distance)
                << VAR(wedge.best_distance);
    }
    const double progress_epsilon = std::max(kNoProgressDistanceEpsilon, kMeasurementDefaultPositionQuantum);
    const bool retaking_baseline = wedge.active && !wedge.HasBaseline();
    if (!wedge.active || retaking_baseline || route.progress_distance + progress_epsilon < wedge.best_distance) {
        wedge.active = true;
        wedge.best_distance = route.progress_distance;
        if (!retaking_baseline) {
            wedge.since = tick.now;
        }
    }
    const int64_t wedge_ms = std::chrono::duration_cast<std::chrono::milliseconds>(tick.now - wedge.since).count();
    if (wedge_ms >= kOffRouteWedgeFailMs) {
        return EndTickWith(FailNavigation(
            "offroute_wedge_timeout",
            "Off-route with no route progress past the wedge timeout; terminating so the pipeline can retry.",
            route.progress_distance,
            NaviMath::NormalizeAngle(route.route_heading - tick.fix.current_heading),
            tick.stalled_ms));
    }
    const bool replan_cooling =
        wedge.last_replan_at.time_since_epoch().count() > 0
        && std::chrono::duration_cast<std::chrono::milliseconds>(tick.now - wedge.last_replan_at).count() < kOffRouteWedgeReplanCooldownMs;
    if (wedge_ms >= kOffRouteWedgeReplanMs && !replan_cooling) {
        wedge.last_replan_at = tick.now;
        LogWarn << "Off-route wedge detected; replanning from current position." << VAR(wedge_ms) << VAR(route.waypoint_distance)
                << VAR(route.cross_track) << VAR(session_->current_node_idx());
        HandleDynamicReplanRequest("offroute_wedge");
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

TickStep NavigationStateMachine::RunObstacleRecovery(const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteTrackingState& route = tick.route;
    const bool forward_hold_futile = runtime_state_.flow.futile_forward_reasserts >= kForwardHoldFutileReassertsBeforeRecovery;
    const bool pinned_without_own_fallback = forward_hold_futile && tick.stalled_ms >= kObstacleRecoveryMinTriggerMs
                                             && !waypoint.StopsOnPromptDetection() && waypoint.action != ActionType::ZIPLINE;
    const bool far_or_strict = tick.effective_progress > kNoProgressMinDistance || waypoint.RequiresStrictArrival();
    const bool should_try_recovery = session_->phase() == NaviPhase::Navigate
                                     && (tick.stalled_ms >= kObstacleRecoveryMinTriggerMs || forward_hold_futile)
                                     && (far_or_strict || pinned_without_own_fallback) && !runtime_state_.cross_tier_escape.active;
    if (!should_try_recovery) {
        return TickStep::Next;
    }
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    if (!anchor) {
        return TickStep::Next;
    }
    DynamicRecoveryState& recovery = runtime_state_.recovery;
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    if (!recovery.active || recovery.anchor_index != anchor->first) {
        recovery.Restart(*position_, tick.now, anchor->first);
    }
    if (escalation.anchor_index != anchor->first) {
        const bool fought_another_anchor = escalation.anchor_index != std::numeric_limits<size_t>::max();
        escalation.Retarget(anchor->first);
        if (fought_another_anchor) {
            device_recovery_.ForgetObservation();
        }
    }

    // 用硬进度时钟计时, 每次小脱困都会重置 recovery.started_at
    const int64_t recovery_elapsed_ms = session_->HardStalledMs(tick.now);
    if (recovery_elapsed_ms > kDynamicRecoveryTotalTimeoutMs) {
        if (waypoint.action == ActionType::ZIPLINE) {
            LogWarn << "Recovery timed out at a zipline tower; dropping the chain and walking." << VAR(recovery_elapsed_ms)
                    << VAR(waypoint.x) << VAR(waypoint.y) << VAR(position_->x) << VAR(position_->y);
            semantic_nodes::AbandonZipline(SemanticContext(), "zipline_recovery_timeout", "stuck at the tower after recovery ran out");
            return TickStep::EndTick;
        }
        return EndTickWith(FailNavigation(
            "dynamic_recovery_timeout",
            "Dynamic recovery timeout reached and navigation was terminated.",
            route.progress_distance,
            NaviMath::NormalizeAngle(route.route_heading - tick.fix.current_heading),
            tick.stalled_ms));
    }

    const bool retry_cooling_down = recovery.last_replan_at.time_since_epoch().count() > 0
                                    && std::chrono::duration_cast<std::chrono::milliseconds>(tick.now - recovery.last_replan_at).count()
                                           < kDynamicRecoveryRetryIntervalMs;
    if (retry_cooling_down) {
        return TickStep::Next;
    }
    recovery.last_replan_at = tick.now;
    return EndTickWith(ClimbRecoveryLadder(tick, far_or_strict));
}

bool NavigationStateMachine::ClimbRecoveryLadder(const NavigateTick& tick, bool far_or_strict)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteTrackingState& route = tick.route;
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    const DeviceRemovalOutcome device_outcome =
        device_recovery_.TryRemove(route, waypoint, escalation.device_attempt_count < kRecoveryDeviceAttempts && far_or_strict);
    if (device_outcome != DeviceRemovalOutcome::NotAttempted) {
        ++escalation.device_attempt_count;
    }
    if (device_outcome == DeviceRemovalOutcome::Escaped) {
        return ResumeAfterEscape("recovery_device_move_escape");
    }
    if (device_outcome == DeviceRemovalOutcome::NeedsFreshFix) {
        return true;
    }

    ++escalation.jump_attempt_count;
    const NaviPosition jump_start = *position_;
    LogInfo << "Dynamic recovery jump pulse issued." << VAR(escalation.jump_attempt_count) << VAR(escalation.detour_attempt_count);
    motion_controller_->SetAction(LocalDriverAction::JumpForward, true);
    SleepFor(kActionJumpSettleMs);
    motion_controller_->SetForwardState(false);
    if (!CaptureCurrentPosition(false) || position_provider_->LastCaptureWasBlackScreen() || !position_->valid) {
        LogWarn << "Dynamic recovery waiting for post-jump local tracking fix." << VAR_AS(stalled_ms, tick.stalled_ms)
                << VAR(escalation.jump_attempt_count);
        SleepFor(kTargetTickMs);
        return true;
    }

    const bool jump_zone_changed = ZonesDiffer(jump_start.zone_id, position_->zone_id);
    const double jump_displacement = std::hypot(position_->x - jump_start.x, position_->y - jump_start.y);
    const double jump_waypoint_distance = std::hypot(waypoint.x - position_->x, waypoint.y - position_->y);
    const bool jump_made_progress = jump_waypoint_distance + kNoProgressDistanceEpsilon < route.waypoint_distance;
    const bool jump_moved_forward = jump_displacement >= kDynamicRecoveryResetDistance * 0.5 && jump_made_progress;
    if (jump_zone_changed || jump_displacement >= kDynamicRecoveryResetDistance || jump_moved_forward) {
        LogInfo << "Dynamic recovery jump escaped obstacle." << VAR(jump_zone_changed) << VAR(jump_displacement) << VAR(jump_moved_forward);
        return ResumeAfterEscape("recovery_jump_escape");
    }
    return EscalatePastJump(tick);
}

bool NavigationStateMachine::EscalatePastJump(const NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    DynamicRecoveryState& recovery = runtime_state_.recovery;
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    const std::optional<DynamicAnchor> post_jump_anchor = ResolveCurrentAnchor(session_, *position_);
    if (!post_jump_anchor) {
        LogWarn << "Dynamic recovery skipped: no future anchor after post-jump local tracking." << VAR(position_->x) << VAR(position_->y)
                << VAR(position_->zone_id);
        SleepFor(kTargetTickMs);
        return true;
    }
    if (post_jump_anchor->first != recovery.anchor_index) {
        recovery.Restart(*position_, tick.now, post_jump_anchor->first);
        recovery.last_replan_at = tick.now;
    }
    if (post_jump_anchor->first != escalation.anchor_index) {
        escalation.Retarget(post_jump_anchor->first);
        escalation.jump_attempt_count = 1;
    }

    if (escalation.jump_attempt_count < kRecoveryJumpAttemptsBeforeDetour) {
        SleepFor(kTargetTickMs);
        return true;
    }
    const double stuck_heading = EffectiveRouteHeading(tick.nav_run_result, route);
    if (!InsidePushThroughDisc(runtime_state_.virtual_no_go, *position_)
        && escalation.detour_attempt_count < kRecoveryDetourAttemptsBeforeUnstick) {
        ++escalation.detour_attempt_count;
        if (TryVirtualNoGoReplan("recovery_navmesh_detour", post_jump_anchor->first, post_jump_anchor->second, *position_, stuck_heading)) {
            SelectPhaseForCurrentWaypoint("recovery_navmesh_detour");
            return true;
        }
        LogWarn << "Dynamic recovery detour attempt failed; switching to physical unstick." << VAR(escalation.detour_attempt_count)
                << VAR(escalation.jump_attempt_count) << VAR(post_jump_anchor->first) << VAR(route.progress_distance)
                << VAR_AS(stalled_ms, tick.stalled_ms);
    }
    if (ExecutePhysicalUnstick(stuck_heading)) {
        return true;
    }
    SleepFor(kTargetTickMs);
    return true;
}

bool NavigationStateMachine::ExecutePhysicalUnstick(double stuck_heading)
{
    LateralBypassState& unstick = runtime_state_.bypass;
    if (unstick.active && std::hypot(position_->x - unstick.origin.x, position_->y - unstick.origin.y) > kUnstickResetDistanceM) {
        unstick.Reset();
    }
    if (!unstick.active) {
        unstick.BeginAt(*position_);
    }

    double distance = kUnstickMinDistanceM;
    const std::optional<navmesh::WorldPoint> target = PlanUnstickTarget(param_, *position_, stuck_heading, unstick.count, &distance);
    if (!target) {
        ++unstick.count;
        LogWarn << "Physical unstick: no on-mesh escape bearing found." << VAR(stuck_heading) << VAR(unstick.count);
        return false;
    }

    const double target_heading = NaviMath::CalcTargetRotation(position_->x, position_->y, target->x, target->y);
    const double heading_delta = NaviMath::CalcDeltaRotation(position_->angle, target_heading);
    motion_controller_->SetForwardState(false);
    SleepFor(kStopWaitMs);
    int units = static_cast<int>(std::lround(heading_delta * action_wrapper_->DefaultTurnUnitsPerDegree()));
    if (units == 0) {
        units = heading_delta > 0.0 ? 1 : -1;
    }
    action_wrapper_->SendViewDeltaSync(units, 0);

    const NaviPosition step_start = *position_;
    double moved = 0.0;
    for (int pulse = 0; pulse < kUnstickMaxPulses; ++pulse) {
        action_wrapper_->PulseForwardSync(kUnstickPulseMs);
        if (!CaptureCurrentPosition(false) || position_provider_->LastCaptureWasBlackScreen() || !position_->valid) {
            break;
        }
        moved = std::hypot(position_->x - step_start.x, position_->y - step_start.y);
        if (moved >= distance * kUnstickSuccessFraction) {
            break;
        }
    }
    motion_controller_->SetForwardState(false);
    ++unstick.count;
    const bool dislodged = moved >= distance * kUnstickSuccessFraction;
    LogInfo << "Physical unstick step executed." << VAR(distance) << VAR(moved) << VAR(dislodged) << VAR(unstick.count)
            << VAR(target_heading) << VAR(target->x) << VAR(target->y);

    // 禁区以位移前的位置为原点, 障碍在那里的前方
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    const bool replanned =
        (anchor && TryVirtualNoGoReplan("recovery_unstick_replan", anchor->first, anchor->second, step_start, stuck_heading))
        || TryApplyDynamicOverlayToNextAnchor("recovery_unstick_replan");
    if (replanned) {
        session_->ResetProgress();
        SelectPhaseForCurrentWaypoint("recovery_unstick_replan");
        return true;
    }
    runtime_state_.route.ResetTracking();
    runtime_state_.dynamic_replan_requested = false;
    runtime_state_.nav_run_dirty = true;
    session_->ResetProgress();
    SelectPhaseForCurrentWaypoint("recovery_physical_unstick");
    return true;
}

} // namespace mapnavigator
