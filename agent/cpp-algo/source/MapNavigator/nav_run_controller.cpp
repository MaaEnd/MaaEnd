#include "nav_run_controller.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <optional>
#include <tuple>
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
    bool before_window = false;
    bool after_window = false;
};

bool CanUseNavRunSteering(const Waypoint& waypoint)
{
    return waypoint.HasPosition() && waypoint.action == ActionType::RUN;
}

std::optional<CorridorProjection> ProjectOntoCorridor(const navmesh::WorldPath& path, size_t start_edge, const NaviPosition& position)
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
                .before_window = edge == start_edge && raw_t < 0.0,
                .after_window = edge + 1 == num_edges && raw_t > 1.0,
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

// How close to a bend the aim may start leading into it. Leading from distance d makes the agent drive the
// chord instead of the corner, which cuts the inside of the bend by about d*sin(turn/2); where the corridor
// is narrow that cut is the wall, so the lead is capped by what the corridor at the bend can absorb. The
// clearance is unknown on a hand-authored line, which keeps the speed-derived budget as it was.
double CornerCommitDistance(const navmesh::WorldPath& path, size_t vertex, double turn_deg, double commit_distance)
{
    if (vertex >= path.clearance.size()) {
        return commit_distance;
    }
    const double clearance = path.clearance[vertex];
    const double cut_per_unit = std::sin(turn_deg * 0.5 * kPi / 180.0);
    if (clearance <= 0.0 || cut_per_unit <= std::numeric_limits<double>::epsilon()) {
        return commit_distance;
    }
    return std::min(commit_distance, clearance / cut_per_unit);
}

navmesh::WorldPoint
    LookaheadOnCorridor(const navmesh::WorldPath& path, const CorridorProjection& projection, double distance, double commit_distance)
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
    double travelled = remaining_on_edge;

    // Never aim past a turn the agent has not started yet. The aim distance is measured in ticks of travel, so
    // on a slow loop it grows until the aim point sits well around a bend while the bend itself is still many
    // world units away -- and the straight line the agent then drives cuts the inside of it, which is a wall.
    // Holding the aim at the vertex where the corridor has bent too far keeps it on the stretch the agent is
    // actually on. The hold lifts once the bend is within commit_distance, which is counted in ticks of travel
    // so the turn always gets the same number of ticks to happen in: a fixed lead in world units would be most
    // of a tick on a slow loop, and the agent would still be facing the old way when it reached the bend.
    const navmesh::WorldPoint& edge_start = path.points[projection.edge_idx];
    const double base_heading = NaviMath::CalcTargetRotation(edge_start.x, edge_start.y, edge_end.x, edge_end.y);
    for (size_t edge = projection.edge_idx + 1; edge < num_edges; ++edge) {
        const navmesh::WorldPoint& a = path.points[edge];
        const navmesh::WorldPoint& b = path.points[edge + 1];
        const double dx = b.x - a.x;
        const double dy = b.y - a.y;
        const double len = std::hypot(dx, dy);
        if (len >= kNavRunCorridorEdgeMinM) {
            const double heading = NaviMath::CalcTargetRotation(a.x, a.y, b.x, b.y);
            const double turn = std::abs(NaviMath::NormalizeAngle(heading - base_heading));
            if (turn > kNavRunLookaheadTurnBudgetDeg && travelled > CornerCommitDistance(path, edge, turn, commit_distance)) {
                return a;
            }
        }
        if (len >= distance) {
            if (len <= std::numeric_limits<double>::epsilon()) {
                return b;
            }
            const double scale = distance / len;
            return { .x = a.x + dx * scale, .y = a.y + dy * scale };
        }
        distance -= len;
        travelled += len;
    }
    return path.points.back();
}

double CorridorAimHeading(const NaviPosition& position, const navmesh::WorldPoint& anchor, const navmesh::WorldPoint& lookahead)
{
    const double dx = lookahead.x - anchor.x;
    const double dy = lookahead.y - anchor.y;
    constexpr double kChordFloorM = 1e-6;
    const double chord = std::hypot(dx, dy);
    if (chord <= kChordFloorM) {
        return NaviMath::CalcTargetRotation(position.x, position.y, lookahead.x, lookahead.y);
    }
    const double reach = std::max(kNavRunAimReachMinM, chord);
    return NaviMath::CalcTargetRotation(position.x, position.y, anchor.x + dx / chord * reach, anchor.y + dy / chord * reach);
}

double UpcomingCorridorTurnDeg(const navmesh::WorldPath& path, const CorridorProjection& projection, double lookahead_distance)
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
            // Sub-cell edges still consume the preview distance, but their heading is quantisation noise.
            if (length >= kNavRunCorridorEdgeMinM) {
                const double heading = NaviMath::CalcTargetRotation(segment_start.x, segment_start.y, segment_end.x, segment_end.y);
                if (!base_heading) {
                    base_heading = heading;
                }
                max_turn = std::max(max_turn, std::abs(NaviMath::NormalizeAngle(heading - *base_heading)));
            }
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

// The authored line from the current waypoint up to and including the anchor, carrying each point's corridor
// half-width so the follower can tell where it has no room to cut. Returns empty unless the anchor is actually
// reached (a control node or path end first truncates the span), so the caller falls back to the navmesh
// corridor instead of trusting a line that stops short of the anchor.
navmesh::WorldPath BuildAuthoredSpanPolyline(const NavigationSession& session, size_t anchor_index)
{
    const std::vector<Waypoint>& waypoints = session.current_path();
    navmesh::WorldPath poly;
    bool reached_anchor = false;
    bool any_clearance = false;
    for (size_t index = session.current_node_idx(); index < waypoints.size(); ++index) {
        const Waypoint& waypoint = waypoints[index];
        if (!waypoint.HasPosition()) {
            break;
        }
        poly.points.push_back({ .x = waypoint.x, .y = waypoint.y });
        poly.clearance.push_back(waypoint.corridor_clearance);
        any_clearance = any_clearance || waypoint.corridor_clearance > 0.0;
        const std::optional<size_t> canonical = session.CanonicalIndexAtCurrentPath(index);
        if (canonical && *canonical == anchor_index) {
            reached_anchor = true;
            break;
        }
    }
    if (!reached_anchor) {
        return {};
    }
    // An empty vector means "no widths known"; a line of nothing but unknowns must not claim zero width.
    if (!any_clearance) {
        poly.clearance.clear();
    }
    return poly;
}

// Cut the line back to where the agent actually stands on it, dropping what is already behind.
// The span always begins at the session's current waypoint, but a waypoint only counts as reached once
// the agent enters a band about a pixel wide, so a pass a pixel wide of it leaves the span starting
// behind the agent -- and a corridor joined to the agent's own position then aims it backwards at
// ground it has no reason to revisit. Only points the agent has gone past along the line are dropped,
// one edge at a time, so the line can never be cut short sideways.
// Returns false while the agent is still short of the head, where a joining segment is the right answer.
bool TrimAuthoredSpanToAgent(navmesh::WorldPath& authored, const NaviPosition& position)
{
    const auto edge_projection = [&](size_t edge) {
        const navmesh::WorldPoint& a = authored.points[edge];
        const navmesh::WorldPoint& b = authored.points[edge + 1];
        const double dx = b.x - a.x;
        const double dy = b.y - a.y;
        const double len_sq = dx * dx + dy * dy;
        const double t =
            len_sq <= std::numeric_limits<double>::epsilon() ? 1.0 : ((position.x - a.x) * dx + (position.y - a.y) * dy) / len_sq;
        return std::tuple { t, a.x + std::clamp(t, 0.0, 1.0) * dx, a.y + std::clamp(t, 0.0, 1.0) * dy };
    };

    // Landing on an edge replaces the vertex it starts from with a point along it, so that corner stops
    // existing. Past a right angle the outgoing edge carries the agent back the way it came, which means the
    // perpendicular foot runs off the end of the incoming edge long before the agent has rounded anything --
    // trim there and the follower is handed a line straight across the corner. Stop one vertex short instead.
    // The test is the turn itself, not a tuned angle: at ninety degrees is exactly where the foot starts lying.
    const auto turns_back = [&](size_t vertex) {
        const navmesh::WorldPoint& prev = authored.points[vertex - 1];
        const navmesh::WorldPoint& curr = authored.points[vertex];
        const navmesh::WorldPoint& next = authored.points[vertex + 1];
        return (curr.x - prev.x) * (next.x - curr.x) + (curr.y - prev.y) * (next.y - curr.y) <= 0.0;
    };

    // Both tests below bound how far the agent may stand from the stretch it claims to have passed. A
    // locator excursion reports a position hundreds of pixels away for a few seconds; without the bound
    // it would read as "already past everything" and delete the rest of the line.
    size_t head = 0;
    while (head + 2 < authored.points.size()) {
        if (turns_back(head + 1)) {
            break;
        }
        const auto [t, foot_x, foot_y] = edge_projection(head);
        if (t < 1.0 || std::hypot(position.x - foot_x, position.y - foot_y) > kNavRunCrossTrackFailM) {
            break;
        }
        ++head;
    }
    // Starting the corridor at the foot is only meaningful while the agent is within the follower's own reach of
    // the line. A perpendicular foot slides past a corner well before the agent has rounded it, so out here the
    // vertex is still load-bearing: drop it and the agent cuts straight across whatever the corner went around.
    // Inside the lookahead it is already on the line and the vertex behind it is only something to aim back at.
    // Past the end of this edge means the only reason we are still on it is the corner ahead we refused to
    // trim through. Pinning the start to its far end would duplicate that vertex right where the follower can
    // least afford a degenerate edge, so hand the whole line back and let the caller join to it instead.
    const auto [t, foot_x, foot_y] = edge_projection(head);
    if (t <= 0.0 || t >= 1.0 || std::hypot(position.x - foot_x, position.y - foot_y) > kLookaheadRadius) {
        return false;
    }
    authored.points.erase(authored.points.begin(), authored.points.begin() + head);
    if (!authored.clearance.empty()) {
        authored.clearance.erase(authored.clearance.begin(), authored.clearance.begin() + head);
    }
    authored.points.front() = { .x = foot_x, .y = foot_y };
    return true;
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
    const NavigationSession& session,
    const NaviPosition& position,
    size_t anchor_index,
    const Waypoint& anchor,
    NavRunReplanReason reason,
    std::chrono::steady_clock::time_point now)
{
    const auto commit = [&](navmesh::WorldPath path, bool literal) {
        plan_.valid = true;
        plan_.zone_id = position.zone_id;
        plan_.anchor_index = anchor_index;
        plan_.anchor_pos = { .x = anchor.x, .y = anchor.y };
        plan_.literal = literal;
        plan_.path = std::move(path);
        plan_.corridor_arc_prefix = BuildCorridorArcPrefix(plan_.path);
        plan_.cursor = 0;
        plan_.planned_at = now;
    };

    navmesh::WorldPath authored = BuildAuthoredSpanPolyline(session, anchor_index);
    const bool has_authored = authored.points.size() >= 2;
    const size_t authored_points = authored.points.size();
    const bool on_authored_line = has_authored && TrimAuthoredSpanToAgent(authored, position);

    const auto commit_authored = [&] {
        if (!on_authored_line) {
            const navmesh::WorldPoint& span_start = authored.points.front();
            if (std::hypot(position.x - span_start.x, position.y - span_start.y) > kMeasurementDefaultPositionQuantum) {
                authored.points.insert(authored.points.begin(), { .x = position.x, .y = position.y });
                if (!authored.clearance.empty()) {
                    authored.clearance.insert(authored.clearance.begin(), authored.clearance.front());
                }
            }
        }
        else {
            LogDebug << "NavRunController span trimmed to agent." << VAR(anchor_index) << VAR(authored_points)
                     << VAR(authored.points.size()) << VAR(authored.points.front().x) << VAR(authored.points.front().y);
        }
        commit(std::move(authored), true);
        return true;
    };

    if (has_authored) {
        return commit_authored();
    }

    const navmesh::WorldPoint start { .x = position.x, .y = position.y };
    const navmesh::WorldPoint goal { .x = anchor.x, .y = anchor.y };
    auto route = PlanNavmeshRoute(param, position.zone_id, start, goal, anchor.target_deck_y);
    if (route && route->ok() && route->path.points.size() >= 2) {
        commit(std::move(route->path), false);
        return true;
    }
    LogDebug << "NavRunController plan build failed." << VAR(static_cast<int>(reason)) << VAR(anchor_index) << VAR(position.zone_id);
    return false;
}

void NavRunController::recordSpeedSample(const NaviPosition& position, std::chrono::steady_clock::time_point now)
{
    ++tick_seq_;
    if (!speed_samples_.empty()) {
        const NavRunSpeedSample& last = speed_samples_.back();
        const double step = std::hypot(position.x - last.x, position.y - last.y);
        if (step <= kMeasurementDefaultPositionQuantum) {
            return;
        }
        // Two ways the pair stops describing one tick of travel: a jump too fast to be travel is a
        // re-acquire landing somewhere else, and a gap this many ticks wide straddles a stall the
        // older sample knows nothing about. Either would read as a misleading rate, so the window
        // restarts instead of carrying the discontinuity.
        const int64_t span_ms = ElapsedMs(last.at, now);
        const double span_sec = static_cast<double>(span_ms) / 1000.0;
        if (span_sec <= 0.0 || tick_seq_ - last.tick_seq > kNavRunSpeedMaxSampleGapTicks || step > kNavRunSpeedJumpMaxPxPerSec * span_sec) {
            speed_samples_.clear();
        }
    }
    speed_samples_.push_back(NavRunSpeedSample { .at = now, .tick_seq = tick_seq_, .x = position.x, .y = position.y });
    while (speed_samples_.size() > kNavRunSpeedMaxSamples
           || (speed_samples_.size() > 1 && ElapsedMs(speed_samples_.front().at, now) > kNavRunSpeedKeepMs)) {
        speed_samples_.erase(speed_samples_.begin());
    }
}

std::optional<double> NavRunController::estimateStepPerTick() const
{
    if (speed_samples_.size() < 2) {
        return std::nullopt;
    }
    const NavRunSpeedSample& newest = speed_samples_.back();
    // Oldest sample still at least a full window back, so the estimate spans real travel rather than
    // one fix pair; before that much history exists the whole span is used instead of stalling.
    size_t oldest = 0;
    for (size_t i = 0; i + 1 < speed_samples_.size(); ++i) {
        if (ElapsedMs(speed_samples_[i].at, newest.at) >= kNavRunSpeedWindowMs) {
            oldest = i;
        }
    }
    if (newest.tick_seq <= speed_samples_[oldest].tick_seq) {
        return std::nullopt;
    }
    double arc = 0.0;
    for (size_t i = oldest; i + 1 < speed_samples_.size(); ++i) {
        arc += std::hypot(speed_samples_[i + 1].x - speed_samples_[i].x, speed_samples_[i + 1].y - speed_samples_[i].y);
    }
    return arc / static_cast<double>(newest.tick_seq - speed_samples_[oldest].tick_seq);
}

double NavRunController::chooseLookaheadDistance(const RouteTrackingState& route) const
{
    if (!route.startup_motion_confirmed) {
        return kNavRunLookaheadLowSpeedM;
    }
    const std::optional<double> step = estimateStepPerTick();
    if (!step) {
        return kNavRunLookaheadLowSpeedM;
    }
    return std::clamp(kNavRunLookaheadPreviewTicks * *step, kNavRunLookaheadMinM, kNavRunLookaheadMaxM);
}

double NavRunController::chooseTurnCommitDistance(double lookahead_distance) const
{
    const std::optional<double> step = estimateStepPerTick();
    const double by_speed = step ? kNavRunTurnCommitTicks * *step : kNavRunLookaheadMinM;
    return std::clamp(by_speed, kNavRunLookaheadMinM, lookahead_distance);
}

NavRunReplanReason NavRunController::detectReplanTrigger(const RouteTrackingState& route, std::chrono::steady_clock::time_point now) const
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
    std::chrono::steady_clock::time_point now)
{
    NavRunTickResult result;

    if (runtime->nav_run_dirty) {
        invalidate();
        runtime->nav_run_dirty = false;
    }

    // Ahead of every early return below, so the speed window keeps filling while the corridor is down
    // and the first steered tick after it comes back already has a usable estimate.
    recordSpeedSample(position, now);

    if (!anchor.HasPosition() || !session->HasCurrentWaypoint()) {
        return result;
    }
    if (!CanUseNavRunSteering(session->CurrentWaypoint())) {
        return result;
    }

    // Published before any of the early returns below (plan build failed, projection lost, cursor clamped
    // outside the window). Without it the caller falls back to the serial waypoint breadcrumb, which sits a
    // couple of pixels away while the anchor is still tens of pixels out, and the no-progress watchdogs read
    // that as "arrived" for as long as the corridor stays down.
    result.straight_to_anchor = std::hypot(position.x - anchor.x, position.y - anchor.y);

    if (plan_.valid) {
        if (plan_.anchor_index != anchor_index) {
            invalidate();
        }
        else if (!position.zone_id.empty() && plan_.zone_id != position.zone_id) {
            invalidate();
        }
    }

    if (!plan_.valid) {
        if (failed_build_anchor_ == anchor_index && ElapsedMs(failed_build_at_, now) < kNavRunPlanFailureCooldownMs) {
            return result;
        }
        if (!buildPlan(param, *session, position, anchor_index, anchor, NavRunReplanReason::AnchorChanged, now)) {
            failed_build_anchor_ = anchor_index;
            failed_build_at_ = now;
            return result;
        }
        failed_build_anchor_ = std::numeric_limits<size_t>::max();
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
    if (time_trigger == NavRunReplanReason::ProgressRegression && projection->cross_track < kNavRunProgressReplanMinCrossTrackM) {
        time_trigger = NavRunReplanReason::None;
        last_progress_seen_ = now;
    }

    const bool needs_replan = hard_off || soft_off || time_trigger != NavRunReplanReason::None;
    if (needs_replan) {
        const bool cooldown_ready = ElapsedMs(plan_.last_soft_replan_at, now) >= kNavRunSoftReplanCooldownMs;
        const bool budget_left = plan_.soft_replan_attempts < kNavRunSoftReplanMaxPerAnchor;
        const NavRunReplanReason reason = hard_off || soft_off ? NavRunReplanReason::OffCorridor : time_trigger;

        // hard_off skips cooldown but never bypasses the budget — once exhausted,
        // outer 3.5 s recovery handles the escalation.
        if (budget_left && (hard_off || cooldown_ready)) {
            plan_.last_soft_replan_at = now;
            plan_.soft_replan_attempts += 1;
            if (buildPlan(param, *session, position, anchor_index, anchor, reason, now)) {
                auto reprojected = ProjectOntoCorridor(plan_.path, plan_.cursor, position);
                if (!reprojected) {
                    invalidate();
                    return result;
                }
                projection = reprojected;
                last_progress_seen_ = now;
                last_remaining_to_anchor_ = std::numeric_limits<double>::infinity();
                result.replanned_with = reason;
                LogInfo << "NavRunController soft replan." << VAR(static_cast<int>(reason)) << VAR(plan_.soft_replan_attempts)
                        << VAR(projection->cross_track) << VAR(anchor_index);
            }
        }
    }

    if ((projection->before_window || projection->after_window) && projection->cross_track > kNavRunCrossTrackFailM) {
        return result;
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
    const double lookahead_distance = chooseLookaheadDistance(route);
    const navmesh::WorldPoint lookahead =
        LookaheadOnCorridor(plan_.path, *projection, lookahead_distance, chooseTurnCommitDistance(lookahead_distance));
    const double corridor_heading = CorridorAimHeading(position, projection->point, lookahead);

    result.has_corridor_heading = true;
    result.corridor_heading = corridor_heading;
    result.lookahead_point = lookahead;
    result.projection_point = projection->point;
    result.cross_track = projection->cross_track;
    result.remaining_to_anchor = remaining;
    result.upcoming_turn_deg = upcoming_turn;
    const double character_arc = CorridorArcLengthTo(plan_.path, plan_.corridor_arc_prefix, *projection);
    const size_t corridor_passed =
        CountCorridorPassedRunWaypoints(*session, plan_.path, plan_.corridor_arc_prefix, anchor_index, character_arc);
    const bool clamped_without_passage =
        projection->before_window || (projection->after_window && projection->cross_track > kNavRunCrossTrackWarnM);
    if (clamped_without_passage) {
        if (corridor_passed > 0) {
            LogDebug << "Corridor passed advance suppressed; corridor projection clamped outside the window." << VAR(corridor_passed)
                     << VAR(session->current_node_idx()) << VAR(projection->before_window) << VAR(projection->after_window)
                     << VAR(projection->cross_track);
        }
    }
    else if (route.startup_motion_confirmed) {
        result.passed_run_waypoints = corridor_passed;
    }
    else if (corridor_passed > 0) {
        LogDebug << "Corridor passed advance blocked before startup movement confirmed." << VAR(corridor_passed)
                 << VAR(session->current_node_idx()) << VAR(character_arc);
    }
    return result;
}

} // namespace mapnavigator
