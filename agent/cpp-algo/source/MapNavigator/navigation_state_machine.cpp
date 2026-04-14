#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <thread>

#include <MaaUtils/Logger.h>

#include "action_executor.h"
#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "navigation_state_machine.h"
#include "pose_estimator.h"
#include "position_provider.h"
#include "recovery_manager.h"
#include "route_tracker.h"
#include "semantic_nodes.h"
#include "steering_controller.h"

namespace mapnavigator
{

namespace
{

struct BootstrapWaypointCandidate
{
    size_t index = std::numeric_limits<size_t>::max();
    double distance = std::numeric_limits<double>::infinity();
};

struct BootstrapContinueCandidate
{
    size_t continue_index = std::numeric_limits<size_t>::max();
    double route_distance = std::numeric_limits<double>::infinity();
    const char* reason = "";
};

bool IsZoneCompatible(const Waypoint& waypoint, const std::string& current_zone_id)
{
    if (!waypoint.HasPosition()) {
        return false;
    }
    if (current_zone_id.empty() || waypoint.zone_id.empty()) {
        return true;
    }
    return waypoint.zone_id == current_zone_id;
}

std::optional<BootstrapContinueCandidate> FindProjectedContinueCandidate(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    std::optional<BootstrapContinueCandidate> best_candidate;
    for (size_t index = 0; index + 1 < path.size(); ++index) {
        const Waypoint& from = path[index];
        const Waypoint& to = path[index + 1];
        if (!IsZoneCompatible(from, position.zone_id) || !IsZoneCompatible(to, position.zone_id)) {
            continue;
        }
        if (!from.zone_id.empty() && !to.zone_id.empty() && from.zone_id != to.zone_id) {
            continue;
        }

        const double segment_x = to.x - from.x;
        const double segment_y = to.y - from.y;
        const double segment_len_sq = segment_x * segment_x + segment_y * segment_y;
        if (segment_len_sq <= std::numeric_limits<double>::epsilon()) {
            continue;
        }

        const double offset_x = position.x - from.x;
        const double offset_y = position.y - from.y;
        const double projection = (offset_x * segment_x + offset_y * segment_y) / segment_len_sq;
        if (projection < 0.0 || projection > 1.0) {
            continue;
        }

        const double projected_x = from.x + projection * segment_x;
        const double projected_y = from.y + projection * segment_y;
        const double route_distance = std::hypot(position.x - projected_x, position.y - projected_y);
        if (route_distance > kBootstrapOwnershipProjectionCorridor) {
            continue;
        }

        size_t continue_index = index + 1;
        const double distance_to_from = std::hypot(position.x - from.x, position.y - from.y);
        const double distance_to_to = std::hypot(position.x - to.x, position.y - to.y);
        if (projection <= kBootstrapOwnershipProjectionFrontThreshold) {
            continue_index = index;
        }
        else if (
            projection <= kBootstrapOwnershipProjectionMiddleThreshold
            && distance_to_from + kBootstrapOwnershipContinueBiasDistance < distance_to_to) {
            continue_index = index;
        }

        if (!best_candidate.has_value() || route_distance < best_candidate->route_distance) {
            best_candidate = BootstrapContinueCandidate {
                .continue_index = continue_index,
                .route_distance = route_distance,
                .reason = "projected_segment",
            };
        }
    }
    return best_candidate;
}

std::optional<BootstrapWaypointCandidate> FindNearestReachableWaypoint(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    std::optional<BootstrapWaypointCandidate> best_candidate;
    for (size_t index = 0; index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        if (!IsZoneCompatible(waypoint, position.zone_id)) {
            continue;
        }

        const double distance = std::hypot(position.x - waypoint.x, position.y - waypoint.y);
        if (distance > kBootstrapOwnershipProjectionCorridor) {
            continue;
        }

        if (!best_candidate.has_value() || distance < best_candidate->distance) {
            best_candidate = BootstrapWaypointCandidate { .index = index, .distance = distance };
        }
    }
    return best_candidate;
}

std::optional<BootstrapContinueCandidate> ResolveBootstrapContinueCandidate(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    const std::optional<BootstrapContinueCandidate> projected = FindProjectedContinueCandidate(path, position);
    const std::optional<BootstrapWaypointCandidate> nearest_waypoint = FindNearestReachableWaypoint(path, position);
    if (!nearest_waypoint.has_value()) {
        return projected;
    }

    BootstrapContinueCandidate fallback {
        .continue_index = nearest_waypoint->index,
        .route_distance = nearest_waypoint->distance,
        .reason = "nearest_waypoint",
    };

    if (!projected.has_value() || fallback.route_distance + 0.75 < projected->route_distance) {
        return fallback;
    }
    return projected;
}

semantic_nodes::Context BuildSemanticContext(
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    MotionController* motion_controller,
    IActionExecutor* action_executor,
    NaviPosition* position,
    NavigationRuntimeState* runtime_state)
{
    semantic_nodes::Context ctx;
    ctx.action_wrapper = action_wrapper;
    ctx.position_provider = position_provider;
    ctx.session = session;
    ctx.motion_controller = motion_controller;
    ctx.action_executor = action_executor;
    ctx.position = position;
    ctx.runtime_state = runtime_state;
    return ctx;
}

} // namespace

NavigationStateMachine::NavigationStateMachine(
    const NaviParam& param,
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    MotionController* motion_controller,
    IActionExecutor* action_executor,
    NaviPosition* position,
    std::function<bool()> should_stop)
    : param_(param)
    , action_wrapper_(action_wrapper)
    , position_provider_(position_provider)
    , session_(session)
    , motion_controller_(motion_controller)
    , action_executor_(action_executor)
    , position_(position)
    , should_stop_(std::move(should_stop))
{
    runtime_state_.pose.estimated_heading = position->angle;
    runtime_state_.pose.initialized = true;
    LogInfo << "Navigation route runner selected. backend=orchestrated";
}

bool NavigationStateMachine::Run()
{
    if (!Bootstrap()) {
        StopMotion();
        return false;
    }

    while (!should_stop_() && session_->phase() != NaviPhase::Finished && session_->phase() != NaviPhase::Failed) {
        if (!TickPhase(session_->phase())) {
            StopMotion();
            return false;
        }
    }

    if (!should_stop_() && session_->phase() != NaviPhase::Failed) {
        session_->HasSatisfiedFinalSuccess(*position_, "navigation_complete");
    }

    StopMotion();
    return !should_stop_() && session_->success();
}

bool NavigationStateMachine::Bootstrap()
{
    const std::optional<BootstrapContinueCandidate> continue_candidate =
        ResolveBootstrapContinueCandidate(session_->original_path(), *position_);
    if (continue_candidate.has_value()) {
        const size_t slice_start = session_->FindRejoinSliceStart(continue_candidate->continue_index);
        if (slice_start < session_->original_path().size()) {
            motion_controller_->SetForwardState(false);
            session_->ApplyRejoinSlice(slice_start, *position_);
            session_->ResetProgress();
            runtime_state_.BeginNavigation(std::chrono::steady_clock::now());
            LogInfo << "Bootstrap route ownership applied." << VAR(continue_candidate->reason) << VAR(continue_candidate->continue_index)
                    << VAR(slice_start);
            SelectPhaseForCurrentWaypoint(continue_candidate->reason);
            return true;
        }
    }

    LogWarn << "Bootstrap ownership fallback to route head." << VAR(position_->x) << VAR(position_->y) << VAR(position_->zone_id);
    runtime_state_.BeginNavigation(std::chrono::steady_clock::now());
    SelectPhaseForCurrentWaypoint("bootstrap_ready");
    return true;
}

bool NavigationStateMachine::TickPhase(NaviPhase phase)
{
    switch (phase) {
    case NaviPhase::Bootstrap:
        SelectPhaseForCurrentWaypoint("bootstrap_dispatch");
        return true;
    case NaviPhase::Navigate:
        return TickNavigate();
    case NaviPhase::WaitTransfer: {
        const semantic_nodes::Result semantic_result = semantic_nodes::TickSemanticFlow(
            BuildSemanticContext(
                action_wrapper_,
                position_provider_,
                session_,
                motion_controller_,
                action_executor_,
                position_,
                &runtime_state_),
            phase);
        if (semantic_result.request_failure) {
            return FailNavigation(semantic_result.failure_reason, semantic_result.failure_log_message, 0.0, 0.0, 0);
        }
        return true;
    }
    case NaviPhase::Finished:
    case NaviPhase::Failed:
        return true;
    }
    return false;
}

bool NavigationStateMachine::CaptureCurrentPosition(bool force_global_search)
{
    return position_provider_->Capture(position_, force_global_search, session_->current_zone_id());
}

bool NavigationStateMachine::TickNavigate()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }

    const semantic_nodes::Context semantic_ctx = BuildSemanticContext(
        action_wrapper_,
        position_provider_,
        session_,
        motion_controller_,
        action_executor_,
        position_,
        &runtime_state_);
    const semantic_nodes::Result active_semantic_result = semantic_nodes::TickSemanticFlow(semantic_ctx, NaviPhase::Navigate);
    if (active_semantic_result.request_failure) {
        return FailNavigation(active_semantic_result.failure_reason, active_semantic_result.failure_log_message, 0.0, 0.0, 0);
    }
    if (active_semantic_result.stay_in_current_tick) {
        return true;
    }

    if (!CaptureCurrentPosition(false)) {
        utils::SleepFor(kLocatorRetryIntervalMs);
        return true;
    }

    const semantic_nodes::Result inline_semantic_result = semantic_nodes::ConsumeInlineSemantics(semantic_ctx);
    if (inline_semantic_result.request_failure) {
        return FailNavigation(inline_semantic_result.failure_reason, inline_semantic_result.failure_log_message, 0.0, 0.0, 0);
    }
    if (inline_semantic_result.stay_in_current_tick) {
        return true;
    }
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }

    if (runtime_state_.semantic.portal_transit_active || session_->phase() != NaviPhase::Navigate) {
        utils::SleepFor(kTargetTickMs);
        return true;
    }

    if (session_->CurrentWaypoint().IsZoneDeclaration()) {
        motion_controller_->SetForwardState(true);
        utils::SleepFor(kTargetTickMs);
        return true;
    }

    const auto now = std::chrono::steady_clock::now();
    const bool startup_grace_elapsed =
        runtime_state_.flow.navigate_started_at.time_since_epoch().count() > 0
        && std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.flow.navigate_started_at).count() >= 3000;
    const PoseEstimate pose = PoseEstimator::Update(
        *position_,
        position_provider_->LastCaptureWasHeld(),
        position_provider_->LastCaptureWasBlackScreen(),
        &runtime_state_.pose);
    const RouteTrackingState route = RouteTracker::Update(session_, &runtime_state_.route, pose.filtered_position, pose.estimated_heading);

    if (route.valid) {
        session_->ObserveProgress(session_->current_node_idx(), route.progress_distance, now);
    }
    const int64_t stalled_ms = session_->StalledMs(now);
    if (stalled_ms < kTargetTickMs * 10 && runtime_state_.recovery.armed) {
        runtime_state_.ResetRecoveryState();
    }

    if (stalled_ms >= kObstacleRecoveryMinTriggerMs && session_->phase() == NaviPhase::Navigate) {
        if (RecoveryManager::Step(motion_controller_, session_, &runtime_state_, pose, route, stalled_ms)) {
            utils::SleepFor(kTargetTickMs);
            return true;
        }
    }

    if (!route.valid) {
        if (pose.degraded_fix) {
            motion_controller_->SetForwardState(false);
        }
        utils::SleepFor(kTargetTickMs);
        return true;
    }

    const Waypoint waypoint = session_->CurrentWaypoint();
    const double arrival_distance =
        waypoint.action == ActionType::PORTAL ? std::max(route.arrival_band, kPortalCommitDistance) : route.arrival_band;
    if (route.waypoint_distance <= arrival_distance) {
        if (!route.startup_motion_confirmed) {
            LogDebug << "Arrival advance blocked before startup movement confirmed." << VAR(session_->current_node_idx())
                     << VAR(route.waypoint_distance) << VAR(arrival_distance) << VAR(route.progress_distance) << VAR(route.cross_track)
                     << VAR(route.projection_anchor);
        }
        else {
            const semantic_nodes::Result arrival_semantic_result =
                semantic_nodes::HandleArrivalSemantic(semantic_ctx, waypoint, route.waypoint_distance);
            if (arrival_semantic_result.request_failure) {
                return FailNavigation(
                    arrival_semantic_result.failure_reason,
                    arrival_semantic_result.failure_log_message,
                    route.waypoint_distance,
                    0.0,
                    stalled_ms);
            }
            if (arrival_semantic_result.consumed) {
                return true;
            }

            const size_t arrived_absolute_node_idx = session_->CurrentAbsoluteNodeIndex();
            if (waypoint.RequiresStrictArrival() && motion_controller_->IsMoving()) {
                motion_controller_->SetForwardState(false);
                utils::SleepFor(kStopWaitMs);
            }
            action_executor_->Execute(waypoint.action);
            session_->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *position_, "waypoint_action_completed");
            session_->AdvanceToNextWaypoint(waypoint.action, "waypoint_action_completed");
            runtime_state_.OnWaypointAdvance();
            if (!session_->HasCurrentWaypoint()) {
                session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
                return true;
            }
            SelectPhaseForCurrentWaypoint("waypoint_action_completed");
            return true;
        }
    }

    const double heading_error = NaviMath::NormalizeAngle(route.route_heading - pose.estimated_heading);
    const SteeringCommand steering = SteeringController::Update(
        heading_error,
        route.signed_cross_track,
        route.projection_anchor,
        motion_controller_->IsMovingForward(),
        &runtime_state_.controller);
    const bool allow_forward = !steering.issued || std::abs(steering.yaw_delta_deg) < 30.0;
    motion_controller_->SetForwardState(allow_forward);
    if (steering.issued) {
        const TurnCommandResult steering_result = motion_controller_->ApplySteering(steering.yaw_delta_deg);
        if (steering_result.issued) {
            PoseEstimator::NotifyAppliedSteering(&runtime_state_.pose, steering_result.issued_delta_degrees);
        }
    }

    const bool allow_sprint =
        allow_forward && motion_controller_->SupportsSprint() && startup_grace_elapsed && param_.sprint_threshold > 0.0
        && route.along_track_remaining > param_.sprint_threshold
        && (runtime_state_.flow.last_auto_sprint_time.time_since_epoch().count() == 0
            || std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.flow.last_auto_sprint_time).count()
                   >= kAutoSprintCooldownMs);
    if (allow_sprint) {
        if (motion_controller_->TriggerSprint()) {
            runtime_state_.flow.last_auto_sprint_time = now;
        }
    }

    if (param_.arrival_timeout > 0 && stalled_ms > param_.arrival_timeout && route.progress_distance > kNoProgressMinDistance) {
        return FailNavigation(
            "no_progress_timeout",
            "No progress timeout reached and navigation was terminated.",
            route.progress_distance,
            NaviMath::NormalizeAngle(route.route_heading - pose.estimated_heading),
            stalled_ms);
    }

    utils::SleepFor(kTargetTickMs);
    return true;
}

void NavigationStateMachine::SelectPhaseForCurrentWaypoint(const char* reason)
{
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return;
    }
    session_->UpdatePhase(NaviPhase::Navigate, reason);
}

void NavigationStateMachine::StopMotion()
{
    motion_controller_->SetForwardState(false);
}

bool NavigationStateMachine::FailNavigation(
    const char* reason,
    const char* log_message,
    double current_distance,
    double yaw_error,
    int64_t stalled_ms)
{
    StopMotion();
    runtime_state_.ResetNavigationAssistState();
    session_->UpdatePhase(NaviPhase::Failed, reason);
    LogError << log_message << VAR(current_distance) << VAR(yaw_error) << VAR(stalled_ms);
    return true;
}

} // namespace mapnavigator
