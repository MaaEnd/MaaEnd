#include "nav_run_controller.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <optional>
#include <utility>

#include <MaaUtils/Logger.h>

#include "navi_config.h"
#include "navi_controller.h"
#include "navi_math.h"
#include "navigation_runtime_state.h"
#include "navigation_session.h"
#include "navmesh_path_expander.h"
#include "route_tracker.h"

namespace mapnavigator
{

namespace
{

struct CorridorProjection
{
    size_t edge_idx = 0;
    double t = 0.0;
    navmesh::WorldPoint point {};
    double cross_track = std::numeric_limits<double>::infinity();
};

bool CanUseNavRunSteering(const Waypoint& waypoint)
{
    return waypoint.HasPosition() && waypoint.action == ActionType::RUN;
}

std::optional<CorridorProjection>
    ProjectOntoCorridor(const navmesh::WorldPath& path, size_t start_edge, const NaviPosition& position)
{
    if (path.points.size() < 2) {
        return std::nullopt;
    }
    const size_t num_edges = path.points.size() - 1;
    if (start_edge >= num_edges) {
        start_edge = num_edges - 1;
    }
    std::optional<CorridorProjection> best;
    for (size_t edge = start_edge; edge < num_edges; ++edge) {
        const navmesh::WorldPoint& a = path.points[edge];
        const navmesh::WorldPoint& b = path.points[edge + 1];
        const double dx = b.x - a.x;
        const double dy = b.y - a.y;
        const double len_sq = dx * dx + dy * dy;
        if (len_sq <= std::numeric_limits<double>::epsilon()) {
            continue;
        }
        const double raw_t = ((position.x - a.x) * dx + (position.y - a.y) * dy) / len_sq;
        const double t = std::clamp(raw_t, 0.0, 1.0);
        const double px = a.x + t * dx;
        const double py = a.y + t * dy;
        const double cross = std::hypot(position.x - px, position.y - py);
        if (!best || cross < best->cross_track) {
            best = CorridorProjection {
                .edge_idx = edge,
                .t = t,
                .point = { .x = px, .y = py },
                .cross_track = cross,
            };
        }
    }
    return best;
}

double RemainingAlongCorridor(const navmesh::WorldPath& path, const CorridorProjection& projection)
{
    if (path.points.size() < 2 || projection.edge_idx + 1 >= path.points.size()) {
        return 0.0;
    }
    double total = std::hypot(
        path.points[projection.edge_idx + 1].x - projection.point.x,
        path.points[projection.edge_idx + 1].y - projection.point.y);
    for (size_t edge = projection.edge_idx + 1; edge + 1 < path.points.size(); ++edge) {
        const navmesh::WorldPoint& a = path.points[edge];
        const navmesh::WorldPoint& b = path.points[edge + 1];
        total += std::hypot(b.x - a.x, b.y - a.y);
    }
    return total;
}

std::vector<double> BuildCorridorArcPrefix(const navmesh::WorldPath& path)
{
    std::vector<double> prefix(path.points.size(), 0.0);
    for (size_t edge = 0; edge + 1 < path.points.size(); ++edge) {
        const navmesh::WorldPoint& a = path.points[edge];
        const navmesh::WorldPoint& b = path.points[edge + 1];
        prefix[edge + 1] = prefix[edge] + std::hypot(b.x - a.x, b.y - a.y);
    }
    return prefix;
}

double CorridorArcLengthTo(const navmesh::WorldPath& path, const std::vector<double>& arc_prefix, const CorridorProjection& projection)
{
    if (projection.edge_idx >= arc_prefix.size() || projection.edge_idx >= path.points.size()) {
        return std::numeric_limits<double>::infinity();
    }
    const navmesh::WorldPoint& edge_start = path.points[projection.edge_idx];
    return arc_prefix[projection.edge_idx] + std::hypot(projection.point.x - edge_start.x, projection.point.y - edge_start.y);
}

bool IsContinuousRunWaypoint(const Waypoint& waypoint)
{
    return waypoint.HasPosition() && waypoint.action == ActionType::RUN && !waypoint.RequiresStrictArrival();
}

// Count how many upcoming continuous-RUN session waypoints the corridor has already carried the
// agent past, scanning forward from the current index. A waypoint counts as passed when its
// closest point on the corridor lies at or behind the agent's own corridor arc-length. The scan
// stops at the anchor or at the first required-semantic / control node so the corridor never
// skips a mandatory stop. This lets the serial index follow corridor progress even when the agent
// has deviated far enough from the original waypoint line that serial cross-track tracking gives
// up advancing it.
size_t CountCorridorPassedRunWaypoints(
    const NavigationSession& session,
    const navmesh::WorldPath& path,
    const std::vector<double>& arc_prefix,
    size_t anchor_index,
    double character_arc)
{
    if (!std::isfinite(character_arc)) {
        return 0;
    }

    const std::vector<Waypoint>& waypoints = session.current_path();
    const double margin = std::max(kMeasurementDefaultPositionQuantum, 0.0);
    size_t count = 0;
    for (size_t index = session.current_node_idx(); index < waypoints.size(); ++index) {
        const std::optional<size_t> canonical = session.CanonicalIndexAtCurrentPath(index);
        if (canonical && *canonical == anchor_index) {
            break;
        }
        const Waypoint& waypoint = waypoints[index];
        if (!IsContinuousRunWaypoint(waypoint)) {
            break;
        }
        const NaviPosition waypoint_pos { .x = waypoint.x, .y = waypoint.y };
        const std::optional<CorridorProjection> waypoint_projection = ProjectOntoCorridor(path, 0, waypoint_pos);
        if (!waypoint_projection) {
            break;
        }
        if (CorridorArcLengthTo(path, arc_prefix, *waypoint_projection) > character_arc + margin) {
            break;
        }
        ++count;
    }
    return count;
}

navmesh::WorldPoint
    LookaheadOnCorridor(const navmesh::WorldPath& path, const CorridorProjection& projection, double distance)
{
    if (path.points.empty()) {
        return projection.point;
    }
    if (path.points.size() < 2) {
        return path.points.front();
    }
    const size_t num_edges = path.points.size() - 1;
    const navmesh::WorldPoint& edge_end = path.points[projection.edge_idx + 1];
    const double dx0 = edge_end.x - projection.point.x;
    const double dy0 = edge_end.y - projection.point.y;
    const double remaining_on_edge = std::hypot(dx0, dy0);

    if (remaining_on_edge >= distance) {
        if (remaining_on_edge <= std::numeric_limits<double>::epsilon()) {
            return edge_end;
        }
        const double scale = distance / remaining_on_edge;
        return { .x = projection.point.x + dx0 * scale, .y = projection.point.y + dy0 * scale };
    }
    distance -= remaining_on_edge;

    for (size_t edge = projection.edge_idx + 1; edge < num_edges; ++edge) {
        const navmesh::WorldPoint& a = path.points[edge];
        const navmesh::WorldPoint& b = path.points[edge + 1];
        const double dx = b.x - a.x;
        const double dy = b.y - a.y;
        const double len = std::hypot(dx, dy);
        if (len >= distance) {
            if (len <= std::numeric_limits<double>::epsilon()) {
                return b;
            }
            const double scale = distance / len;
            return { .x = a.x + dx * scale, .y = a.y + dy * scale };
        }
        distance -= len;
    }
    return path.points.back();
}

double UpcomingCorridorTurnDeg(
    const navmesh::WorldPath& path,
    const CorridorProjection& projection,
    double lookahead_distance)
{
    if (path.points.size() < 2 || projection.edge_idx + 1 >= path.points.size() || lookahead_distance <= 0.0) {
        return 0.0;
    }

    const size_t num_edges = path.points.size() - 1;
    navmesh::WorldPoint segment_start = projection.point;
    std::optional<double> base_heading;
    double max_turn = 0.0;
    double remaining = lookahead_distance;

    for (size_t edge = projection.edge_idx; edge < num_edges && remaining > 0.0; ++edge) {
        const navmesh::WorldPoint& segment_end = path.points[edge + 1];
        const double dx = segment_end.x - segment_start.x;
        const double dy = segment_end.y - segment_start.y;
        const double length = std::hypot(dx, dy);
        if (length > std::numeric_limits<double>::epsilon()) {
            const double heading = NaviMath::CalcTargetRotation(segment_start.x, segment_start.y, segment_end.x, segment_end.y);
            if (!base_heading) {
                base_heading = heading;
            }
            max_turn = std::max(max_turn, std::abs(NaviMath::NormalizeAngle(heading - *base_heading)));
            remaining -= length;
        }
        segment_start = segment_end;
    }

    return max_turn;
}

int64_t ElapsedMs(std::chrono::steady_clock::time_point from, std::chrono::steady_clock::time_point to)
{
    if (from.time_since_epoch().count() == 0) {
        return std::numeric_limits<int64_t>::max();
    }
    return std::chrono::duration_cast<std::chrono::milliseconds>(to - from).count();
}

} // namespace

void NavRunController::invalidate()
{
    plan_ = NavRunPlan {};
    last_progress_seen_ = {};
    last_remaining_to_anchor_ = std::numeric_limits<double>::infinity();
}

bool NavRunController::buildPlan(
    const NaviParam& param,
    const NaviPosition& position,
    size_t anchor_index,
    const Waypoint& anchor,
    NavRunReplanReason reason,
    std::chrono::steady_clock::time_point now)
{
    const navmesh::WorldPoint start { .x = position.x, .y = position.y };
    const navmesh::WorldPoint goal { .x = anchor.x, .y = anchor.y };
    auto route = PlanNavmeshRoute(param, position.zone_id, start, goal);
    if (!route || !route->ok() || route->path.points.size() < 2) {
        LogDebug << "NavRunController plan build failed." << VAR(static_cast<int>(reason)) << VAR(anchor_index)
                 << VAR(position.zone_id);
        return false;
    }

    plan_.valid = true;
    plan_.zone_id = position.zone_id;
    plan_.anchor_index = anchor_index;
    plan_.anchor_pos = { .x = anchor.x, .y = anchor.y };
    plan_.path = std::move(route->path);
    plan_.corridor_arc_prefix = BuildCorridorArcPrefix(plan_.path);
    plan_.cursor = 0;
    plan_.planned_at = now;
    return true;
}

double NavRunController::chooseLookaheadDistance(
    const RouteTrackingState& route, bool sprint_active, double upcoming_turn_deg) const
{
    if (upcoming_turn_deg >= kNavRunSharpTurnDeg) {
        return kNavRunLookaheadSharpTurnM;
    }
    if (!route.startup_motion_confirmed) {
        return kNavRunLookaheadLowSpeedM;
    }
    if (sprint_active) {
        return kNavRunLookaheadSprintM;
    }
    return kNavRunLookaheadWalkM;
}

NavRunReplanReason
    NavRunController::detectReplanTrigger(const RouteTrackingState& route, std::chrono::steady_clock::time_point now) const
{
    if (route.startup_motion_confirmed && ElapsedMs(last_progress_seen_, now) >= kNavRunProgressRegressionMs) {
        return NavRunReplanReason::ProgressRegression;
    }
    return NavRunReplanReason::None;
}

NavRunTickResult NavRunController::tick(
    NavigationSession* session,
    NavigationRuntimeState* runtime,
    const NaviPosition& position,
    const RouteTrackingState& route,
    const NaviParam& param,
    size_t anchor_index,
    const Waypoint& anchor,
    bool sprint_active,
    std::chrono::steady_clock::time_point now)
{
    NavRunTickResult result;

    if (runtime->nav_run_dirty) {
        invalidate();
        runtime->nav_run_dirty = false;
    }

    if (!anchor.HasPosition() || !session->HasCurrentWaypoint()) {
        return result;
    }
    if (!CanUseNavRunSteering(session->CurrentWaypoint())) {
        return result;
    }

    if (plan_.valid) {
        if (plan_.anchor_index != anchor_index) {
            invalidate();
        }
        else if (!position.zone_id.empty() && plan_.zone_id != position.zone_id) {
            invalidate();
        }
    }

    if (!plan_.valid) {
        if (!buildPlan(param, position, anchor_index, anchor, NavRunReplanReason::AnchorChanged, now)) {
            return result;
        }
        last_progress_seen_ = now;
        last_remaining_to_anchor_ = std::numeric_limits<double>::infinity();
    }

    auto projection = ProjectOntoCorridor(plan_.path, plan_.cursor, position);
    if (!projection) {
        invalidate();
        return result;
    }
    plan_.cursor = projection->edge_idx;

    const bool hard_off = projection->cross_track > kNavRunCrossTrackFailM;
    const bool soft_off = projection->cross_track > kNavRunCrossTrackWarnM;
    NavRunReplanReason time_trigger = detectReplanTrigger(route, now);
    if (time_trigger == NavRunReplanReason::ProgressRegression
        && projection->cross_track < kNavRunProgressReplanMinCrossTrackM) {
        time_trigger = NavRunReplanReason::None;
        last_progress_seen_ = now;
    }

    const bool needs_replan = hard_off || soft_off || time_trigger != NavRunReplanReason::None;
    if (needs_replan) {
        const bool cooldown_ready = ElapsedMs(plan_.last_soft_replan_at, now) >= kNavRunSoftReplanCooldownMs;
        const bool budget_left = plan_.soft_replan_attempts < kNavRunSoftReplanMaxPerAnchor;
        const NavRunReplanReason reason =
            hard_off || soft_off ? NavRunReplanReason::OffCorridor : time_trigger;

        // hard_off skips cooldown but never bypasses the budget — once exhausted,
        // outer 3.5 s recovery handles the escalation.
        if (budget_left && (hard_off || cooldown_ready)) {
            plan_.last_soft_replan_at = now;
            plan_.soft_replan_attempts += 1;
            if (buildPlan(param, position, anchor_index, anchor, reason, now)) {
                auto reprojected = ProjectOntoCorridor(plan_.path, plan_.cursor, position);
                if (!reprojected) {
                    invalidate();
                    return result;
                }
                projection = reprojected;
                last_progress_seen_ = now;
                last_remaining_to_anchor_ = std::numeric_limits<double>::infinity();
                result.replanned_with = reason;
                LogInfo << "NavRunController soft replan." << VAR(static_cast<int>(reason))
                        << VAR(plan_.soft_replan_attempts) << VAR(projection->cross_track) << VAR(anchor_index);
            }
        }
    }

    const double remaining = RemainingAlongCorridor(plan_.path, *projection);
    if (last_remaining_to_anchor_ - remaining >= kRouteProgressEpsilon) {
        last_progress_seen_ = now;
        last_remaining_to_anchor_ = remaining;
    }
    else if (remaining < last_remaining_to_anchor_) {
        last_remaining_to_anchor_ = remaining;
    }

    const double upcoming_turn = UpcomingCorridorTurnDeg(plan_.path, *projection, kNavRunUpcomingTurnLookaheadM);
    const double lookahead_distance = chooseLookaheadDistance(route, sprint_active, upcoming_turn);
    const navmesh::WorldPoint lookahead = LookaheadOnCorridor(plan_.path, *projection, lookahead_distance);
    const double corridor_heading = NaviMath::CalcTargetRotation(position.x, position.y, lookahead.x, lookahead.y);

    result.has_corridor_heading = true;
    result.corridor_heading = corridor_heading;
    result.lookahead_point = lookahead;
    result.cross_track = projection->cross_track;
    result.remaining_to_anchor = remaining;
    result.upcoming_turn_deg = upcoming_turn;
    const double character_arc = CorridorArcLengthTo(plan_.path, plan_.corridor_arc_prefix, *projection);
    result.passed_run_waypoints =
        CountCorridorPassedRunWaypoints(*session, plan_.path, plan_.corridor_arc_prefix, anchor_index, character_arc);
    return result;
}

} // namespace mapnavigator
