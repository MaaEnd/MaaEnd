#pragma once

#include <chrono>
#include <cstdint>
#include <limits>
#include <optional>
#include <string>
#include <vector>

#include "navi_domain_types.h"

namespace mapnavigator
{

enum class NaviPhase
{
    Bootstrap,
    Navigate,
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
    std::optional<size_t> CurrentAbsoluteNodeIndex() const;

    bool HasCanonicalFinalGoal() const;
    const Waypoint& CanonicalFinalGoal() const;
    bool HasReachedCanonicalFinalGoal(const NaviPosition& position) const;
    bool HasSatisfiedFinalSuccess(const NaviPosition& position, const char* reason);
    void NoteCanonicalFinalGoalConsumed(std::optional<size_t> consumed_absolute_index, const NaviPosition& position, const char* reason);
    void NoteRouteTailConsumed(const NaviPosition& position, const char* reason);

    bool success() const;
    bool HasCurrentWaypoint() const;
    const Waypoint& CurrentWaypoint() const;
    const Waypoint& CurrentPathAt(size_t index) const;
    std::optional<size_t> CanonicalIndexAtCurrent() const;
    std::optional<size_t> CanonicalIndexAtCurrentPath(size_t index) const;

    const std::string& current_zone_id() const;
    void UpdateCurrentZone(const std::string& zone_id);
    void AdvanceToNextWaypoint(const char* reason);
    void AdvanceToNextWaypoint(ActionType expected_action, const char* reason);
    void SkipPastWaypoint(size_t waypoint_idx, const char* reason);

    void ResetProgress();
    void ObserveProgress(size_t waypoint_idx, double actual_distance, const std::chrono::steady_clock::time_point& now);

    int64_t StalledMs(const std::chrono::steady_clock::time_point& now) const;
    double best_distance_to_target() const;

    // Hard no-progress watchdog. Mirrors ObserveProgress/StalledMs but is *only* cleared by a genuine route
    // change (waypoint advance / skip / dynamic overlay) — never by ResetProgress. Dynamic-recovery escapes
    // call ResetProgress on every small jump, so the ordinary stall clock can be reset indefinitely while the
    // agent thrashes in place; this clock survives those resets and lets the recovery timeout actually fire.
    void ObserveHardProgress(size_t waypoint_idx, double actual_distance, const std::chrono::steady_clock::time_point& now);
    int64_t HardStalledMs(const std::chrono::steady_clock::time_point& now) const;
    void ResetHardProgress();

    void ApplyDynamicOverlay(
        std::vector<Waypoint> generated_prefix,
        size_t continue_index,
        const NaviPosition& pos,
        bool reset_hard_progress = true);

    NaviPhase phase() const;
    void UpdatePhase(NaviPhase next_phase, const char* reason);

private:
    std::vector<Waypoint> original_path_;
    std::vector<Waypoint> current_path_;
    size_t path_origin_index_ = 0;
    size_t generated_prefix_size_ = 0;
    size_t current_node_idx_ = 0;
    std::string current_zone_id_;
    NaviPhase phase_ = NaviPhase::Bootstrap;
    size_t canonical_final_goal_index_ = std::numeric_limits<size_t>::max();
    bool success_ = false;
    bool route_tail_consumed_ = false;
    bool final_arrival_evidence_ = false;

    size_t progress_waypoint_idx_ = std::numeric_limits<size_t>::max();
    double best_distance_to_target_ = std::numeric_limits<double>::max();
    std::chrono::steady_clock::time_point last_progress_time_ {};
    bool progress_initialized_ = false;

    size_t hard_progress_waypoint_idx_ = std::numeric_limits<size_t>::max();
    double hard_best_distance_ = std::numeric_limits<double>::max();
    std::chrono::steady_clock::time_point hard_last_progress_time_ {};
    bool hard_progress_initialized_ = false;

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
