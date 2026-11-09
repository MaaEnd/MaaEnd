#include <algorithm>
#include <chrono>
#include <cmath>
#include <optional>

#include <meojson/json.hpp>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../planning/route_tracker.h"
#include "../semantic/semantic_helpers.h"
#include "../semantic/semantic_nodes.h"
#include "../semantic/strict_settle.h"
#include "../sensing/position_provider.h"
#include "internal.h"

namespace mapnavigator
{

namespace
{

std::optional<DynamicAnchor>
    resolve_nav_run_anchor(const NaviParam& param, Session* session, const NaviPosition& position, const RouteMeasurement& route)
{
    if (!route.valid || !HasCurrentWaypoint(*session)) {
        return std::nullopt;
    }
    const Waypoint& current_waypoint = CurrentWaypoint(*session);
    if (!current_waypoint.has_position || current_waypoint.action != ActionType::RUN) {
        return std::nullopt;
    }
    if (RequiresStrictArrival(current_waypoint)) {
        return DynamicAnchor { CanonicalIndexAtCurrent(*session).value_or(kUnaddressableAnchorIndex), current_waypoint };
    }
    return ResolveCurrentAnchor(param, session, position);
}

// 台沿下落的落点: 进圈时人可能还在台上, 沿下落方向越过它才算到
bool short_of_drop(const Waypoint& waypoint, const NaviPosition& position)
{
    if (!waypoint.drop_from) {
        return false;
    }
    const double dx = waypoint.x - (*waypoint.drop_from)[0];
    const double dy = waypoint.y - (*waypoint.drop_from)[1];
    return (position.x - waypoint.x) * dx + (position.y - waypoint.y) * dy < 0.0;
}

} // namespace

double EffectiveRouteHeading(const NavRunTickResult& nav_run_result, const RouteMeasurement& route)
{
    return nav_run_result.has_corridor_heading ? nav_run_result.corridor_heading : route.route_heading;
}

void NavigationStateMachine::trackRoute(NavigateTick& tick)
{
    tick.now = std::chrono::steady_clock::now();
    tick.fix = readFix(tick.now);
    const size_t node_idx_before_tracking = session_->current_node_idx;
    tick.route = UpdateRouteTracking(&runtime_state_.route, session_, *position_);
    if (session_->current_node_idx != node_idx_before_tracking) {
        runtime_state_.recovery = {};
    }
    tickNavRun(tick);
    consumeCorridorPassedWaypoints(tick);
}

TickFix NavigationStateMachine::readFix(std::chrono::steady_clock::time_point now)
{
    TickFix fix;
    fix.startup_grace_elapsed =
        runtime_state_.flow.navigate_started_at.time_since_epoch().count() > 0
        && std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.flow.navigate_started_at).count() >= 3000;
    fix.current_heading = NormalizeAngle(position_->angle);
    fix.drops_turn_sends = motion_controller_->steering_profile.drops_turn_sends;
    // 站着或刚按前进时小地图箭头跟不上镜头, 这时按镜头朝向转
    fix.arrow_lags_camera = position_->camera_angle.has_value()
                            && std::abs(NormalizeAngle(*position_->camera_angle - position_->angle)) > kSteerArrowLagCameraDeg;
    fix.steering_on_camera =
        fix.drops_turn_sends && (fix.arrow_lags_camera || (position_->camera_angle.has_value() && !motion_controller_->is_moving_forward));
    fix.steer_heading = fix.steering_on_camera ? NormalizeAngle(*position_->camera_angle) : fix.current_heading;
    fix.degraded_fix = position_provider_->last_capture_was_black_screen || !position_->valid;
    fix.fix_age_ms = position_->timestamp.time_since_epoch().count() > 0
                         ? std::chrono::duration_cast<std::chrono::milliseconds>(now - position_->timestamp).count()
                         : 0;
    return fix;
}

void NavigationStateMachine::tickNavRun(NavigateTick& tick)
{
    if (const std::optional<DynamicAnchor> nav_run_anchor = resolve_nav_run_anchor(param_, session_, *position_, tick.route)) {
        tick.nav_run_anchor_index = nav_run_anchor->first;
        tick.nav_run_result = TickNavRun(
            &nav_run_,
            session_,
            &runtime_state_,
            motion_controller_,
            *position_,
            tick.route,
            param_,
            nav_run_anchor->second,
            nav_run_anchor->first,
            tick.now);
    }
    if (tick.nav_run_result.planning > std::chrono::steady_clock::duration::zero()) {
        PauseWedge(&runtime_state_.offroute, tick.now);
        ExcludeFromStallClocks(session_, tick.nav_run_result.planning);
        tick.now += tick.nav_run_result.planning;
    }
}

void NavigationStateMachine::consumeCorridorPassedWaypoints(NavigateTick& tick)
{
    size_t consumed = 0;
    while (consumed < tick.nav_run_result.passed_run_waypoints && HasCurrentWaypoint(*session_)
           && IsContinuousRun(CurrentWaypoint(*session_))) {
        AdvanceToNextWaypoint(session_, ActionType::RUN, "navmesh_corridor_passed_run_waypoint");
        ++consumed;
    }
    if (consumed == 0) {
        return;
    }
    runtime_state_.recovery = {};
    runtime_state_.global_reacquire_streak = 0;
    tick.route = UpdateRouteTracking(&runtime_state_.route, session_, *position_);
}

TickStep NavigationStateMachine::checkRouteProgress(NavigateTick& tick)
{
    const RouteMeasurement& route = tick.route;
    const NavRunTickResult& nav_run_result = tick.nav_run_result;
    tick.effective_progress = observeNavigationProgress(route, nav_run_result.straight_to_anchor, tick.nav_run_anchor_index, tick.now);
    // 只有偏离走廊的重规划会换走廊; 进度倒退的重规划不清停滞计时
    if (nav_run_result.replanned_with == NavRunReplanReason::OffCorridor) {
        ResetProgress(session_);
    }
    if (runtime_state_.recovery.active) {
        const bool recovery_zone_changed = ZonesDifferInGeometry(param_, runtime_state_.recovery.anchor_pos.zone_id, position_->zone_id);
        const double recovery_displacement =
            std::hypot(position_->x - runtime_state_.recovery.anchor_pos.x, position_->y - runtime_state_.recovery.anchor_pos.y);
        if (recovery_zone_changed || recovery_displacement >= kDynamicRecoveryResetDistance) {
            LogInfo << "Dynamic recovery escaped obstacle." << VAR(recovery_zone_changed) << VAR(recovery_displacement);
            return EndTickWith(resumeAfterEscape("recovery_escape"));
        }
    }
    tick.stalled_ms = StalledMs(*session_, tick.now);
    UpdateDeviceFeeding(
        &device_recovery_,
        tick.stalled_ms,
        runtime_state_.recovery_escalation.device_attempt_count < kRecoveryDeviceAttempts);

    if (!route.valid) {
        if (tick.fix.degraded_fix) {
            HoldPosition(motion_controller_);
        }
        SleepFor(kTargetTickMs);
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

double NavigationStateMachine::observeNavigationProgress(
    const RouteMeasurement& route,
    double straight_to_anchor,
    const std::optional<size_t>& anchor_index,
    const std::chrono::steady_clock::time_point& now)
{
    const bool from_anchor = std::isfinite(straight_to_anchor);
    const double effective_progress = from_anchor ? straight_to_anchor : route.progress_distance;
    if (!route.valid) {
        return effective_progress;
    }
    ProgressIdentityState& identity = runtime_state_.progress_identity;
    if (identity.from_anchor != from_anchor) {
        identity.from_anchor = from_anchor;
        if (identity.source_reset_idx != session_->current_node_idx) {
            identity.source_reset_idx = session_->current_node_idx;
            ResetProgress(session_);
            ResetHardProgress(session_);
        }
    }
    ObserveProgress(session_, session_->current_node_idx, effective_progress, now);
    // 脱困重置不了硬进度时钟, 恢复超时才可能触发
    const size_t hard_progress_key =
        from_anchor && anchor_index ? *anchor_index : ProgressIdentityState::kSerialKeyBias + session_->current_node_idx;
    ObserveHardProgress(session_, hard_progress_key, effective_progress, now);
    return effective_progress;
}

double NavigationStateMachine::resolveArrivalDistance(const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteMeasurement& route = tick.route;
    double arrival_distance = std::max(route.arrival_band, TraitsOf(waypoint.action).commit_distance);
    if (StopsOnPromptDetection(waypoint) && HardStalledMs(*session_, tick.now) > kCollectArrivalRelaxMs) {
        const double relaxed = UntightenedArrivalBand(waypoint, kMeasurementDefaultPositionQuantum);
        if (relaxed > arrival_distance && route.waypoint_distance <= relaxed) {
            LogInfo << "Prompt-point arrival band relaxed after no progress."
                    << VAR_AS(session_->current_node_idx(), session_->current_node_idx) << VAR(route.waypoint_distance)
                    << VAR(arrival_distance) << VAR(relaxed);
        }
        arrival_distance = std::max(arrival_distance, relaxed);
    }
    if (waypoint.action == ActionType::ZIPLINE && !IsZiplineMounted(runtime_state_)
        && HardStalledMs(*session_, tick.now) <= kCollectArrivalRelaxMs) {
        arrival_distance = std::min(arrival_distance, kCollectArrivalBandWu);
        if (runtime_state_.zipline_approach.press_missed) {
            arrival_distance = std::min(arrival_distance, kZiplineRestandBandWu);
        }
    }
    if (IsAsyncInteract(waypoint) && PromptMissedAt(runtime_state_.interact_approach, CurrentAbsoluteNodeIndex(*session_))
        && tick.now - runtime_state_.interact_approach.missed_at <= std::chrono::milliseconds(kCollectArrivalRelaxMs)) {
        arrival_distance = std::min(arrival_distance, kStrictSettleAcceptBandWu);
    }
    return arrival_distance;
}

TickStep NavigationStateMachine::handleWaypointArrival(NavigateTick& tick)
{
    const RouteMeasurement& route = tick.route;
    const double arrival_distance = tick.arrival_distance;
    const bool reached = route.waypoint_distance <= arrival_distance && !short_of_drop(tick.waypoint, *position_);
    if (!reached) {
        return TickStep::Next;
    }
    if (!route.startup_motion_confirmed) {
        LogDebug << "Arrival advance blocked before startup movement confirmed."
                 << VAR_AS(session_->current_node_idx(), session_->current_node_idx) << VAR(route.waypoint_distance)
                 << VAR(arrival_distance) << VAR(route.progress_distance) << VAR(route.cross_track) << VAR(route.projection_anchor);
        return TickStep::Next;
    }
    if (const TickStep step = settleAtArrival(tick); step != TickStep::Next) {
        return step;
    }
    // 退出走路模式时可能同步等控制器, 期间收到取消就别再执行到点动作
    if (should_stop_()) {
        return TickStep::EndTick;
    }
    return passArrivedWaypoints(tick);
}

TickStep NavigationStateMachine::settleAtArrival(const NavigateTick& tick)
{
    const RouteMeasurement& route = tick.route;
    const Waypoint& waypoint = tick.waypoint;
    if (!SettlesAtArrival(waypoint)) {
        return TickStep::Next;
    }
    if (route.waypoint_distance <= route.arrival_band) {
        const semantic_nodes::Context semantic_ctx = semanticContext();
        // 这一拍可能已跨过步行带, 所以先停车
        semantic_nodes::StopMotionAndCommitment(semantic_ctx);
        walkmode::RequestWalk(walk_mode_, true);
        const bool settled = semantic_nodes::SettleAtStrictGoal(semantic_ctx, waypoint, should_stop_);
        // 原地转镜头不经过转向环, 转向环记着的待生效转角要清掉
        runtime_state_.steering_rate = {};
        if (should_stop_()) {
            walkmode::RequestWalk(walk_mode_, false);
            return TickStep::EndTick;
        }
        if (!settled) {
            // 定位失效时人可能没到点, 交给丢定位处理
            if (!position_->valid) {
                walkmode::RequestWalk(walk_mode_, false);
                return EndTickWith(handleLocalizationLoss());
            }
            LogWarn << "Strict arrival correction incomplete; arrival action remains non-fatal."
                    << VAR_AS(session_->current_node_idx(), session_->current_node_idx) << VAR(position_->valid);
        }
    }
    walkmode::RequestWalk(walk_mode_, false);
    return TickStep::Next;
}

// 普通走廊点到了就接着用这帧转向, 需要额外动作的点单独占一拍
TickStep NavigationStateMachine::passArrivedWaypoints(NavigateTick& tick)
{
    RouteMeasurement& route = tick.route;
    Waypoint& waypoint = tick.waypoint;
    double& arrival_distance = tick.arrival_distance;
    do {
        const auto arrival_started_at = std::chrono::steady_clock::now();
        const semantic_nodes::Result arrival_result = semantic_nodes::HandleArrival(semanticContext(), waypoint, route.waypoint_distance);
        if (const int64_t arrival_ms = ElapsedMs(arrival_started_at); arrival_ms >= kFailureTrailSlowArrivalMs) {
            NoteTimedFailure(
                &failure_trail_,
                std::chrono::steady_clock::now(),
                "arrival " + json::value(waypoint.action).as_string(),
                arrival_ms);
        }
        if (arrival_result.rope_unavailable != 0) {
            return EndTickWith(abandonRope(arrival_result.rope_unavailable));
        }
        if (arrival_result.request_failure) {
            return EndTickWith(failNavigation(
                arrival_result.failure_reason,
                arrival_result.failure_log_message,
                route.waypoint_distance,
                0.0,
                tick.stalled_ms));
        }
        if (!tick.fix.drops_turn_sends || !IsContinuousRun(waypoint) || session_->phase != NaviPhase::Navigate
            || !HasCurrentWaypoint(*session_)) {
            return TickStep::EndTick;
        }
        route = UpdateRouteTracking(&runtime_state_.route, session_, *position_);
        waypoint = CurrentWaypoint(*session_);
        if (!route.valid || !IsContinuousRun(waypoint) || waypoint.drop_from) {
            return TickStep::EndTick;
        }
        arrival_distance = std::max(route.arrival_band, TraitsOf(waypoint.action).commit_distance);
    } while (route.waypoint_distance <= arrival_distance && route.startup_motion_confirmed);
    LogDebug << "Passed corridor waypoints; steering on this fix." << VAR_AS(session_->current_node_idx(), session_->current_node_idx)
             << VAR(route.waypoint_distance) << VAR(arrival_distance);
    return TickStep::Next;
}

} // namespace mapnavigator
