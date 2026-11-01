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

struct ProgressClock
{
    size_t target_key = std::numeric_limits<size_t>::max();
    double best_distance = std::numeric_limits<double>::max();
    std::chrono::steady_clock::time_point last_progress_time {};
    bool initialized = false;

    void observe(size_t key, double actual_distance, std::chrono::steady_clock::time_point now);
    int64_t stalledMs(std::chrono::steady_clock::time_point now) const;
    void pause(std::chrono::steady_clock::duration paused);

    void reset() { *this = {}; }
};

struct NavigationSession
{
    explicit NavigationSession(const std::vector<Waypoint>& path, const NaviPosition& initial_pos);

    const std::vector<Waypoint>& originalPath() const;
    const std::vector<Waypoint>& currentPath() const;
    size_t pathOriginIndex() const;
    size_t currentNodeIdx() const;
    std::optional<size_t> currentAbsoluteNodeIndex() const;

    bool hasCanonicalFinalGoal() const;
    const Waypoint& canonicalFinalGoal() const;
    bool hasReachedCanonicalFinalGoal(const NaviPosition& position) const;
    bool hasSatisfiedFinalSuccess(const NaviPosition& position, const char* reason);
    void noteCanonicalFinalGoalConsumed(std::optional<size_t> consumed_absolute_index, const NaviPosition& position, const char* reason);
    void noteRouteTailConsumed(const NaviPosition& position, const char* reason);

    bool success() const;
    bool hasCurrentWaypoint() const;
    const Waypoint& currentWaypoint() const;
    const Waypoint& currentPathAt(size_t index) const;
    std::optional<double> walkedFloorY(const NaviPosition& position) const;
    std::optional<size_t> canonicalIndexAtCurrent() const;
    std::optional<size_t> canonicalIndexAtCurrentPath(size_t index) const;

    const std::string& currentZoneId() const;
    void updateCurrentZone(const std::string& zone_id);
    void advanceToNextWaypoint(const char* reason);
    void advanceToNextWaypoint(ActionType expected_action, const char* reason);
    void skipPastWaypoint(size_t waypoint_idx, const char* reason);
    // 只改当前航点坐标, 动作和后续点不变, 走廊仍指向原坐标
    bool retargetCurrentWaypoint(double x, double y, const char* reason);

    void resetProgress();
    void observeProgress(size_t waypoint_idx, double actual_distance, const std::chrono::steady_clock::time_point& now);

    int64_t stalledMs(const std::chrono::steady_clock::time_point& now) const;
    double bestDistanceToTarget() const;

    // 只在航点前进、跳点或换路时清零, resetProgress 不清, 原地打转时恢复超时才触发得了
    void observeHardProgress(size_t target_key, double actual_distance, const std::chrono::steady_clock::time_point& now);
    int64_t hardStalledMs(const std::chrono::steady_clock::time_point& now) const;
    void resetHardProgress();
    void excludeFromStallClocks(std::chrono::steady_clock::duration paused);

    std::optional<double> landedTowerDeckY(const NaviPosition& position) const;
    void leaveLandedTowerDeck();
    // 换路后航点下标归零, 推不出人站在哪根架子上, 由这里记着
    void noteStandingTower(const std::optional<ZiplineNodeRef>& tower);

    void applyDynamicOverlay(std::vector<Waypoint> generated_prefix, size_t continue_index, const NaviPosition& pos);
    void replaceRoute(std::vector<Waypoint> path, const NaviPosition& pos, const char* reason);

    NaviPhase phase() const;
    void updatePhase(NaviPhase next_phase, const char* reason);

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

    ProgressClock progress_;
    ProgressClock hard_progress_;

    [[nodiscard]] bool requireCurrentWaypoint(const char* reason) const;
    [[nodiscard]] bool requireWaypointIndex(size_t index, const char* reason) const;
    std::optional<size_t> canonicalIndexOf(size_t index) const;
    void leaveCurrentWaypoint(size_t next_index);
    void recordFinalArrivalEvidence(const NaviPosition& position, bool verified_at_tail_consumption, const char* reason);
    void commitSuccessfulCompletion(const char* reason);
    double finalGoalAcceptanceBand() const;
};

} // namespace mapnavigator
