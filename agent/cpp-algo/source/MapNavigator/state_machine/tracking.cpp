#include "tracking.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <optional>

#include <meojson/json.hpp>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../control/walk_mode.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../planning/route_tracker.h"
#include "../process_state.h"
#include "../semantic/semantic_helpers.h"
#include "../semantic/semantic_nodes.h"
#include "../semantic/strict_settle.h"
#include "../sensing/position_provider.h"
#include "../sensing/task_stop.h"
#include "internal.h"
#include "localization.h"
#include "replan.h"

namespace mapnavigator
{

namespace
{

std::optional<DynamicAnchor> resolve_nav_run_anchor(const Session& session, const NaviPosition& position, const RouteMeasurement& route)
{
    if (!route.valid || !HasCurrentWaypoint(session)) {
        return std::nullopt;
    }
    const Waypoint& current_waypoint = CurrentWaypoint(session);
    if (!current_waypoint.has_position || current_waypoint.action != ActionType::RUN) {
        return std::nullopt;
    }
    if (RequiresStrictArrival(current_waypoint)) {
        return DynamicAnchor { CanonicalIndexAtCurrent(session).value_or(kUnaddressableAnchorIndex), current_waypoint };
    }
    return ResolveCurrentAnchor(session, position);
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

TickFix read_fix(const Navigator& nav, TimePoint now)
{
    TickFix fix;
    fix.startup_grace_elapsed =
        nav.runtime_state.flow.navigate_started_at.time_since_epoch().count() > 0
        && std::chrono::duration_cast<std::chrono::milliseconds>(now - nav.runtime_state.flow.navigate_started_at).count() >= 3000;
    fix.current_heading = NormalizeAngle(nav.position->angle);
    fix.drops_turn_sends = nav.motion_controller->steering_profile.drops_turn_sends;
    // 站着或刚按前进时小地图箭头跟不上镜头, 这时按镜头朝向转
    fix.arrow_lags_camera = nav.position->camera_angle.has_value()
                            && std::abs(NormalizeAngle(*nav.position->camera_angle - nav.position->angle)) > kSteerArrowLagCameraDeg;
    fix.steering_on_camera =
        fix.drops_turn_sends
        && (fix.arrow_lags_camera || (nav.position->camera_angle.has_value() && !nav.motion_controller->is_moving_forward));
    fix.steer_heading = fix.steering_on_camera ? NormalizeAngle(*nav.position->camera_angle) : fix.current_heading;
    fix.degraded_fix = nav.position_provider->last_capture_was_black_screen || !nav.position->valid;
    fix.fix_age_ms = nav.position->timestamp.time_since_epoch().count() > 0
                         ? std::chrono::duration_cast<std::chrono::milliseconds>(now - nav.position->timestamp).count()
                         : 0;
    return fix;
}

void tick_nav_run(Navigator* nav, NavigateTick* tick)
{
    if (const std::optional<DynamicAnchor> nav_run_anchor = resolve_nav_run_anchor(*nav->session, *nav->position, tick->route)) {
        tick->nav_run_anchor_index = nav_run_anchor->first;
        tick->nav_run_result = TickNavRun(
            &nav->nav_run,
            nav->session,
            &nav->runtime_state,
            nav->motion_controller,
            *nav->position,
            tick->route,
            *nav->param,
            nav_run_anchor->second,
            nav_run_anchor->first,
            tick->now);
    }
    if (tick->nav_run_result.planning > std::chrono::steady_clock::duration::zero()) {
        PauseWedge(&nav->runtime_state.offroute, tick->now);
        ExcludeFromStallClocks(nav->session, tick->nav_run_result.planning);
        tick->now += tick->nav_run_result.planning;
    }
}

RouteMeasurement advance_and_measure_route(Navigator* nav)
{
    RouteTrackerState* state = &nav->runtime_state.route;
    const bool startup_motion_confirmed = UpdateStartupGate(state, *nav->session, *nav->position);
    AdvancePassedRunWaypoints(nav->session, state, *nav->position);
    return MeasureRoute(*state, *nav->session, *nav->position, startup_motion_confirmed);
}

void consume_corridor_passed_waypoints(Navigator* nav, NavigateTick* tick)
{
    size_t consumed = 0;
    while (consumed < tick->nav_run_result.passed_run_waypoints && HasCurrentWaypoint(*nav->session)
           && IsContinuousRun(CurrentWaypoint(*nav->session))) {
        LeaveCurrentWaypoint(nav->session, ActionType::RUN, "navmesh_corridor_passed_run_waypoint");
        ++consumed;
    }
    if (consumed == 0) {
        return;
    }
    ClearDynamicRecovery(&nav->runtime_state.recovery);
    ResetGlobalReacquireStreak(&nav->runtime_state);
    tick->route = advance_and_measure_route(nav);
}

double observe_navigation_progress(
    Navigator* nav,
    const RouteMeasurement& route,
    double straight_to_anchor,
    const std::optional<size_t>& anchor_index,
    TimePoint now)
{
    const bool from_anchor = std::isfinite(straight_to_anchor);
    const double effective_progress = from_anchor ? straight_to_anchor : route.progress_distance;
    if (!route.valid) {
        return effective_progress;
    }
    ProgressIdentityState& identity = nav->runtime_state.progress_identity;
    if (identity.from_anchor != from_anchor) {
        identity.from_anchor = from_anchor;
        if (identity.source_reset_idx != nav->session->current_node_idx) {
            identity.source_reset_idx = nav->session->current_node_idx;
            ResetProgress(nav->session);
            ResetHardProgress(nav->session);
        }
    }
    ProgressClock* progress = &nav->session->visit.progress;
    if (ProgressClockTracks(*progress, nav->session->current_node_idx)) {
        ObserveProgressClock(progress, effective_progress, now);
    }
    else {
        RestartProgressClock(progress, nav->session->current_node_idx, effective_progress, now);
    }
    // 脱困重置不了硬进度时钟, 恢复超时才可能触发
    const size_t hard_progress_key =
        from_anchor && anchor_index ? *anchor_index : ProgressIdentityState::kSerialKeyBias + nav->session->current_node_idx;
    ProgressClock* hard_progress = &nav->session->visit.hard_progress;
    if (ProgressClockTracks(*hard_progress, hard_progress_key)) {
        ObserveProgressClock(hard_progress, effective_progress, now);
    }
    else {
        RestartProgressClock(hard_progress, hard_progress_key, effective_progress, now);
    }
    return effective_progress;
}

TickStep settle_at_arrival(Navigator* nav, const NavigateTick& tick)
{
    const RouteMeasurement& route = tick.route;
    const Waypoint& waypoint = tick.waypoint;
    if (!SettlesAtArrival(waypoint)) {
        return TickStep::Next;
    }
    if (route.waypoint_distance <= route.arrival_band) {
        // 这一拍可能已跨过步行带, 所以先停车
        semantic_nodes::HoldAndArmCameraAlign(nav);
        walkmode::RequestWalk(&nav->process->walk, nav->input_backend, true);
        const bool settled = semantic_nodes::SettleAtStrictGoal(nav, waypoint);
        // 原地转镜头不经过转向环, 转向环记着的待生效转角要清掉
        nav->runtime_state.steering_rate = {};
        if (TaskStopping(nav->maa_context)) {
            walkmode::RequestWalk(&nav->process->walk, nav->input_backend, false);
            return TickStep::EndTick;
        }
        if (!settled) {
            // 定位失效时人可能没到点, 交给丢定位处理
            if (!nav->position->valid) {
                walkmode::RequestWalk(&nav->process->walk, nav->input_backend, false);
                return EndTickWith(HandleLocalizationLoss(nav));
            }
            LogWarn << "Strict arrival correction incomplete; arrival action remains non-fatal." << VAR(nav->session->current_node_idx)
                    << VAR(nav->position->valid);
        }
    }
    walkmode::RequestWalk(&nav->process->walk, nav->input_backend, false);
    return TickStep::Next;
}

// 普通走廊点到了就接着用这帧转向, 需要额外动作的点单独占一拍
TickStep pass_arrived_waypoints(Navigator* nav, NavigateTick* tick)
{
    RouteMeasurement& route = tick->route;
    Waypoint& waypoint = tick->waypoint;
    double& arrival_distance = tick->arrival_distance;
    do {
        const auto arrival_started_at = std::chrono::steady_clock::now();
        const semantic_nodes::Result arrival_result = semantic_nodes::HandleArrival(nav, waypoint, route.waypoint_distance);
        if (const int64_t arrival_ms = ElapsedMs(arrival_started_at); arrival_ms >= kFailureTrailSlowArrivalMs) {
            NoteTimedFailure(
                &nav->failure_trail,
                std::chrono::steady_clock::now(),
                "arrival " + json::value(waypoint.action).as_string(),
                arrival_ms);
        }
        if (arrival_result.rope_unavailable != 0) {
            return EndTickWith(AbandonRope(nav, arrival_result.rope_unavailable));
        }
        if (arrival_result.request_failure) {
            return EndTickWith(FailNavigation(
                nav,
                arrival_result.failure_reason,
                arrival_result.failure_log_message,
                route.waypoint_distance,
                0.0,
                tick->stalled_ms));
        }
        if (!tick->fix.drops_turn_sends || !IsContinuousRun(waypoint) || nav->session->phase != NaviPhase::Navigate
            || !HasCurrentWaypoint(*nav->session)) {
            return TickStep::EndTick;
        }
        route = advance_and_measure_route(nav);
        waypoint = CurrentWaypoint(*nav->session);
        if (!route.valid || !IsContinuousRun(waypoint) || waypoint.drop_from) {
            return TickStep::EndTick;
        }
        arrival_distance = std::max(route.arrival_band, TraitsOf(waypoint.action).commit_distance);
    } while (route.waypoint_distance <= arrival_distance && route.startup_motion_confirmed);
    LogDebug << "Passed corridor waypoints; steering on this fix." << VAR(nav->session->current_node_idx) << VAR(route.waypoint_distance)
             << VAR(arrival_distance);
    return TickStep::Next;
}

std::optional<TickStep> end_on_recovery_escape(Navigator* nav)
{
    if (nav->runtime_state.recovery.active) {
        const bool recovery_zone_changed = ZonesDiffer(nav->runtime_state.recovery.anchor_pos.zone_id, nav->position->zone_id);
        const double recovery_displacement = std::hypot(
            nav->position->x - nav->runtime_state.recovery.anchor_pos.x,
            nav->position->y - nav->runtime_state.recovery.anchor_pos.y);
        if (recovery_zone_changed || recovery_displacement >= kDynamicRecoveryResetDistance) {
            LogInfo << "Dynamic recovery escaped obstacle." << VAR(recovery_zone_changed) << VAR(recovery_displacement);
            return EndTickWith(ResumeAfterEscape(nav, "recovery_escape"));
        }
    }
    return std::nullopt;
}

} // namespace

double EffectiveRouteHeading(const NavRunTickResult& nav_run_result, const RouteMeasurement& route)
{
    return nav_run_result.has_corridor_heading ? nav_run_result.corridor_heading : route.route_heading;
}

void TrackRoute(Navigator* nav, NavigateTick* tick)
{
    tick->now = std::chrono::steady_clock::now();
    tick->fix = read_fix(*nav, tick->now);
    const size_t node_idx_before_tracking = nav->session->current_node_idx;
    tick->route = advance_and_measure_route(nav);
    if (nav->session->current_node_idx != node_idx_before_tracking) {
        ClearDynamicRecovery(&nav->runtime_state.recovery);
    }
    tick_nav_run(nav, tick);
    consume_corridor_passed_waypoints(nav, tick);
}

TickStep RunRouteProgress(Navigator* nav, NavigateTick* tick)
{
    const RouteMeasurement& route = tick->route;
    const NavRunTickResult& nav_run_result = tick->nav_run_result;
    tick->effective_progress =
        observe_navigation_progress(nav, route, nav_run_result.straight_to_anchor, tick->nav_run_anchor_index, tick->now);
    // 只有偏离走廊的重规划会换走廊; 进度倒退的重规划不清停滞计时
    if (nav_run_result.replanned_with == NavRunReplanReason::OffCorridor) {
        ResetProgress(nav->session);
    }
    if (const std::optional<TickStep> step = end_on_recovery_escape(nav)) {
        return *step;
    }
    tick->stalled_ms = StalledMs(*nav->session, tick->now);
    UpdateDeviceFeeding(
        &nav->device_recovery,
        tick->stalled_ms,
        nav->runtime_state.recovery_escalation.device_attempt_count < kRecoveryDeviceAttempts);

    if (!route.valid) {
        if (tick->fix.degraded_fix) {
            HoldPosition(nav->motion_controller);
        }
        SleepFor(kTargetTickMs);
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

double ResolveArrivalDistance(const Navigator& nav, const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteMeasurement& route = tick.route;
    double arrival_distance = std::max(route.arrival_band, TraitsOf(waypoint.action).commit_distance);
    if (StopsOnPromptDetection(waypoint) && HardStalledMs(*nav.session, tick.now) > kCollectArrivalRelaxMs) {
        const double relaxed = UntightenedArrivalBand(waypoint, kMeasurementDefaultPositionQuantum);
        if (relaxed > arrival_distance && route.waypoint_distance <= relaxed) {
            LogInfo << "Prompt-point arrival band relaxed after no progress." << VAR(nav.session->current_node_idx)
                    << VAR(route.waypoint_distance) << VAR(arrival_distance) << VAR(relaxed);
        }
        arrival_distance = std::max(arrival_distance, relaxed);
    }
    if (waypoint.action == ActionType::ZIPLINE && !IsZiplineMounted(nav.runtime_state)
        && HardStalledMs(*nav.session, tick.now) <= kCollectArrivalRelaxMs) {
        arrival_distance = std::min(arrival_distance, kCollectArrivalBandWu);
        if (nav.runtime_state.zipline_approach.press_missed) {
            arrival_distance = std::min(arrival_distance, kZiplineRestandBandWu);
        }
    }
    if (IsAsyncInteract(waypoint) && PromptMissedAt(nav.runtime_state.interact_approach, CurrentAbsoluteNodeIndex(*nav.session))
        && tick.now - nav.runtime_state.interact_approach.missed_at <= std::chrono::milliseconds(kCollectArrivalRelaxMs)) {
        arrival_distance = std::min(arrival_distance, kStrictSettleAcceptBandWu);
    }
    return arrival_distance;
}

TickStep HandleWaypointArrival(Navigator* nav, NavigateTick* tick)
{
    const RouteMeasurement& route = tick->route;
    const double arrival_distance = tick->arrival_distance;
    const bool reached = route.waypoint_distance <= arrival_distance && !short_of_drop(tick->waypoint, *nav->position);
    if (!reached) {
        return TickStep::Next;
    }
    if (!route.startup_motion_confirmed) {
        LogDebug << "Arrival advance blocked before startup movement confirmed." << VAR(nav->session->current_node_idx)
                 << VAR(route.waypoint_distance) << VAR(arrival_distance) << VAR(route.progress_distance) << VAR(route.cross_track)
                 << VAR(route.projection_anchor);
        return TickStep::Next;
    }
    if (const TickStep step = settle_at_arrival(nav, *tick); step != TickStep::Next) {
        return step;
    }
    // 退出走路模式时可能同步等控制器, 期间收到取消就别再执行到点动作
    if (TaskStopping(nav->maa_context)) {
        return TickStep::EndTick;
    }
    return pass_arrived_waypoints(nav, tick);
}

} // namespace mapnavigator
