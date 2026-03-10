#include <algorithm>
#include <cmath>
#include <limits>
#include <unordered_map>

#include "navi_config.h"
#include "navi_math.h"
#include "route_rejoin.h"

namespace mapnavigator
{

namespace
{

bool IsZoneCompatible(const Waypoint& waypoint, const std::string& current_zone_id)
{
    if (!waypoint.HasPosition()) {
        return false;
    }
    if (current_zone_id.empty() || waypoint.zone_id.empty()) {
        return true;
    }
    return waypoint.zone_id == current_zone_id;
}

bool CanUseSegment(const Waypoint& from, const Waypoint& to, const std::string& current_zone_id)
{
    if (!IsZoneCompatible(from, current_zone_id) || !IsZoneCompatible(to, current_zone_id)) {
        return false;
    }
    if (from.RequiresStrictArrival() || to.RequiresStrictArrival()) {
        return false;
    }
    if (!from.zone_id.empty() && !to.zone_id.empty() && from.zone_id != to.zone_id) {
        return false;
    }
    return true;
}

double ComputeIndexBias(size_t candidate_index, size_t preferred_index, int backtrack_window)
{
    if (candidate_index + static_cast<size_t>(std::max(backtrack_window, 0)) < preferred_index) {
        return static_cast<double>(preferred_index - candidate_index) * kRejoinBacktrackPenaltyPerNode;
    }
    if (candidate_index > preferred_index) {
        const size_t forward_offset = std::min(candidate_index - preferred_index, static_cast<size_t>(kRejoinForwardBonusMaxNodes));
        return -static_cast<double>(forward_offset) * kRejoinForwardBonusPerNode;
    }
    return 0.0;
}

double ComputeApproachPenalty(const NaviPosition& pos, double heading_degrees, double target_x, double target_y, double distance)
{
    if (distance <= std::numeric_limits<double>::epsilon()) {
        return 0.0;
    }

    const double approach_heading = NaviMath::CalcTargetRotation(pos.x, pos.y, target_x, target_y);
    const double heading_delta = std::abs(NaviMath::NormalizeAngle(approach_heading - heading_degrees));
    if (distance > kRejoinCloseApproachDistance && heading_delta >= kRejoinReverseRejectDegrees) {
        return std::numeric_limits<double>::infinity();
    }
    return heading_delta * kRejoinHeadingPenaltyWeight;
}

} // namespace

RouteRejoinPlanner::RouteRejoinPlanner(double abort_distance, int candidate_limit, int backtrack_window)
    : abort_distance_(abort_distance)
    , candidate_limit_(std::max(candidate_limit, 1))
    , backtrack_window_(std::max(backtrack_window, 0))
{
}

RouteRejoinPlan
    RouteRejoinPlanner::Plan(const NaviPosition& pos, double heading_degrees, const std::vector<Waypoint>& path, size_t preferred_index)
        const
{
    RouteRejoinPlan plan;
    plan.nearest_route_distance = std::numeric_limits<double>::infinity();

    if (path.empty()) {
        plan.abort = true;
        return plan;
    }

    preferred_index = std::min(preferred_index, path.size() - 1);
    std::vector<RouteRejoinCandidate> raw_candidates;

    for (size_t i = 0; i < path.size(); ++i) {
        const Waypoint& waypoint = path[i];
        if (!IsZoneCompatible(waypoint, pos.zone_id)) {
            continue;
        }

        const double waypoint_distance = std::hypot(pos.x - waypoint.x, pos.y - waypoint.y);
        plan.nearest_route_distance = std::min(plan.nearest_route_distance, waypoint_distance);

        const double approach_penalty = ComputeApproachPenalty(pos, heading_degrees, waypoint.x, waypoint.y, waypoint_distance);
        if (!std::isfinite(approach_penalty)) {
            continue;
        }

        RouteRejoinCandidate candidate;
        candidate.decision = RejoinDecisionType::Waypoint;
        candidate.continue_index = i;
        candidate.route_distance = waypoint_distance;
        candidate.score = waypoint_distance + approach_penalty + ComputeIndexBias(i, preferred_index, backtrack_window_);
        raw_candidates.push_back(candidate);
    }

    for (size_t i = 0; i + 1 < path.size(); ++i) {
        const Waypoint& from = path[i];
        const Waypoint& to = path[i + 1];
        if (!CanUseSegment(from, to, pos.zone_id)) {
            continue;
        }

        const double seg_x = to.x - from.x;
        const double seg_y = to.y - from.y;
        const double seg_len_sq = seg_x * seg_x + seg_y * seg_y;
        if (seg_len_sq <= std::numeric_limits<double>::epsilon()) {
            continue;
        }

        const double rel_x = pos.x - from.x;
        const double rel_y = pos.y - from.y;
        const double raw_projection = (rel_x * seg_x + rel_y * seg_y) / seg_len_sq;
        const double clamped_projection = std::clamp(raw_projection, 0.0, 1.0);
        const double projected_x = from.x + seg_x * clamped_projection;
        const double projected_y = from.y + seg_y * clamped_projection;
        const double route_distance = std::hypot(pos.x - projected_x, pos.y - projected_y);
        plan.nearest_route_distance = std::min(plan.nearest_route_distance, route_distance);

        size_t continue_index = i + 1;
        if (clamped_projection <= kRejoinSegmentFrontThreshold && i >= preferred_index) {
            continue_index = i;
        }
        else if (
            clamped_projection <= kRejoinSegmentMiddleThreshold
            && std::hypot(pos.x - from.x, pos.y - from.y) + kRejoinSegmentContinueBiasDistance < std::hypot(pos.x - to.x, pos.y - to.y)
            && i + static_cast<size_t>(backtrack_window_) >= preferred_index) {
            continue_index = i;
        }

        const double approach_penalty = ComputeApproachPenalty(pos, heading_degrees, projected_x, projected_y, route_distance);
        if (!std::isfinite(approach_penalty)) {
            continue;
        }

        RouteRejoinCandidate candidate;
        candidate.decision = RejoinDecisionType::Segment;
        candidate.continue_index = continue_index;
        candidate.route_distance = route_distance;
        candidate.score = route_distance + approach_penalty + ComputeIndexBias(continue_index, preferred_index, backtrack_window_)
                          - kRejoinSegmentPreferenceBonus;
        raw_candidates.push_back(candidate);
    }

    if (!std::isfinite(plan.nearest_route_distance) || plan.nearest_route_distance > abort_distance_) {
        plan.abort = true;
        plan.candidates.clear();
        return plan;
    }

    if (raw_candidates.empty()) {
        plan.abort = true;
        return plan;
    }

    std::sort(raw_candidates.begin(), raw_candidates.end(), [](const RouteRejoinCandidate& lhs, const RouteRejoinCandidate& rhs) {
        if (lhs.score != rhs.score) {
            return lhs.score < rhs.score;
        }
        if (lhs.route_distance != rhs.route_distance) {
            return lhs.route_distance < rhs.route_distance;
        }
        return lhs.continue_index < rhs.continue_index;
    });

    std::unordered_map<size_t, RouteRejoinCandidate> deduped;
    for (const auto& candidate : raw_candidates) {
        auto it = deduped.find(candidate.continue_index);
        if (it == deduped.end() || candidate.score < it->second.score) {
            deduped[candidate.continue_index] = candidate;
        }
    }

    plan.candidates.reserve(deduped.size());
    for (const auto& [_, candidate] : deduped) {
        plan.candidates.push_back(candidate);
    }

    std::sort(plan.candidates.begin(), plan.candidates.end(), [](const RouteRejoinCandidate& lhs, const RouteRejoinCandidate& rhs) {
        if (lhs.score != rhs.score) {
            return lhs.score < rhs.score;
        }
        if (lhs.route_distance != rhs.route_distance) {
            return lhs.route_distance < rhs.route_distance;
        }
        return lhs.continue_index < rhs.continue_index;
    });

    if (static_cast<int>(plan.candidates.size()) > candidate_limit_) {
        plan.candidates.resize(static_cast<size_t>(candidate_limit_));
    }

    return plan;
}

} // namespace mapnavigator
