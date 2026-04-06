#include <cmath>
#include <limits>
#include <optional>

#include <MaaUtils/Logger.h>

#include "Runners/common/runner_phase_utils.h"
#include "action_executor.h"
#include "action_wrapper.h"
#include "controller_type_utils.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navigation_state_machine.h"

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
        else if (projection <= kBootstrapOwnershipProjectionMiddleThreshold
                 && distance_to_from + kBootstrapOwnershipContinueBiasDistance < distance_to_to) {
            continue_index = index;
        }

        if (!best_candidate.has_value() || route_distance < best_candidate->route_distance
            || (route_distance == best_candidate->route_distance && continue_index < best_candidate->continue_index)) {
            best_candidate = BootstrapContinueCandidate {
                .continue_index = continue_index,
                .route_distance = route_distance,
                .reason = "bootstrap_projected_segment",
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

        const double distance = std::hypot(waypoint.x - position.x, waypoint.y - position.y);
        if (!best_candidate.has_value() || distance < best_candidate->distance) {
            best_candidate = BootstrapWaypointCandidate {
                .index = index,
                .distance = distance,
            };
        }
    }
    if (!best_candidate.has_value() || best_candidate->distance > kBootstrapOwnershipMaxDistance) {
        return std::nullopt;
    }
    return best_candidate;
}

std::optional<BootstrapContinueCandidate> ResolveBootstrapContinueCandidate(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    const std::optional<BootstrapWaypointCandidate> nearest_waypoint = FindNearestReachableWaypoint(path, position);
    if (nearest_waypoint.has_value() && nearest_waypoint->distance <= path[nearest_waypoint->index].GetLookahead()) {
        return BootstrapContinueCandidate {
            .continue_index = nearest_waypoint->index,
            .route_distance = nearest_waypoint->distance,
            .reason = "bootstrap_nearby_waypoint",
        };
    }

    if (std::optional<BootstrapContinueCandidate> projected_candidate = FindProjectedContinueCandidate(path, position);
        projected_candidate.has_value()) {
        return projected_candidate;
    }

    if (!nearest_waypoint.has_value()) {
        return std::nullopt;
    }
    return BootstrapContinueCandidate {
        .continue_index = nearest_waypoint->index,
        .route_distance = nearest_waypoint->distance,
        .reason = "bootstrap_nearest_waypoint",
    };
}

bool ApplyBootstrapOwnershipCandidate(
    NavigationSession* session,
    MotionController* motion_controller,
    NaviPosition* position,
    size_t continue_index,
    const char* reason)
{
    if (continue_index >= session->original_path().size()) {
        return false;
    }

    const size_t slice_start = session->FindRejoinSliceStart(continue_index);
    if (slice_start >= session->original_path().size()) {
        return false;
    }

    motion_controller->Stop();
    session->ApplyRejoinSlice(slice_start, *position);
    motion_controller->ClearForwardCommit();
    session->ResetProgress();
    session->ResetDriverProgressTracking();

    LogInfo << "Bootstrap route ownership applied." << VAR(reason) << VAR(continue_index) << VAR(slice_start);
    return true;
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
    , session_(session)
    , motion_controller_(motion_controller)
    , position_(position)
    , should_stop_(std::move(should_stop))
    , zone_transition_runner_(
          position_provider,
          session_,
          motion_controller_,
          position_,
          &runtime_state_,
          should_stop_)
    , transfer_wait_runner_(
          position_provider,
          session_,
          motion_controller_,
          position_,
          &runtime_state_,
          should_stop_)
    , heading_align_runner_(action_wrapper, session_, motion_controller_, position_, should_stop_)
    , advance_route_runner_(
          param_,
          action_wrapper,
          position_provider,
          session_,
          motion_controller_,
          action_executor,
          position_,
          &runtime_state_,
          &zone_transition_runner_,
          should_stop_)
    , serial_route_runner_(
          param_,
          action_wrapper,
          position_provider,
          session_,
          motion_controller_,
          action_executor,
          position_,
          &runtime_state_,
          &zone_transition_runner_,
          should_stop_)
    , use_serial_route_runner_(
          action_wrapper != nullptr && IsAdbLikeControllerType(action_wrapper->controller_type()))
{
    LogInfo << "Navigation route runner selected." << VAR(use_serial_route_runner_);
}

bool NavigationStateMachine::Run()
{
    if (!Bootstrap()) {
        StopMotionAndCommitment(motion_controller_);
        return false;
    }

    while (!should_stop_() && session_->phase() != NaviPhase::Finished && session_->phase() != NaviPhase::Failed) {
        if (!TickPhase(session_->phase())) {
            StopMotionAndCommitment(motion_controller_);
            return false;
        }
    }

    if (should_stop_() && session_->phase() != NaviPhase::Finished && session_->phase() != NaviPhase::Failed) {
        const int phase_value = static_cast<int>(session_->phase());
        LogWarn << "Navigation loop stopped before reaching a terminal phase." << VAR(phase_value)
                << VAR(session_->current_node_idx()) << VAR(session_->path_origin_index());
    }

    if (!should_stop_() && session_->phase() != NaviPhase::Failed) {
        session_->HasSatisfiedFinalSuccess(*position_, "navigation_complete");
    }

    StopMotionAndCommitment(motion_controller_);
    return !should_stop_() && session_->success();
}

bool NavigationStateMachine::Bootstrap()
{
    const std::optional<BootstrapContinueCandidate> continue_candidate =
        ResolveBootstrapContinueCandidate(session_->original_path(), *position_);
    if (continue_candidate.has_value()
        && ApplyBootstrapOwnershipCandidate(
            session_,
            motion_controller_,
            position_,
            continue_candidate->continue_index,
            continue_candidate->reason)) {
        SelectPhaseForCurrentWaypoint(session_, position_, continue_candidate->reason);
        LogInfo << "Mathematical Odometry Engine Start.";
        return true;
    }

    LogWarn << "Bootstrap ownership fallback to route head." << VAR(position_->x) << VAR(position_->y) << VAR(position_->zone_id);
    SelectPhaseForCurrentWaypoint(session_, position_, "bootstrap_ready");
    LogInfo << "Mathematical Odometry Engine Start.";
    return true;
}

bool NavigationStateMachine::TickPhase(NaviPhase phase)
{
    switch (phase) {
    case NaviPhase::Bootstrap:
        SelectPhaseForCurrentWaypoint(session_, position_, "bootstrap_dispatch");
        return true;
    case NaviPhase::AlignHeading:
        return heading_align_runner_.Tick();
    case NaviPhase::AdvanceOnRoute:
        if (use_serial_route_runner_) {
            return serial_route_runner_.Tick();
        }
        return advance_route_runner_.Tick();
    case NaviPhase::WaitZoneTransition:
        return zone_transition_runner_.Tick();
    case NaviPhase::WaitTransfer:
        return transfer_wait_runner_.Tick();
    case NaviPhase::Finished:
    case NaviPhase::Failed:
        return true;
    }
    return false;
}

} // namespace mapnavigator
