#include <algorithm>
#include <cassert>
#include <cmath>

#include <MaaUtils/Logger.h>

#include "local_driver_lite.h"
#include "navi_config.h"
#include "navigation_session.h"

namespace mapnavigator
{

namespace
{

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
    case NaviPhase::WaitRelocation:
        return "WaitRelocation";
    case NaviPhase::RecoverRejoin:
        return "RecoverRejoin";
    case NaviPhase::ExactTargetRefine:
        return "ExactTargetRefine";
    case NaviPhase::Finished:
        return "Finished";
    case NaviPhase::Failed:
        return "Failed";
    }
    return "Unknown";
}

void LogPhaseTransition(NaviPhase from_phase, NaviPhase to_phase, const char* reason, size_t current_node_idx, size_t path_origin_index)
{
    const char* from_phase_name = NaviPhaseName(from_phase);
    const char* to_phase_name = NaviPhaseName(to_phase);
    LogInfo << "Phase transition." << VAR(from_phase_name) << VAR(to_phase_name) << VAR(reason) << VAR(current_node_idx)
            << VAR(path_origin_index);
}

} // namespace

NavigationSession::NavigationSession(const std::vector<Waypoint>& path, const NaviPosition& initial_pos)
    : original_path_(path)
    , current_path_(path)
    , virtual_yaw_(initial_pos.angle)
    , current_zone_id_(initial_pos.zone_id)
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
    if (current_path_.empty() || current_node_idx_ >= current_path_.size()) {
        return path_origin_index_;
    }
    return path_origin_index_ + current_node_idx_;
}

bool NavigationSession::HasCurrentWaypoint() const
{
    return current_node_idx_ < current_path_.size();
}

const Waypoint& NavigationSession::CurrentWaypoint() const
{
    RequireCurrentWaypoint("current_waypoint");
    return current_path_[current_node_idx_];
}

const Waypoint& NavigationSession::CurrentPathAt(size_t index) const
{
    RequireWaypointIndex(index, "current_path_at");
    return current_path_[index];
}

double NavigationSession::virtual_yaw() const
{
    return virtual_yaw_;
}

void NavigationSession::SyncVirtualYaw(double yaw, [[maybe_unused]] const char* reason)
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

int NavigationSession::IncrementStraightStableFrames()
{
    return ++straight_stable_frames_;
}

const std::string& NavigationSession::current_zone_id() const
{
    return current_zone_id_;
}

void NavigationSession::UpdateCurrentZone(const std::string& zone_id, [[maybe_unused]] const char* reason)
{
    current_zone_id_ = zone_id;
}

bool NavigationSession::is_waiting_for_zone_switch() const
{
    return is_waiting_for_zone_switch_;
}

void NavigationSession::SetWaitingForZoneSwitch(bool waiting, [[maybe_unused]] const char* reason)
{
    is_waiting_for_zone_switch_ = waiting;
}

void NavigationSession::ConfirmZone(const std::string& zone_id, const NaviPosition& pos, const char* reason)
{
    UpdateCurrentZone(zone_id, reason);
    SyncVirtualYaw(pos.angle, reason);
    ResetStraightStableFrames();
}

bool NavigationSession::has_previous_driver_pos() const
{
    return has_previous_driver_pos_;
}

const NaviPosition& NavigationSession::previous_driver_pos() const
{
    assert(has_previous_driver_pos_ && "NavigationSession previous_driver_pos requires an observation");
    return previous_driver_pos_;
}

double NavigationSession::previous_driver_distance() const
{
    return previous_driver_distance_;
}

void NavigationSession::RecordDriverObservation(double distance, const NaviPosition& pos)
{
    previous_driver_distance_ = distance;
    previous_driver_pos_ = pos;
    has_previous_driver_pos_ = true;
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
}

void NavigationSession::AdvanceToNextWaypoint(ActionType expected_action, const char* reason)
{
    (void)expected_action;
    RequireCurrentWaypoint(reason);
    assert(
        current_path_[current_node_idx_].action == expected_action
        && "NavigationSession waypoint advance violated expected action contract");
    AdvanceToNextWaypoint(reason);
}

void NavigationSession::SkipPastWaypoint(size_t waypoint_idx, const char* reason)
{
    RequireWaypointIndex(waypoint_idx, reason);
    assert(waypoint_idx >= current_node_idx_ && "NavigationSession cannot skip backward in current_path");
    current_node_idx_ = waypoint_idx + 1;
}

void NavigationSession::ResetDriverProgressTracking(LocalDriverLite* local_driver)
{
    previous_driver_distance_ = std::numeric_limits<double>::quiet_NaN();
    has_previous_driver_pos_ = false;
    if (local_driver != nullptr) {
        local_driver->OnWaypointChanged();
    }
}

void NavigationSession::ResetProgress()
{
    progress_waypoint_idx_ = std::numeric_limits<size_t>::max();
    best_actual_distance_ = std::numeric_limits<double>::max();
    no_progress_recovery_attempts_ = 0;
    last_progress_time_ = {};
    last_recovery_time_ = {};
    progress_initialized_ = false;
}

void NavigationSession::ObserveProgress(size_t waypoint_idx, double actual_distance, const std::chrono::steady_clock::time_point& now)
{
    if (!progress_initialized_ || progress_waypoint_idx_ != waypoint_idx) {
        progress_waypoint_idx_ = waypoint_idx;
        best_actual_distance_ = actual_distance;
        no_progress_recovery_attempts_ = 0;
        last_progress_time_ = now;
        last_recovery_time_ = now;
        progress_initialized_ = true;
        return;
    }

    if (actual_distance + kNoProgressDistanceEpsilon < best_actual_distance_) {
        best_actual_distance_ = actual_distance;
        no_progress_recovery_attempts_ = 0;
        last_progress_time_ = now;
        last_recovery_time_ = now;
    }
}

void NavigationSession::MarkRecoveryAttempt(double actual_distance, const std::chrono::steady_clock::time_point& now)
{
    if (!progress_initialized_) {
        progress_initialized_ = true;
    }

    best_actual_distance_ = actual_distance;
    ++no_progress_recovery_attempts_;
    last_recovery_time_ = now;
    if (last_progress_time_.time_since_epoch().count() == 0) {
        last_progress_time_ = now;
    }
}

double NavigationSession::best_actual_distance() const
{
    return best_actual_distance_;
}

int NavigationSession::no_progress_recovery_attempts() const
{
    return no_progress_recovery_attempts_;
}

int64_t NavigationSession::StalledMs(const std::chrono::steady_clock::time_point& now) const
{
    if (!progress_initialized_ || last_progress_time_.time_since_epoch().count() == 0) {
        return 0;
    }
    return std::chrono::duration_cast<std::chrono::milliseconds>(now - last_progress_time_).count();
}

int64_t NavigationSession::RecoveryCooldownMs(const std::chrono::steady_clock::time_point& now) const
{
    if (!progress_initialized_ || last_recovery_time_.time_since_epoch().count() == 0) {
        return 0;
    }
    return std::chrono::duration_cast<std::chrono::milliseconds>(now - last_recovery_time_).count();
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
    if (forward_projection <= 0.0) {
        return false;
    }

    const double projection_ratio = forward_projection / segment_len_sq;
    const double projected_x = current_waypoint.x + segment_x * projection_ratio;
    const double projected_y = current_waypoint.y + segment_y * projection_ratio;
    const double lateral_distance = std::hypot(current_pos_x - projected_x, current_pos_y - projected_y);
    const double corridor_radius =
        std::max({ kWaypointPassThroughCorridor, current_waypoint.GetLookahead(), next_waypoint.GetLookahead() });

    return lateral_distance <= corridor_radius;
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
    straight_stable_frames_ = 0;
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

    LogPhaseTransition(phase_, next_phase, reason, current_node_idx_, path_origin_index_);
    phase_ = next_phase;
}

void NavigationSession::RequireCurrentWaypoint(const char* reason) const
{
    (void)reason;
    assert(HasCurrentWaypoint() && "NavigationSession requires a current waypoint");
}

void NavigationSession::RequireWaypointIndex(size_t index, const char* reason) const
{
    (void)index;
    (void)reason;
    assert(index < current_path_.size() && "NavigationSession waypoint index out of range");
}

} // namespace mapnavigator
