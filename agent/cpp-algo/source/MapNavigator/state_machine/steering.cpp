#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <optional>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../sensing/latency_observer.h"
#include "../sensing/sensitivity_observer.h"
#include "internal.h"
#include "steering_loop.h"

namespace mapnavigator
{

double NavigationStateMachine::SteerAlongCorridor(const NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    const NavRunTickResult& nav_run_result = tick.nav_run_result;
    const Waypoint& waypoint = tick.waypoint;
    const TickFix& fix = tick.fix;
    SteeringLoop steering_loop(runtime_state_.steering_rate, runtime_state_.flow, motion_controller_, walk_mode_, position_);
    const double effective_route_heading = EffectiveRouteHeading(nav_run_result, route);
    const SteeringLoop::Reading reading = steering_loop.Observe(fix.steer_heading, tick.now, fix.drops_turn_sends);
    const auto [turn_achieved_deg, turn_residual_deg, turn_elapsed_ms] = reading.last_turn;
    const auto [heading_rate_deg, heading_rate_raw_delta_deg, heading_rate_gap_ms, heading_rate_gap_ticks] = reading.heading_rate;
    const uint64_t tick_seq = runtime_state_.flow.tick_seq;

    const double heading_error = NaviMath::NormalizeAngle(effective_route_heading - fix.steer_heading);
    const SteeringCommand steering = steering_loop.Decide(heading_error, heading_rate_deg);

    // 慢循环里大转角要晚一拍才落地, 原地转免得冲过弯
    const double turn_abs_deg = std::abs(steering.yaw_delta_deg);
    const bool pivot_in_place = fix.drops_turn_sends && steering.issued
                                && (reading.steer_period_ms.has_value() ? reading.slow_loop && turn_abs_deg >= kSlowLoopPivotTurnDeg
                                                                        : turn_abs_deg >= kStartupPivotTurnDeg);
    const bool corner_brake = steering_loop.BrakeForSharpCorner(nav_run_result, reading.slow_loop);
    motion_controller_->SetForwardState(!pivot_in_place && !corner_brake);

    const auto [issued_delta_deg, steer_send_ms] =
        steering_loop.Send(steering, tick.gap_ms, fix.steer_heading, tick.now, fix.drops_turn_sends);
    sensitivity::RecordTick(maa_context_, tick_seq, position_->camera_angle, issued_delta_deg, fix.degraded_fix);
    steering_loop.UpdateForwardHold(issued_delta_deg, heading_error, runtime_state_.recovery.active);

    const double lookahead_x = nav_run_result.lookahead_point.x;
    const double lookahead_y = nav_run_result.lookahead_point.y;
    const double projection_x = nav_run_result.projection_point.x;
    const double projection_y = nav_run_result.projection_point.y;
    const int nav_run_replan_reason = static_cast<int>(nav_run_result.replanned_with);
    LogDebug << "TickNavigate corridor target." << VAR(session_->current_node_idx()) << VAR(waypoint.x) << VAR(waypoint.y)
             << VAR(waypoint.RequiresStrictArrival()) << VAR(lookahead_x) << VAR(lookahead_y) << VAR(projection_x) << VAR(projection_y)
             << VAR(nav_run_result.remaining_to_anchor) << VAR(nav_run_result.straight_to_anchor)
             << VAR(nav_run_result.passed_run_waypoints) << VAR(nav_run_replan_reason) << VAR(route.along_track_remaining)
             << VAR(route.cross_track) << VAR(route.projection_anchor) << VAR_AS(arrival_distance, tick.arrival_distance)
             << VAR(position_->zone_id) << VAR_AS(stalled_ms, tick.stalled_ms);

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

    latency::RecordStage(latency::Stage::Other, std::max<int64_t>(0, tick_compute_ms - tick.capture_ms - steer_send_ms));
    latency::RecordTick(maa_context_, runtime_state_.flow.tick_seq, tick.gap_ms);
    return heading_error;
}

void NavigationStateMachine::UpdateAutoSprint(const NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    const NavRunTickResult& nav_run_result = tick.nav_run_result;
    const bool heading_aligned_for_sprint = std::abs(tick.heading_error) < kAutoSprintMaxHeadingErrorDeg;
    const bool corridor_turn_calm =
        !nav_run_result.has_corridor_heading || nav_run_result.upcoming_turn_deg < kAutoSprintMaxUpcomingTurnDeg;
    const bool turn_calm = heading_aligned_for_sprint && corridor_turn_calm;
    const bool target_requires_strict_arrival = tick.waypoint.RequiresStrictArrival();
    const double sprint_remaining = nav_run_result.has_corridor_heading && std::isfinite(nav_run_result.remaining_to_anchor)
                                        ? nav_run_result.remaining_to_anchor
                                        : route.along_track_remaining;
    const bool has_strict_arrival_braking_room =
        !target_requires_strict_arrival || route.waypoint_distance > tick.arrival_distance + kStrictArrivalSprintBrakeDistance;
    const bool allow_sprint =
        turn_calm && motion_controller_->SupportsSprint() && tick.fix.startup_grace_elapsed && param_.sprint_threshold > 0.0
        && has_strict_arrival_braking_room && sprint_remaining > param_.sprint_threshold
        && (runtime_state_.flow.last_auto_sprint_time.time_since_epoch().count() == 0
            || std::chrono::duration_cast<std::chrono::milliseconds>(tick.now - runtime_state_.flow.last_auto_sprint_time).count()
                   >= kAutoSprintCooldownMs);
    LogDebug << "TickNavigate sprint gate." << VAR(allow_sprint) << VAR(heading_aligned_for_sprint) << VAR(corridor_turn_calm)
             << VAR_AS(startup_grace_elapsed, tick.fix.startup_grace_elapsed) << VAR(has_strict_arrival_braking_room)
             << VAR(sprint_remaining) << VAR(param_.sprint_threshold);
    if (allow_sprint) {
        if (motion_controller_->TriggerSprint()) {
            runtime_state_.flow.last_auto_sprint_time = tick.now;
        }
    }
}

void NavigationStateMachine::UpdateWalkMode(NaviPhase phase)
{
    if (phase == NaviPhase::WaitFind || phase == NaviPhase::WaitJumpPad) {
        walk_mode_.Request(true);
        return;
    }

    const bool recovering = runtime_state_.recovery.active || runtime_state_.cross_tier_escape.active;
    if (phase == NaviPhase::Navigate && position_->valid && !recovering && !runtime_state_.river_fall.pending
        && runtime_state_.flow.corner_walk) {
        walk_mode_.Request(true);
        return;
    }

    PromptDistance nearest = NearestPromptDistance();
    const bool has_waypoint = session_->HasCurrentWaypoint();
    const ActionTraits traits = has_waypoint ? session_->CurrentWaypoint().Traits() : ActionTraits {};
    const bool plain_approach = traits.walk_approach;
    bool settling_approach = false;
    if (has_waypoint && session_->CurrentWaypoint().SettlesAtArrival()) {
        const Waypoint& goal = session_->CurrentWaypoint();
        const double dx = goal.x - position_->x;
        const double dy = goal.y - position_->y;
        const double distance_sq = dx * dx + dy * dy;
        if (nearest.distance_sq < 0.0 || distance_sq < nearest.distance_sq) {
            nearest.distance_sq = distance_sq;
            nearest.is_zipline = goal.action == ActionType::ZIPLINE;
        }
        settling_approach = true;
    }
    const bool startup_blocks_walk = !runtime_state_.route.startup_motion_confirmed && !traits.walk_at_startup;
    if (phase != NaviPhase::Navigate || !position_->valid || nearest.distance_sq < 0.0 || recovering
        || !(plain_approach || settling_approach) || startup_blocks_walk) {
        walk_mode_.Request(false);
        return;
    }

    const bool was_engaged = walk_mode_.engaged();
    const double enter_band = nearest.is_zipline ? kZiplineWalkEnterBandWu : kCollectWalkEnterBandWu;
    const double exit_band = nearest.is_zipline ? kZiplineWalkExitBandWu : kCollectWalkExitBandWu;
    const double band = was_engaged ? exit_band : enter_band;
    const double dig_band = was_engaged ? kDigWalkExitBandWu : kDigWalkEnterBandWu;
    const bool approaching_dig = nearest.dig_distance_sq >= 0.0 && nearest.dig_distance_sq <= dig_band * dig_band;
    walk_mode_.Request(nearest.distance_sq <= band * band || approaching_dig);
    const bool walking = walk_mode_.engaged();
    if (walking != was_engaged) {
        const double nearest_stop_point = std::sqrt(nearest.distance_sq);
        LogInfo << "Walk mode boundary crossed." << VAR(walking) << VAR(nearest.is_zipline) << VAR(nearest_stop_point) << VAR(position_->x)
                << VAR(position_->y);
    }
}

} // namespace mapnavigator
