#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <thread>

#include <MaaUtils/Logger.h>

#include "../action_executor.h"
#include "../action_wrapper.h"
#include "../motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navigation_session.h"
#include "../position_provider.h"
#include "common/runner_phase_utils.h"
#include "serial_route_runner.h"
#include "zone_transition_runner.h"

namespace mapnavigator
{

namespace
{

constexpr double kSerialRouteCompensationProgressThreshold = 0.45;
constexpr double kSerialRouteStallJumpThresholdRatio = 0.6;
constexpr int32_t kSerialRouteStallJumpMinMs = 600;
constexpr int32_t kSerialRouteStallJumpGraceMs = 700;

bool ShouldProbePortalBeforeAdvance(ActionType action, double portal_distance)
{
    return action == ActionType::PORTAL || portal_distance <= kZoneTransitionIsolationDistance;
}

double ComputeSegmentProgressRatio(double ax, double ay, double bx, double by, double px, double py)
{
    const double segment_x = bx - ax;
    const double segment_y = by - ay;
    const double segment_len_sq = segment_x * segment_x + segment_y * segment_y;
    if (segment_len_sq <= std::numeric_limits<double>::epsilon()) {
        return 1.0;
    }

    const double offset_x = px - ax;
    const double offset_y = py - ay;
    return std::clamp((offset_x * segment_x + offset_y * segment_y) / segment_len_sq, 0.0, 1.0);
}

} // namespace

SerialRouteRunner::SerialRouteRunner(
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
    , position_provider_(position_provider)
    , session_(session)
    , motion_controller_(motion_controller)
    , position_(position)
    , runtime_state_(runtime_state)
    , zone_transition_runner_(zone_transition_runner)
    , waypoint_arrival_handler_(position_provider_, session_, motion_controller_, action_executor, position_, runtime_state_)
{
    (void)action_wrapper;
    (void)should_stop;
}

bool SerialRouteRunner::Tick()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }

    if (session_->CurrentWaypoint().IsControlNode()) {
        SelectPhaseForCurrentWaypoint(session_, position_, "serial_route_control_node");
        return true;
    }

    const std::string expected_zone_id = session_->CurrentExpectedZone();
    const Waypoint& current_waypoint = session_->CurrentWaypoint();
    const double last_known_portal_distance = session_->DistanceToAdjacentPortal(session_->current_node_idx(), position_->x, position_->y);
    if (ShouldProbePortalBeforeAdvance(current_waypoint.action, last_known_portal_distance)
        && zone_transition_runner_->TryFastPortalZoneTransition("serial_route_portal_probe")) {
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

    if (!position_provider_->Capture(position_, false, expected_zone_id)) {
        if (!expected_zone_id.empty() && zone_transition_runner_->TryFastPortalZoneTransition("serial_route_capture_failed", false)) {
            return true;
        }
        SleepFor(kLocatorRetryIntervalMs);
        return true;
    }

    if (expected_zone_id.empty() && position_->zone_id != session_->current_zone_id()) {
        zone_transition_runner_->HandleImplicitZoneTransition(expected_zone_id);
        return true;
    }

    if (session_->phase() != NaviPhase::AdvanceOnRoute) {
        return true;
    }

    if (!session_->HasCurrentWaypoint() || session_->CurrentWaypoint().IsControlNode()) {
        SelectPhaseForCurrentWaypoint(session_, position_, "serial_route_transitioned");
        return true;
    }

    RefreshSegmentState();

    const auto loop_now = std::chrono::steady_clock::now();
    const Waypoint& waypoint = session_->CurrentWaypoint();
    const RouteProgressSample route_progress = session_->BuildRouteProgressSample(session_->current_node_idx(), position_->x, position_->y);
    const double actual_distance = std::hypot(waypoint.x - position_->x, waypoint.y - position_->y);
    session_->ObserveProgress(session_->current_node_idx(), route_progress, actual_distance, loop_now);
    const int64_t stalled_ms = session_->StalledMs(loop_now);
    const uint64_t frame_seq = ++next_navigation_frame_seq_;
    motion_controller_->UpdateTurnLifecycle(frame_seq);
    const bool post_turn_forward_commit_active = motion_controller_->IsForwardCommitActive(loop_now);
    const bool jump_grace_active =
        segment_jump_grace_until_.time_since_epoch().count() > 0 && loop_now < segment_jump_grace_until_;

    const bool portal_commit_ready = waypoint.action == ActionType::PORTAL && actual_distance <= kPortalCommitDistance;
    if (IsWaypointArrived(actual_distance, waypoint) || portal_commit_ready) {
        StopMotionAndCommitment(motion_controller_);
        segment_aligned_ = false;
        segment_compensated_ = false;
        segment_return_attempted_ = false;
        segment_jump_attempted_ = false;
        segment_realign_not_before_ = {};
        segment_jump_grace_until_ = {};
        return waypoint_arrival_handler_.HandleArrival(
            position_->x,
            position_->y,
            actual_distance,
            session_->DistanceToAdjacentPortal(session_->current_node_idx(), position_->x, position_->y),
            false,
            portal_commit_ready);
    }

    const double target_yaw = NaviMath::CalcTargetRotation(position_->x, position_->y, waypoint.x, waypoint.y);
    const double sensor_yaw_error = NaviMath::NormalizeAngle(target_yaw - position_->angle);
    if (!segment_aligned_) {
        return TryAlignTowardsWaypoint(waypoint, expected_zone_id, frame_seq, sensor_yaw_error);
    }

    motion_controller_->EnsureForwardMotion(true);
    const double lateral_distance =
        ComputeLateralDistance(segment_origin_x_, segment_origin_y_, waypoint.x, waypoint.y, position_->x, position_->y);
    if (post_turn_forward_commit_active) {
        SleepFor(kTargetTickMs);
        return true;
    }

    if (TryCompensateDeviation(waypoint, expected_zone_id, frame_seq, sensor_yaw_error, actual_distance, lateral_distance)) {
        return true;
    }

    if (TryRecoverFromSevereDeviation(
            waypoint,
            expected_zone_id,
            frame_seq,
            sensor_yaw_error,
            actual_distance,
            lateral_distance,
            stalled_ms)) {
        return true;
    }

    if (TryRecoverFromStallByJump(waypoint, actual_distance, stalled_ms)) {
        return true;
    }

    if (jump_grace_active) {
        SleepFor(kTargetTickMs);
        return true;
    }

    if (ShouldFailForDeviation(lateral_distance, stalled_ms)) {
        return FailNavigation(
            "serial_route_deviation_timeout",
            actual_distance,
            sensor_yaw_error,
            stalled_ms,
            "Serial route mode remained off-route after recovery attempt.");
    }

    if (param_.arrival_timeout > 0 && stalled_ms > param_.arrival_timeout && actual_distance > kNoProgressMinDistance) {
        return FailNavigation(
            "serial_route_no_progress_timeout",
            actual_distance,
            sensor_yaw_error,
            stalled_ms,
            "Serial route mode reached no-progress timeout.");
    }

    SleepFor(kTargetTickMs);
    return true;
}

bool SerialRouteRunner::IsWaypointArrived(double actual_distance, const Waypoint& waypoint) const
{
    return actual_distance <= waypoint.GetLookahead();
}

bool SerialRouteRunner::TryAlignTowardsWaypoint(
    const Waypoint& waypoint,
    const std::string& expected_zone_id,
    uint64_t frame_seq,
    double sensor_yaw_error)
{
    const auto now = std::chrono::steady_clock::now();
    if (segment_realign_not_before_.time_since_epoch().count() > 0 && now < segment_realign_not_before_) {
        motion_controller_->EnsureForwardMotion(true);
        SleepFor(kTargetTickMs);
        return true;
    }

    StopMotionAndCommitment(motion_controller_);
    session_->SyncVirtualYaw(position_->angle);
    if (std::abs(sensor_yaw_error) <= kSerialRouteHeadingEpsilon) {
        segment_aligned_ = true;
        segment_realign_not_before_ = {};
        motion_controller_->EnsureForwardMotion(true);
        SleepFor(kTargetTickMs);
        return true;
    }

    const TurnCommandResult turn_result = motion_controller_->InjectMouseAndTrack(
        sensor_yaw_error,
        false,
        expected_zone_id,
        kWaitAfterFirstTurnMs,
        TurnActionKind::RouteResume,
        frame_seq,
        false,
        session_->SteeringTrimYawThreshold(waypoint.RequiresStrictArrival()));
    if (!turn_result.issued) {
        segment_realign_not_before_ = now + std::chrono::milliseconds(kSerialRouteRetryDelayMs);
        motion_controller_->EnsureForwardMotion(true);
        SleepFor(kTargetTickMs);
        return true;
    }

    segment_aligned_ = true;
    segment_realign_not_before_ = {};
    motion_controller_->ArmForwardCommit(sensor_yaw_error, "serial_route_align");
    motion_controller_->EnsureForwardMotion(true);
    SleepFor(kTargetTickMs);
    return true;
}

double SerialRouteRunner::ComputePathFollowYawError(const Waypoint& waypoint) const
{
    const double segment_x = waypoint.x - segment_origin_x_;
    const double segment_y = waypoint.y - segment_origin_y_;
    const double segment_length = std::hypot(segment_x, segment_y);
    if (segment_length <= std::numeric_limits<double>::epsilon()) {
        const double target_yaw = NaviMath::CalcTargetRotation(position_->x, position_->y, waypoint.x, waypoint.y);
        return NaviMath::NormalizeAngle(target_yaw - session_->virtual_yaw());
    }

    const double dir_x = segment_x / segment_length;
    const double dir_y = segment_y / segment_length;
    const double offset_x = position_->x - segment_origin_x_;
    const double offset_y = position_->y - segment_origin_y_;
    const double projected_distance = std::clamp(offset_x * dir_x + offset_y * dir_y, 0.0, segment_length);
    const double remaining_distance = std::max(0.0, segment_length - projected_distance);
    if (remaining_distance <= waypoint.GetLookahead()) {
        const double target_yaw = NaviMath::CalcTargetRotation(position_->x, position_->y, waypoint.x, waypoint.y);
        return NaviMath::NormalizeAngle(target_yaw - session_->virtual_yaw());
    }

    double lookahead_distance = std::max(waypoint.GetLookahead(), kSerialRoutePathFollowLookahead);
    lookahead_distance = std::min(lookahead_distance, remaining_distance);
    const double guidance_distance = std::min(segment_length, projected_distance + lookahead_distance);
    const double guidance_x = segment_origin_x_ + dir_x * guidance_distance;
    const double guidance_y = segment_origin_y_ + dir_y * guidance_distance;
    const double guidance_yaw = NaviMath::CalcTargetRotation(position_->x, position_->y, guidance_x, guidance_y);
    return NaviMath::NormalizeAngle(guidance_yaw - session_->virtual_yaw());
}

bool SerialRouteRunner::TryCompensateDeviation(
    const Waypoint& waypoint,
    const std::string& expected_zone_id,
    uint64_t frame_seq,
    double sensor_yaw_error,
    double actual_distance,
    double lateral_distance)
{
    const double segment_progress =
        ComputeSegmentProgressRatio(segment_origin_x_, segment_origin_y_, waypoint.x, waypoint.y, position_->x, position_->y);
    if (segment_compensated_ || position_provider_->LastCaptureWasHeld()
        || segment_progress < kSerialRouteCompensationProgressThreshold || lateral_distance < kSerialRouteDeviationThreshold
        || actual_distance <= waypoint.GetLookahead() + kSerialRouteCompensationMinDistance) {
        return false;
    }

    StopMotionAndCommitment(motion_controller_);
    session_->SyncVirtualYaw(position_->angle);
    const double guidance_yaw_error = ComputePathFollowYawError(waypoint);
    LogWarn << "Serial route deviation compensation triggered."
            << VAR(actual_distance) << VAR(segment_progress) << VAR(lateral_distance) << VAR(sensor_yaw_error)
            << VAR(guidance_yaw_error);
    const TurnCommandResult turn_result = motion_controller_->InjectMouseAndTrack(
        guidance_yaw_error,
        false,
        expected_zone_id,
        kWaitAfterFirstTurnMs,
        TurnActionKind::RouteResume,
        frame_seq,
        false,
        session_->SteeringTrimYawThreshold(waypoint.RequiresStrictArrival()));
    if (!turn_result.issued) {
        segment_realign_not_before_ = std::chrono::steady_clock::now() + std::chrono::milliseconds(kSerialRouteRetryDelayMs);
        motion_controller_->EnsureForwardMotion(true);
        SleepFor(kTargetTickMs);
        return true;
    }

    segment_compensated_ = true;
    segment_aligned_ = true;
    segment_return_attempted_ = false;
    segment_realign_not_before_ = {};
    segment_origin_x_ = position_->x;
    segment_origin_y_ = position_->y;
    const double commit_yaw_error =
        std::copysign(std::max(std::abs(guidance_yaw_error), kPostTurnForwardCommitMinDegrees), guidance_yaw_error);
    motion_controller_->ArmForwardCommit(commit_yaw_error, "serial_route_compensation");
    motion_controller_->EnsureForwardMotion(true);
    SleepFor(kTargetTickMs);
    return true;
}

bool SerialRouteRunner::TryRecoverFromSevereDeviation(
    const Waypoint& waypoint,
    const std::string& expected_zone_id,
    uint64_t frame_seq,
    double sensor_yaw_error,
    double actual_distance,
    double lateral_distance,
    int64_t stalled_ms)
{
    if (segment_return_attempted_ || !segment_compensated_ || lateral_distance < kSerialRouteDeviationFailThreshold
        || stalled_ms < kSevereDivergenceStallMs) {
        return false;
    }

    LogWarn << "Serial route severe deviation recovery triggered."
            << VAR(actual_distance) << VAR(lateral_distance) << VAR(sensor_yaw_error) << VAR(stalled_ms);

    segment_return_attempted_ = true;
    segment_aligned_ = false;
    segment_compensated_ = false;
    segment_realign_not_before_ = {};
    segment_origin_x_ = position_->x;
    segment_origin_y_ = position_->y;
    session_->ResetProgress();
    return TryAlignTowardsWaypoint(waypoint, expected_zone_id, frame_seq, sensor_yaw_error);
}

bool SerialRouteRunner::TryRecoverFromStallByJump(const Waypoint& waypoint, double actual_distance, int64_t stalled_ms)
{
    (void)waypoint;
    if (segment_jump_attempted_ || param_.arrival_timeout <= 0 || position_provider_->LastCaptureWasHeld()
        || actual_distance <= kNoProgressMinDistance) {
        return false;
    }

    const int64_t jump_threshold_ceiling = std::max<int64_t>(1, param_.arrival_timeout - kTargetTickMs);
    const int64_t jump_threshold_floor = std::min<int64_t>(kSerialRouteStallJumpMinMs, jump_threshold_ceiling);
    const int64_t jump_threshold = std::clamp(
        static_cast<int64_t>(param_.arrival_timeout * kSerialRouteStallJumpThresholdRatio),
        jump_threshold_floor,
        jump_threshold_ceiling);
    if (stalled_ms < jump_threshold) {
        return false;
    }

    segment_jump_attempted_ = true;
    segment_jump_grace_until_ =
        std::chrono::steady_clock::now() + std::chrono::milliseconds(kSerialRouteStallJumpGraceMs);
    motion_controller_->SetAction(LocalDriverAction::JumpForward, true);
    LogWarn << "Serial route stall jump recovery triggered." << VAR(actual_distance) << VAR(stalled_ms)
            << VAR(jump_threshold);
    SleepFor(kTargetTickMs);
    return true;
}

bool SerialRouteRunner::ShouldFailForDeviation(double lateral_distance, int64_t stalled_ms) const
{
    return segment_return_attempted_ && segment_compensated_ && lateral_distance >= kSerialRouteDeviationFailThreshold
           && stalled_ms >= kSevereDivergenceStallMs;
}

bool SerialRouteRunner::FailNavigation(
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

void SerialRouteRunner::RefreshSegmentState()
{
    const size_t absolute_idx = session_->CurrentAbsoluteNodeIndex();
    if (absolute_idx == tracking_waypoint_absolute_idx_) {
        return;
    }

    tracking_waypoint_absolute_idx_ = absolute_idx;
    segment_aligned_ = false;
    segment_compensated_ = false;
    segment_return_attempted_ = false;
    segment_jump_attempted_ = false;
    segment_realign_not_before_ = {};
    segment_jump_grace_until_ = {};
    segment_origin_x_ = position_->x;
    segment_origin_y_ = position_->y;

    const std::vector<Waypoint>& path = session_->current_path();
    const size_t waypoint_idx = session_->current_node_idx();
    if (waypoint_idx >= path.size()) {
        return;
    }
    const Waypoint& current_waypoint = path[waypoint_idx];
    for (size_t index = waypoint_idx; index > 0; --index) {
        const Waypoint& previous_waypoint = path[index - 1];
        if (!previous_waypoint.HasPosition()) {
            continue;
        }
        if (!previous_waypoint.zone_id.empty() && !current_waypoint.zone_id.empty()
            && previous_waypoint.zone_id != current_waypoint.zone_id) {
            break;
        }
        segment_origin_x_ = previous_waypoint.x;
        segment_origin_y_ = previous_waypoint.y;
        break;
    }
}

void SerialRouteRunner::SleepFor(int millis) const
{
    if (millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(millis));
}

double SerialRouteRunner::ComputeLateralDistance(double ax, double ay, double bx, double by, double px, double py)
{
    const double segment_x = bx - ax;
    const double segment_y = by - ay;
    const double segment_len_sq = segment_x * segment_x + segment_y * segment_y;
    if (segment_len_sq <= std::numeric_limits<double>::epsilon()) {
        return std::hypot(px - ax, py - ay);
    }

    const double offset_x = px - ax;
    const double offset_y = py - ay;
    const double projection = std::clamp((offset_x * segment_x + offset_y * segment_y) / segment_len_sq, 0.0, 1.0);
    const double projected_x = ax + projection * segment_x;
    const double projected_y = ay + projection * segment_y;
    return std::hypot(px - projected_x, py - projected_y);
}

} // namespace mapnavigator
