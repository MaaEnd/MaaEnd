#include <algorithm>
#include <chrono>
#include <cmath>
#include <optional>

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

std::optional<DynamicAnchor> resolve_nav_run_anchor(NavigationSession* session, const NaviPosition& position, const RouteMeasurement& route)
{
    if (!route.valid || !session->hasCurrentWaypoint()) {
        return std::nullopt;
    }
    const Waypoint& current_waypoint = session->currentWaypoint();
    if (!current_waypoint.hasPosition() || current_waypoint.action != ActionType::RUN) {
        return std::nullopt;
    }
    if (current_waypoint.requiresStrictArrival()) {
        return DynamicAnchor { session->canonicalIndexAtCurrent().value_or(kUnaddressableAnchorIndex), current_waypoint };
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

} // namespace

double EffectiveRouteHeading(const NavRunTickResult& nav_run_result, const RouteMeasurement& route)
{
    return nav_run_result.has_corridor_heading ? nav_run_result.corridor_heading : route.route_heading;
}

void NavigationStateMachine::trackRoute(NavigateTick& tick)
{
    tick.now = std::chrono::steady_clock::now();
    tick.fix = readFix(tick.now);
    const size_t node_idx_before_tracking = session_->currentNodeIdx();
    tick.route = RouteTracker::update(session_, &runtime_state_.route, *position_);
    if (session_->currentNodeIdx() != node_idx_before_tracking) {
        runtime_state_.recovery.reset();
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
    fix.current_heading = NaviMath::normalizeAngle(position_->angle);
    fix.drops_turn_sends = motion_controller_->steeringDropsTurnSends();
    // 站着或刚按前进时小地图箭头跟不上镜头, 这时按镜头朝向转
    fix.arrow_lags_camera = position_->camera_angle.has_value()
                            && std::abs(NaviMath::normalizeAngle(*position_->camera_angle - position_->angle)) > kSteerArrowLagCameraDeg;
    fix.steering_on_camera =
        fix.drops_turn_sends && (fix.arrow_lags_camera || (position_->camera_angle.has_value() && !motion_controller_->isMovingForward()));
    fix.steer_heading = fix.steering_on_camera ? NaviMath::normalizeAngle(*position_->camera_angle) : fix.current_heading;
    fix.degraded_fix = position_provider_->lastCaptureWasBlackScreen() || !position_->valid;
    fix.fix_age_ms = position_->timestamp.time_since_epoch().count() > 0
                         ? std::chrono::duration_cast<std::chrono::milliseconds>(now - position_->timestamp).count()
                         : 0;
    return fix;
}

void NavigationStateMachine::tickNavRun(NavigateTick& tick)
{
    if (const std::optional<DynamicAnchor> nav_run_anchor = resolve_nav_run_anchor(session_, *position_, tick.route)) {
        tick.nav_run_anchor_index = nav_run_anchor->first;
        tick.nav_run_result = nav_run_controller_.tick(
            session_,
            &runtime_state_,
            *position_,
            tick.route,
            param_,
            nav_run_anchor->first,
            nav_run_anchor->second,
            tick.now,
            [this] { motion_controller_->setForwardState(false); });
    }
    if (tick.nav_run_result.planning > std::chrono::steady_clock::duration::zero()) {
        runtime_state_.offroute.pauseAt(tick.now);
        session_->excludeFromStallClocks(tick.nav_run_result.planning);
        tick.now += tick.nav_run_result.planning;
    }
}

void NavigationStateMachine::consumeCorridorPassedWaypoints(NavigateTick& tick)
{
    size_t consumed = 0;
    while (consumed < tick.nav_run_result.passed_run_waypoints && session_->hasCurrentWaypoint()
           && session_->currentWaypoint().isContinuousRun()) {
        session_->advanceToNextWaypoint(ActionType::RUN, "navmesh_corridor_passed_run_waypoint");
        ++consumed;
    }
    if (consumed == 0) {
        return;
    }
    runtime_state_.recovery.reset();
    runtime_state_.global_reacquire_streak = 0;
    tick.route = RouteTracker::update(session_, &runtime_state_.route, *position_);
}

TickStep NavigationStateMachine::checkRouteProgress(NavigateTick& tick)
{
    const RouteMeasurement& route = tick.route;
    const NavRunTickResult& nav_run_result = tick.nav_run_result;
    tick.effective_progress = observeNavigationProgress(route, nav_run_result.straight_to_anchor, tick.nav_run_anchor_index, tick.now);
    // 用硬进度时钟判, 脱困自己重铺路线不会重置它
    if (runtime_state_.cross_tier_escape.active && session_->hardStalledMs(tick.now) >= kCrossTierEscapeHardStallMs) {
        const double goal_dist =
            std::hypot(position_->x - runtime_state_.cross_tier_escape.goal_x, position_->y - runtime_state_.cross_tier_escape.goal_y);
        runtime_state_.cross_tier_escape.reset();
        return EndTickWith(failNavigation(
            "crosstier_escape_stalled",
            "Cross-tier escape made no corridor progress (walled or unfollowable); terminating so the pipeline can retry.",
            goal_dist,
            0.0,
            session_->hardStalledMs(tick.now)));
    }
    // 只有偏离走廊的重规划会换走廊; 进度倒退的重规划不清停滞计时
    if (nav_run_result.replanned_with == NavRunReplanReason::OffCorridor) {
        session_->resetProgress();
    }
    if (runtime_state_.recovery.active) {
        const bool recovery_zone_changed = ZonesDiffer(runtime_state_.recovery.anchor_pos.zone_id, position_->zone_id);
        const double recovery_displacement =
            std::hypot(position_->x - runtime_state_.recovery.anchor_pos.x, position_->y - runtime_state_.recovery.anchor_pos.y);
        if (recovery_zone_changed || recovery_displacement >= kDynamicRecoveryResetDistance) {
            LogInfo << "Dynamic recovery escaped obstacle." << VAR(recovery_zone_changed) << VAR(recovery_displacement);
            return EndTickWith(resumeAfterEscape("recovery_escape"));
        }
    }
    tick.stalled_ms = session_->stalledMs(tick.now);
    device_recovery_.updateFeeding(tick.stalled_ms, runtime_state_.recovery_escalation.device_attempt_count < kRecoveryDeviceAttempts);

    if (!route.valid) {
        if (tick.fix.degraded_fix) {
            motion_controller_->setForwardState(false);
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
        if (identity.source_reset_idx != session_->currentNodeIdx()) {
            identity.source_reset_idx = session_->currentNodeIdx();
            session_->resetProgress();
            session_->resetHardProgress();
        }
    }
    session_->observeProgress(session_->currentNodeIdx(), effective_progress, now);
    // 脱困重置不了硬进度时钟, 恢复超时才可能触发
    const size_t hard_progress_key =
        from_anchor && anchor_index ? *anchor_index : ProgressIdentityState::kSerialKeyBias + session_->currentNodeIdx();
    session_->observeHardProgress(hard_progress_key, effective_progress, now);
    return effective_progress;
}

double NavigationStateMachine::resolveArrivalDistance(const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteMeasurement& route = tick.route;
    double arrival_distance = std::max(route.arrival_band, waypoint.traits().commit_distance);
    if (waypoint.stopsOnPromptDetection() && session_->hardStalledMs(tick.now) > kCollectArrivalRelaxMs) {
        const double relaxed = waypoint.arrivalBand(kMeasurementDefaultPositionQuantum, /*relax_tight_band=*/true);
        if (relaxed > arrival_distance && route.waypoint_distance <= relaxed) {
            LogInfo << "Prompt-point arrival band relaxed after no progress."
                    << VAR_AS(session_->current_node_idx(), session_->currentNodeIdx()) << VAR(route.waypoint_distance)
                    << VAR(arrival_distance) << VAR(relaxed);
        }
        arrival_distance = std::max(arrival_distance, relaxed);
    }
    if (waypoint.action == ActionType::ZIPLINE && !runtime_state_.isZiplineMounted()
        && session_->hardStalledMs(tick.now) <= kCollectArrivalRelaxMs) {
        arrival_distance = std::min(arrival_distance, kCollectArrivalBandWu);
        if (runtime_state_.zipline_approach.press_missed) {
            arrival_distance = std::min(arrival_distance, kZiplineRestandBandWu);
        }
    }
    if (waypoint.isAsyncInteract() && runtime_state_.interact_approach.promptMissedAt(session_->currentAbsoluteNodeIndex())
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
                 << VAR_AS(session_->current_node_idx(), session_->currentNodeIdx()) << VAR(route.waypoint_distance)
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
    if (!waypoint.settlesAtArrival()) {
        return TickStep::Next;
    }
    if (route.waypoint_distance <= route.arrival_band) {
        const semantic_nodes::Context semantic_ctx = semanticContext();
        // 这一拍可能已跨过步行带, 所以先停车
        semantic_nodes::StopMotionAndCommitment(semantic_ctx);
        walk_mode_.request(true);
        const bool settled = semantic_nodes::SettleAtStrictGoal(semantic_ctx, waypoint, should_stop_);
        // 原地转镜头不经过转向环, 转向环记着的待生效转角要清掉
        runtime_state_.steering_rate.reset();
        if (should_stop_()) {
            walk_mode_.request(false);
            return TickStep::EndTick;
        }
        if (!settled) {
            // 定位失效时人可能没到点, 交给丢定位处理
            if (!position_->valid) {
                walk_mode_.request(false);
                return EndTickWith(handleLocalizationLoss());
            }
            LogWarn << "Strict arrival correction incomplete; arrival action remains non-fatal."
                    << VAR_AS(session_->current_node_idx(), session_->currentNodeIdx()) << VAR(position_->valid);
        }
    }
    walk_mode_.request(false);
    return TickStep::Next;
}

// 普通走廊点到了就接着用这帧转向, 需要额外动作的点单独占一拍
TickStep NavigationStateMachine::passArrivedWaypoints(NavigateTick& tick)
{
    RouteMeasurement& route = tick.route;
    Waypoint& waypoint = tick.waypoint;
    double& arrival_distance = tick.arrival_distance;
    do {
        const semantic_nodes::Result arrival_result = semantic_nodes::HandleArrival(semanticContext(), waypoint, route.waypoint_distance);
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
        if (!tick.fix.drops_turn_sends || !waypoint.isContinuousRun() || session_->phase() != NaviPhase::Navigate
            || !session_->hasCurrentWaypoint()) {
            return TickStep::EndTick;
        }
        route = RouteTracker::update(session_, &runtime_state_.route, *position_);
        waypoint = session_->currentWaypoint();
        if (!route.valid || !waypoint.isContinuousRun() || waypoint.drop_from) {
            return TickStep::EndTick;
        }
        arrival_distance = std::max(route.arrival_band, waypoint.traits().commit_distance);
    } while (route.waypoint_distance <= arrival_distance && route.startup_motion_confirmed);
    LogDebug << "Passed corridor waypoints; steering on this fix." << VAR_AS(session_->current_node_idx(), session_->currentNodeIdx())
             << VAR(route.waypoint_distance) << VAR(arrival_distance);
    return TickStep::Next;
}

} // namespace mapnavigator
