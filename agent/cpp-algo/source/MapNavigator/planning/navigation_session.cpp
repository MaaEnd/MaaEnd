#include <algorithm>
#include <cassert>
#include <cmath>
#include <optional>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_time.h"
#include "navigation_session.h"

namespace mapnavigator
{

namespace
{

// 越界时的替身, has_position 为 false, 调用方会走无坐标分支
const Waypoint& empty_waypoint()
{
    static const Waypoint empty {};
    return empty;
}

size_t resolve_canonical_final_goal_index(const std::vector<Waypoint>& path)
{
    for (size_t index = path.size(); index > 0; --index) {
        if (path[index - 1].has_position) {
            return index - 1;
        }
    }
    return std::numeric_limits<size_t>::max();
}

int64_t clock_stalled_ms(const ProgressClock& clock, TimePoint now)
{
    if (!clock.initialized || IsUnset(clock.last_progress_time)) {
        return 0;
    }
    return ElapsedMs(clock.last_progress_time, now);
}

void pause_clock(ProgressClock* clock, Duration paused)
{
    if (clock->initialized && !IsUnset(clock->last_progress_time)) {
        clock->last_progress_time += paused;
    }
}

[[nodiscard]] bool require_current_waypoint(const Session& session, const char* reason)
{
    if (HasCurrentWaypoint(session)) {
        return true;
    }
    LogError << "Current waypoint is required." << VAR(reason) << VAR_AS(current_node_idx_, session.current_node_idx)
             << VAR_AS(current_path_.size(), session.current_path.size());
    return false;
}

[[nodiscard]] bool require_waypoint_index(const Session& session, size_t index, const char* reason)
{
    if (index < session.current_path.size()) {
        return true;
    }
    LogError << "Waypoint index is out of range." << VAR(reason) << VAR(index) << VAR_AS(current_path_.size(), session.current_path.size());
    return false;
}

std::optional<size_t> canonical_index_of(const Session& session, size_t index)
{
    if (index < session.generated_prefix_size) {
        return std::nullopt;
    }
    return session.path_origin_index + index - session.generated_prefix_size;
}

bool has_canonical_final_goal(const Session& session)
{
    return session.canonical_final_goal_index < session.original_path.size();
}

const Waypoint& canonical_final_goal(const Session& session)
{
    if (!has_canonical_final_goal(session)) {
        LogError << "CanonicalFinalGoal requires a position node."
                 << VAR_AS(canonical_final_goal_index_, session.canonical_final_goal_index)
                 << VAR_AS(original_path_.size(), session.original_path.size());
        return empty_waypoint();
    }
    return session.original_path[session.canonical_final_goal_index];
}

double final_goal_acceptance_band(const Session& session)
{
    if (!has_canonical_final_goal(session)) {
        return 0.0;
    }
    const Waypoint& final_goal = canonical_final_goal(session);
    return RequiresStrictArrival(final_goal) ? kStrictArrivalLookaheadRadius : GetLookahead(final_goal);
}

bool has_reached_canonical_final_goal(const Session& session, const NaviPosition& position)
{
    if (!has_canonical_final_goal(session)) {
        return false;
    }
    const Waypoint& final_goal = canonical_final_goal(session);
    if (!final_goal.zone_id.empty() && position.zone_id != final_goal.zone_id) {
        return false;
    }
    const double distance = std::hypot(final_goal.x - position.x, final_goal.y - position.y);
    return distance <= final_goal_acceptance_band(session);
}

void record_final_arrival_evidence(Session* session, const NaviPosition& position, bool verified_at_tail_consumption, const char* reason)
{
    if (session->final_arrival_evidence) {
        return;
    }
    session->final_arrival_evidence = true;
    const double distance_to_final_goal =
        has_canonical_final_goal(*session)
            ? std::hypot(canonical_final_goal(*session).x - position.x, canonical_final_goal(*session).y - position.y)
            : 0.0;
    LogInfo << "Final arrival evidence recorded." << VAR(reason) << VAR(verified_at_tail_consumption)
            << VAR_AS(evidence_index, session->canonical_final_goal_index) << VAR(distance_to_final_goal);
}

void commit_successful_completion(Session* session, const char* reason)
{
    session->success = true;
    UpdatePhase(session, NaviPhase::Finished, reason);
}

void leave_current_waypoint(Session* session, size_t next_index)
{
    session->current_node_idx = next_index;
    session->visit = {};
}

} // namespace

const char* navi_phase_name(NaviPhase phase)
{
    switch (phase) {
    case NaviPhase::Bootstrap:
        return "Bootstrap";
    case NaviPhase::Navigate:
        return "Navigate";
    case NaviPhase::WaitTransfer:
        return "WaitTransfer";
    case NaviPhase::WaitZipline:
        return "WaitZipline";
    case NaviPhase::WaitFind:
        return "WaitFind";
    case NaviPhase::WaitJumpPad:
        return "WaitJumpPad";
    case NaviPhase::WaitRope:
        return "WaitRope";
    case NaviPhase::Finished:
        return "Finished";
    case NaviPhase::Failed:
        return "Failed";
    }
    return "Unknown";
}

Session MakeSession(const std::vector<Waypoint>& path, const NaviPosition& initial_pos)
{
    Session session;
    session.original_path = path;
    session.current_path = path;
    session.current_zone_id = initial_pos.zone_id;
    session.canonical_final_goal_index = resolve_canonical_final_goal_index(path);
    return session;
}

std::optional<size_t> CurrentAbsoluteNodeIndex(const Session& session)
{
    return canonical_index_of(session, session.current_node_idx);
}

bool HasCurrentWaypoint(const Session& session)
{
    return session.current_node_idx < session.current_path.size();
}

const Waypoint& CurrentWaypoint(const Session& session)
{
    if (!require_current_waypoint(session, "CurrentWaypoint")) {
        return empty_waypoint();
    }
    return session.current_path[session.current_node_idx];
}

const Waypoint& CurrentPathAt(const Session& session, size_t index)
{
    if (!require_waypoint_index(session, index, "CurrentPathAt")) {
        return empty_waypoint();
    }
    return session.current_path[index];
}

std::optional<double> WalkedFloorY(const Session& session, const NaviPosition& position)
{
    if (!HasCurrentWaypoint(session)) {
        return std::nullopt;
    }
    const size_t current = session.current_node_idx;
    const Waypoint& to = session.current_path[current];
    const Waypoint* from = current > 0 && session.current_path[current - 1].has_position ? &session.current_path[current - 1] : nullptr;
    if (from == nullptr || !from->route_floor_y) {
        return to.route_floor_y;
    }
    if (!to.has_position || !to.route_floor_y) {
        return from->route_floor_y;
    }
    // 台沿下落的落点不插值: 沿下落方向越过它之前算台上
    if (to.drop_from) {
        const double ddx = to.x - (*to.drop_from)[0];
        const double ddy = to.y - (*to.drop_from)[1];
        return (position.x - to.x) * ddx + (position.y - to.y) * ddy < 0.0 ? from->route_floor_y : to.route_floor_y;
    }
    const double dx = to.x - from->x;
    const double dy = to.y - from->y;
    const double len_sq = dx * dx + dy * dy;
    const double t = len_sq > 0.0 ? std::clamp(((position.x - from->x) * dx + (position.y - from->y) * dy) / len_sq, 0.0, 1.0) : 1.0;
    return *from->route_floor_y + t * (*to.route_floor_y - *from->route_floor_y);
}

std::optional<size_t> CanonicalIndexAtCurrent(const Session& session)
{
    if (!require_current_waypoint(session, "CanonicalIndexAtCurrent")) {
        return std::nullopt;
    }
    return CanonicalIndexAtCurrentPath(session, session.current_node_idx);
}

std::optional<size_t> CanonicalIndexAtCurrentPath(const Session& session, size_t index)
{
    if (!require_waypoint_index(session, index, "CanonicalIndexAtCurrentPath")) {
        return std::nullopt;
    }
    return canonical_index_of(session, index);
}

std::optional<double> LandedTowerDeckY(const Session& session, const NaviPosition& position)
{
    if (session.visit.left_landed_deck) {
        return std::nullopt;
    }
    const ZiplineNodeRef* tower = session.visit.standing_tower ? &*session.visit.standing_tower : nullptr;
    if (tower == nullptr && session.current_node_idx != 0 && session.current_node_idx <= session.current_path.size()) {
        const Waypoint& hop = session.current_path[session.current_node_idx - 1];
        if (hop.action == ActionType::ZIPLINE && hop.zipline_hop) {
            tower = &hop.zipline_hop->landing;
        }
    }
    if (tower == nullptr || !tower->has_world || std::hypot(position.x - tower->x, position.y - tower->y) >= kZiplineLandingBandWu) {
        return std::nullopt;
    }
    return tower->height;
}

bool FinalSuccessReady(const Session& session, const NaviPosition& position)
{
    return !session.success && (session.final_arrival_evidence || has_reached_canonical_final_goal(session, position));
}

void CommitFinalSuccess(Session* session, const NaviPosition& position, const char* reason)
{
    if (!session->final_arrival_evidence) {
        record_final_arrival_evidence(session, position, session->route_tail_consumed, reason);
    }
    commit_successful_completion(session, reason);
}

void NoteCanonicalFinalGoalConsumed(
    Session* session,
    std::optional<size_t> consumed_absolute_index,
    const NaviPosition& position,
    const char* reason)
{
    if (!has_canonical_final_goal(*session) || !consumed_absolute_index
        || *consumed_absolute_index != session->canonical_final_goal_index) {
        return;
    }
    record_final_arrival_evidence(session, position, false, reason);
}

void ConcludeRouteTail(Session* session, const NaviPosition& position, const char* reason)
{
    session->route_tail_consumed = true;
    if (!has_canonical_final_goal(*session)) {
        commit_successful_completion(session, reason);
        return;
    }
    if (has_reached_canonical_final_goal(*session, position)) {
        record_final_arrival_evidence(session, position, true, "route_tail_consumed_in_final_goal_band");
    }
    if (session->final_arrival_evidence) {
        commit_successful_completion(session, reason);
        return;
    }
    LogError << "Route tail consumed without final success evidence." << VAR(position.x) << VAR(position.y) << VAR(position.angle)
             << VAR_AS(current_node_idx_, session->current_node_idx) << VAR_AS(path_origin_index_, session->path_origin_index);
    UpdatePhase(session, NaviPhase::Failed, "route_tail_without_final_success");
}

void UpdateCurrentZone(Session* session, const std::string& zone_id)
{
    session->current_zone_id = zone_id;
}

void LeaveCurrentWaypoint(Session* session, [[maybe_unused]] ActionType expected_action, const char* reason)
{
    if (!require_current_waypoint(*session, reason)) {
        return;
    }
    assert(session->current_path[session->current_node_idx].action == expected_action && "Unexpected action while leaving waypoint.");
    leave_current_waypoint(session, session->current_node_idx + 1);
}

void LeaveWaypointsThrough(Session* session, size_t waypoint_idx, const char* reason)
{
    if (!require_waypoint_index(*session, waypoint_idx, reason)) {
        return;
    }
    assert(waypoint_idx >= session->current_node_idx && "LeaveWaypointsThrough cannot move backward.");
    leave_current_waypoint(session, waypoint_idx + 1);
}

void NoteInteraction(Session* session)
{
    session->last_interaction_at = std::chrono::steady_clock::now();
}

bool RetargetCurrentWaypoint(Session* session, double x, double y, const char* reason)
{
    if (!require_current_waypoint(*session, "RetargetCurrentWaypoint")) {
        return false;
    }
    Waypoint& waypoint = session->current_path[session->current_node_idx];
    if (!waypoint.has_position) {
        return false;
    }
    LogInfo << "Waypoint retargeted." << VAR(reason) << VAR_AS(current_node_idx_, session->current_node_idx) << VAR(waypoint.x)
            << VAR(waypoint.y) << VAR(x) << VAR(y);
    waypoint.x = x;
    waypoint.y = y;
    return true;
}

bool ProgressClockTracks(const ProgressClock& clock, size_t key)
{
    return clock.initialized && clock.target_key == key;
}

void RestartProgressClock(ProgressClock* clock, size_t key, double actual_distance, TimePoint now)
{
    clock->target_key = key;
    clock->best_distance = actual_distance;
    clock->last_progress_time = now;
    clock->initialized = true;
}

void ObserveProgressClock(ProgressClock* clock, double actual_distance, TimePoint now)
{
    const double progress_epsilon = std::max(kNoProgressDistanceEpsilon, kMeasurementDefaultPositionQuantum);
    if (actual_distance < clock->best_distance - progress_epsilon) {
        clock->best_distance = actual_distance;
        clock->last_progress_time = now;
    }
}

void ResetProgress(Session* session)
{
    session->visit.progress = {};
}

int64_t StalledMs(const Session& session, TimePoint now)
{
    return clock_stalled_ms(session.visit.progress, now);
}

void ResetHardProgress(Session* session)
{
    session->visit.hard_progress = {};
}

int64_t HardStalledMs(const Session& session, TimePoint now)
{
    return clock_stalled_ms(session.visit.hard_progress, now);
}

void ExcludeFromStallClocks(Session* session, Duration paused)
{
    if (paused <= Duration::zero()) {
        return;
    }
    pause_clock(&session->visit.progress, paused);
    pause_clock(&session->visit.hard_progress, paused);
}

void LeaveLandedTowerDeck(Session* session)
{
    session->visit.left_landed_deck = true;
}

void NoteStandingTower(Session* session, const std::optional<ZiplineNodeRef>& tower)
{
    session->visit.standing_tower = tower;
}

void ApplyDynamicOverlay(Session* session, std::vector<Waypoint> generated_prefix, size_t continue_index, const NaviPosition& pos)
{
    // 越界下标会让下面的 insert 拿到反向区间, 绕成超大分配
    if (continue_index > session->original_path.size()) {
        LogError << "Dynamic overlay continue index is out of range." << VAR(continue_index)
                 << VAR_AS(original_path_.size(), session->original_path.size());
        continue_index = session->original_path.size();
    }

    const bool retargeted = continue_index != session->path_origin_index;
    session->current_path = std::move(generated_prefix);
    const size_t generated_count = session->current_path.size();
    session->current_path.insert(
        session->current_path.end(),
        session->original_path.begin() + static_cast<std::ptrdiff_t>(continue_index),
        session->original_path.end());

    session->path_origin_index = continue_index;
    session->generated_prefix_size = generated_count;
    session->current_node_idx = 0;
    session->current_zone_id = pos.zone_id;
    ResetProgress(session);
    if (retargeted) {
        ResetHardProgress(session);
    }
    LogInfo << "Dynamic route overlay applied." << VAR(generated_count) << VAR(continue_index)
            << VAR_AS(current_path_.size(), session->current_path.size()) << VAR(pos.x) << VAR(pos.y) << VAR(pos.zone_id);
}

void ReplaceRoute(Session* session, std::vector<Waypoint> path, const NaviPosition& pos, const char* reason)
{
    session->original_path = std::move(path);
    session->current_path = session->original_path;
    session->path_origin_index = 0;
    session->generated_prefix_size = 0;
    session->current_node_idx = 0;
    session->current_zone_id = pos.zone_id;
    session->canonical_final_goal_index = resolve_canonical_final_goal_index(session->original_path);
    session->route_tail_consumed = false;
    ResetProgress(session);
    ResetHardProgress(session);
    LogInfo << "Navigation route replaced." << VAR(reason) << VAR_AS(original_path_.size(), session->original_path.size())
            << VAR_AS(canonical_final_goal_index_, session->canonical_final_goal_index) << VAR(pos.x) << VAR(pos.y) << VAR(pos.zone_id);
}

void UpdatePhase(Session* session, NaviPhase next_phase, const char* reason)
{
    if (session->phase == next_phase) {
        return;
    }
    const char* from_phase_name = navi_phase_name(session->phase);
    const char* to_phase_name = navi_phase_name(next_phase);
    LogInfo << "Phase transition." << VAR(from_phase_name) << VAR(to_phase_name) << VAR(reason)
            << VAR_AS(current_node_idx_, session->current_node_idx) << VAR_AS(path_origin_index_, session->path_origin_index);
    session->phase = next_phase;
}

} // namespace mapnavigator
