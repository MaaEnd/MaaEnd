#pragma once

#include <chrono>
#include <cstdint>
#include <limits>
#include <optional>
#include <string>
#include <vector>

#include "../navi_domain_types.h"

namespace mapnavigator
{

enum class NaviPhase
{
    Bootstrap,
    Navigate,
    WaitTransfer,
    // 卡死检测、绕障、重规划、采集扫描只在 Navigate 下运行, 各 Wait 阶段借此全部屏蔽
    WaitZipline,
    WaitFind,
    WaitJumpPad,
    WaitRope,
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
    std::optional<double> WalkedFloorY(const NaviPosition& position) const;
    std::optional<size_t> CanonicalIndexAtCurrent() const;
    std::optional<size_t> CanonicalIndexAtCurrentPath(size_t index) const;

    const std::string& current_zone_id() const;
    void UpdateCurrentZone(const std::string& zone_id);
    void AdvanceToNextWaypoint(const char* reason);
    void AdvanceToNextWaypoint(ActionType expected_action, const char* reason);
    void SkipPastWaypoint(size_t waypoint_idx, const char* reason);
    // 只改当前航点坐标, 动作和后续点不变, 走廊仍指向原坐标
    bool RetargetCurrentWaypoint(double x, double y, const char* reason);

    void ResetProgress();
    void ObserveProgress(size_t waypoint_idx, double actual_distance, const std::chrono::steady_clock::time_point& now);

    int64_t StalledMs(const std::chrono::steady_clock::time_point& now) const;
    double best_distance_to_target() const;

    // 只在航点前进、跳点或换路时清零, ResetProgress 不清, 原地打转时恢复超时才触发得了
    void ObserveHardProgress(size_t target_key, double actual_distance, const std::chrono::steady_clock::time_point& now);
    int64_t HardStalledMs(const std::chrono::steady_clock::time_point& now) const;
    void ResetHardProgress();
    void ExcludeFromStallClocks(std::chrono::steady_clock::duration paused);

    std::optional<double> LandedTowerDeckY(const NaviPosition& position) const;
    void LeaveLandedTowerDeck();
    // 换路后航点下标归零, 推不出人站在哪根架子上, 由这里记着
    void NoteStandingTower(const std::optional<ZiplineNodeRef>& tower);

    void ApplyDynamicOverlay(std::vector<Waypoint> generated_prefix, size_t continue_index, const NaviPosition& pos);
    void ReplaceRoute(std::vector<Waypoint> path, const NaviPosition& pos, const char* reason);

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
    bool left_landed_deck_ = false;
    std::optional<ZiplineNodeRef> standing_tower_;

    size_t progress_waypoint_idx_ = std::numeric_limits<size_t>::max();
    double best_distance_to_target_ = std::numeric_limits<double>::max();
    std::chrono::steady_clock::time_point last_progress_time_ {};
    bool progress_initialized_ = false;

    size_t hard_progress_target_key_ = std::numeric_limits<size_t>::max();
    double hard_best_distance_ = std::numeric_limits<double>::max();
    std::chrono::steady_clock::time_point hard_last_progress_time_ {};
    bool hard_progress_initialized_ = false;

    [[nodiscard]] bool RequireCurrentWaypoint(const char* reason) const;
    [[nodiscard]] bool RequireWaypointIndex(size_t index, const char* reason) const;
    void RecordFinalArrivalEvidence(
        const NaviPosition& position,
        bool verified_at_tail_consumption,
        size_t evidence_index,
        const char* reason);
    void CommitSuccessfulCompletion(const NaviPosition& position, const char* reason);
    double FinalGoalAcceptanceBand() const;
};

} // namespace mapnavigator
