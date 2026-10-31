#include <algorithm>
#include <array>
#include <chrono>
#include <cmath>
#include <cstddef>
#include <filesystem>
#include <format>
#include <iterator>
#include <limits>
#include <memory>
#include <optional>
#include <string>
#include <unordered_set>
#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_param.h"
#include "../navi_time.h"
#include "../zipline/zipline_leg_planner.h"
#include "navmesh_cache.h"
#include "navmesh_path_expander.h"

namespace mapnavigator
{

namespace
{

struct LegState
{
    navmesh::WorldPoint route_start;
    // 本字段和 route_start_deck_y 只有整条链的头一腿有值
    std::optional<double> route_start_floor_y;
    std::optional<double> route_start_deck_y;
    std::string current_zone;
    std::string navmesh_zone;

    void moveRouteStart(const navmesh::WorldPoint& point)
    {
        route_start = point;
        route_start_floor_y.reset();
        route_start_deck_y.reset();
    }

    RouteEndpoints legTo(const navmesh::WorldPoint& goal, float goal_floor_y, std::optional<double> goal_deck_y) const
    {
        return RouteEndpoints {
            .start = route_start,
            .start_floor_y = route_start_floor_y,
            .start_deck_y = route_start_deck_y,
            .goal = goal,
            .goal_floor_y = goal_floor_y,
            .goal_deck_y = goal_deck_y,
        };
    }
};

thread_local NavmeshExpansionFailure g_expansion_failure;

constexpr double kBlindTargetFallbackSnapRadius = 12.0;
constexpr double kBlindTargetProbeStep = 2.0;
// 只吸收坐标往返转换的浮点误差
constexpr double kSameRoutePointWu = 0.01;
constexpr int64_t kBlindTargetProbeBudgetMs = 8000;

std::string describe_authored_waypoint(const Waypoint& waypoint, std::optional<size_t> authored_index)
{
    std::string description = authored_index ? std::format("第 {} 个路点", *authored_index + 1) : "当前路点";
    if (waypoint.hasPosition()) {
        description += std::format(" [{:.2f}, {:.2f}]", waypoint.x, waypoint.y);
    }
    if (!waypoint.target_tier.empty()) {
        description += std::format("（层级 {}）", waypoint.target_tier);
    }
    return description;
}

NavmeshExpansionFailure zone_failure(std::string code, std::string message, const LegState& state)
{
    return NavmeshExpansionFailure {
        .code = std::move(code),
        .message = std::move(message),
        .zone_id = state.current_zone,
    };
}

navmesh::BaseNavRouteRequest build_route_request(
    const navmesh::BaseNavPack& pack,
    const std::string& locator_zone,
    const std::string& navmesh_zone,
    const RouteEndpoints& endpoints,
    const std::vector<VirtualNoGoDisc>* no_go,
    const std::vector<uint32_t>* rope_off = nullptr)
{
    navmesh::BaseNavRouteRequest request;
    request.zone_name = navmesh_zone;
    request.start = endpoints.start;
    request.goal = endpoints.goal;
    if (rope_off != nullptr) {
        request.rope_off = *rope_off;
    }
    // push_through 的禁区已判定堵住唯一通路, 不再当墙
    if (no_go != nullptr) {
        for (const VirtualNoGoDisc& disc : *no_go) {
            if (!disc.push_through && disc.zone_id == locator_zone) {
                request.no_go_discs.push_back({ .center = { .x = disc.x, .y = disc.y }, .radius = disc.radius });
            }
        }
    }
    if (endpoints.goal_deck_y) {
        request.goal_deck_y = static_cast<float>(*endpoints.goal_deck_y);
    }
    if (endpoints.start_deck_y) {
        request.start_deck_y = static_cast<float>(*endpoints.start_deck_y);
    }
    const float zone_floor_y = pack.floorYForZoneName(locator_zone);
    request.start_floor_y = endpoints.start_floor_y ? static_cast<float>(*endpoints.start_floor_y) : zone_floor_y;
    // 终点兜底取 zone 主层, 被覆盖的起点高度只代表人站在哪
    request.goal_floor_y = endpoints.goal_floor_y > navmesh::kBaseNavFloorYValidMin ? endpoints.goal_floor_y : zone_floor_y;
    return request;
}

struct ExpansionContext
{
    const NaviParam& param;
    const CachedNavmesh& mesh;
    const std::function<bool()>& should_stop;
    std::vector<NavmeshRouteDiagnostic>* diagnostics = nullptr;
    const std::vector<VirtualNoGoDisc>* no_go = nullptr;
    // 判定不可用的升降索, 空指针表示这次不用升降索
    const std::vector<uint32_t>* rope_off = nullptr;
    NavmeshExpansionFailure& failure;
    // 终态, 整条展开到此为止
    bool no_go_rejected = false;

    navmesh::BaseNavRouteRequest
        legRequest(const LegState& state, const navmesh::WorldPoint& goal, float goal_floor_y, std::optional<double> goal_deck_y) const
    {
        return build_route_request(
            mesh.pack,
            state.current_zone,
            state.navmesh_zone,
            state.legTo(goal, goal_floor_y, goal_deck_y),
            no_go,
            rope_off);
    }

    bool cancelIfStopped(const LegState& state)
    {
        if (!should_stop()) {
            return false;
        }
        failure = zone_failure("cancelled", "路线规划已取消", state);
        return true;
    }

    void failWaypoint(
        std::string code,
        std::string detail,
        const Waypoint& waypoint,
        std::optional<size_t> authored_index,
        const LegState& state,
        const navmesh::BaseNavRouteResult* route_result = nullptr,
        std::optional<navmesh::WorldPoint> segment_goal = std::nullopt)
    {
        NavmeshExpansionFailure recorded {
            .code = std::move(code),
            .message = describe_authored_waypoint(waypoint, authored_index) + detail,
            .authored_index = authored_index,
            .zone_id = state.current_zone,
            .segment_start = state.route_start,
            .segment_goal = segment_goal,
            .target_tier = waypoint.target_tier,
            .target_deck_y = waypoint.target_deck_y,
        };
        if (waypoint.hasPosition()) {
            recorded.target = navmesh::WorldPoint { .x = waypoint.x, .y = waypoint.y };
        }
        if (route_result != nullptr) {
            recorded.route_status = navmesh::ToString(route_result->status);
            recorded.route_error = route_result->error;
            recorded.gap_start = route_result->gap_start;
            recorded.gap_goal = route_result->gap_goal;
            recorded.gap_distance = route_result->gap_distance;
        }
        failure = std::move(recorded);
    }
};

bool is_navmesh_waypoint(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::NAVMESH && waypoint.hasPosition();
}

bool is_global_route_target(const Waypoint& waypoint)
{
    return !waypoint.isZoneDeclaration() && waypoint.hasPosition();
}

bool contains_navmesh_waypoint(const std::vector<Waypoint>& path)
{
    return std::any_of(path.begin(), path.end(), is_navmesh_waypoint);
}

bool contains_global_route_target(const std::vector<Waypoint>& path)
{
    return std::any_of(path.begin(), path.end(), is_global_route_target);
}

bool has_explicit_coordinate_frame(const Waypoint& waypoint)
{
    return !waypoint.target_tier.empty() && (waypoint.hasPosition() || waypoint.heading_uses_target);
}

std::string route_error_text(const navmesh::BaseNavRouteResult& route_result)
{
    return route_result.error.empty() ? navmesh::ToString(route_result.status) : route_result.error;
}

std::vector<std::vector<double>> path_points_for_log(const navmesh::WorldPath& path)
{
    std::vector<std::vector<double>> result;
    result.reserve(path.points.size());
    for (const navmesh::WorldPoint& point : path.points) {
        result.push_back({ point.x, point.y });
    }
    return result;
}

void update_state_from_regular_waypoint(const Waypoint& waypoint, LegState& state)
{
    if (waypoint.hasPosition()) {
        state.moveRouteStart({ .x = waypoint.x, .y = waypoint.y });
    }
    if (!waypoint.zone_id.empty()) {
        state.current_zone = waypoint.zone_id;
    }
}

struct ProjectedTarget
{
    navmesh::WorldPoint point;
    float floor_y = navmesh::kBaseNavFloorYNone;
    std::optional<double> deck_y;
};

// 无 target_tier 时坐标已是底图像素, 有则按该 tier 投影到底图并取它的层高
ProjectedTarget resolve_projected_target(const navmesh::BaseNavPack& pack, const Waypoint& waypoint)
{
    const navmesh::WorldPoint raw { .x = waypoint.x, .y = waypoint.y };
    if (waypoint.target_tier.empty()) {
        return { raw, navmesh::kBaseNavFloorYNone, waypoint.target_deck_y };
    }
    const auto projection = pack.projectToBase(waypoint.target_tier, waypoint.x, waypoint.y);
    if (!projection) {
        LogWarn << "NAVMESH target_tier unknown; treating target as base-frame." << VAR(waypoint.target_tier) << VAR(waypoint.x)
                << VAR(waypoint.y);
        return { raw, navmesh::kBaseNavFloorYNone, waypoint.target_deck_y };
    }
    return { navmesh::WorldPoint { .x = projection->x, .y = projection->y },
             pack.floorYForZoneName(waypoint.target_tier),
             waypoint.target_deck_y };
}

std::optional<Waypoint> project_regular_waypoint_to_base(const navmesh::BaseNavPack& pack, const Waypoint& waypoint)
{
    if (!has_explicit_coordinate_frame(waypoint)) {
        return waypoint;
    }

    const auto projection = pack.projectToBase(waypoint.target_tier, waypoint.x, waypoint.y);
    if (!projection) {
        LogError << "MapNavigator waypoint target_tier is unknown." << VAR(waypoint.target_tier) << VAR(waypoint.x) << VAR(waypoint.y);
        return std::nullopt;
    }

    Waypoint projected = waypoint;
    projected.x = projection->x;
    projected.y = projection->y;
    projected.target_tier.clear();
    return projected;
}

std::optional<Waypoint> project_authored_target(const ExpansionContext& ctx, const Waypoint& waypoint)
{
    if (is_navmesh_waypoint(waypoint) || !ctx.param.normalize_position_via_navmesh) {
        return waypoint;
    }
    return project_regular_waypoint_to_base(ctx.mesh.pack, waypoint);
}

void copy_plan_debug(navmesh::recast::RecastPlanResult::Debug& debug, NavmeshRouteDiagnostic& out)
{
    out.timing.window_ms = debug.timing.window_ms;
    out.timing.topology_ms = debug.timing.topology_ms;
    out.timing.geometry_ms = debug.timing.geometry_ms;
    out.timing.pull_ms = debug.timing.pull_ms;
    out.timing.assemble_ms = debug.timing.assemble_ms;
    out.timing.lift_ms = debug.timing.lift_ms;
    out.timing.total_ms = debug.timing.total_ms;
    out.x0 = debug.x0;
    out.y0 = debug.y0;
    out.nx = debug.nx;
    out.ny = debug.ny;
    out.cell_size = debug.cell_size;
    out.topology_cells = std::move(debug.topology_cells);
    out.topology_heights = std::move(debug.topology_heights);
    out.taut_points = std::move(debug.taut_points);
    out.pulled_points = std::move(debug.pulled_points);
    out.assembled_points = std::move(debug.assembled_points);
    out.planned_points = std::move(debug.planned_points);
    out.warnings = std::move(debug.warnings);
}

navmesh::BaseNavRouteResult plan_corridor_route(
    const CachedNavmesh& mesh,
    const navmesh::BaseNavRouteRequest& request,
    const std::function<bool()>& should_stop = {},
    NavmeshRouteDiagnostic* out_diagnostic = nullptr)
{
    navmesh::BaseNavRouteResult result;
    const navmesh::BaseNavZone* zone = mesh.pack.findZoneByName(request.zone_name);
    if (zone == nullptr) {
        result.status = navmesh::BaseNavRouteStatus::ZoneNotFound;
        result.error = "未找到 navmesh 区域: " + request.zone_name;
        return result;
    }
    const float start_floor = request.start_floor_y > navmesh::kBaseNavFloorYValidMin ? request.start_floor_y : request.floor_y;
    const float goal_floor = request.goal_floor_y > navmesh::kBaseNavFloorYValidMin ? request.goal_floor_y : request.floor_y;
    auto plan = mesh.engine.plan(
        request.zone_name,
        request.start,
        request.goal,
        start_floor,
        goal_floor,
        request.goal_deck_y,
        request.start_deck_y,
        request.no_go_discs,
        should_stop,
        request.rope_off ? &*request.rope_off : nullptr);
    result.gap_start = plan.debug.gap_start;
    result.gap_goal = plan.debug.gap_goal;
    result.gap_distance = plan.debug.gap_distance;
    if (!plan.ok || plan.points.size() < 2) {
        result.error = plan.error.empty() ? "规划结果没有可执行路径点" : plan.error;
        if (plan.no_go) {
            result.status = navmesh::BaseNavRouteStatus::NoGo;
        }
        LogWarn << "RECAST plan failed." << VAR(request.zone_name) << VAR(request.no_go_discs.size()) << VAR(result.error);
        return result;
    }
    for (const std::string& warning : plan.warnings) {
        LogWarn << "RECAST plan warning." << VAR(request.zone_name) << VAR(warning);
    }
    std::string tier_notes;
    for (const std::string& note : plan.debug.tier_notes) {
        tier_notes += (tier_notes.empty() ? "" : " | ") + note;
    }
    LogInfo << "RECAST plan window." << VAR(request.zone_name) << VAR(plan.debug.tier) << VAR(plan.debug.nx) << VAR(plan.debug.ny)
            << VAR(plan.debug.timing.window_ms) << VAR(plan.debug.timing.topology_ms) << VAR(plan.debug.timing.geometry_ms)
            << VAR(plan.debug.timing.total_ms) << VAR(request.no_go_discs.size()) << VAR(tier_notes);
    result.status = navmesh::BaseNavRouteStatus::Success;
    result.path.zone_id = zone->zone_id;
    result.path.zone_name = request.zone_name;
    result.path.points = std::move(plan.points);
    result.path.clearance = std::move(plan.clearance);
    result.path.heights = std::move(plan.heights);
    result.path.waypoints = std::move(plan.waypoints);
    result.path.drops = std::move(plan.drops);
    result.path.ropes = std::move(plan.ropes);
    result.cost = plan.length;
    if (out_diagnostic != nullptr) {
        out_diagnostic->start = request.start;
        out_diagnostic->goal = request.goal;
        copy_plan_debug(plan.debug, *out_diagnostic);
    }
    return result;
}

// 离网不到到点余量时不算脱网, 同样返回空
std::optional<navmesh::BaseNavSnapResult> find_start_recovery_entry(
    const NaviParam& param,
    const CachedNavmesh& mesh,
    const std::string& navmesh_zone,
    const std::string& current_zone,
    const navmesh::BaseNavRouteRequest& request,
    std::string* out_error)
{
    const navmesh::BaseNavZone* zone = mesh.pack.findZoneByName(navmesh_zone);
    if (zone == nullptr) {
        if (out_error != nullptr) {
            *out_error = "找不到当前区域的 navmesh";
        }
        return std::nullopt;
    }
    const double radius = std::max(param.navmesh_snap_radius, kStartRecoveryMaxBlindWalk);
    const float floor_y = request.start_deck_y > navmesh::kBaseNavFloorYValidMin ? request.start_deck_y : request.start_floor_y;
    const auto entry = mesh.planner.snap(zone->zone_id, request.start, radius, floor_y);
    if (!entry) {
        LogWarn << "NAVMESH start recovery rejected: no mesh point within the blind-walk budget." << VAR(navmesh_zone) << VAR(current_zone)
                << VAR(request.start.x) << VAR(request.start.y) << VAR(radius);
        if (out_error != nullptr) {
            *out_error = std::format("起点在 {:.2f}px 盲走预算内找不到可走网格", radius);
        }
        return std::nullopt;
    }
    if (entry->distance <= kWaypointArrivalSlack) {
        return std::nullopt;
    }
    return entry;
}

void log_generated_navmesh_path(
    const LegState& state,
    const navmesh::BaseNavRouteRequest& request,
    const navmesh::BaseNavRouteResult& route_result)
{
    const size_t path_point_count = route_result.path.points.size();
    const std::vector<double> navmesh_start_point { request.start.x, request.start.y };
    const std::vector<double> navmesh_goal_point { request.goal.x, request.goal.y };
    const auto navmesh_path_points = path_points_for_log(route_result.path);
    const auto navmesh_segment_breaks = route_result.path.segment_breaks;
    LogInfo << "NAVMESH generated path." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(route_result.cost)
            << VAR(path_point_count) << VAR(navmesh_start_point) << VAR(navmesh_goal_point) << VAR(navmesh_segment_breaks)
            << VAR(navmesh_path_points);
}

struct GeneratedWaypointWriter
{
    const navmesh::WorldPath& path;
    std::vector<Waypoint>& out_path;
    const navmesh::BaseNavPlanner* drivability_planner = nullptr;
    uint16_t drivable_zone_id = 0;
    size_t last_anchor = 0;

    double clearanceAt(size_t index) const { return index < path.clearance.size() ? path.clearance[index] : 0.0; }

    std::optional<double> heightAt(size_t index) const
    {
        return index < path.heights.size() ? std::optional<double>(path.heights[index]) : std::nullopt;
    }

    void markDrop(size_t index)
    {
        for (const navmesh::DropLanding& drop : path.drops) {
            if (drop.index == index && index > 0) {
                Waypoint& landing = out_path.back();
                landing.strict_arrival = true;
                landing.target_deck_y = drop.height;
                landing.drop_from = std::array<double, 2> { path.points[index - 1].x, path.points[index - 1].y };
            }
        }
    }

    void markRope(size_t index)
    {
        for (const navmesh::RopeRide& rope : path.ropes) {
            if (rope.index != index || index == 0) {
                continue;
            }
            Waypoint top = std::move(out_path.back());
            out_path.pop_back();
            const navmesh::WorldPoint& base = path.points[index - 1];
            if (out_path.empty() || out_path.back().x != base.x || out_path.back().y != base.y) {
                out_path.emplace_back(base.x, base.y, ActionType::RUN);
                out_path.back().corridor_clearance = clearanceAt(index - 1);
                out_path.back().route_floor_y = heightAt(index - 1);
            }
            Waypoint& bottom = out_path.back();
            bottom.action = ActionType::INTERACT;
            bottom.strict_arrival = true;
            bottom.rope_ride = RopeRideInfo { .uid = rope.uid, .ride_s = rope.ride_s, .top_height = rope.top_height };
            top.strict_arrival = true;
            top.target_deck_y = rope.top_height;
            top.rope_top = true;
            out_path.push_back(std::move(top));
        }
    }

    void emitPoint(size_t index, bool strict_arrival)
    {
        out_path.emplace_back(path.points[index].x, path.points[index].y, ActionType::RUN);
        out_path.back().strict_arrival = strict_arrival;
        out_path.back().corridor_clearance = clearanceAt(index);
        out_path.back().route_floor_y = heightAt(index);
        markDrop(index);
        markRope(index);
    }

    void appendInteriorCorners(size_t loop_end)
    {
        for (size_t index = 1; index < loop_end; ++index) {
            emitPoint(index, index + 1 == path.points.size());
        }
    }

    void appendPulledCorners(size_t anchor)
    {
        // 规划器已拉直的路线直接照抄下标, 这里的判据在叠层处会把楼下那层的捷径判成可走
        if (!path.waypoints.empty() && last_anchor == 0 && anchor + 1 == path.points.size()) {
            for (size_t index = 0; index + 1 < path.waypoints.size(); ++index) {
                emitPoint(path.waypoints[index], false);
            }
            return;
        }
        if (drivability_planner == nullptr || anchor <= last_anchor + 1) {
            return;
        }
        // 一次拉直最多吞的原始顶点数, 只为限制二次复杂度
        constexpr size_t kMaxPullSpan = 64;
        size_t cursor = last_anchor;
        while (cursor + 1 < anchor) {
            size_t reach = cursor;
            size_t reach_limit = std::min(anchor, cursor + kMaxPullSpan);
            for (const navmesh::DropLanding& drop : path.drops) {
                if (drop.index > cursor && drop.index < reach_limit) {
                    reach_limit = drop.index;
                }
            }
            for (const navmesh::RopeRide& rope : path.ropes) {
                if (rope.index > cursor && rope.index < reach_limit) {
                    reach_limit = rope.index;
                }
            }
            double swallowed_clearance = std::numeric_limits<double>::infinity();
            while (reach < reach_limit) {
                // 捷径不能比它吞掉的最窄走廊点更窄, 宽度未知按 0
                const double required = std::isfinite(swallowed_clearance) ? swallowed_clearance : 0.0;
                if (!drivability_planner->isRouteSegmentDrivable(drivable_zone_id, path.points[cursor], path.points[reach + 1], required)) {
                    break;
                }
                swallowed_clearance = std::min(swallowed_clearance, clearanceAt(reach + 1));
                ++reach;
            }
            // 走廊自己的下一条边也可能判不过, 照样前进一格, 保证循环推进
            if (reach == cursor) {
                ++reach;
            }
            if (reach >= anchor) {
                return;
            }
            emitPoint(reach, false);
            cursor = reach;
        }
    }

    void flushLegTo(size_t anchor, bool strict_arrival)
    {
        appendPulledCorners(anchor);
        emitPoint(anchor, strict_arrival);
        last_anchor = anchor;
    }
};

bool append_generated_waypoints(
    const navmesh::WorldPath& world_path,
    std::vector<Waypoint>& out_path,
    const GeneratedWaypointOptions& options,
    const navmesh::BaseNavPlanner* drivability_planner = nullptr,
    uint16_t drivable_zone_id = 0)
{
    if (world_path.points.empty()) {
        return false;
    }

    GeneratedWaypointWriter writer {
        .path = world_path,
        .out_path = out_path,
        .drivability_planner = drivability_planner,
        .drivable_zone_id = drivable_zone_id,
    };
    const size_t total = world_path.points.size();
    const size_t loop_end = options.include_goal ? total : total - 1;
    if (options.emit_interior_corners) {
        writer.appendInteriorCorners(loop_end);
        return true;
    }

    const std::unordered_set<size_t> segment_breaks(world_path.segment_breaks.begin(), world_path.segment_breaks.end());
    for (size_t index = 1; index < loop_end; ++index) {
        if (!segment_breaks.contains(index)) {
            continue;
        }
        const size_t emit_idx = index - 1;
        if (emit_idx == 0) {
            continue;
        }
        writer.flushLegTo(emit_idx, options.strict_segment_breaks);
    }

    if (total >= 2) {
        if (options.include_goal) {
            writer.flushLegTo(total - 1, true);
        }
        else {
            writer.appendPulledCorners(total - 1);
        }
    }

    return true;
}

bool append_blind_target_fallback(
    ExpansionContext& ctx,
    LegState& state,
    const navmesh::WorldPoint& base_target,
    float goal_floor_y,
    std::vector<Waypoint>& out_path,
    std::string* out_error)
{
    const auto fail = [out_error](std::string message) {
        if (out_error != nullptr) {
            *out_error = std::move(message);
        }
        return false;
    };
    const navmesh::WorldPoint target = base_target;
    const navmesh::WorldPoint start = state.route_start;
    const double total = std::hypot(target.x - start.x, target.y - start.y);
    if (total < 1e-6) {
        return fail("目标与起点重合，但没有生成有效路线");
    }
    const navmesh::BaseNavZone* zone = ctx.mesh.pack.findZoneByName(state.navmesh_zone);
    if (zone == nullptr) {
        return fail("找不到当前区域的 navmesh");
    }
    const double snap_radius = std::max(ctx.param.navmesh_snap_radius, kBlindTargetFallbackSnapRadius);
    const float snap_floor =
        goal_floor_y > navmesh::kBaseNavFloorYValidMin ? goal_floor_y : ctx.mesh.pack.floorYForZoneName(state.current_zone);

    // 探针剩余缺口至少是 offset - snap_radius, 超出这个上限的探针必然过不了缺口检查
    const double probe_limit = std::min(total, kBlindTargetMaxExtension + snap_radius);

    navmesh::BaseNavRouteResult approach;
    NavmeshRouteDiagnostic accepted_diagnostic;
    bool found = false;
    double blind_gap = 0.0;
    const auto probe_started_at = std::chrono::steady_clock::now();
    for (double offset = 0.0; offset <= probe_limit + 1e-6; offset += kBlindTargetProbeStep) {
        if (ctx.should_stop()) {
            return fail("规划已取消");
        }
        const int64_t probe_elapsed_ms = ElapsedMs(probe_started_at);
        if (probe_elapsed_ms > kBlindTargetProbeBudgetMs) {
            LogWarn << "NAVMESH blind-target fallback gave up: probe budget exhausted." << VAR(state.navmesh_zone)
                    << VAR(state.current_zone) << VAR(target.x) << VAR(target.y) << VAR(offset) << VAR(probe_limit)
                    << VAR(probe_elapsed_ms);
            return fail(std::format("目标附近探测超过 {} ms 预算", kBlindTargetProbeBudgetMs));
        }
        const double t = std::min(offset / total, 1.0);
        const navmesh::WorldPoint probe {
            .x = target.x + (start.x - target.x) * t,
            .y = target.y + (start.y - target.y) * t,
        };
        const auto entry = ctx.mesh.planner.snap(zone->zone_id, probe, snap_radius, snap_floor);
        if (!entry) {
            continue;
        }
        const auto request = ctx.legRequest(state, entry->point, goal_floor_y, std::nullopt);
        NavmeshRouteDiagnostic diagnostic;
        const auto route = plan_corridor_route(ctx.mesh, request, ctx.should_stop, ctx.diagnostics == nullptr ? nullptr : &diagnostic);
        if (!route.ok() || route.path.points.empty()) {
            continue;
        }
        const navmesh::WorldPoint reached = route.path.points.back();
        blind_gap = std::hypot(target.x - reached.x, target.y - reached.y);
        approach = route;
        accepted_diagnostic = std::move(diagnostic);
        found = true;
        break;
    }

    if (!found) {
        return fail("目标附近没有与起点连通的网格接近点");
    }
    if (blind_gap > kBlindTargetMaxExtension) {
        LogWarn << "NAVMESH blind-target fallback rejected: residual gap too large." << VAR(state.navmesh_zone) << VAR(state.current_zone)
                << VAR(target.x) << VAR(target.y) << VAR(blind_gap) << VAR(kBlindTargetMaxExtension);
        return fail(std::format("剩余盲走距离 {:.2f}px 超过 {:.2f}px 上限", blind_gap, kBlindTargetMaxExtension));
    }

    if (!append_generated_waypoints(approach.path, out_path, {}, &ctx.mesh.planner, approach.path.zone_id)) {
        return fail("网格接近路线没有生成可执行路点");
    }
    if (ctx.diagnostics != nullptr) {
        ctx.diagnostics->push_back(std::move(accepted_diagnostic));
    }
    if (blind_gap > kWaypointArrivalSlack) {
        out_path.emplace_back(target.x, target.y, ActionType::RUN);
        out_path.back().strict_arrival = false;
    }
    state.moveRouteStart(target);
    LogInfo << "NAVMESH blind-target fallback applied." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(target.x)
            << VAR(target.y) << VAR(blind_gap) << VAR(approach.path.points.back().x) << VAR(approach.path.points.back().y);
    return true;
}

bool append_start_recovery(
    const ExpansionContext& ctx,
    LegState& state,
    const navmesh::BaseNavRouteRequest& request,
    std::vector<Waypoint>& out_path,
    std::string* out_error)
{
    const auto entry = find_start_recovery_entry(ctx.param, ctx.mesh, state.navmesh_zone, state.current_zone, request, out_error);
    if (!entry) {
        return false;
    }

    out_path.emplace_back(entry->point.x, entry->point.y, ActionType::RUN);
    out_path.back().strict_arrival = false;
    state.moveRouteStart(entry->point);
    LogInfo << "NAVMESH start off mesh; walking onto the nearest mesh point first." << VAR(state.navmesh_zone) << VAR(state.current_zone)
            << VAR(request.start.x) << VAR(request.start.y) << VAR(entry->point.x) << VAR(entry->point.y) << VAR(entry->distance);
    return true;
}

// 离落点不到最小瞄准距离的点, 定位误差折算出的方位就是噪声
std::optional<ZiplineMountSpot>
    pick_exit_aim_point(const std::vector<Waypoint>& out_path, size_t departure_index, const ZiplineNodeRef& dismount)
{
    std::optional<ZiplineMountSpot> farthest;
    double farthest_distance = 0.0;
    for (size_t index = departure_index; index < out_path.size(); ++index) {
        const double distance = std::hypot(out_path[index].x - dismount.x, out_path[index].y - dismount.y);
        if (distance >= kNavRunAimReachMinM) {
            return ZiplineMountSpot { .x = out_path[index].x, .y = out_path[index].y };
        }
        if (distance > farthest_distance) {
            farthest_distance = distance;
            farthest = ZiplineMountSpot { .x = out_path[index].x, .y = out_path[index].y };
        }
    }
    return farthest;
}

// walking 非空时滑索只在严格更省时顶替走路, 为空时用来桥接步行不连通的两侧
bool try_append_zipline_leg(
    ExpansionContext& ctx,
    LegState& state,
    const ProjectedTarget& target,
    const navmesh::BaseNavRouteResult* walking,
    std::vector<Waypoint>& out_path)
{
    const navmesh::WorldPath* walking_path = walking == nullptr ? nullptr : &walking->path;
    auto route = PlanZiplineRoute(
        ctx.param,
        {
            .locator_zone = state.current_zone,
            .navmesh_zone = state.navmesh_zone,
            .start = state.route_start,
            .goal = target.point,
            .walking_path = walking_path,
            .goal_deck_y = target.deck_y,
            .start_floor_y = state.route_start_floor_y,
            .start_deck_y = state.route_start_deck_y,
            .should_stop = ctx.should_stop,
            .capture_diagnostics = ctx.diagnostics != nullptr,
        });
    if (!route || route->approach.points.empty() || route->departure.points.empty() || route->towers.size() < 2) {
        return false;
    }

    const size_t insert_index = out_path.size();
    // 上索点在下面自己补, 起点已在索下时通用追加一个点都不会产出
    append_generated_waypoints(route->approach, out_path, { .include_goal = false }, &ctx.mesh.planner, route->approach.zone_id);
    const navmesh::WorldPoint mount = route->approach.points.back();
    // 架子坐标是角格锚点, 走到跟前常碰不到设备, 所以头一跳瞄第一个站位
    const navmesh::WorldPoint mount_spot = route->mount_spots.empty() ? mount : route->mount_spots.front();
    for (size_t hop = 0; hop + 1 < route->towers.size(); ++hop) {
        const zipline::ZiplineNode& from = route->towers[hop];
        const zipline::ZiplineNode& to = route->towers[hop + 1];
        const ZiplineNodeRef from_ref = ToNodeRef(from);
        const ZiplineNodeRef to_ref = ToNodeRef(to);

        out_path.emplace_back(hop == 0 ? mount_spot.x : from.x, hop == 0 ? mount_spot.y : from.y, ActionType::ZIPLINE);
        out_path.back().strict_arrival = true;
        out_path.back().target_deck_y = from.height;

        ZiplineHopPlan hop_plan;
        hop_plan.mount = from_ref;
        hop_plan.landing = to_ref;
        hop_plan.planned_elevation_deg = NaviMath::calcElevation(from_ref, to_ref);
        if (hop == 0) {
            for (const navmesh::WorldPoint& spot : route->mount_spots) {
                hop_plan.mount_spots.push_back(ZiplineMountSpot { .x = spot.x, .y = spot.y });
            }
        }
        if (hop < route->hop_alternates.size()) {
            for (const zipline::ZiplineNode& other : route->hop_alternates[hop]) {
                hop_plan.siblings.push_back(ToNodeRef(other));
            }
        }
        hop_plan.chain_continues = hop + 2 < route->towers.size();
        out_path.back().zipline_hop = hop_plan;
    }

    // landing 是下索段末端, 这一跳的落点是 dismount
    const size_t last_hop_index = out_path.size() - 1;
    const ZiplineNodeRef dismount = out_path[last_hop_index].zipline_hop->landing;
    const size_t departure_index = out_path.size();
    const navmesh::WorldPoint landing = route->departure.points.back();
    append_generated_waypoints(route->departure, out_path, {}, &ctx.mesh.planner, route->departure.zone_id);
    if (out_path.size() == departure_index) {
        out_path.emplace_back(landing.x, landing.y, ActionType::RUN);
        out_path.back().strict_arrival = true;
    }
    out_path[last_hop_index].zipline_hop->exit_aim = pick_exit_aim_point(out_path, departure_index, dismount);
    if (target.deck_y) {
        out_path.back().target_deck_y = target.deck_y;
    }

    state.moveRouteStart(landing);
    if (ctx.diagnostics != nullptr) {
        ctx.diagnostics->insert(
            ctx.diagnostics->end(),
            std::make_move_iterator(route->diagnostics.begin()),
            std::make_move_iterator(route->diagnostics.end()));
    }
    const bool walking_baseline_available = walking != nullptr;
    LogInfo << "Expanded NAVMESH waypoint via zipline." << VAR(state.navmesh_zone) << VAR(state.current_zone)
            << VAR(walking_baseline_available) << VAR(route->cost) << VAR(insert_index) << VAR(out_path.size() - insert_index)
            << VAR(route->towers.size()) << VAR(mount_spot.x) << VAR(mount_spot.y) << VAR(route->mount_spots.size())
            << VAR(route->towers.back().x) << VAR(route->towers.back().y);
    return true;
}

enum class LegMode
{
    Authored,
    GlobalProbe,
};

struct NavmeshLeg
{
    ExpansionContext& ctx;
    LegState& state;
    std::vector<Waypoint>& out_path;
    const Waypoint& waypoint;
    LegMode mode = LegMode::Authored;
    std::optional<size_t> authored_index;

    std::chrono::steady_clock::time_point expand_started_at {};
    ProjectedTarget target {};
    navmesh::BaseNavRouteRequest request {};
    navmesh::BaseNavRouteResult route_result {};
    NavmeshRouteDiagnostic route_diagnostic {};
    bool start_recovered = false;
    std::string start_recovery_error {};
    int64_t plan_ms = 0;

    bool append()
    {
        expand_started_at = std::chrono::steady_clock::now();
        target = resolve_projected_target(ctx.mesh.pack, waypoint);
        request = ctx.legRequest(state, target.point, target.floor_y, target.deck_y);
        planWithStartRecovery();
        return route_result.ok() ? commitWalking() : handleUnreachable();
    }

    NavmeshRouteDiagnostic* diagnosticSink() { return ctx.diagnostics == nullptr ? nullptr : &route_diagnostic; }

    void fail(std::string code, std::string detail)
    {
        ctx.failWaypoint(std::move(code), std::move(detail), waypoint, authored_index, state, &route_result, target.point);
    }

    void planWithStartRecovery()
    {
        const auto plan_started_at = std::chrono::steady_clock::now();
        route_result = plan_corridor_route(ctx.mesh, request, ctx.should_stop, diagnosticSink());
        if (!route_result.ok() && route_result.status != navmesh::BaseNavRouteStatus::NoGo
            && append_start_recovery(ctx, state, request, out_path, &start_recovery_error)) {
            request.start = state.route_start;
            route_diagnostic = {};
            route_result = plan_corridor_route(ctx.mesh, request, ctx.should_stop, diagnosticSink());
            start_recovered = true;
        }
        plan_ms = ElapsedMs(plan_started_at);
    }

    bool handleUnreachable()
    {
        // 下面的兜底都不看可走面, 禁区必须先拦下, 否则会穿过禁区
        if (route_result.status == navmesh::BaseNavRouteStatus::NoGo) {
            LogError << "NAVMESH waypoint rejected by a virtual no-go zone." << VAR(state.navmesh_zone) << VAR(state.current_zone)
                     << VAR(target.point.x) << VAR(target.point.y) << VAR(route_result.error);
            fail("route_no_go", " 无法规划：" + route_result.error);
            ctx.no_go_rejected = true;
            return false;
        }
        // 滑索能桥接步行不连通的两侧, 优先于盲走和作者提示回退
        if (try_append_zipline_leg(ctx, state, target, nullptr, out_path)) {
            return true;
        }
        if (ctx.cancelIfStopped(state)) {
            return false;
        }
        // 盲走兜底只看二维, 有面声明时用它会把错层当成到达
        if (target.deck_y) {
            if (mode == LegMode::Authored) {
                LogError << "Failed to plan NAVMESH waypoint on the declared deck." << VAR(state.navmesh_zone) << VAR(state.current_zone)
                         << VAR(target.point.x) << VAR(target.point.y) << VAR(*target.deck_y)
                         << VAR(navmesh::ToString(route_result.status));
                const std::string route_error = route_error_text(route_result);
                const bool disconnected = route_result.gap_start.has_value() && route_result.gap_goal.has_value();
                fail(
                    disconnected ? "route_disconnected" : "target_deck_unreachable",
                    disconnected ? std::format(" 的指定目标面与起点不连通（target_deck_y={:.2f}）：{}", *target.deck_y, route_error)
                                 : std::format(" 无法抵达指定目标面（target_deck_y={:.2f}）：{}", *target.deck_y, route_error));
            }
            else {
                LogDebug << "Global NAVMESH target is unavailable on the declared deck." << VAR(state.navmesh_zone)
                         << VAR(state.current_zone) << VAR(target.point.x) << VAR(target.point.y) << VAR(*target.deck_y)
                         << VAR(navmesh::ToString(route_result.status));
            }
            return false;
        }
        // 全局规划须完整抵达终点才能跳过作者提示, 盲走结果不算完整路线
        if (mode == LegMode::GlobalProbe) {
            LogDebug << "Global NAVMESH target not directly reachable; preserving authored hints." << VAR(state.navmesh_zone)
                     << VAR(state.current_zone) << VAR(target.point.x) << VAR(target.point.y)
                     << VAR(navmesh::ToString(route_result.status));
            return false;
        }
        LogWarn << "NAVMESH waypoint not directly reachable; attempting blind-target fallback." << VAR(state.navmesh_zone)
                << VAR(state.current_zone) << VAR(target.point.x) << VAR(target.point.y) << VAR(navmesh::ToString(route_result.status));
        std::string blind_fallback_error;
        if (append_blind_target_fallback(ctx, state, target.point, target.floor_y, out_path, &blind_fallback_error)) {
            return true;
        }
        if (ctx.cancelIfStopped(state)) {
            return false;
        }
        LogError << "Failed to plan NAVMESH waypoint." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(target.point.x)
                 << VAR(target.point.y) << VAR(navmesh::ToString(route_result.status));
        std::string detail = " 无法规划：" + route_error_text(route_result);
        if (!start_recovery_error.empty()) {
            detail += "；起点恢复失败：" + start_recovery_error;
        }
        if (!blind_fallback_error.empty()) {
            detail += "；盲走兜底失败：" + blind_fallback_error;
        }
        fail("route_unreachable", std::move(detail));
        return false;
    }

    bool commitWalking()
    {
        log_generated_navmesh_path(state, request, route_result);
        if (try_append_zipline_leg(ctx, state, target, &route_result, out_path)) {
            return true;
        }
        const size_t insert_index = out_path.size();
        if (!append_generated_waypoints(route_result.path, out_path, {}, &ctx.mesh.planner, route_result.path.zone_id)) {
            LogError << "NAVMESH planning returned an empty path." << VAR(state.navmesh_zone);
            fail("empty_route", " 的规划结果没有可执行路点");
            return false;
        }

        // 这一腿没追加点时末点属于上一腿, 不能盖
        if (target.deck_y && out_path.size() > insert_index) {
            out_path.back().target_deck_y = target.deck_y;
        }
        state.moveRouteStart(route_result.path.points.back());
        if (ctx.diagnostics != nullptr) {
            ctx.diagnostics->push_back(std::move(route_diagnostic));
        }
        const size_t path_point_count = route_result.path.points.size();
        const size_t appended_waypoints = out_path.size() - insert_index;
        const size_t route_waypoints = out_path.size();
        const int64_t expand_ms = ElapsedMs(expand_started_at);
        LogInfo << "Expanded NAVMESH waypoint." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(path_point_count)
                << VAR(insert_index) << VAR(appended_waypoints) << VAR(route_waypoints) << VAR(start_recovered) << VAR(plan_ms)
                << VAR(expand_ms);
        return true;
    }
};

bool append_navmesh_leg(
    ExpansionContext& ctx,
    LegState& state,
    const Waypoint& waypoint,
    LegMode mode,
    std::optional<size_t> authored_index,
    std::vector<Waypoint>& out_path)
{
    NavmeshLeg leg {
        .ctx = ctx,
        .state = state,
        .out_path = out_path,
        .waypoint = waypoint,
        .mode = mode,
        .authored_index = authored_index,
    };
    return leg.append();
}

bool append_authored_waypoint(
    ExpansionContext& ctx,
    LegState& state,
    const Waypoint& waypoint,
    size_t authored_index,
    std::vector<Waypoint>& out_path)
{
    if (is_navmesh_waypoint(waypoint)) {
        return append_navmesh_leg(ctx, state, waypoint, LegMode::Authored, authored_index, out_path);
    }
    const auto projected_waypoint = project_authored_target(ctx, waypoint);
    if (!projected_waypoint) {
        ctx.failWaypoint("unknown_target_tier", " 的坐标层级不存在", waypoint, authored_index, state);
        return false;
    }
    out_path.push_back(*projected_waypoint);
    update_state_from_regular_waypoint(*projected_waypoint, state);
    return true;
}

bool is_action_at_previous_point(const std::vector<Waypoint>& path, size_t begin, size_t end)
{
    if (begin == 0 || begin + 1 != end) {
        return false;
    }
    const Waypoint& previous = path[begin - 1];
    const Waypoint& action = path[end - 1];
    const bool same_deck =
        !action.target_deck_y || (previous.target_deck_y && std::abs(*previous.target_deck_y - *action.target_deck_y) <= 1e-6);
    return previous.hasPosition() && previous.target_tier == action.target_tier && action.action != ActionType::NAVMESH
           && action.action != ActionType::RUN && same_deck
           && std::hypot(action.x - previous.x, action.y - previous.y) <= kSameRoutePointWu;
}

Waypoint make_global_target(const Waypoint& endpoint)
{
    Waypoint global_target(endpoint.x, endpoint.y, ActionType::NAVMESH);
    global_target.strict_arrival = true;
    global_target.zone_id = endpoint.zone_id;
    global_target.target_tier = endpoint.target_tier;
    global_target.target_deck_y = endpoint.target_deck_y;
    return global_target;
}

bool append_global_route_group(ExpansionContext& ctx, LegState& state, size_t begin, size_t end, std::vector<Waypoint>& out_path)
{
    if (begin >= end) {
        return true;
    }

    const std::vector<Waypoint>& path = ctx.param.path;
    const Waypoint& authored_endpoint = path[end - 1];
    const bool endpoint_is_hard = authored_endpoint.closesGlobalRouteGroup();
    size_t target_index = end;
    while (target_index > begin && !path[target_index - 1].hasPosition()) {
        --target_index;
    }
    if (target_index == begin) {
        if (endpoint_is_hard) {
            return append_authored_waypoint(ctx, state, authored_endpoint, end - 1, out_path);
        }
        LogInfo << "Skipped optional route actions without a position target." << VAR(begin) << VAR(end);
        return true;
    }

    const Waypoint& authored_target = path[target_index - 1];
    const auto projected_endpoint = project_authored_target(ctx, authored_target);
    if (!projected_endpoint) {
        ctx.failWaypoint("unknown_target_tier", " 的坐标层级不存在", authored_target, target_index - 1, state);
        return false;
    }

    // 动作点和上一作者点同坐标同面时直接追加, 照常规划拿到空路径会被当成走不通去找滑索
    if (endpoint_is_hard && is_action_at_previous_point(path, begin, end)) {
        out_path.push_back(*projected_endpoint);
        update_state_from_regular_waypoint(*projected_endpoint, state);
        LogInfo << "Action point sits where the previous leg ended; appended without planning." << VAR(projected_endpoint->x)
                << VAR(projected_endpoint->y) << VAR(static_cast<int>(authored_endpoint.action));
        return true;
    }

    const Waypoint global_target = make_global_target(*projected_endpoint);
    const LegState original_state = state;
    const size_t original_path_size = out_path.size();
    if (append_navmesh_leg(ctx, state, global_target, LegMode::GlobalProbe, target_index - 1, out_path)) {
        if (endpoint_is_hard && !authored_endpoint.hasPosition()) {
            if (!append_authored_waypoint(ctx, state, authored_endpoint, end - 1, out_path)) {
                return false;
            }
        }
        else if (endpoint_is_hard && authored_endpoint.action != ActionType::NAVMESH && authored_endpoint.action != ActionType::RUN) {
            out_path.push_back(*projected_endpoint);
            update_state_from_regular_waypoint(*projected_endpoint, state);
        }
        const size_t skipped_hints = end - begin - (endpoint_is_hard ? 1 : 0);
        LogInfo << "Expanded authored route globally." << VAR(skipped_hints) << VAR(authored_target.x) << VAR(authored_target.y)
                << VAR(endpoint_is_hard);
        return true;
    }
    if (ctx.cancelIfStopped(state)) {
        return false;
    }
    if (ctx.no_go_rejected) {
        return false;
    }

    state = original_state;
    out_path.resize(original_path_size);
    LogInfo << "Global authored route unavailable; replaying authored hints." << VAR(begin) << VAR(end) << VAR(authored_target.x)
            << VAR(authored_target.y) << VAR(endpoint_is_hard);
    for (size_t index = begin; index < end; ++index) {
        if (!append_authored_waypoint(ctx, state, path[index], index, out_path)) {
            return false;
        }
    }
    return true;
}

void append_zone_declaration(LegState& state, const Waypoint& waypoint, std::vector<Waypoint>& out_path)
{
    state.current_zone = waypoint.zone_id;
    out_path.push_back(waypoint);
}

void stamp_authored_group(std::vector<Waypoint>& out_path, size_t output_begin, size_t group_begin)
{
    for (size_t output = output_begin; output < out_path.size(); ++output) {
        out_path[output].authored_group_begin = group_begin;
    }
}

size_t global_route_group_end(const std::vector<Waypoint>& path, size_t begin)
{
    size_t end = begin;
    while (end < path.size() && !path[end].isZoneDeclaration()) {
        if (path[end++].closesGlobalRouteGroup()) {
            break;
        }
    }
    return end;
}

bool append_authored_route(ExpansionContext& ctx, LegState& state, std::vector<Waypoint>& out_path)
{
    const std::vector<Waypoint>& path = ctx.param.path;
    for (size_t index = 0; index < path.size(); ++index) {
        if (ctx.cancelIfStopped(state)) {
            return false;
        }
        const size_t output_begin = out_path.size();
        if (path[index].isZoneDeclaration()) {
            append_zone_declaration(state, path[index], out_path);
        }
        else if (!append_authored_waypoint(ctx, state, path[index], index, out_path)) {
            return false;
        }
        stamp_authored_group(out_path, output_begin, index);
    }
    return true;
}

bool append_globally_planned_route(ExpansionContext& ctx, LegState& state, std::vector<Waypoint>& out_path)
{
    const std::vector<Waypoint>& path = ctx.param.path;
    for (size_t index = 0; index < path.size();) {
        if (ctx.cancelIfStopped(state)) {
            return false;
        }
        const size_t output_begin = out_path.size();
        const bool zone_declaration = path[index].isZoneDeclaration();
        const size_t group_end = zone_declaration ? index + 1 : global_route_group_end(path, index);
        if (zone_declaration) {
            append_zone_declaration(state, path[index], out_path);
        }
        else if (!append_global_route_group(ctx, state, index, group_end, out_path)) {
            return false;
        }
        stamp_authored_group(out_path, output_begin, index);
        index = group_end;
    }
    return true;
}

std::optional<std::string> infer_preload_navmesh_zone(const NaviParam& param)
{
    std::string current_zone = param.map_name;
    for (const Waypoint& waypoint : param.path) {
        if (waypoint.isZoneDeclaration()) {
            current_zone = waypoint.zone_id;
            continue;
        }
        const bool projects_to_base = param.normalize_position_via_navmesh && has_explicit_coordinate_frame(waypoint);
        if (is_navmesh_waypoint(waypoint) || projects_to_base) {
            std::string navmesh_zone = InferBaseNavZone(current_zone, param.map_name);
            if (navmesh_zone.empty() && !waypoint.target_tier.empty()) {
                navmesh_zone = InferBaseNavZone(waypoint.target_tier, param.map_name);
            }
            if (navmesh_zone.empty()) {
                return std::nullopt;
            }
            return navmesh_zone;
        }
        if (!waypoint.zone_id.empty()) {
            current_zone = waypoint.zone_id;
        }
    }
    return std::nullopt;
}

} // namespace

std::string InitialExpectedZone(const NaviParam& param)
{
    if (param.path.empty()) {
        return {};
    }
    const std::string expected_zone = param.path.front().zone_id.empty() ? param.map_name : param.path.front().zone_id;
    return IsBaseNavZoneName(expected_zone) ? std::string() : expected_zone;
}

void PreloadNavmeshWaypoints(const NaviParam& param)
{
    const auto navmesh_zone = infer_preload_navmesh_zone(param);
    if (!navmesh_zone) {
        return;
    }
    PreloadNavmesh(ResolveNavmeshFilePath(param.navmesh_file), *navmesh_zone);
}

bool ExpandNavmeshWaypoints(
    const NaviParam& param,
    const NaviPosition& initial_pos,
    const std::function<bool()>& should_stop,
    std::vector<Waypoint>& out_path,
    std::vector<NavmeshRouteDiagnostic>* out_diagnostics,
    const std::vector<VirtualNoGoDisc>* no_go,
    std::optional<double> start_deck_y)
{
    NavmeshExpansionFailure& failure = g_expansion_failure;
    failure = {};
    if (out_diagnostics != nullptr) {
        out_diagnostics->clear();
    }
    const bool contains_navmesh = contains_navmesh_waypoint(param.path);
    const bool needs_global_planning = param.zipline_enabled && contains_global_route_target(param.path);
    const bool needs_regular_projection =
        param.normalize_position_via_navmesh && std::any_of(param.path.begin(), param.path.end(), [](const Waypoint& waypoint) {
            return has_explicit_coordinate_frame(waypoint) && (waypoint.hasPosition() || waypoint.closesGlobalRouteGroup());
        });
    if (!contains_navmesh && !needs_global_planning && !needs_regular_projection) {
        out_path = param.path;
        return true;
    }

    const std::string current_zone = initial_pos.zone_id.empty() ? param.map_name : initial_pos.zone_id;
    LegState state {
        .route_start = { .x = initial_pos.x, .y = initial_pos.y },
        .route_start_floor_y = initial_pos.floor_y,
        .route_start_deck_y = start_deck_y,
        .current_zone = current_zone,
        .navmesh_zone = InferBaseNavZone(current_zone, param.map_name),
    };
    if (state.navmesh_zone.empty()) {
        LogError << "Failed to infer NAVMESH base zone." << VAR(state.current_zone) << VAR(param.map_name);
        failure = zone_failure(
            "zone_unresolved",
            std::format("无法根据当前位置区域 {} 与 map_name {} 推断 navmesh 区域", state.current_zone, param.map_name),
            state);
        if (!contains_navmesh && !needs_regular_projection) {
            out_path = param.path;
            LogInfo << "Global route planning unavailable; keeping the authored path.";
            return true;
        }
        return false;
    }

    const std::filesystem::path navmesh_path = ResolveNavmeshFilePath(param.navmesh_file);
    const auto expand_started_at = std::chrono::steady_clock::now();
    const auto mesh = LoadCachedNavmesh(navmesh_path, state.navmesh_zone);
    const int64_t navmesh_load_ms = ElapsedMs(expand_started_at);
    if (!mesh) {
        if (!contains_navmesh && !needs_regular_projection) {
            out_path = param.path;
            LogInfo << "Global route navmesh unavailable; keeping the authored path." << VAR_AS(state->navmesh_zone, state.navmesh_zone);
            return true;
        }
        failure = zone_failure(
            "navmesh_load_failed",
            std::format("无法加载区域 {} 的 navmesh 数据（{}）", state.navmesh_zone, MAA_NS::path_to_utf8_string(navmesh_path)),
            state);
        return false;
    }

    out_path.clear();
    ExpansionContext ctx {
        .param = param,
        .mesh = *mesh,
        .should_stop = should_stop,
        .diagnostics = out_diagnostics,
        .no_go = no_go,
        .rope_off = param.rope_interact_text.empty() ? nullptr : &param.rope_off,
        .failure = failure,
    };
    const bool expanded =
        needs_global_planning ? append_globally_planned_route(ctx, state, out_path) : append_authored_route(ctx, state, out_path);
    if (!expanded) {
        if (out_diagnostics != nullptr) {
            out_diagnostics->clear();
        }
        if (failure.message.empty()) {
            failure = zone_failure("expansion_failed", "路线展开失败，未返回具体原因", state);
        }
        return false;
    }
    size_t rope_rides = 0;
    for (Waypoint& waypoint : out_path) {
        if (waypoint.rope_ride) {
            waypoint.interact_text = param.rope_interact_text;
            ++rope_rides;
        }
    }
    const int64_t expand_total_ms = ElapsedMs(expand_started_at);
    const size_t authored_waypoints = param.path.size();
    const size_t expanded_waypoints = out_path.size();
    LogInfo << "NAVMESH route expansion finished." << VAR_AS(state->navmesh_zone, state.navmesh_zone) << VAR(authored_waypoints)
            << VAR(expanded_waypoints) << VAR(rope_rides) << VAR(navmesh_load_ms) << VAR(expand_total_ms);
    return true;
}

NavmeshExpansionFailure CurrentNavmeshExpansionFailure()
{
    return g_expansion_failure;
}

std::optional<navmesh::BaseNavRouteResult> PlanNavmeshRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const RouteEndpoints& endpoints,
    NavmeshRouteDiagnostic* out_diagnostic,
    const std::vector<VirtualNoGoDisc>* no_go,
    std::optional<navmesh::WorldPoint>* out_start_recovery_entry)
{
    const navmesh::WorldPoint& start = endpoints.start;
    const navmesh::WorldPoint& goal = endpoints.goal;
    const std::string navmesh_zone = InferBaseNavZone(locator_zone, param.map_name);
    if (navmesh_zone.empty()) {
        LogWarn << "Failed to infer NAVMESH base zone." << VAR(locator_zone) << VAR(param.map_name);
        return std::nullopt;
    }

    const std::filesystem::path navmesh_path = ResolveNavmeshFilePath(param.navmesh_file);
    const auto load_started_at = std::chrono::steady_clock::now();
    const auto mesh = LoadCachedNavmesh(navmesh_path, navmesh_zone);
    const int64_t navmesh_load_ms = ElapsedMs(load_started_at);
    if (!mesh) {
        return std::nullopt;
    }

    const auto request = build_route_request(mesh->pack, locator_zone, navmesh_zone, endpoints, no_go);
    const auto plan_started_at = std::chrono::steady_clock::now();
    auto route_result = plan_corridor_route(*mesh, request, {}, out_diagnostic);
    // 禁区判掉的不接, 盲走会径直穿过禁区; 入口搜索半径可能超出盲走预算, 所以再卡一次距离
    if (!route_result.ok() && out_start_recovery_entry != nullptr && route_result.status != navmesh::BaseNavRouteStatus::NoGo) {
        const auto entry = find_start_recovery_entry(param, *mesh, navmesh_zone, locator_zone, request, nullptr);
        if (entry && entry->distance <= kStartRecoveryMaxBlindWalk) {
            navmesh::BaseNavRouteRequest recovered = request;
            recovered.start = entry->point;
            auto retry = plan_corridor_route(*mesh, recovered, {}, out_diagnostic);
            if (retry.ok()) {
                LogInfo << "NAVMESH replan start off mesh; walking onto the nearest mesh point first." << VAR(navmesh_zone)
                        << VAR(locator_zone) << VAR(start.x) << VAR(start.y) << VAR(entry->point.x) << VAR(entry->point.y)
                        << VAR(entry->distance);
                *out_start_recovery_entry = entry->point;
                route_result = std::move(retry);
            }
        }
    }
    const int64_t plan_ms = ElapsedMs(plan_started_at);
    if (!route_result.ok()) {
        LogWarn << "Failed to plan NAVMESH route." << VAR(navmesh_zone) << VAR(locator_zone) << VAR(start.x) << VAR(start.y) << VAR(goal.x)
                << VAR(goal.y) << VAR(navmesh::ToString(route_result.status)) << VAR(plan_ms);
        return std::nullopt;
    }

    LogInfo << "NAVMESH route planned." << VAR(navmesh_zone) << VAR(locator_zone) << VAR(route_result.cost)
            << VAR(route_result.path.points.size()) << VAR(plan_ms) << VAR(navmesh_load_ms);
    return route_result;
}

bool AppendGeneratedNavmeshWaypoints(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::BaseNavRouteResult& route,
    std::vector<Waypoint>& out_path,
    const GeneratedWaypointOptions& options)
{
    const ZoneNavmesh zone = LoadZoneNavmesh(param, locator_zone);
    if (!zone.mesh) {
        LogWarn << "Generated navmesh waypoints emitted without a drivability check." << VAR(locator_zone)
                << VAR_AS(navmesh_zone, zone.navmesh_zone);
        return append_generated_waypoints(route.path, out_path, options);
    }
    return append_generated_waypoints(route.path, out_path, options, &zone.mesh->planner, route.path.zone_id);
}

} // namespace mapnavigator
