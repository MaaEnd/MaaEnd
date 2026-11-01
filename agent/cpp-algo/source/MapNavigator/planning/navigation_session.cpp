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
        if (path[index - 1].hasPosition()) {
            return index - 1;
        }
    }
    return std::numeric_limits<size_t>::max();
}

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

} // namespace

void ProgressClock::observe(size_t key, double actual_distance, std::chrono::steady_clock::time_point now)
{
    const double progress_epsilon = std::max(kNoProgressDistanceEpsilon, kMeasurementDefaultPositionQuantum);
    if (!initialized || target_key != key) {
        target_key = key;
        best_distance = actual_distance;
        last_progress_time = now;
        initialized = true;
        return;
    }

    if (actual_distance < best_distance - progress_epsilon) {
        best_distance = actual_distance;
        last_progress_time = now;
    }
}

int64_t ProgressClock::stalledMs(std::chrono::steady_clock::time_point now) const
{
    if (!initialized || IsUnset(last_progress_time)) {
        return 0;
    }
    return ElapsedMs(last_progress_time, now);
}

void ProgressClock::pause(std::chrono::steady_clock::duration paused)
{
    if (initialized && !IsUnset(last_progress_time)) {
        last_progress_time += paused;
    }
}

NavigationSession::NavigationSession(const std::vector<Waypoint>& path, const NaviPosition& initial_pos)
    : original_path_(path)
    , current_path_(path)
    , current_zone_id_(initial_pos.zone_id)
    , canonical_final_goal_index_(resolve_canonical_final_goal_index(path))
{
}

const std::vector<Waypoint>& NavigationSession::originalPath() const
{
    return original_path_;
}

const std::vector<Waypoint>& NavigationSession::currentPath() const
{
    return current_path_;
}

size_t NavigationSession::pathOriginIndex() const
{
    return path_origin_index_;
}

size_t NavigationSession::currentNodeIdx() const
{
    return current_node_idx_;
}

std::optional<size_t> NavigationSession::currentAbsoluteNodeIndex() const
{
    return canonicalIndexOf(current_node_idx_);
}

bool NavigationSession::hasCanonicalFinalGoal() const
{
    return canonical_final_goal_index_ < original_path_.size();
}

const Waypoint& NavigationSession::canonicalFinalGoal() const
{
    if (!hasCanonicalFinalGoal()) {
        LogError << "CanonicalFinalGoal requires a position node." << VAR(canonical_final_goal_index_) << VAR(original_path_.size());
        return empty_waypoint();
    }
    return original_path_[canonical_final_goal_index_];
}

double NavigationSession::finalGoalAcceptanceBand() const
{
    if (!hasCanonicalFinalGoal()) {
        return 0.0;
    }
    const Waypoint& final_goal = canonicalFinalGoal();
    return final_goal.requiresStrictArrival() ? kStrictArrivalLookaheadRadius : final_goal.getLookahead();
}

bool NavigationSession::hasReachedCanonicalFinalGoal(const NaviPosition& position) const
{
    if (!hasCanonicalFinalGoal()) {
        return false;
    }
    const Waypoint& final_goal = canonicalFinalGoal();
    if (!final_goal.zone_id.empty() && position.zone_id != final_goal.zone_id) {
        return false;
    }
    const double distance = std::hypot(final_goal.x - position.x, final_goal.y - position.y);
    return distance <= finalGoalAcceptanceBand();
}

bool NavigationSession::hasSatisfiedFinalSuccess(const NaviPosition& position, const char* reason)
{
    if (success_) {
        return true;
    }
    if (!final_arrival_evidence_ && !hasReachedCanonicalFinalGoal(position)) {
        return false;
    }
    if (!final_arrival_evidence_) {
        recordFinalArrivalEvidence(position, route_tail_consumed_, reason);
    }
    commitSuccessfulCompletion(reason);
    return true;
}

void NavigationSession::recordFinalArrivalEvidence(const NaviPosition& position, bool verified_at_tail_consumption, const char* reason)
{
    if (final_arrival_evidence_) {
        return;
    }
    final_arrival_evidence_ = true;
    const double distance_to_final_goal =
        hasCanonicalFinalGoal() ? std::hypot(canonicalFinalGoal().x - position.x, canonicalFinalGoal().y - position.y) : 0.0;
    LogInfo << "Final arrival evidence recorded." << VAR(reason) << VAR(verified_at_tail_consumption)
            << VAR_AS(evidence_index, canonical_final_goal_index_) << VAR(distance_to_final_goal);
}

void NavigationSession::commitSuccessfulCompletion(const char* reason)
{
    success_ = true;
    updatePhase(NaviPhase::Finished, reason);
}

void NavigationSession::noteCanonicalFinalGoalConsumed(
    std::optional<size_t> consumed_absolute_index,
    const NaviPosition& position,
    const char* reason)
{
    if (!hasCanonicalFinalGoal() || !consumed_absolute_index || *consumed_absolute_index != canonical_final_goal_index_) {
        return;
    }
    recordFinalArrivalEvidence(position, false, reason);
}

void NavigationSession::noteRouteTailConsumed(const NaviPosition& position, const char* reason)
{
    route_tail_consumed_ = true;
    if (!hasCanonicalFinalGoal()) {
        commitSuccessfulCompletion(reason);
        return;
    }
    if (hasReachedCanonicalFinalGoal(position)) {
        recordFinalArrivalEvidence(position, true, "route_tail_consumed_in_final_goal_band");
    }
    if (final_arrival_evidence_) {
        commitSuccessfulCompletion(reason);
        return;
    }
    LogError << "Route tail consumed without final success evidence." << VAR(position.x) << VAR(position.y) << VAR(position.angle)
             << VAR(current_node_idx_) << VAR(path_origin_index_);
    updatePhase(NaviPhase::Failed, "route_tail_without_final_success");
}

bool NavigationSession::success() const
{
    return success_;
}

bool NavigationSession::hasCurrentWaypoint() const
{
    return current_node_idx_ < current_path_.size();
}

const Waypoint& NavigationSession::currentWaypoint() const
{
    if (!requireCurrentWaypoint("CurrentWaypoint")) {
        return empty_waypoint();
    }
    return current_path_[current_node_idx_];
}

const Waypoint& NavigationSession::currentPathAt(size_t index) const
{
    if (!requireWaypointIndex(index, "CurrentPathAt")) {
        return empty_waypoint();
    }
    return current_path_[index];
}

std::optional<double> NavigationSession::walkedFloorY(const NaviPosition& position) const
{
    if (!hasCurrentWaypoint()) {
        return std::nullopt;
    }
    const Waypoint& to = current_path_[current_node_idx_];
    const Waypoint* from =
        current_node_idx_ > 0 && current_path_[current_node_idx_ - 1].hasPosition() ? &current_path_[current_node_idx_ - 1] : nullptr;
    if (from == nullptr || !from->route_floor_y) {
        return to.route_floor_y;
    }
    if (!to.hasPosition() || !to.route_floor_y) {
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

std::optional<size_t> NavigationSession::canonicalIndexAtCurrent() const
{
    if (!requireCurrentWaypoint("CanonicalIndexAtCurrent")) {
        return std::nullopt;
    }
    return canonicalIndexAtCurrentPath(current_node_idx_);
}

std::optional<size_t> NavigationSession::canonicalIndexAtCurrentPath(size_t index) const
{
    if (!requireWaypointIndex(index, "CanonicalIndexAtCurrentPath")) {
        return std::nullopt;
    }
    return canonicalIndexOf(index);
}

std::optional<size_t> NavigationSession::canonicalIndexOf(size_t index) const
{
    if (index < generated_prefix_size_) {
        return std::nullopt;
    }
    return path_origin_index_ + index - generated_prefix_size_;
}

const std::string& NavigationSession::currentZoneId() const
{
    return current_zone_id_;
}

void NavigationSession::updateCurrentZone(const std::string& zone_id)
{
    current_zone_id_ = zone_id;
}

void NavigationSession::advanceToNextWaypoint(const char* reason)
{
    if (!requireCurrentWaypoint(reason)) {
        return;
    }
    leaveCurrentWaypoint(current_node_idx_ + 1);
}

void NavigationSession::advanceToNextWaypoint(ActionType expected_action, const char* reason)
{
    (void)expected_action;
    if (!requireCurrentWaypoint(reason)) {
        return;
    }
    assert(current_path_[current_node_idx_].action == expected_action && "Unexpected action while advancing waypoint.");
    advanceToNextWaypoint(reason);
}

void NavigationSession::skipPastWaypoint(size_t waypoint_idx, const char* reason)
{
    if (!requireWaypointIndex(waypoint_idx, reason)) {
        return;
    }
    assert(waypoint_idx >= current_node_idx_ && "SkipPastWaypoint cannot move backward.");
    leaveCurrentWaypoint(waypoint_idx + 1);
}

void NavigationSession::leaveCurrentWaypoint(size_t next_index)
{
    current_node_idx_ = next_index;
    left_landed_deck_ = false;
    standing_tower_.reset();
    resetProgress();
    resetHardProgress();
}

bool NavigationSession::retargetCurrentWaypoint(double x, double y, const char* reason)
{
    if (!requireCurrentWaypoint("RetargetCurrentWaypoint")) {
        return false;
    }
    Waypoint& waypoint = current_path_[current_node_idx_];
    if (!waypoint.hasPosition()) {
        return false;
    }
    LogInfo << "Waypoint retargeted." << VAR(reason) << VAR(current_node_idx_) << VAR(waypoint.x) << VAR(waypoint.y) << VAR(x) << VAR(y);
    waypoint.x = x;
    waypoint.y = y;
    // 旧坐标上攒的最好距离会让无进展判据误以为人在倒退
    resetProgress();
    return true;
}

void NavigationSession::resetProgress()
{
    progress_.reset();
}

void NavigationSession::observeProgress(size_t waypoint_idx, double actual_distance, const std::chrono::steady_clock::time_point& now)
{
    progress_.observe(waypoint_idx, actual_distance, now);
}

int64_t NavigationSession::stalledMs(const std::chrono::steady_clock::time_point& now) const
{
    return progress_.stalledMs(now);
}

double NavigationSession::bestDistanceToTarget() const
{
    return progress_.best_distance;
}

void NavigationSession::observeHardProgress(size_t target_key, double actual_distance, const std::chrono::steady_clock::time_point& now)
{
    hard_progress_.observe(target_key, actual_distance, now);
}

int64_t NavigationSession::hardStalledMs(const std::chrono::steady_clock::time_point& now) const
{
    return hard_progress_.stalledMs(now);
}

void NavigationSession::resetHardProgress()
{
    hard_progress_.reset();
}

void NavigationSession::excludeFromStallClocks(std::chrono::steady_clock::duration paused)
{
    if (paused <= std::chrono::steady_clock::duration::zero()) {
        return;
    }
    progress_.pause(paused);
    hard_progress_.pause(paused);
}

std::optional<double> NavigationSession::landedTowerDeckY(const NaviPosition& position) const
{
    if (left_landed_deck_) {
        return std::nullopt;
    }
    const ZiplineNodeRef* tower = standing_tower_ ? &*standing_tower_ : nullptr;
    if (tower == nullptr && current_node_idx_ != 0 && current_node_idx_ <= current_path_.size()) {
        const Waypoint& hop = current_path_[current_node_idx_ - 1];
        if (hop.action == ActionType::ZIPLINE && hop.zipline_hop) {
            tower = &hop.zipline_hop->landing;
        }
    }
    if (tower == nullptr || !tower->has_world || std::hypot(position.x - tower->x, position.y - tower->y) >= kZiplineLandingBandWu) {
        return std::nullopt;
    }
    return tower->height;
}

void NavigationSession::leaveLandedTowerDeck()
{
    left_landed_deck_ = true;
}

void NavigationSession::noteStandingTower(const std::optional<ZiplineNodeRef>& tower)
{
    standing_tower_ = tower;
}

void NavigationSession::applyDynamicOverlay(std::vector<Waypoint> generated_prefix, size_t continue_index, const NaviPosition& pos)
{
    // 越界下标会让下面的 insert 拿到反向区间, 绕成超大分配
    if (continue_index > original_path_.size()) {
        LogError << "Dynamic overlay continue index is out of range." << VAR(continue_index) << VAR(original_path_.size());
        continue_index = original_path_.size();
    }

    const bool retargeted = continue_index != path_origin_index_;
    current_path_ = std::move(generated_prefix);
    const size_t generated_count = current_path_.size();
    current_path_.insert(current_path_.end(), original_path_.begin() + static_cast<std::ptrdiff_t>(continue_index), original_path_.end());

    path_origin_index_ = continue_index;
    generated_prefix_size_ = generated_count;
    current_node_idx_ = 0;
    current_zone_id_ = pos.zone_id;
    resetProgress();
    if (retargeted) {
        resetHardProgress();
    }
    LogInfo << "Dynamic route overlay applied." << VAR(generated_count) << VAR(continue_index) << VAR(current_path_.size()) << VAR(pos.x)
            << VAR(pos.y) << VAR(pos.zone_id);
}

void NavigationSession::replaceRoute(std::vector<Waypoint> path, const NaviPosition& pos, const char* reason)
{
    original_path_ = std::move(path);
    current_path_ = original_path_;
    path_origin_index_ = 0;
    generated_prefix_size_ = 0;
    current_node_idx_ = 0;
    current_zone_id_ = pos.zone_id;
    canonical_final_goal_index_ = resolve_canonical_final_goal_index(original_path_);
    route_tail_consumed_ = false;
    resetProgress();
    resetHardProgress();
    LogInfo << "Navigation route replaced." << VAR(reason) << VAR(original_path_.size()) << VAR(canonical_final_goal_index_) << VAR(pos.x)
            << VAR(pos.y) << VAR(pos.zone_id);
}

NaviPhase NavigationSession::phase() const
{
    return phase_;
}

void NavigationSession::updatePhase(NaviPhase next_phase, const char* reason)
{
    if (phase_ == next_phase) {
        return;
    }
    const char* from_phase_name = navi_phase_name(phase_);
    const char* to_phase_name = navi_phase_name(next_phase);
    LogInfo << "Phase transition." << VAR(from_phase_name) << VAR(to_phase_name) << VAR(reason) << VAR(current_node_idx_)
            << VAR(path_origin_index_);
    phase_ = next_phase;
}

bool NavigationSession::requireCurrentWaypoint(const char* reason) const
{
    if (hasCurrentWaypoint()) {
        return true;
    }
    LogError << "Current waypoint is required." << VAR(reason) << VAR(current_node_idx_) << VAR(current_path_.size());
    return false;
}

bool NavigationSession::requireWaypointIndex(size_t index, const char* reason) const
{
    if (index < current_path_.size()) {
        return true;
    }
    LogError << "Waypoint index is out of range." << VAR(reason) << VAR(index) << VAR(current_path_.size());
    return false;
}

} // namespace mapnavigator
