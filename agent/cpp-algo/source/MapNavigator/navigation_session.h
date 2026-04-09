#pragma once

#include <chrono>
#include <cstdint>
#include <limits>
#include <string>
#include <vector>

#include "navi_domain_types.h"

namespace mapnavigator
{

struct RouteProgressSample
{
    bool valid = false;
    double route_progress = 0.0;
};

enum class NaviPhase
{
    Bootstrap,
    AlignHeading,
    AdvanceOnRoute,
    WaitZoneTransition,
    WaitTransfer,
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

    bool HasCanonicalFinalGoal() const;
    const Waypoint& CanonicalFinalGoal() const;
    bool HasReachedCanonicalFinalGoal(const NaviPosition& position) const;
    bool HasSatisfiedFinalSuccess(const NaviPosition& position, const char* reason);
    void NoteCanonicalFinalGoalConsumed(size_t consumed_absolute_index, const NaviPosition& position, const char* reason);
    void NoteRouteTailConsumed(const NaviPosition& position, const char* reason);

    bool success() const;
    bool HasCurrentWaypoint() const;
    const Waypoint& CurrentWaypoint() const;
    const Waypoint& CurrentPathAt(size_t index) const;

    double virtual_yaw() const;
    void SyncVirtualYaw(double yaw);

    int straight_stable_frames() const;
    void ResetStraightStableFrames();

    const std::string& current_zone_id() const;
    void UpdateCurrentZone(const std::string& zone_id);
    bool is_waiting_for_zone_switch() const;
    void SetWaitingForZoneSwitch(bool waiting);
    void ConfirmZone(const std::string& zone_id, const NaviPosition& pos, const char* reason);

    std::string CurrentExpectedZone() const;
    void AdvanceToNextWaypoint(const char* reason);
    void AdvanceToNextWaypoint(ActionType expected_action, const char* reason);
    void SkipPastWaypoint(size_t waypoint_idx, const char* reason);

    void ResetDriverProgressTracking();
    void ResetProgress();
    void ObserveProgress(
        size_t waypoint_idx,
        const RouteProgressSample& route_progress,
        double actual_distance,
        const std::chrono::steady_clock::time_point& now);

    int64_t StalledMs(const std::chrono::steady_clock::time_point& now) const;
    double TurnInPlaceYawThreshold() const;
    double SteeringTrimYawThreshold(bool strict_arrival) const;

    RouteProgressSample BuildRouteProgressSample(size_t waypoint_idx, double current_pos_x, double current_pos_y) const;
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
    NaviPhase phase_ = NaviPhase::Bootstrap;
    size_t canonical_final_goal_index_ = std::numeric_limits<size_t>::max();
    bool success_ = false;
    bool route_tail_consumed_ = false;
    bool final_arrival_evidence_ = false;

    size_t progress_waypoint_idx_ = std::numeric_limits<size_t>::max();
    double best_actual_distance_ = std::numeric_limits<double>::max();
    double best_route_progress_ = -std::numeric_limits<double>::infinity();
    std::chrono::steady_clock::time_point last_progress_time_ {};
    bool progress_initialized_ = false;

    std::vector<double> original_path_progress_prefix_;

    void RequireCurrentWaypoint(const char* reason) const;
    void RequireWaypointIndex(size_t index, const char* reason) const;
    void RecordFinalArrivalEvidence(
        const NaviPosition& position,
        bool verified_at_tail_consumption,
        size_t evidence_index,
        const char* reason);
    void CommitSuccessfulCompletion(const NaviPosition& position, const char* reason);
    double FinalGoalAcceptanceBand() const;
};

} // namespace mapnavigator
