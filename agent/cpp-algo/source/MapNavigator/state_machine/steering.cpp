#include "steering.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <optional>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../control/walk_mode.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../process_state.h"
#include "../sensing/latency_observer.h"
#include "../sensing/sensitivity_observer.h"
#include "internal.h"
#include "prompts.h"
#include "steering_loop.h"

namespace mapnavigator
{

namespace
{

bool move_or_hold_for_turn(Navigator* nav, const NavigateTick& tick, const SteeringReading& reading, const SteeringCommand& steering)
{
    // 慢循环里大转角要晚一拍才落地, 原地转免得冲过弯
    const double turn_abs_deg = std::abs(steering.yaw_delta_deg);
    const bool pivot_in_place = tick.fix.drops_turn_sends && steering.issued
                                && (reading.steer_period_ms.has_value() ? reading.slow_loop && turn_abs_deg >= kSlowLoopPivotTurnDeg
                                                                        : turn_abs_deg >= kStartupPivotTurnDeg);
    const bool corner_brake = ShouldBrakeForSharpCorner(nav->runtime_state.flow, tick.nav_run_result, reading.slow_loop);
    if (corner_brake) {
        LatchBrakedCorner(&nav->runtime_state.flow, tick.nav_run_result);
    }
    UpdateCornerWalk(&nav->runtime_state.flow, *nav->position, tick.nav_run_result);
    if (!pivot_in_place && !corner_brake) {
        MoveForward(nav->motion_controller);
    }
    else {
        HoldPosition(nav->motion_controller);
    }
    return corner_brake;
}

} // namespace

double SteerAlongCorridor(Navigator* nav, const NavigateTick& tick)
{
    const RouteMeasurement& route = tick.route;
    const NavRunTickResult& nav_run_result = tick.nav_run_result;
    const Waypoint& waypoint = tick.waypoint;
    const TickFix& fix = tick.fix;
    const double effective_route_heading = EffectiveRouteHeading(nav_run_result, route);
    const SteeringReading reading = ObserveSteering(
        &nav->runtime_state.steering_rate,
        &nav->runtime_state.flow,
        nav->process->walk,
        *nav->motion_controller,
        fix.steer_heading,
        tick.now,
        fix.drops_turn_sends);
    const auto [turn_achieved_deg, turn_residual_deg, turn_elapsed_ms] = reading.last_turn;
    const auto [heading_rate_deg, heading_rate_raw_delta_deg, heading_rate_gap_ms, heading_rate_gap_ticks] = reading.heading_rate;
    const uint64_t tick_seq = nav->runtime_state.flow.tick_seq;

    const double heading_error = NormalizeAngle(effective_route_heading - fix.steer_heading);
    const SteeringCommand steering =
        DecideSteering(&nav->runtime_state.steering_rate, *nav->motion_controller, heading_error, heading_rate_deg);

    const bool corner_brake = move_or_hold_for_turn(nav, tick, reading, steering);

    const auto [issued_delta_deg, steer_send_ms] = SendSteering(
        &nav->runtime_state.steering_rate,
        nav->motion_controller,
        nav->process,
        steering,
        tick.gap_ms,
        fix.steer_heading,
        tick.now,
        fix.drops_turn_sends);
    sensitivity::RecordTick(
        &nav->process->sensitivity,
        nav->maa_context,
        tick_seq,
        nav->position->camera_angle,
        issued_delta_deg,
        fix.degraded_fix);
    UpdateForwardHold(
        &nav->runtime_state.flow,
        nav->motion_controller,
        nav->process->walk,
        *nav->position,
        issued_delta_deg,
        heading_error,
        nav->runtime_state.recovery.active);

    const double lookahead_x = nav_run_result.lookahead_point.x;
    const double lookahead_y = nav_run_result.lookahead_point.y;
    const double projection_x = nav_run_result.projection_point.x;
    const double projection_y = nav_run_result.projection_point.y;
    const int nav_run_replan_reason = static_cast<int>(nav_run_result.replanned_with);
    LogDebug << "TickNavigate corridor target." << VAR(nav->session->current_node_idx) << VAR(waypoint.x) << VAR(waypoint.y)
             << VAR(RequiresStrictArrival(waypoint)) << VAR(lookahead_x) << VAR(lookahead_y) << VAR(projection_x) << VAR(projection_y)
             << VAR(nav_run_result.remaining_to_anchor) << VAR(nav_run_result.straight_to_anchor)
             << VAR(nav_run_result.passed_run_waypoints) << VAR(nav_run_replan_reason) << VAR(route.along_track_remaining)
             << VAR(route.cross_track) << VAR(route.projection_anchor) << VAR_AS(arrival_distance, tick.arrival_distance)
             << VAR(nav->position->zone_id) << VAR_AS(stalled_ms, tick.stalled_ms);

    const int64_t tick_compute_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - tick.started_at).count();
    // 前方没有急弯或还没测出速度时记 -1
    const std::optional<SharpCorner>& corner = nav_run_result.corner;
    const double corner_distance = corner ? corner->distance : -1.0;
    const double corner_turn_deg = corner ? corner->turn_deg : -1.0;
    const double corner_lead_distance = corner ? corner->lead_distance : -1.0;
    const double step_per_tick = nav_run_result.step_per_tick.value_or(-1.0);
    LogDebug << "TickNavigate steering decision." << VAR(tick_seq) << VAR_AS(current_heading, fix.current_heading)
             << VAR_AS(steer_heading, fix.steer_heading) << VAR_AS(steering_on_camera, fix.steering_on_camera)
             << VAR_AS(arrow_lags_camera, fix.arrow_lags_camera) << VAR(route.route_heading) << VAR(effective_route_heading)
             << VAR(nav_run_result.has_corridor_heading) << VAR(nav_run_result.cross_track) << VAR(nav_run_result.upcoming_turn_deg)
             << VAR(corner_distance) << VAR(corner_turn_deg) << VAR(corner_lead_distance) << VAR(step_per_tick) << VAR(corner_brake)
             << VAR(heading_rate_deg) << VAR(heading_rate_raw_delta_deg) << VAR(heading_rate_gap_ms) << VAR(heading_rate_gap_ticks)
             << VAR(heading_error) << VAR(steering.yaw_delta_deg) << VAR(issued_delta_deg) << VAR(turn_achieved_deg)
             << VAR(turn_residual_deg) << VAR(turn_elapsed_ms) << VAR(route.waypoint_distance) << VAR(route.on_route)
             << VAR_AS(degraded_fix, fix.degraded_fix) << VAR_AS(capture_ms, tick.capture_ms) << VAR_AS(fix_age_ms, fix.fix_age_ms)
             << VAR_AS(tick_gap_ms, tick.gap_ms) << VAR(tick_compute_ms);

    latency::RecordStage(
        &nav->process->latency,
        latency::Stage::Other,
        std::max<int64_t>(0, tick_compute_ms - tick.capture_ms - steer_send_ms));
    latency::RecordTick(&nav->process->latency, nav->maa_context, nav->runtime_state.flow.tick_seq, tick.gap_ms);
    return heading_error;
}

void UpdateAutoSprint(Navigator* nav, const NavigateTick& tick)
{
    const RouteMeasurement& route = tick.route;
    const NavRunTickResult& nav_run_result = tick.nav_run_result;
    const bool heading_aligned_for_sprint = std::abs(tick.heading_error) < kAutoSprintMaxHeadingErrorDeg;
    const bool corridor_turn_calm =
        !nav_run_result.has_corridor_heading || nav_run_result.upcoming_turn_deg < kAutoSprintMaxUpcomingTurnDeg;
    const bool turn_calm = heading_aligned_for_sprint && corridor_turn_calm;
    const bool target_requires_strict_arrival = RequiresStrictArrival(tick.waypoint);
    const double sprint_remaining = nav_run_result.has_corridor_heading && std::isfinite(nav_run_result.remaining_to_anchor)
                                        ? nav_run_result.remaining_to_anchor
                                        : route.along_track_remaining;
    const bool has_strict_arrival_braking_room =
        !target_requires_strict_arrival || route.waypoint_distance > tick.arrival_distance + kStrictArrivalSprintBrakeDistance;
    const bool allow_sprint =
        turn_calm && CanSprint(*nav->motion_controller) && tick.fix.startup_grace_elapsed && nav->param->sprint_threshold > 0.0
        && has_strict_arrival_braking_room && sprint_remaining > nav->param->sprint_threshold
        && (nav->runtime_state.flow.last_auto_sprint_time.time_since_epoch().count() == 0
            || std::chrono::duration_cast<std::chrono::milliseconds>(tick.now - nav->runtime_state.flow.last_auto_sprint_time).count()
                   >= kAutoSprintCooldownMs);
    LogDebug << "TickNavigate sprint gate." << VAR(allow_sprint) << VAR(heading_aligned_for_sprint) << VAR(corridor_turn_calm)
             << VAR_AS(startup_grace_elapsed, tick.fix.startup_grace_elapsed) << VAR(has_strict_arrival_braking_room)
             << VAR(sprint_remaining) << VAR(nav->param->sprint_threshold);
    if (allow_sprint && SprintAllowed(*nav->motion_controller)) {
        TriggerSprint(nav->motion_controller, &nav->process->walk);
        nav->runtime_state.flow.last_auto_sprint_time = tick.now;
    }
}

void UpdateWalkMode(Navigator* nav, NaviPhase phase)
{
    if (phase == NaviPhase::WaitFind || phase == NaviPhase::WaitJumpPad) {
        walkmode::RequestWalk(&nav->process->walk, nav->input_backend, true);
        return;
    }

    const bool recovering = nav->runtime_state.recovery.active;
    if (phase == NaviPhase::Navigate && nav->position->valid && !recovering && !nav->runtime_state.river_fall.pending
        && nav->runtime_state.flow.corner_walk) {
        walkmode::RequestWalk(&nav->process->walk, nav->input_backend, true);
        return;
    }

    PromptDistance nearest = NearestPromptDistance(*nav);
    const bool has_waypoint = HasCurrentWaypoint(*nav->session);
    const ActionTraits traits = has_waypoint ? TraitsOf(CurrentWaypoint(*nav->session).action) : ActionTraits {};
    const bool plain_approach = traits.walk_approach;
    bool settling_approach = false;
    if (has_waypoint && SettlesAtArrival(CurrentWaypoint(*nav->session))) {
        const Waypoint& goal = CurrentWaypoint(*nav->session);
        const double dx = goal.x - nav->position->x;
        const double dy = goal.y - nav->position->y;
        const double distance_sq = dx * dx + dy * dy;
        if (nearest.distance_sq < 0.0 || distance_sq < nearest.distance_sq) {
            nearest.distance_sq = distance_sq;
            nearest.is_zipline = goal.action == ActionType::ZIPLINE;
        }
        settling_approach = true;
    }
    const bool startup_blocks_walk = !nav->runtime_state.route.startup_motion_confirmed && !traits.walk_at_startup;
    if (phase != NaviPhase::Navigate || !nav->position->valid || nearest.distance_sq < 0.0 || recovering
        || !(plain_approach || settling_approach) || startup_blocks_walk) {
        walkmode::RequestWalk(&nav->process->walk, nav->input_backend, false);
        return;
    }

    const bool was_engaged = walkmode::Engaged(nav->process->walk);
    const double enter_band = nearest.is_zipline ? kZiplineWalkEnterBandWu : kCollectWalkEnterBandWu;
    const double exit_band = nearest.is_zipline ? kZiplineWalkExitBandWu : kCollectWalkExitBandWu;
    const double band = was_engaged ? exit_band : enter_band;
    const double dig_band = was_engaged ? kDigWalkExitBandWu : kDigWalkEnterBandWu;
    const bool approaching_dig = nearest.dig_distance_sq >= 0.0 && nearest.dig_distance_sq <= dig_band * dig_band;
    walkmode::RequestWalk(&nav->process->walk, nav->input_backend, nearest.distance_sq <= band * band || approaching_dig);
    const bool walking = walkmode::Engaged(nav->process->walk);
    if (walking != was_engaged) {
        const double nearest_stop_point = std::sqrt(nearest.distance_sq);
        LogInfo << "Walk mode boundary crossed." << VAR(walking) << VAR(nearest.is_zipline) << VAR(nearest_stop_point)
                << VAR(nav->position->x) << VAR(nav->position->y);
    }
}

} // namespace mapnavigator
