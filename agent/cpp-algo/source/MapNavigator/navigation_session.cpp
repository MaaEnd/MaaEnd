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

const char* NaviPhaseName(NaviPhase phase)
{
    switch (phase) {
    case NaviPhase::Bootstrap:
        return "Bootstrap";
    case NaviPhase::Navigate:
        return "Navigate";
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
    , current_zone_id_(initial_pos.zone_id)
    , canonical_final_goal_index_(ResolveCanonicalFinalGoalIndex(path))
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
    const double distance_to_final_goal =
        HasCanonicalFinalGoal() ? std::hypot(CanonicalFinalGoal().x - position.x, CanonicalFinalGoal().y - position.y) : 0.0;
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
    LogError << "Route tail consumed without final success evidence." << VAR(position.x) << VAR(position.y) << VAR(position.angle)
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

const std::string& NavigationSession::current_zone_id() const
{
    return current_zone_id_;
}

void NavigationSession::UpdateCurrentZone(const std::string& zone_id)
{
    current_zone_id_ = zone_id;
}

void NavigationSession::AdvanceToNextWaypoint(const char* reason)
{
    RequireCurrentWaypoint(reason);
    ++current_node_idx_;
    ResetProgress();
}

void NavigationSession::AdvanceToNextWaypoint(ActionType expected_action, const char* reason)
{
    (void)expected_action;
    RequireCurrentWaypoint(reason);
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

void NavigationSession::ResetProgress()
{
    progress_waypoint_idx_ = std::numeric_limits<size_t>::max();
    best_distance_to_target_ = std::numeric_limits<double>::max();
    last_progress_time_ = {};
    progress_initialized_ = false;
}

void NavigationSession::ObserveProgress(size_t waypoint_idx, double actual_distance, const std::chrono::steady_clock::time_point& now)
{
    const double progress_epsilon = std::max(kNoProgressDistanceEpsilon, kMeasurementDefaultPositionQuantum);
    if (!progress_initialized_ || progress_waypoint_idx_ != waypoint_idx) {
        progress_waypoint_idx_ = waypoint_idx;
        best_distance_to_target_ = actual_distance;
        last_progress_time_ = now;
        progress_initialized_ = true;
        return;
    }

    if (actual_distance < best_distance_to_target_ - progress_epsilon) {
        best_distance_to_target_ = actual_distance;
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

double NavigationSession::best_distance_to_target() const
{
    return best_distance_to_target_;
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
    current_zone_id_ = pos.zone_id;
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
