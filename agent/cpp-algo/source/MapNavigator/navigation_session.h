#pragma once

#include <cstdint>
#include <limits>
#include <string>
#include <vector>

#include "navi_domain_types.h"

namespace mapnavigator
{

class LocalDriverLite;

enum class NaviPhase
{
    Bootstrap,
    AlignHeading,
    AdvanceOnRoute,
    WaitZoneTransition,
    WaitRelocation,
    RecoverRejoin,
    Finished,
    Failed,
};

struct NavigationSession
{
    explicit NavigationSession(const std::vector<Waypoint>& path, const NaviPosition& initial_pos);

    const std::vector<Waypoint>& original_path() const;
    const std::vector<Waypoint>& current_path() const;
    size_t path_origin_index() const;
    size_t current_node_idx() const;
    size_t CurrentAbsoluteNodeIndex() const;
    bool HasCurrentWaypoint() const;
    const Waypoint& CurrentWaypoint() const;
    const Waypoint& CurrentPathAt(size_t index) const;
    double virtual_yaw() const;
    void SyncVirtualYaw(double yaw, const char* reason);
    int straight_stable_frames() const;
    void ResetStraightStableFrames();
    int IncrementStraightStableFrames();
    const std::string& current_zone_id() const;
    void UpdateCurrentZone(const std::string& zone_id, const char* reason);
    bool is_waiting_for_zone_switch() const;
    void SetWaitingForZoneSwitch(bool waiting, const char* reason);
    void ConfirmZone(const std::string& zone_id, const NaviPosition& pos, const char* reason);
    bool has_previous_driver_pos() const;
    const NaviPosition& previous_driver_pos() const;
    double previous_driver_distance() const;
    void RecordDriverObservation(double distance, const NaviPosition& pos);
    std::string CurrentExpectedZone() const;
    void AdvanceToNextWaypoint(const char* reason);
    void AdvanceToNextWaypoint(ActionType expected_action, const char* reason);
    void SkipPastWaypoint(size_t waypoint_idx, const char* reason);
    void ResetDriverProgressTracking(LocalDriverLite* local_driver);
    void ResetProgress();
    void ObserveProgress(size_t waypoint_idx, double actual_distance, const std::chrono::steady_clock::time_point& now);
    void MarkRecoveryAttempt(double actual_distance, const std::chrono::steady_clock::time_point& now);
    double best_actual_distance() const;
    int no_progress_recovery_attempts() const;
    int64_t StalledMs(const std::chrono::steady_clock::time_point& now) const;
    int64_t RecoveryCooldownMs(const std::chrono::steady_clock::time_point& now) const;
    size_t FindNextPositionNode(size_t waypoint_idx) const;
    bool ShouldAdvanceByPassThrough(size_t waypoint_idx, double current_pos_x, double current_pos_y) const;
    double DistanceToAdjacentPortal(size_t waypoint_idx, double current_pos_x, double current_pos_y) const;
    size_t FindRejoinSliceStart(size_t continue_index) const;
    void ApplyRejoinSlice(size_t slice_start, const NaviPosition& pos);
    NaviPhase phase() const;
    void UpdatePhase(NaviPhase next_phase, const char* reason);

private:
    std::vector<Waypoint> original_path_;
    std::vector<Waypoint> current_path_;
    size_t path_origin_index_ = 0;
    size_t current_node_idx_ = 0;
    double virtual_yaw_ = 0.0;
    int straight_stable_frames_ = 0;
    std::string current_zone_id_;
    bool is_waiting_for_zone_switch_ = false;
    double previous_driver_distance_ = std::numeric_limits<double>::quiet_NaN();
    NaviPosition previous_driver_pos_;
    bool has_previous_driver_pos_ = false;
    NaviPhase phase_ = NaviPhase::Bootstrap;

    size_t progress_waypoint_idx_ = std::numeric_limits<size_t>::max();
    double best_actual_distance_ = std::numeric_limits<double>::max();
    int no_progress_recovery_attempts_ = 0;
    std::chrono::steady_clock::time_point last_progress_time_ {};
    std::chrono::steady_clock::time_point last_recovery_time_ {};
    bool progress_initialized_ = false;

    void RequireCurrentWaypoint(const char* reason) const;
    void RequireWaypointIndex(size_t index, const char* reason) const;
};

} // namespace mapnavigator
