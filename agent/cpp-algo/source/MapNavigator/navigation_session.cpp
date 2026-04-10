#include <algorithm>
#include <cassert>
#include <cmath>

#include <MaaUtils/Logger.h>

#include "navi_config.h"
#include "navigation_session.h"

namespace mapnavigator
{

namespace
{

size_t ResolveCanonicalFinalGoalIndex(const std::vector<Waypoint>& path)
{
    for (size_t index = path.size(); index > 0; --index) {
        if (path[index - 1].HasPosition()) {
            return index - 1;
        }
    }
    return std::numeric_limits<size_t>::max();
}

std::vector<double> BuildPathProgressPrefix(const std::vector<Waypoint>& path)
{
    std::vector<double> prefix(path.size(), 0.0);
    double accumulated = 0.0;
    size_t previous_position_idx = std::numeric_limits<size_t>::max();
    for (size_t index = 0; index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        if (!waypoint.HasPosition()) {
            prefix[index] = accumulated;
            continue;
        }
        if (previous_position_idx != std::numeric_limits<size_t>::max()) {
            const Waypoint& previous_waypoint = path[previous_position_idx];
            if ((previous_waypoint.zone_id.empty() || waypoint.zone_id.empty() || previous_waypoint.zone_id == waypoint.zone_id)
                && previous_waypoint.HasPosition()) {
                accumulated += std::hypot(waypoint.x - previous_waypoint.x, waypoint.y - previous_waypoint.y);
            }
        }
        previous_position_idx = index;
        prefix[index] = accumulated;
    }
    return prefix;
}

const char* NaviPhaseName(NaviPhase phase)
{
    switch (phase) {
    case NaviPhase::Bootstrap:
        return "Bootstrap";
    case NaviPhase::AlignHeading:
        return "AlignHeading";
    case NaviPhase::AdvanceOnRoute:
        return "AdvanceOnRoute";
    case NaviPhase::WaitZoneTransition:
        return "WaitZoneTransition";
    case NaviPhase::WaitTransfer:
        return "WaitTransfer";
    case NaviPhase::Finished:
        return "Finished";
    case NaviPhase::Failed:
        return "Failed";
    }
    return "Unknown";
}

} // namespace

NavigationSession::NavigationSession(const std::vector<Waypoint>& path, const NaviPosition& initial_pos)
    : original_path_(path)
    , current_path_(path)
    , virtual_yaw_(initial_pos.angle)
    , current_zone_id_(initial_pos.zone_id)
    , canonical_final_goal_index_(ResolveCanonicalFinalGoalIndex(path))
    , original_path_progress_prefix_(BuildPathProgressPrefix(path))
{
}

const std::vector<Waypoint>& NavigationSession::original_path() const
{
    return original_path_;
}

const std::vector<Waypoint>& NavigationSession::current_path() const
{
    return current_path_;
}

size_t NavigationSession::path_origin_index() const
{
    return path_origin_index_;
}

size_t NavigationSession::current_node_idx() const
{
    return current_node_idx_;
}

size_t NavigationSession::CurrentAbsoluteNodeIndex() const
{
    return path_origin_index_ + std::min(current_node_idx_, current_path_.size());
}

bool NavigationSession::HasCanonicalFinalGoal() const
{
    return canonical_final_goal_index_ < original_path_.size();
}

const Waypoint& NavigationSession::CanonicalFinalGoal() const
{
    assert(HasCanonicalFinalGoal() && "CanonicalFinalGoal requires a position node.");
    return original_path_[canonical_final_goal_index_];
}

double NavigationSession::FinalGoalAcceptanceBand() const
{
    if (!HasCanonicalFinalGoal()) {
        return 0.0;
    }
    const Waypoint& final_goal = CanonicalFinalGoal();
    return final_goal.RequiresStrictArrival() ? kStrictArrivalLookaheadRadius : final_goal.GetLookahead();
}

bool NavigationSession::HasReachedCanonicalFinalGoal(const NaviPosition& position) const
{
    if (!HasCanonicalFinalGoal()) {
        return false;
    }
    const Waypoint& final_goal = CanonicalFinalGoal();
    if (!final_goal.zone_id.empty() && position.zone_id != final_goal.zone_id) {
        return false;
    }
    const double distance = std::hypot(final_goal.x - position.x, final_goal.y - position.y);
    return distance <= FinalGoalAcceptanceBand();
}

bool NavigationSession::HasSatisfiedFinalSuccess(const NaviPosition& position, const char* reason)
{
    if (success_) {
        return true;
    }
    if (!final_arrival_evidence_ && !HasReachedCanonicalFinalGoal(position)) {
        return false;
    }
    if (!final_arrival_evidence_) {
        RecordFinalArrivalEvidence(position, route_tail_consumed_, canonical_final_goal_index_, reason);
    }
    CommitSuccessfulCompletion(position, reason);
    return true;
}

void NavigationSession::RecordFinalArrivalEvidence(
    const NaviPosition& position,
    bool verified_at_tail_consumption,
    size_t evidence_index,
    const char* reason)
{
    if (final_arrival_evidence_) {
        return;
    }
    final_arrival_evidence_ = true;
    const double distance_to_final_goal = HasCanonicalFinalGoal()
                                              ? std::hypot(CanonicalFinalGoal().x - position.x, CanonicalFinalGoal().y - position.y)
                                              : 0.0;
    LogInfo << "Final arrival evidence recorded." << VAR(reason) << VAR(verified_at_tail_consumption) << VAR(evidence_index)
            << VAR(distance_to_final_goal);
}

void NavigationSession::CommitSuccessfulCompletion(const NaviPosition& position, const char* reason)
{
    (void)position;
    success_ = true;
    UpdatePhase(NaviPhase::Finished, reason);
}

void NavigationSession::NoteCanonicalFinalGoalConsumed(size_t consumed_absolute_index, const NaviPosition& position, const char* reason)
{
    if (!HasCanonicalFinalGoal() || consumed_absolute_index != canonical_final_goal_index_) {
        return;
    }
    RecordFinalArrivalEvidence(position, false, canonical_final_goal_index_, reason);
}

void NavigationSession::NoteRouteTailConsumed(const NaviPosition& position, const char* reason)
{
    route_tail_consumed_ = true;
    if (HasReachedCanonicalFinalGoal(position)) {
        RecordFinalArrivalEvidence(position, true, canonical_final_goal_index_, "route_tail_consumed_in_final_goal_band");
    }
    if (final_arrival_evidence_) {
        CommitSuccessfulCompletion(position, reason);
        return;
    }
    const double position_x = position.x;
    const double position_y = position.y;
    const double position_angle = position.angle;
    LogError << "Route tail consumed without final success evidence." << VAR(position_x) << VAR(position_y) << VAR(position_angle)
             << VAR(current_node_idx_) << VAR(path_origin_index_);
    UpdatePhase(NaviPhase::Failed, "route_tail_without_final_success");
}

bool NavigationSession::success() const
{
    return success_;
}

bool NavigationSession::HasCurrentWaypoint() const
{
    return current_node_idx_ < current_path_.size();
}

const Waypoint& NavigationSession::CurrentWaypoint() const
{
    RequireCurrentWaypoint("CurrentWaypoint");
    return current_path_[current_node_idx_];
}

const Waypoint& NavigationSession::CurrentPathAt(size_t index) const
{
    RequireWaypointIndex(index, "CurrentPathAt");
    return current_path_[index];
}

double NavigationSession::virtual_yaw() const
{
    return virtual_yaw_;
}

void NavigationSession::SyncVirtualYaw(double yaw)
{
    virtual_yaw_ = yaw;
}

int NavigationSession::straight_stable_frames() const
{
    return straight_stable_frames_;
}

void NavigationSession::ResetStraightStableFrames()
{
    straight_stable_frames_ = 0;
}

const std::string& NavigationSession::current_zone_id() const
{
    return current_zone_id_;
}

void NavigationSession::UpdateCurrentZone(const std::string& zone_id)
{
    current_zone_id_ = zone_id;
}

bool NavigationSession::is_waiting_for_zone_switch() const
{
    return is_waiting_for_zone_switch_;
}

void NavigationSession::SetWaitingForZoneSwitch(bool waiting)
{
    is_waiting_for_zone_switch_ = waiting;
}

void NavigationSession::ConfirmZone(const std::string& zone_id, const NaviPosition& pos, const char* reason)
{
    (void)reason;
    current_zone_id_ = zone_id;
    SyncVirtualYaw(pos.angle);
    ResetStraightStableFrames();
}

std::string NavigationSession::CurrentExpectedZone() const
{
    if (is_waiting_for_zone_switch_ || current_node_idx_ >= current_path_.size()) {
        return {};
    }
    return current_path_[current_node_idx_].zone_id;
}

void NavigationSession::AdvanceToNextWaypoint(const char* reason)
{
    RequireCurrentWaypoint(reason);
    ++current_node_idx_;
    ResetProgress();
}

void NavigationSession::AdvanceToNextWaypoint(ActionType expected_action, const char* reason)
{
    RequireCurrentWaypoint(reason);
    (void)expected_action;
    assert(current_path_[current_node_idx_].action == expected_action && "Unexpected action while advancing waypoint.");
    AdvanceToNextWaypoint(reason);
}

void NavigationSession::SkipPastWaypoint(size_t waypoint_idx, const char* reason)
{
    RequireWaypointIndex(waypoint_idx, reason);
    assert(waypoint_idx >= current_node_idx_ && "SkipPastWaypoint cannot move backward.");
    current_node_idx_ = waypoint_idx + 1;
    ResetProgress();
}

void NavigationSession::ResetDriverProgressTracking()
{
}

void NavigationSession::ResetProgress()
{
    progress_waypoint_idx_ = std::numeric_limits<size_t>::max();
    best_actual_distance_ = std::numeric_limits<double>::max();
    best_route_progress_ = -std::numeric_limits<double>::infinity();
    last_progress_time_ = {};
    progress_initialized_ = false;
}

void NavigationSession::ObserveProgress(
    size_t waypoint_idx,
    const RouteProgressSample& route_progress,
    double actual_distance,
    const std::chrono::steady_clock::time_point& now)
{
    if (!progress_initialized_ || progress_waypoint_idx_ != waypoint_idx) {
        progress_waypoint_idx_ = waypoint_idx;
        best_actual_distance_ = actual_distance;
        best_route_progress_ = route_progress.valid ? route_progress.route_progress : -std::numeric_limits<double>::infinity();
        last_progress_time_ = now;
        progress_initialized_ = true;
        return;
    }

    const bool route_improved = route_progress.valid && route_progress.route_progress > best_route_progress_ + kRouteProgressEpsilon;
    const bool distance_improved = actual_distance + kNoProgressDistanceEpsilon < best_actual_distance_;
    if (route_improved || distance_improved) {
        best_actual_distance_ = std::min(best_actual_distance_, actual_distance);
        if (route_progress.valid) {
            best_route_progress_ = std::max(best_route_progress_, route_progress.route_progress);
        }
        last_progress_time_ = now;
    }
}

int64_t NavigationSession::StalledMs(const std::chrono::steady_clock::time_point& now) const
{
    if (!progress_initialized_ || last_progress_time_.time_since_epoch().count() == 0) {
        return 0;
    }
    return std::chrono::duration_cast<std::chrono::milliseconds>(now - last_progress_time_).count();
}

double NavigationSession::TurnInPlaceYawThreshold() const
{
    return kLocalDriverTurnInPlaceYawDegrees;
}

double NavigationSession::SteeringTrimYawThreshold(bool strict_arrival) const
{
    return strict_arrival ? kMicroThreshold : 6.0;
}

RouteProgressSample NavigationSession::BuildRouteProgressSample(size_t waypoint_idx, double current_pos_x, double current_pos_y) const
{
    RouteProgressSample sample;
    if (current_path_.empty() || waypoint_idx >= current_path_.size()) {
        return sample;
    }

    const size_t absolute_waypoint_idx = path_origin_index_ + waypoint_idx;
    if (absolute_waypoint_idx >= original_path_.size()) {
        return sample;
    }

    const Waypoint& current_waypoint = original_path_[absolute_waypoint_idx];
    if (!current_waypoint.HasPosition()) {
        return sample;
    }

    const size_t next_position_idx = FindNextPositionNode(waypoint_idx);
    if (!current_waypoint.RequiresStrictArrival() && next_position_idx < current_path_.size()) {
        const Waypoint& next_waypoint = current_path_[next_position_idx];
        if (next_waypoint.HasPosition()
            && (current_waypoint.zone_id.empty() || next_waypoint.zone_id.empty() || current_waypoint.zone_id == next_waypoint.zone_id)) {
            const double segment_x = next_waypoint.x - current_waypoint.x;
            const double segment_y = next_waypoint.y - current_waypoint.y;
            const double segment_len_sq = segment_x * segment_x + segment_y * segment_y;
            if (segment_len_sq > std::numeric_limits<double>::epsilon()) {
                const double segment_length = std::sqrt(segment_len_sq);
                const double offset_x = current_pos_x - current_waypoint.x;
                const double offset_y = current_pos_y - current_waypoint.y;
                const double projection = (offset_x * segment_x + offset_y * segment_y) / segment_len_sq;
                const double clamped_projection = std::clamp(projection, 0.0, 1.0);

                sample.valid = true;
                sample.route_progress = original_path_progress_prefix_[absolute_waypoint_idx] + segment_length * clamped_projection;
                return sample;
            }
        }
    }

    sample.valid = true;
    sample.route_progress = original_path_progress_prefix_[absolute_waypoint_idx];
    return sample;
}

size_t NavigationSession::FindNextPositionNode(size_t waypoint_idx) const
{
    for (size_t index = waypoint_idx + 1; index < current_path_.size(); ++index) {
        if (current_path_[index].HasPosition()) {
            return index;
        }
    }
    return current_path_.size();
}

bool NavigationSession::ShouldAdvanceByPassThrough(size_t waypoint_idx, double current_pos_x, double current_pos_y) const
{
    if (waypoint_idx >= current_path_.size()) {
        return false;
    }
    const Waypoint& current_waypoint = current_path_[waypoint_idx];
    if (!current_waypoint.HasPosition() || current_waypoint.RequiresStrictArrival()) {
        return false;
    }

    const size_t next_position_idx = FindNextPositionNode(waypoint_idx);
    if (next_position_idx >= current_path_.size() || next_position_idx != waypoint_idx + 1) {
        return false;
    }

    const Waypoint& next_waypoint = current_path_[next_position_idx];
    if (!next_waypoint.HasPosition() || next_waypoint.RequiresStrictArrival()) {
        return false;
    }
    if (current_waypoint.zone_id != next_waypoint.zone_id) {
        return false;
    }

    const double segment_x = next_waypoint.x - current_waypoint.x;
    const double segment_y = next_waypoint.y - current_waypoint.y;
    const double segment_len_sq = segment_x * segment_x + segment_y * segment_y;
    if (segment_len_sq <= std::numeric_limits<double>::epsilon()) {
        return false;
    }

    const double current_offset_x = current_pos_x - current_waypoint.x;
    const double current_offset_y = current_pos_y - current_waypoint.y;
    const double forward_projection = current_offset_x * segment_x + current_offset_y * segment_y;
    const double projection_ratio = forward_projection / segment_len_sq;
    const double clamped_projection_ratio = std::clamp(projection_ratio, 0.0, 1.0);
    const double projected_x = current_waypoint.x + segment_x * clamped_projection_ratio;
    const double projected_y = current_waypoint.y + segment_y * clamped_projection_ratio;
    const double lateral_distance = std::hypot(current_pos_x - projected_x, current_pos_y - projected_y);
    const double current_distance = std::hypot(current_waypoint.x - current_pos_x, current_waypoint.y - current_pos_y);
    const double next_distance = std::hypot(next_waypoint.x - current_pos_x, next_waypoint.y - current_pos_y);
    if (current_distance > current_waypoint.GetLookahead()) {
        return false;
    }

    const double corridor_radius = std::max({ kWaypointPassThroughCorridor, current_waypoint.GetLookahead(), next_waypoint.GetLookahead() });
    const bool progressed_beyond_waypoint = projection_ratio >= 0.0;
    const bool deep_into_next_leg = projection_ratio >= 0.85;
    const bool next_waypoint_is_closer = next_distance < current_distance;
    const bool close_enough_to_corridor = lateral_distance <= corridor_radius;
    return close_enough_to_corridor && (deep_into_next_leg || (progressed_beyond_waypoint && next_waypoint_is_closer));
}

double NavigationSession::DistanceToAdjacentPortal(size_t waypoint_idx, double current_pos_x, double current_pos_y) const
{
    if (current_path_.empty() || waypoint_idx >= current_path_.size()) {
        return std::numeric_limits<double>::max();
    }

    const size_t start_idx = waypoint_idx > 0 ? waypoint_idx - 1 : 0;
    const size_t end_idx = std::min(waypoint_idx + 1, current_path_.size() - 1);
    double min_distance = std::numeric_limits<double>::max();
    for (size_t index = start_idx; index <= end_idx; ++index) {
        const Waypoint& waypoint = current_path_[index];
        if (!waypoint.HasPosition() || waypoint.action != ActionType::PORTAL) {
            continue;
        }
        min_distance = std::min(min_distance, std::hypot(waypoint.x - current_pos_x, waypoint.y - current_pos_y));
    }
    return min_distance;
}

size_t NavigationSession::FindRejoinSliceStart(size_t continue_index) const
{
    size_t slice_start = continue_index;
    while (slice_start > 0 && original_path_[slice_start - 1].IsZoneDeclaration()) {
        --slice_start;
    }
    return slice_start;
}

void NavigationSession::ApplyRejoinSlice(size_t slice_start, const NaviPosition& pos)
{
    current_path_.assign(original_path_.begin() + static_cast<std::ptrdiff_t>(slice_start), original_path_.end());
    path_origin_index_ = slice_start;
    current_node_idx_ = 0;
    is_waiting_for_zone_switch_ = false;
    current_zone_id_ = pos.zone_id;
    virtual_yaw_ = pos.angle;
    ResetStraightStableFrames();
    ResetDriverProgressTracking();
    ResetProgress();
}

NaviPhase NavigationSession::phase() const
{
    return phase_;
}

void NavigationSession::UpdatePhase(NaviPhase next_phase, const char* reason)
{
    if (phase_ == next_phase) {
        return;
    }
    const char* from_phase_name = NaviPhaseName(phase_);
    const char* to_phase_name = NaviPhaseName(next_phase);
    LogInfo << "Phase transition." << VAR(from_phase_name) << VAR(to_phase_name) << VAR(reason) << VAR(current_node_idx_)
            << VAR(path_origin_index_);
    phase_ = next_phase;
}

void NavigationSession::RequireCurrentWaypoint(const char* reason) const
{
    (void)reason;
    assert(HasCurrentWaypoint() && "Current waypoint is required.");
}

void NavigationSession::RequireWaypointIndex(size_t index, const char* reason) const
{
    (void)index;
    (void)reason;
    assert(index < current_path_.size() && "Waypoint index is out of range.");
}

} // namespace mapnavigator
