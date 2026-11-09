#pragma once

#include <cstdint>
#include <limits>
#include <optional>
#include <string>
#include <vector>

#include "../navi_domain_types.h"
#include "../navi_time.h"

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

const char* navi_phase_name(NaviPhase phase);

struct ProgressClock
{
    size_t target_key = std::numeric_limits<size_t>::max();
    double best_distance = std::numeric_limits<double>::max();
    TimePoint last_progress_time {};
    bool initialized = false;
};

// 换路点时整个清掉
struct WaypointVisit
{
    bool left_landed_deck = false;
    // 换路后航点下标归零, 推不出人站在哪根架子上, 由这里记着
    std::optional<ZiplineNodeRef> standing_tower;
    ProgressClock progress;
    // 只在航点前进、跳点或换路时清零, ResetProgress 不清, 原地打转时恢复超时才触发得了
    ProgressClock hard_progress;
};

struct Session
{
    std::vector<Waypoint> original_path;
    std::vector<Waypoint> current_path;
    size_t path_origin_index = 0;
    size_t generated_prefix_size = 0;
    size_t current_node_idx = 0;
    std::string current_zone_id;
    NaviPhase phase = NaviPhase::Bootstrap;
    size_t canonical_final_goal_index = std::numeric_limits<size_t>::max();
    bool success = false;
    bool route_tail_consumed = false;
    bool final_arrival_evidence = false;
    // 导航自己最近一次发起可能留下界面的动作的时刻
    TimePoint last_interaction_at {};
    WaypointVisit visit;
};

Session MakeSession(const std::vector<Waypoint>& path, const NaviPosition& initial_pos);

std::optional<size_t> CurrentAbsoluteNodeIndex(const Session& session);
bool HasCurrentWaypoint(const Session& session);
const Waypoint& CurrentWaypoint(const Session& session);
const Waypoint& CurrentPathAt(const Session& session, size_t index);
std::optional<double> WalkedFloorY(const Session& session, const NaviPosition& position);
std::optional<size_t> CanonicalIndexAtCurrent(const Session& session);
std::optional<size_t> CanonicalIndexAtCurrentPath(const Session& session, size_t index);
std::optional<double> LandedTowerDeckY(const Session& session, const NaviPosition& position);

bool FinalSuccessReady(const Session& session, const NaviPosition& position);
void CommitFinalSuccess(Session* session, const NaviPosition& position, const char* reason);
void NoteCanonicalFinalGoalConsumed(
    Session* session,
    std::optional<size_t> consumed_absolute_index,
    const NaviPosition& position,
    const char* reason);
void NoteRouteTailConsumed(Session* session, const NaviPosition& position, const char* reason);

void UpdateCurrentZone(Session* session, const std::string& zone_id);
void AdvanceToNextWaypoint(Session* session, const char* reason);
void AdvanceToNextWaypoint(Session* session, ActionType expected_action, const char* reason);
void SkipPastWaypoint(Session* session, size_t waypoint_idx, const char* reason);
void NoteInteraction(Session* session);
// 只改当前航点坐标, 动作和后续点不变, 走廊仍指向原坐标
bool RetargetCurrentWaypoint(Session* session, double x, double y, const char* reason);

void ResetProgress(Session* session);
void ObserveProgress(Session* session, size_t waypoint_idx, double actual_distance, TimePoint now);
int64_t StalledMs(const Session& session, TimePoint now);
void ResetHardProgress(Session* session);
void ObserveHardProgress(Session* session, size_t target_key, double actual_distance, TimePoint now);
int64_t HardStalledMs(const Session& session, TimePoint now);
void ExcludeFromStallClocks(Session* session, Duration paused);

void LeaveLandedTowerDeck(Session* session);
void NoteStandingTower(Session* session, const std::optional<ZiplineNodeRef>& tower);

void ApplyDynamicOverlay(Session* session, std::vector<Waypoint> generated_prefix, size_t continue_index, const NaviPosition& pos);
void ReplaceRoute(Session* session, std::vector<Waypoint> path, const NaviPosition& pos, const char* reason);

void UpdatePhase(Session* session, NaviPhase next_phase, const char* reason);

} // namespace mapnavigator
