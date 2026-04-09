#include <algorithm>
#include <chrono>
#include <cmath>
#include <thread>

#include <MaaUtils/Logger.h>

#include "../action_executor.h"
#include "../action_wrapper.h"
#include "../motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navigation_session.h"
#include "../position_provider.h"
#include "advance_route_runner.h"
#include "common/runner_phase_utils.h"
#include "zone_transition_runner.h"

namespace mapnavigator
{

namespace
{

bool ShouldProbePortalBeforeAdvance(ActionType action, double portal_distance)
{
    return action == ActionType::PORTAL || portal_distance <= kZoneTransitionIsolationDistance;
}

bool ShouldConsumeWaypoint(double actual_distance, double lookahead_distance, bool advanced_by_pass_through, bool portal_commit_ready)
{
    return actual_distance < lookahead_distance || advanced_by_pass_through || portal_commit_ready;
}

bool ShouldSuppressAggressiveCorrection(bool last_capture_was_held, int64_t capture_latency_ms)
{
    return last_capture_was_held || capture_latency_ms >= kHighLatencyCaptureMs;
}

} // namespace

AdvanceRouteRunner::AdvanceRouteRunner(
    const NaviParam& param,
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    MotionController* motion_controller,
    IActionExecutor* action_executor,
    NaviPosition* position,
    NavigationRuntimeState* runtime_state,
    ZoneTransitionRunner* zone_transition_runner,
    std::function<bool()> should_stop)
    : param_(param)
    , action_wrapper_(action_wrapper)
    , position_provider_(position_provider)
    , session_(session)
    , motion_controller_(motion_controller)
    , action_executor_(action_executor)
    , position_(position)
    , runtime_state_(runtime_state)
    , zone_transition_runner_(zone_transition_runner)
    , waypoint_arrival_handler_(position_provider_, session_, motion_controller_, action_executor_, position_, runtime_state_)
    , should_stop_(std::move(should_stop))
{
}

bool AdvanceRouteRunner::Tick()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }

    if (session_->CurrentWaypoint().IsControlNode()) {
        SelectPhaseForCurrentWaypoint(session_, position_, "advance_control_node");
        return true;
    }

    const std::string expected_zone_id = session_->CurrentExpectedZone();
    const Waypoint& current_waypoint = session_->CurrentWaypoint();
    const double last_known_portal_distance = session_->DistanceToAdjacentPortal(session_->current_node_idx(), position_->x, position_->y);
    if (ShouldProbePortalBeforeAdvance(current_waypoint.action, last_known_portal_distance)
        && zone_transition_runner_->TryFastPortalZoneTransition("advance_portal_probe")) {
        return true;
    }

    if (runtime_state_->post_zone_transition_reacquire_pending_) {
        if (!position_provider_->Capture(position_, false, expected_zone_id)) {
            SleepFor(kLocatorRetryIntervalMs);
            return true;
        }
        runtime_state_->post_zone_transition_reacquire_pending_ = false;
        session_->SyncVirtualYaw(position_->angle);
        if (expected_zone_id.empty() && position_->zone_id != session_->current_zone_id()) {
            zone_transition_runner_->HandleImplicitZoneTransition(expected_zone_id);
            return true;
        }
    }

    const auto capture_started_at = std::chrono::steady_clock::now();
    if (!position_provider_->Capture(position_, false, expected_zone_id)) {
        if (!expected_zone_id.empty() && zone_transition_runner_->TryFastPortalZoneTransition("capture_failed_zone_probe", false)) {
            return true;
        }
        SleepFor(kLocatorRetryIntervalMs);
        return true;
    }
    const auto capture_finished_at = std::chrono::steady_clock::now();
    const int64_t capture_latency_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(capture_finished_at - capture_started_at).count();

    if (expected_zone_id.empty() && position_->zone_id != session_->current_zone_id()) {
        zone_transition_runner_->HandleImplicitZoneTransition(expected_zone_id);
        return true;
    }

    if (session_->phase() != NaviPhase::AdvanceOnRoute) {
        return true;
    }

    if (!session_->HasCurrentWaypoint() || session_->CurrentWaypoint().IsControlNode()) {
        SelectPhaseForCurrentWaypoint(session_, position_, "advance_transitioned");
        return true;
    }

    const auto loop_now = std::chrono::steady_clock::now();
    const uint64_t frame_seq = ++next_navigation_frame_seq_;
    motion_controller_->UpdateTurnLifecycle(frame_seq);
    const Waypoint& waypoint = session_->CurrentWaypoint();
    const RouteProgressSample route_progress = session_->BuildRouteProgressSample(session_->current_node_idx(), position_->x, position_->y);
    const double actual_distance = std::hypot(waypoint.x - position_->x, waypoint.y - position_->y);
    session_->ObserveProgress(session_->current_node_idx(), route_progress, actual_distance, loop_now);
    const int64_t stalled_ms = session_->StalledMs(loop_now);
    const bool post_turn_forward_commit_active = motion_controller_->IsForwardCommitActive(loop_now);

    const bool advanced_by_pass_through = session_->ShouldAdvanceByPassThrough(session_->current_node_idx(), position_->x, position_->y);
    const double portal_distance = session_->DistanceToAdjacentPortal(session_->current_node_idx(), position_->x, position_->y);
    const bool portal_commit_ready = waypoint.action == ActionType::PORTAL && actual_distance <= kPortalCommitDistance;
    if (ShouldConsumeWaypoint(actual_distance, waypoint.GetLookahead(), advanced_by_pass_through, portal_commit_ready)) {
        return waypoint_arrival_handler_.HandleArrival(
            position_->x,
            position_->y,
            actual_distance,
            portal_distance,
            advanced_by_pass_through,
            portal_commit_ready);
    }

    const bool uses_touch_backend = action_wrapper_ != nullptr && action_wrapper_->uses_touch_backend();
    if (uses_touch_backend && !post_turn_forward_commit_active && !motion_controller_->IsMovingForward()) {
        motion_controller_->EnsureForwardMotion(true);
        SleepFor(kTargetTickMs);
        return true;
    }

    if (ShouldSuppressAggressiveCorrection(position_provider_->LastCaptureWasHeld(), capture_latency_ms)) {
        motion_controller_->EnsureForwardMotion(true);
        SleepFor(kTargetTickMs);
        return true;
    }

    const double target_yaw = NaviMath::CalcTargetRotation(position_->x, position_->y, waypoint.x, waypoint.y);
    const double sensor_yaw_error = NaviMath::NormalizeAngle(target_yaw - position_->angle);
    if (TryTurnInPlace(sensor_yaw_error, actual_distance, stalled_ms, post_turn_forward_commit_active, frame_seq)) {
        return true;
    }

    if (!post_turn_forward_commit_active) {
        TryMovingTrim(
            NaviMath::NormalizeAngle(target_yaw - session_->virtual_yaw()),
            waypoint.RequiresStrictArrival(),
            frame_seq);
    }

    if (!post_turn_forward_commit_active && ShouldFailForDivergence(sensor_yaw_error, actual_distance, stalled_ms)) {
        return FailNavigation(
            "severe_divergence",
            actual_distance,
            sensor_yaw_error,
            stalled_ms,
            "Route diverged for too long and was terminated.");
    }

    if (!post_turn_forward_commit_active && param_.arrival_timeout > 0 && stalled_ms > param_.arrival_timeout
        && actual_distance > kNoProgressMinDistance) {
        return FailNavigation(
            "no_progress_timeout",
            actual_distance,
            sensor_yaw_error,
            stalled_ms,
            "No progress timeout reached and navigation was terminated.");
    }

    if (post_turn_forward_commit_active) {
        motion_controller_->EnsureForwardMotion(true);
    }
    else if (!motion_controller_->IsMovingForward()) {
        motion_controller_->EnsureForwardMotion(false);
    }
    MaybeTriggerAutoSprint(EstimateSprintSegmentDistance(), portal_distance <= kZoneTransitionIsolationDistance);
    SleepFor(kTargetTickMs);
    return true;
}

bool AdvanceRouteRunner::TryTurnInPlace(
    double sensor_yaw_error,
    double actual_distance,
    int64_t stalled_ms,
    bool post_turn_forward_commit_active,
    uint64_t frame_seq)
{
    if (post_turn_forward_commit_active) {
        return false;
    }
    if (std::abs(sensor_yaw_error) <= session_->TurnInPlaceYawThreshold()) {
        return false;
    }
    if (actual_distance <= kLookaheadRadius) {
        return false;
    }
    if (position_provider_->LastCaptureWasHeld()) {
        return false;
    }
    if (motion_controller_->HasActiveTurnLifecycle()) {
        return false;
    }
    if (motion_controller_->IsMoving() && stalled_ms < kTurnInPlaceStallMs) {
        return false;
    }

    const bool uses_touch_backend = action_wrapper_ != nullptr && action_wrapper_->uses_touch_backend();
    LogWarn << "Turn-in-place correction triggered." << VAR(sensor_yaw_error) << VAR(actual_distance) << VAR(stalled_ms);
    if (!uses_touch_backend) {
        StopMotionAndCommitment(motion_controller_);
    }
    session_->SyncVirtualYaw(position_->angle);
    const TurnCommandResult turn_result = motion_controller_->InjectMouseAndTrack(
        sensor_yaw_error,
        false,
        session_->CurrentExpectedZone(),
        kWaitAfterFirstTurnMs,
        TurnActionKind::TurnInPlace,
        frame_seq,
        !uses_touch_backend,
        session_->SteeringTrimYawThreshold(false));
    if (!turn_result.issued) {
        if (uses_touch_backend) {
            motion_controller_->EnsureForwardMotion(true);
            return true;
        }
        return false;
    }
    motion_controller_->ArmForwardCommit(sensor_yaw_error, "turn_in_place");
    if (uses_touch_backend) {
        motion_controller_->EnsureForwardMotion(true);
    }
    session_->ResetStraightStableFrames();
    return true;
}

bool AdvanceRouteRunner::TryMovingTrim(double guidance_yaw_error, bool strict_arrival, uint64_t frame_seq)
{
    const double abs_guidance_yaw_error = std::abs(guidance_yaw_error);
    const double turn_in_place_threshold = session_->TurnInPlaceYawThreshold();
    const double moving_trim_threshold = session_->SteeringTrimYawThreshold(strict_arrival);
    if (abs_guidance_yaw_error <= moving_trim_threshold || abs_guidance_yaw_error > turn_in_place_threshold) {
        return false;
    }
    if (!motion_controller_->IsMovingForward()) {
        return false;
    }

    const TurnCommandResult trim_result = motion_controller_->InjectMouseAndTrack(
        guidance_yaw_error,
        false,
        session_->CurrentExpectedZone(),
        0,
        TurnActionKind::SteeringTrim,
        frame_seq,
        false,
        moving_trim_threshold);
    return trim_result.issued;
}

bool AdvanceRouteRunner::ShouldFailForDivergence(double sensor_yaw_error, double actual_distance, int64_t stalled_ms) const
{
    return std::abs(sensor_yaw_error) >= kSevereDivergenceYawDegrees && actual_distance > kSevereDivergenceDistance
           && stalled_ms >= kSevereDivergenceStallMs;
}

bool AdvanceRouteRunner::FailNavigation(
    const char* reason,
    double actual_distance,
    double sensor_yaw_error,
    int64_t stalled_ms,
    const char* log_message)
{
    StopMotionAndCommitment(motion_controller_);
    session_->ResetStraightStableFrames();
    session_->UpdatePhase(NaviPhase::Failed, reason);
    LogError << log_message << VAR(actual_distance) << VAR(sensor_yaw_error) << VAR(stalled_ms);
    return true;
}

double AdvanceRouteRunner::EstimateSprintSegmentDistance() const
{
    if (!session_->HasCurrentWaypoint()) {
        return 0.0;
    }

    const std::vector<Waypoint>& path = session_->current_path();
    const size_t current_idx = session_->current_node_idx();
    if (current_idx >= path.size()) {
        return 0.0;
    }

    const Waypoint& current_waypoint = path[current_idx];
    if (!current_waypoint.HasPosition()) {
        return 0.0;
    }

    const double current_leg_distance = std::hypot(current_waypoint.x - position_->x, current_waypoint.y - position_->y);
    for (size_t index = current_idx + 1; index < path.size(); ++index) {
        const Waypoint& next_waypoint = path[index];
        if (!next_waypoint.HasPosition()) {
            break;
        }
        if (!current_waypoint.zone_id.empty() && !next_waypoint.zone_id.empty() && next_waypoint.zone_id != current_waypoint.zone_id) {
            break;
        }
        if (next_waypoint.RequiresStrictArrival() || next_waypoint.action != ActionType::RUN) {
            break;
        }

        return current_leg_distance + std::hypot(next_waypoint.x - current_waypoint.x, next_waypoint.y - current_waypoint.y);
    }

    return current_leg_distance;
}

void AdvanceRouteRunner::MaybeTriggerAutoSprint(double sprint_segment_distance, bool near_portal_transition)
{
    if (param_.sprint_threshold <= 0.0 || near_portal_transition || !motion_controller_->IsMovingForward()
        || sprint_segment_distance <= param_.sprint_threshold) {
        return;
    }
    const auto now = std::chrono::steady_clock::now();
    if (runtime_state_->last_auto_sprint_time_.time_since_epoch().count() > 0
        && std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_->last_auto_sprint_time_).count()
               < kAutoSprintCooldownMs) {
        return;
    }
    action_wrapper_->TriggerSprintSync();
    motion_controller_->NotifySprintTriggered();
    runtime_state_->last_auto_sprint_time_ = now;
    LogInfo << "Auto sprint triggered." << VAR(sprint_segment_distance);
}

void AdvanceRouteRunner::SleepFor(int millis) const
{
    if (millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(millis));
}

} // namespace mapnavigator
