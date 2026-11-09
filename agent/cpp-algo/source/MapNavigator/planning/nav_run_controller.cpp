#include "nav_run_controller.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <optional>
#include <tuple>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_param.h"
#include "../navi_time.h"
#include "../state_machine/navigation_runtime_state.h"
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
    bool capped = false;
};

bool can_use_nav_run_steering(const Waypoint& waypoint)
{
    return waypoint.has_position && waypoint.action == ActionType::RUN;
}

std::optional<CorridorProjection> project_onto_corridor(
    const navmesh::WorldPath& path,
    const std::vector<double>& arc_prefix,
    size_t start_edge,
    const NaviPosition& position,
    double max_arc = std::numeric_limits<double>::infinity())
{
    if (path.points.size() < 2) {
        return std::nullopt;
    }
    const size_t num_edges = path.points.size() - 1;
    if (start_edge >= num_edges) {
        start_edge = num_edges - 1;
    }
    std::optional<CorridorProjection> best;
    for (size_t edge = start_edge; edge < num_edges && arc_prefix[edge] <= max_arc; ++edge) {
        const navmesh::WorldPoint& a = path.points[edge];
        const navmesh::WorldPoint& b = path.points[edge + 1];
        const double dx = b.x - a.x;
        const double dy = b.y - a.y;
        const double len_sq = dx * dx + dy * dy;
        if (len_sq <= std::numeric_limits<double>::epsilon()) {
            continue;
        }
        const double raw_t = ((position.x - a.x) * dx + (position.y - a.y) * dy) / len_sq;
        const double reach = std::min((max_arc - arc_prefix[edge]) / std::sqrt(len_sq), 1.0);
        const double t = std::clamp(raw_t, 0.0, reach);
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
                .capped = reach < 1.0 && raw_t > reach,
            };
        }
    }
    return best;
}

double remaining_along_corridor(const navmesh::WorldPath& path, const CorridorProjection& projection)
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

std::vector<double> build_corridor_arc_prefix(const navmesh::WorldPath& path)
{
    std::vector<double> prefix(path.points.size(), 0.0);
    for (size_t edge = 0; edge + 1 < path.points.size(); ++edge) {
        const navmesh::WorldPoint& a = path.points[edge];
        const navmesh::WorldPoint& b = path.points[edge + 1];
        prefix[edge + 1] = prefix[edge] + std::hypot(b.x - a.x, b.y - a.y);
    }
    return prefix;
}

double corridor_arc_length_to(const navmesh::WorldPath& path, const std::vector<double>& arc_prefix, const CorridorProjection& projection)
{
    if (projection.edge_idx >= arc_prefix.size() || projection.edge_idx >= path.points.size()) {
        return std::numeric_limits<double>::infinity();
    }
    const navmesh::WorldPoint& edge_start = path.points[projection.edge_idx];
    return arc_prefix[projection.edge_idx] + std::hypot(projection.point.x - edge_start.x, projection.point.y - edge_start.y);
}

size_t count_corridor_passed_run_waypoints(
    const Session& session,
    const navmesh::WorldPath& path,
    const std::vector<double>& arc_prefix,
    size_t anchor_index,
    double character_arc)
{
    if (!std::isfinite(character_arc)) {
        return 0;
    }

    const std::vector<Waypoint>& waypoints = session.current_path;
    const double margin = std::max(kMeasurementDefaultPositionQuantum, 0.0);
    size_t count = 0;
    for (size_t index = session.current_node_idx; index < waypoints.size(); ++index) {
        const std::optional<size_t> canonical = CanonicalIndexAtCurrentPath(session, index);
        if (canonical && *canonical == anchor_index) {
            break;
        }
        const Waypoint& waypoint = waypoints[index];
        if (!IsContinuousRun(waypoint)) {
            break;
        }
        const NaviPosition waypoint_pos { .x = waypoint.x, .y = waypoint.y };
        const std::optional<CorridorProjection> waypoint_projection = project_onto_corridor(path, arc_prefix, 0, waypoint_pos);
        if (!waypoint_projection) {
            break;
        }
        if (corridor_arc_length_to(path, arc_prefix, *waypoint_projection) > character_arc + margin) {
            break;
        }
        ++count;
    }
    return count;
}

// 提前 d 切弯会把内侧切掉约 d*sin(turn/2), 所以提前量按拐点处半宽封顶
double corner_commit_distance(const navmesh::WorldPath& path, size_t vertex, double turn_deg, double commit_distance)
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
    lookahead_on_corridor(const navmesh::WorldPath& path, const CorridorProjection& projection, double distance, double commit_distance)
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

    // 急弯进入 commit_distance 之前瞄点停在拐点上, 否则瞄点绕过弯后直线会切穿内侧的墙
    const navmesh::WorldPoint& edge_start = path.points[projection.edge_idx];
    const double base_heading = CalcTargetRotation(edge_start.x, edge_start.y, edge_end.x, edge_end.y);
    for (size_t edge = projection.edge_idx + 1; edge < num_edges; ++edge) {
        const navmesh::WorldPoint& a = path.points[edge];
        const navmesh::WorldPoint& b = path.points[edge + 1];
        const double dx = b.x - a.x;
        const double dy = b.y - a.y;
        const double len = std::hypot(dx, dy);
        if (len >= kNavRunCorridorEdgeMinM) {
            const double heading = CalcTargetRotation(a.x, a.y, b.x, b.y);
            const double turn = std::abs(NormalizeAngle(heading - base_heading));
            if (turn > kNavRunLookaheadTurnBudgetDeg && travelled > corner_commit_distance(path, edge, turn, commit_distance)) {
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

// 和 lookahead_on_corridor 的停靠不同, 拐点进了提前量范围仍会返回, 刹车才看得到瞄点已切入的弯
std::optional<SharpCorner> next_sharp_corner(const navmesh::WorldPath& path, const CorridorProjection& projection, double commit_distance)
{
    if (path.points.size() < 3 || projection.edge_idx + 1 >= path.points.size()) {
        return std::nullopt;
    }
    const size_t num_edges = path.points.size() - 1;
    const navmesh::WorldPoint& edge_start = path.points[projection.edge_idx];
    const navmesh::WorldPoint& edge_end = path.points[projection.edge_idx + 1];
    const double base_heading = CalcTargetRotation(edge_start.x, edge_start.y, edge_end.x, edge_end.y);
    double travelled = std::hypot(edge_end.x - projection.point.x, edge_end.y - projection.point.y);
    for (size_t edge = projection.edge_idx + 1; edge < num_edges && travelled <= kCornerBrakeScanM; ++edge) {
        const navmesh::WorldPoint& a = path.points[edge];
        const navmesh::WorldPoint& b = path.points[edge + 1];
        const double len = std::hypot(b.x - a.x, b.y - a.y);
        if (len >= kNavRunCorridorEdgeMinM) {
            const double turn = std::abs(NormalizeAngle(CalcTargetRotation(a.x, a.y, b.x, b.y) - base_heading));
            if (turn > kNavRunLookaheadTurnBudgetDeg) {
                return SharpCorner {
                    .distance = travelled,
                    .turn_deg = turn,
                    .lead_distance = corner_commit_distance(path, edge, turn, commit_distance),
                    .point = a,
                };
            }
        }
        travelled += len;
    }
    return std::nullopt;
}

double corridor_aim_heading(const NaviPosition& position, const navmesh::WorldPoint& anchor, const navmesh::WorldPoint& lookahead)
{
    const double dx = lookahead.x - anchor.x;
    const double dy = lookahead.y - anchor.y;
    constexpr double kChordFloorM = 1e-6;
    const double chord = std::hypot(dx, dy);
    if (chord <= kChordFloorM) {
        return CalcTargetRotation(position.x, position.y, lookahead.x, lookahead.y);
    }
    const double reach = std::max(kNavRunAimReachMinM, chord);
    return CalcTargetRotation(position.x, position.y, anchor.x + dx / chord * reach, anchor.y + dy / chord * reach);
}

double upcoming_corridor_turn_deg(const navmesh::WorldPath& path, const CorridorProjection& projection, double lookahead_distance)
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
            // 过短的边照样扣预览距离, 但朝向是量化噪声, 不参与转角
            if (length >= kNavRunCorridorEdgeMinM) {
                const double heading = CalcTargetRotation(segment_start.x, segment_start.y, segment_end.x, segment_end.y);
                if (!base_heading) {
                    base_heading = heading;
                }
                max_turn = std::max(max_turn, std::abs(NormalizeAngle(heading - *base_heading)));
            }
            remaining -= length;
        }
        segment_start = segment_end;
    }

    return max_turn;
}

int64_t elapsed_ms_or_max(std::chrono::steady_clock::time_point from, std::chrono::steady_clock::time_point to)
{
    return IsUnset(from) ? std::numeric_limits<int64_t>::max() : ElapsedMs(from, to);
}

// 线没走到锚点就返回空, 调用方据此改用 navmesh 走廊
navmesh::WorldPath build_authored_span_polyline(const Session& session, size_t anchor_index)
{
    const std::vector<Waypoint>& waypoints = session.current_path;
    navmesh::WorldPath poly;
    bool reached_anchor = false;
    bool any_clearance = false;
    for (size_t index = session.current_node_idx; index < waypoints.size(); ++index) {
        const Waypoint& waypoint = waypoints[index];
        if (!waypoint.has_position) {
            break;
        }
        poly.points.push_back({ .x = waypoint.x, .y = waypoint.y });
        poly.clearance.push_back(waypoint.corridor_clearance);
        any_clearance = any_clearance || waypoint.corridor_clearance > 0.0;
        const std::optional<size_t> canonical = CanonicalIndexAtCurrentPath(session, index);
        if (canonical && *canonical == anchor_index) {
            reached_anchor = true;
            break;
        }
    }
    if (!reached_anchor) {
        return {};
    }
    // clearance 为空表示宽度未知, 全是未知时不能留一串 0
    if (!any_clearance) {
        poly.clearance.clear();
    }
    return poly;
}

// 起跳点判到达时人常还站在台上, 只剩落点会触发重规划, 所以把起跳点补回线头
bool prepend_drop_top_while_short(
    navmesh::WorldPath* authored,
    const Session& session,
    const Waypoint& anchor,
    const NaviPosition& position)
{
    const size_t current = session.current_node_idx;
    if (authored->points.size() != 1 || !anchor.drop_from || current == 0) {
        return false;
    }
    const Waypoint& top = session.current_path[current - 1];
    const double top_x = (*anchor.drop_from)[0];
    const double top_y = (*anchor.drop_from)[1];
    if (!IsContinuousRun(top) || top.x != top_x || top.y != top_y) {
        return false;
    }
    const bool short_of_drop = (position.x - anchor.x) * (anchor.x - top_x) + (position.y - anchor.y) * (anchor.y - top_y) < 0.0;
    if (!short_of_drop || std::hypot(position.x - top_x, position.y - top_y) > ArrivalBand(top, kMeasurementDefaultPositionQuantum)) {
        return false;
    }
    authored->points.insert(authored->points.begin(), { .x = top_x, .y = top_y });
    if (!authored->clearance.empty() || top.corridor_clearance > 0.0) {
        authored->clearance = { top.corridor_clearance, anchor.corridor_clearance };
    }
    return true;
}

std::tuple<double, double, double> edge_projection(const navmesh::WorldPath& authored, const NaviPosition& position, size_t edge)
{
    const navmesh::WorldPoint& a = authored.points[edge];
    const navmesh::WorldPoint& b = authored.points[edge + 1];
    const double dx = b.x - a.x;
    const double dy = b.y - a.y;
    const double len_sq = dx * dx + dy * dy;
    const double t = len_sq <= std::numeric_limits<double>::epsilon() ? 1.0 : ((position.x - a.x) * dx + (position.y - a.y) * dy) / len_sq;
    return std::tuple { t, a.x + std::clamp(t, 0.0, 1.0) * dx, a.y + std::clamp(t, 0.0, 1.0) * dy };
}

// 转角超过 90° 时垂足会提前滑出入边, 在这裁会切穿拐角, 所以停在拐点前
bool turns_back(const navmesh::WorldPath& authored, size_t vertex)
{
    const navmesh::WorldPoint& prev = authored.points[vertex - 1];
    const navmesh::WorldPoint& curr = authored.points[vertex];
    const navmesh::WorldPoint& next = authored.points[vertex + 1];
    return (curr.x - prev.x) * (next.x - curr.x) + (curr.y - prev.y) * (next.y - curr.y) <= 0.0;
}

// 返回 false 表示人还没到线头, 调用方应补一段连到线头
bool trim_authored_span_to_agent(navmesh::WorldPath* authored, const NaviPosition& position)
{
    // 下面两处限距离是防定位短暂跳到几百像素外时把整条线当成走过删掉
    size_t head = 0;
    while (head + 2 < authored->points.size()) {
        if (turns_back(*authored, head + 1)) {
            break;
        }
        const auto [t, foot_x, foot_y] = edge_projection(*authored, position, head);
        if (t < 1.0 || std::hypot(position.x - foot_x, position.y - foot_y) > kNavRunCrossTrackFailM) {
            break;
        }
        ++head;
    }
    // 垂足落在边端点上会和拐点重合成退化边, 所以 t 取不到 0 和 1
    const auto [t, foot_x, foot_y] = edge_projection(*authored, position, head);
    if (t <= 0.0 || t >= 1.0 || std::hypot(position.x - foot_x, position.y - foot_y) > kLookaheadRadius) {
        return false;
    }
    authored->points.erase(authored->points.begin(), authored->points.begin() + head);
    if (!authored->clearance.empty()) {
        authored->clearance.erase(authored->clearance.begin(), authored->clearance.begin() + head);
    }
    authored->points.front() = { .x = foot_x, .y = foot_y };
    return true;
}

void commit_plan(NavRunPlan* plan, navmesh::WorldPath path, const NaviPosition& position, size_t anchor_index)
{
    plan->valid = true;
    plan->zone_id = position.zone_id;
    plan->anchor_index = anchor_index;
    plan->path = std::move(path);
    plan->corridor_arc_prefix = build_corridor_arc_prefix(plan->path);
    plan->cursor = 0;
    plan->projected_arc = 0.0;
    plan->projected_from = { .x = position.x, .y = position.y };
}

void prepend_agent_point(navmesh::WorldPath* authored, const NaviPosition& position)
{
    const navmesh::WorldPoint& span_start = authored->points.front();
    if (std::hypot(position.x - span_start.x, position.y - span_start.y) > kMeasurementDefaultPositionQuantum) {
        authored->points.insert(authored->points.begin(), { .x = position.x, .y = position.y });
        if (!authored->clearance.empty()) {
            authored->clearance.insert(authored->clearance.begin(), authored->clearance.front());
        }
    }
}

std::optional<CorridorProjection> project_agent(NavRunPlan* plan, const NaviPosition& position)
{
    const double moved = std::hypot(position.x - plan->projected_from.x, position.y - plan->projected_from.y);
    const double max_arc = plan->projected_arc + moved + kNavRunProjectionAdvanceSlackM;
    auto projected = project_onto_corridor(plan->path, plan->corridor_arc_prefix, plan->cursor, position, max_arc);
    if (projected && !projected->capped) {
        plan->projected_arc = corridor_arc_length_to(plan->path, plan->corridor_arc_prefix, *projected);
        plan->projected_from = { .x = position.x, .y = position.y };
    }
    return projected;
}

void invalidate(NavRun* nav_run)
{
    nav_run->plan = NavRunPlan {};
    nav_run->last_progress_seen = {};
    nav_run->last_remaining_to_anchor = std::numeric_limits<double>::infinity();
}

void restart_progress_watch(NavRun* nav_run, std::chrono::steady_clock::time_point now)
{
    nav_run->last_progress_seen = now;
    nav_run->last_remaining_to_anchor = std::numeric_limits<double>::infinity();
}

void record_speed_sample(NavRun* nav_run, const NaviPosition& position, std::chrono::steady_clock::time_point now)
{
    ++nav_run->tick_seq;
    if (!nav_run->speed_samples.empty()) {
        const NavRunSpeedSample& last = nav_run->speed_samples.back();
        const double step = std::hypot(position.x - last.x, position.y - last.y);
        if (step <= kMeasurementDefaultPositionQuantum) {
            return;
        }
        const int64_t span_ms = elapsed_ms_or_max(last.at, now);
        const double span_sec = static_cast<double>(span_ms) / 1000.0;
        if (span_sec <= 0.0 || nav_run->tick_seq - last.tick_seq > kNavRunSpeedMaxSampleGapTicks
            || step > kNavRunSpeedJumpMaxPxPerSec * span_sec) {
            nav_run->speed_samples.clear();
        }
    }
    nav_run->speed_samples.push_back(NavRunSpeedSample { .at = now, .tick_seq = nav_run->tick_seq, .x = position.x, .y = position.y });
    while (nav_run->speed_samples.size() > kNavRunSpeedMaxSamples
           || (nav_run->speed_samples.size() > 1 && elapsed_ms_or_max(nav_run->speed_samples.front().at, now) > kNavRunSpeedKeepMs)) {
        nav_run->speed_samples.erase(nav_run->speed_samples.begin());
    }
}

std::optional<double> estimate_step_per_tick(const NavRun& nav_run)
{
    if (nav_run.speed_samples.size() < 2) {
        return std::nullopt;
    }
    const NavRunSpeedSample& newest = nav_run.speed_samples.back();
    size_t oldest = 0;
    for (size_t i = 0; i + 1 < nav_run.speed_samples.size(); ++i) {
        if (elapsed_ms_or_max(nav_run.speed_samples[i].at, newest.at) >= kNavRunSpeedWindowMs) {
            oldest = i;
        }
    }
    if (newest.tick_seq <= nav_run.speed_samples[oldest].tick_seq) {
        return std::nullopt;
    }
    double arc = 0.0;
    for (size_t i = oldest; i + 1 < nav_run.speed_samples.size(); ++i) {
        arc += std::hypot(
            nav_run.speed_samples[i + 1].x - nav_run.speed_samples[i].x,
            nav_run.speed_samples[i + 1].y - nav_run.speed_samples[i].y);
    }
    return arc / static_cast<double>(newest.tick_seq - nav_run.speed_samples[oldest].tick_seq);
}

double choose_lookahead_distance(const RouteMeasurement& route, std::optional<double> step)
{
    if (!route.startup_motion_confirmed) {
        return kNavRunLookaheadLowSpeedM;
    }
    if (!step) {
        return kNavRunLookaheadLowSpeedM;
    }
    return std::clamp(kNavRunLookaheadPreviewTicks * *step, kNavRunLookaheadMinM, kNavRunLookaheadMaxM);
}

double choose_turn_commit_distance(double lookahead_distance, std::optional<double> step)
{
    const double by_speed = step ? kNavRunTurnCommitTicks * *step : kNavRunLookaheadMinM;
    return std::clamp(by_speed, kNavRunLookaheadMinM, lookahead_distance);
}

NavRunReplanReason detect_replan_trigger(const NavRun& nav_run, const RouteMeasurement& route, std::chrono::steady_clock::time_point now)
{
    if (route.startup_motion_confirmed && elapsed_ms_or_max(nav_run.last_progress_seen, now) >= kNavRunProgressRegressionMs) {
        return NavRunReplanReason::ProgressRegression;
    }
    return NavRunReplanReason::None;
}

void track_progress(NavRun* nav_run, double remaining, std::chrono::steady_clock::time_point now)
{
    if (nav_run->last_remaining_to_anchor - remaining >= kRouteProgressEpsilon) {
        nav_run->last_progress_seen = now;
        nav_run->last_remaining_to_anchor = remaining;
    }
    else if (remaining < nav_run->last_remaining_to_anchor) {
        nav_run->last_remaining_to_anchor = remaining;
    }
}

struct TickInputs
{
    const Session& session;
    const NavigationRuntimeState& runtime;
    const NaviPosition& position;
    const RouteMeasurement& route;
    const NaviParam& param;
    size_t anchor_index = 0;
    const Waypoint& anchor;
};

// 同步规划的耗时计入 result.planning, 并把 now 顺延同样的时长
std::optional<navmesh::BaseNavRouteResult>
    plan_by_astar_holding(Motion* motion, const TickInputs& input, std::chrono::steady_clock::time_point* now, NavRunTickResult* result)
{
    const NaviPosition& position = input.position;
    const Waypoint& anchor = input.anchor;
    const navmesh::WorldPoint start { .x = position.x, .y = position.y };
    const navmesh::WorldPoint goal { .x = anchor.x, .y = anchor.y };
    if (motion != nullptr) {
        HoldPosition(motion);
    }
    const auto plan_started_at = std::chrono::steady_clock::now();
    auto route = PlanNavmeshRoute(
        input.param,
        position.zone_id,
        {
            .start = start,
            .start_floor_y = WalkedFloorY(input.session, position),
            .start_deck_y = LandedTowerDeckY(input.session, position),
            .goal = goal,
            .goal_deck_y = anchor.target_deck_y,
        },
        nullptr,
        &input.runtime.virtual_no_go);
    const auto planning = std::chrono::steady_clock::now() - plan_started_at;
    result->planning += planning;
    *now += planning;
    return route;
}

bool build_plan(
    NavRun* nav_run,
    Motion* motion,
    const TickInputs& input,
    NavRunReplanReason reason,
    std::chrono::steady_clock::time_point* now,
    NavRunTickResult* result)
{
    const Session& session = input.session;
    const NaviPosition& position = input.position;
    const size_t anchor_index = input.anchor_index;
    const Waypoint& anchor = input.anchor;

    navmesh::WorldPath authored = build_authored_span_polyline(session, anchor_index);
    if (prepend_drop_top_while_short(&authored, session, anchor, position)) {
        LogDebug << "NavRunController drop top kept on span." << VAR(anchor_index) << VAR(position.x) << VAR(position.y);
    }
    const bool has_authored = authored.points.size() >= 2;
    const size_t authored_points = authored.points.size();

    if (has_authored) {
        if (trim_authored_span_to_agent(&authored, position)) {
            LogDebug << "NavRunController span trimmed to agent." << VAR(anchor_index) << VAR(authored_points)
                     << VAR(authored.points.size()) << VAR(authored.points.front().x) << VAR(authored.points.front().y);
        }
        else {
            prepend_agent_point(&authored, position);
        }
        commit_plan(&nav_run->plan, std::move(authored), position, anchor_index);
        return true;
    }

    // 手写裸坐标的末点直接走直线, 交给 A* 会为贴回网格绕远路
    if (authored.points.size() == 1 && IsContinuousRun(anchor) && anchor.corridor_clearance <= 0.0
        && std::hypot(position.x - anchor.x, position.y - anchor.y) > kMeasurementDefaultPositionQuantum) {
        LogDebug << "NavRunController trailing authored point kept literal." << VAR(anchor_index) << VAR(anchor.x) << VAR(anchor.y);
        prepend_agent_point(&authored, position);
        commit_plan(&nav_run->plan, std::move(authored), position, anchor_index);
        return true;
    }

    auto route = plan_by_astar_holding(motion, input, now, result);
    if (route && route->ok() && route->path.points.size() >= 2) {
        commit_plan(&nav_run->plan, std::move(route->path), position, anchor_index);
        return true;
    }
    LogDebug << "NavRunController plan build failed." << VAR(static_cast<int>(reason)) << VAR(anchor_index) << VAR(position.zone_id);
    return false;
}

bool ensure_plan(
    NavRun* nav_run,
    Motion* motion,
    const TickInputs& input,
    std::chrono::steady_clock::time_point* now,
    NavRunTickResult* result)
{
    if (nav_run->plan.valid
        && (nav_run->plan.anchor_index != input.anchor_index
            || (!input.position.zone_id.empty() && nav_run->plan.zone_id != input.position.zone_id))) {
        invalidate(nav_run);
    }
    if (nav_run->plan.valid) {
        return true;
    }
    if (nav_run->failed_build_anchor == input.anchor_index
        && elapsed_ms_or_max(nav_run->failed_build_at, *now) < kNavRunPlanFailureCooldownMs) {
        return false;
    }
    if (!build_plan(nav_run, motion, input, NavRunReplanReason::AnchorChanged, now, result)) {
        nav_run->failed_build_anchor = input.anchor_index;
        nav_run->failed_build_at = *now;
        return false;
    }
    nav_run->failed_build_anchor = std::numeric_limits<size_t>::max();
    restart_progress_watch(nav_run, *now);
    return true;
}

// 重建成功时返回触发原因, 否则返回 None
NavRunReplanReason soft_replan_if_due(
    NavRun* nav_run,
    Motion* motion,
    const TickInputs& input,
    double cross_track,
    std::chrono::steady_clock::time_point* now,
    NavRunTickResult* result)
{
    const bool hard_off = cross_track > kNavRunCrossTrackFailM;
    const bool soft_off = cross_track > kNavRunCrossTrackWarnM;
    NavRunReplanReason time_trigger = detect_replan_trigger(*nav_run, input.route, *now);
    if (time_trigger == NavRunReplanReason::ProgressRegression && cross_track < kNavRunProgressReplanMinCrossTrackM) {
        time_trigger = NavRunReplanReason::None;
        nav_run->last_progress_seen = *now;
    }
    if (!hard_off && !soft_off && time_trigger == NavRunReplanReason::None) {
        return NavRunReplanReason::None;
    }

    const bool cooldown_ready = elapsed_ms_or_max(nav_run->plan.last_soft_replan_at, *now) >= kNavRunSoftReplanCooldownMs;
    const bool budget_left = nav_run->plan.soft_replan_attempts < kNavRunSoftReplanMaxPerAnchor;
    if (!budget_left || (!hard_off && !cooldown_ready)) {
        return NavRunReplanReason::None;
    }
    const NavRunReplanReason reason = hard_off || soft_off ? NavRunReplanReason::OffCorridor : time_trigger;
    nav_run->plan.soft_replan_attempts += 1;
    const bool built = build_plan(nav_run, motion, input, reason, now, result);
    nav_run->plan.last_soft_replan_at = *now;
    return built ? reason : NavRunReplanReason::None;
}

void note_corridor_passed(NavRunTickResult* result, const NavRun& nav_run, const CorridorProjection& projection, const TickInputs& input)
{
    const double character_arc = corridor_arc_length_to(nav_run.plan.path, nav_run.plan.corridor_arc_prefix, projection);
    const size_t corridor_passed = count_corridor_passed_run_waypoints(
        input.session,
        nav_run.plan.path,
        nav_run.plan.corridor_arc_prefix,
        input.anchor_index,
        character_arc);
    const bool clamped_without_passage =
        projection.before_window || projection.capped || (projection.after_window && projection.cross_track > kNavRunCrossTrackWarnM);
    if (clamped_without_passage) {
        if (corridor_passed > 0) {
            LogDebug << "Corridor passed advance suppressed; corridor projection clamped outside the window." << VAR(corridor_passed)
                     << VAR_AS(session->current_node_idx(), input.session.current_node_idx) << VAR(projection.before_window)
                     << VAR(projection.after_window) << VAR(projection.capped) << VAR(projection.cross_track);
        }
    }
    else if (input.route.startup_motion_confirmed) {
        result->passed_run_waypoints = corridor_passed;
    }
    else if (corridor_passed > 0) {
        LogDebug << "Corridor passed advance blocked before startup movement confirmed." << VAR(corridor_passed)
                 << VAR_AS(session->current_node_idx(), input.session.current_node_idx) << VAR(character_arc);
    }
}

} // namespace

NavRunTickResult TickNavRun(
    NavRun* nav_run,
    Session* session,
    NavigationRuntimeState* runtime,
    Motion* motion,
    const NaviPosition& position,
    const RouteMeasurement& route,
    const NaviParam& param,
    const Waypoint& anchor,
    size_t anchor_index,
    std::chrono::steady_clock::time_point now)
{
    NavRunTickResult result;
    if (runtime->nav_run_dirty) {
        invalidate(nav_run);
        ClearNavRunDirty(runtime);
    }

    // 必须在下面所有提前返回之前, 走廊不可用时测速窗口也要继续积累
    record_speed_sample(nav_run, position, now);

    if (!anchor.has_position || !HasCurrentWaypoint(*session)) {
        return result;
    }
    if (!can_use_nav_run_steering(CurrentWaypoint(*session))) {
        return result;
    }

    // 必须在下面所有提前返回之前赋值, 否则看门狗会拿近处路点的距离误判已到达
    result.straight_to_anchor = std::hypot(position.x - anchor.x, position.y - anchor.y);

    const TickInputs input {
        .session = *session,
        .runtime = *runtime,
        .position = position,
        .route = route,
        .param = param,
        .anchor_index = anchor_index,
        .anchor = anchor,
    };
    if (!ensure_plan(nav_run, motion, input, &now, &result)) {
        return result;
    }

    auto projection = project_agent(&nav_run->plan, position);
    if (!projection) {
        invalidate(nav_run);
        return result;
    }
    if (!projection->capped) {
        nav_run->plan.cursor = projection->edge_idx;
    }
    for (const navmesh::DropLanding& drop : nav_run->plan.path.drops) {
        if (nav_run->plan.cursor + 1 >= drop.index) {
            LeaveLandedTowerDeck(session);
            break;
        }
    }

    const NavRunReplanReason reason = soft_replan_if_due(nav_run, motion, input, projection->cross_track, &now, &result);
    if (reason != NavRunReplanReason::None) {
        projection = project_agent(&nav_run->plan, position);
        if (!projection) {
            invalidate(nav_run);
            return result;
        }
        restart_progress_watch(nav_run, now);
        result.replanned_with = reason;
        LogInfo << "NavRunController soft replan." << VAR(static_cast<int>(reason))
                << VAR_AS(plan_.soft_replan_attempts, nav_run->plan.soft_replan_attempts) << VAR(projection->cross_track)
                << VAR(anchor_index);
    }

    if ((projection->before_window || projection->after_window) && projection->cross_track > kNavRunCrossTrackFailM) {
        return result;
    }

    const double remaining = remaining_along_corridor(nav_run->plan.path, *projection);
    track_progress(nav_run, remaining, now);

    const std::optional<double> step = estimate_step_per_tick(*nav_run);
    const double lookahead_distance = choose_lookahead_distance(route, step);
    const double turn_commit_distance = choose_turn_commit_distance(lookahead_distance, step);
    const navmesh::WorldPoint lookahead = lookahead_on_corridor(nav_run->plan.path, *projection, lookahead_distance, turn_commit_distance);
    result.has_corridor_heading = true;
    result.corridor_heading = corridor_aim_heading(position, projection->point, lookahead);
    result.lookahead_point = lookahead;
    result.projection_point = projection->point;
    result.cross_track = projection->cross_track;
    result.remaining_to_anchor = remaining;
    result.upcoming_turn_deg = upcoming_corridor_turn_deg(nav_run->plan.path, *projection, kNavRunUpcomingTurnLookaheadM);
    result.step_per_tick = step;
    result.corner = next_sharp_corner(nav_run->plan.path, *projection, turn_commit_distance);

    note_corridor_passed(&result, *nav_run, *projection, input);
    return result;
}

} // namespace mapnavigator
