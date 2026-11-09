#include <algorithm>
#include <array>
#include <chrono>
#include <cmath>
#include <cstddef>
#include <filesystem>
#include <format>
#include <functional>
#include <iterator>
#include <limits>
#include <memory>
#include <optional>
#include <string>
#include <unordered_set>
#include <utility>
#include <vector>

#include <MaaFramework/MaaAPI.h>
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
};

void move_route_start(LegState* state, const navmesh::WorldPoint& point)
{
    state->route_start = point;
    state->route_start_floor_y.reset();
    state->route_start_deck_y.reset();
}

RouteEndpoints leg_endpoints(const LegState& state, const navmesh::WorldPoint& goal, float goal_floor_y, std::optional<double> goal_deck_y)
{
    return RouteEndpoints {
        .start = state.route_start,
        .start_floor_y = state.route_start_floor_y,
        .start_deck_y = state.route_start_deck_y,
        .goal = goal,
        .goal_floor_y = goal_floor_y,
        .goal_deck_y = goal_deck_y,
    };
}

constexpr double kBlindTargetFallbackSnapRadius = 12.0;
constexpr double kBlindTargetProbeStep = 2.0;
constexpr int64_t kBlindTargetProbeBudgetMs = 8000;

std::string describe_authored_waypoint(const Waypoint& waypoint, std::optional<size_t> authored_index)
{
    std::string description = authored_index ? std::format("第 {} 个路点", *authored_index + 1) : "当前路点";
    if (waypoint.has_position) {
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
    const NaviParam* param = nullptr;
    const CachedNavmesh* mesh = nullptr;
    MaaContext* maa_context = nullptr;
    std::vector<NavmeshRouteDiagnostic>* diagnostics = nullptr;
    const std::vector<VirtualNoGoDisc>* no_go = nullptr;
    // 判定不可用的升降索, 空指针表示这次不用升降索
    const std::vector<uint32_t>* rope_off = nullptr;
    NavmeshExpansionFailure* failure = nullptr;
};

navmesh::BaseNavRouteRequest leg_request(
    const ExpansionContext& ctx,
    const LegState& state,
    const navmesh::WorldPoint& goal,
    float goal_floor_y,
    std::optional<double> goal_deck_y)
{
    return build_route_request(
        ctx.mesh->pack,
        state.current_zone,
        state.navmesh_zone,
        leg_endpoints(state, goal, goal_floor_y, goal_deck_y),
        ctx.no_go,
        ctx.rope_off);
}

bool cancel_if_stopped(ExpansionContext* ctx, const LegState& state)
{
    const bool stopping = ctx->maa_context != nullptr && MaaTaskerStopping(MaaContextGetTasker(ctx->maa_context));
    if (!stopping) {
        return false;
    }
    *ctx->failure = zone_failure("cancelled", "路线规划已取消", state);
    return true;
}

void fail_waypoint(
    ExpansionContext* ctx,
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
    if (waypoint.has_position) {
        recorded.target = navmesh::WorldPoint { .x = waypoint.x, .y = waypoint.y };
    }
    if (route_result != nullptr) {
        recorded.route_status = navmesh::ToString(route_result->status);
        recorded.route_error = route_result->error;
        recorded.gap_start = route_result->gap_start;
        recorded.gap_goal = route_result->gap_goal;
        recorded.gap_distance = route_result->gap_distance;
    }
    *ctx->failure = std::move(recorded);
}

bool is_navmesh_waypoint(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::NAVMESH && waypoint.has_position;
}

bool contains_navmesh_waypoint(const std::vector<Waypoint>& path)
{
    return std::any_of(path.begin(), path.end(), is_navmesh_waypoint);
}

bool contains_positioned_waypoint(const std::vector<Waypoint>& path)
{
    return std::any_of(path.begin(), path.end(), [](const Waypoint& waypoint) { return waypoint.has_position; });
}

bool has_explicit_coordinate_frame(const Waypoint& waypoint)
{
    return !waypoint.target_tier.empty() && (waypoint.has_position || waypoint.heading_uses_target);
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

void update_state_from_regular_waypoint(LegState* state, const Waypoint& waypoint)
{
    if (waypoint.has_position) {
        move_route_start(state, { .x = waypoint.x, .y = waypoint.y });
    }
    if (!waypoint.zone_id.empty()) {
        state->current_zone = waypoint.zone_id;
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
    if (is_navmesh_waypoint(waypoint) || !ctx.param->normalize_position_via_navmesh) {
        return waypoint;
    }
    return project_regular_waypoint_to_base(ctx.mesh->pack, waypoint);
}

void move_plan_debug(NavmeshRouteDiagnostic* out, navmesh::recast::RecastPlanResult::Debug* debug)
{
    out->timing.window_ms = debug->timing.window_ms;
    out->timing.topology_ms = debug->timing.topology_ms;
    out->timing.geometry_ms = debug->timing.geometry_ms;
    out->timing.pull_ms = debug->timing.pull_ms;
    out->timing.assemble_ms = debug->timing.assemble_ms;
    out->timing.lift_ms = debug->timing.lift_ms;
    out->timing.total_ms = debug->timing.total_ms;
    out->x0 = debug->x0;
    out->y0 = debug->y0;
    out->nx = debug->nx;
    out->ny = debug->ny;
    out->cell_size = debug->cell_size;
    out->topology_cells = std::move(debug->topology_cells);
    out->topology_heights = std::move(debug->topology_heights);
    out->taut_points = std::move(debug->taut_points);
    out->pulled_points = std::move(debug->pulled_points);
    out->assembled_points = std::move(debug->assembled_points);
    out->planned_points = std::move(debug->planned_points);
    out->warnings = std::move(debug->warnings);
}

navmesh::BaseNavRouteResult plan_corridor_route(
    const CachedNavmesh& mesh,
    const navmesh::BaseNavRouteRequest& request,
    MaaContext* maa_context,
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
    std::function<bool()> should_stop;
    if (maa_context != nullptr) {
        should_stop = [maa_context] {
            return MaaTaskerStopping(MaaContextGetTasker(maa_context));
        };
    }
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
        move_plan_debug(out_diagnostic, &plan.debug);
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

double clearance_at(const navmesh::WorldPath& path, size_t index)
{
    return index < path.clearance.size() ? path.clearance[index] : 0.0;
}

std::optional<double> height_at(const navmesh::WorldPath& path, size_t index)
{
    return index < path.heights.size() ? std::optional<double>(path.heights[index]) : std::nullopt;
}

void mark_drop(std::vector<Waypoint>* out_path, const navmesh::WorldPath& path, size_t index)
{
    for (const navmesh::DropLanding& drop : path.drops) {
        if (drop.index == index && index > 0) {
            Waypoint& landing = out_path->back();
            landing.strict_arrival = true;
            landing.target_deck_y = drop.height;
            landing.drop_from = std::array<double, 2> { path.points[index - 1].x, path.points[index - 1].y };
        }
    }
}

void mark_rope(std::vector<Waypoint>* out_path, const navmesh::WorldPath& path, size_t index)
{
    for (const navmesh::RopeRide& rope : path.ropes) {
        if (rope.index != index || index == 0) {
            continue;
        }
        Waypoint top = std::move(out_path->back());
        out_path->pop_back();
        const navmesh::WorldPoint& base = path.points[index - 1];
        if (out_path->empty() || out_path->back().x != base.x || out_path->back().y != base.y) {
            out_path->push_back(Waypoint { .x = base.x, .y = base.y, .action = ActionType::RUN });
            out_path->back().corridor_clearance = clearance_at(path, index - 1);
            out_path->back().route_floor_y = height_at(path, index - 1);
        }
        Waypoint& bottom = out_path->back();
        bottom.action = ActionType::INTERACT;
        bottom.strict_arrival = true;
        bottom.rope_ride = RopeRideInfo { .uid = rope.uid, .ride_s = rope.ride_s, .top_height = rope.top_height };
        top.strict_arrival = true;
        top.target_deck_y = rope.top_height;
        top.rope_top = true;
        out_path->push_back(std::move(top));
    }
}

void emit_point(std::vector<Waypoint>* out_path, const navmesh::WorldPath& path, size_t index, bool strict_arrival)
{
    out_path->push_back(Waypoint { .x = path.points[index].x, .y = path.points[index].y, .action = ActionType::RUN });
    out_path->back().strict_arrival = strict_arrival;
    out_path->back().corridor_clearance = clearance_at(path, index);
    out_path->back().route_floor_y = height_at(path, index);
    mark_drop(out_path, path, index);
    mark_rope(out_path, path, index);
}

void append_pulled_corners(
    std::vector<Waypoint>* out_path,
    const navmesh::WorldPath& path,
    const navmesh::BaseNavPlanner* drivability_planner,
    uint16_t drivable_zone_id,
    size_t last_anchor,
    size_t anchor)
{
    // 规划器已拉直的路线直接照抄下标, 这里的判据在叠层处会把楼下那层的捷径判成可走
    if (!path.waypoints.empty() && last_anchor == 0 && anchor + 1 == path.points.size()) {
        for (size_t index = 0; index + 1 < path.waypoints.size(); ++index) {
            emit_point(out_path, path, path.waypoints[index], false);
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
            swallowed_clearance = std::min(swallowed_clearance, clearance_at(path, reach + 1));
            ++reach;
        }
        // 走廊自己的下一条边也可能判不过, 照样前进一格, 保证循环推进
        if (reach == cursor) {
            ++reach;
        }
        if (reach >= anchor) {
            return;
        }
        emit_point(out_path, path, reach, false);
        cursor = reach;
    }
}

bool append_generated_waypoints(
    std::vector<Waypoint>* out_path,
    const navmesh::WorldPath& world_path,
    const GeneratedWaypointOptions& options,
    const navmesh::BaseNavPlanner* drivability_planner = nullptr,
    uint16_t drivable_zone_id = 0)
{
    if (world_path.points.empty()) {
        return false;
    }

    size_t last_anchor = 0;
    const size_t total = world_path.points.size();
    const size_t loop_end = options.include_goal ? total : total - 1;
    const std::unordered_set<size_t> segment_breaks(world_path.segment_breaks.begin(), world_path.segment_breaks.end());
    for (size_t index = 1; index < loop_end; ++index) {
        if (!segment_breaks.contains(index)) {
            continue;
        }
        const size_t emit_idx = index - 1;
        if (emit_idx == 0) {
            continue;
        }
        append_pulled_corners(out_path, world_path, drivability_planner, drivable_zone_id, last_anchor, emit_idx);
        emit_point(out_path, world_path, emit_idx, options.strict_segment_breaks);
        last_anchor = emit_idx;
    }

    if (total >= 2) {
        if (options.include_goal) {
            append_pulled_corners(out_path, world_path, drivability_planner, drivable_zone_id, last_anchor, total - 1);
            emit_point(out_path, world_path, total - 1, true);
        }
        else {
            append_pulled_corners(out_path, world_path, drivability_planner, drivable_zone_id, last_anchor, total - 1);
        }
    }

    return true;
}

bool fail_with(std::string* out_error, std::string message)
{
    if (out_error != nullptr) {
        *out_error = std::move(message);
    }
    return false;
}

bool append_blind_target_fallback(
    ExpansionContext* ctx,
    LegState* state,
    std::vector<Waypoint>* out_path,
    std::string* out_error,
    const navmesh::WorldPoint& base_target,
    float goal_floor_y)
{
    const navmesh::WorldPoint target = base_target;
    const navmesh::WorldPoint start = state->route_start;
    const double total = std::hypot(target.x - start.x, target.y - start.y);
    if (total < 1e-6) {
        return fail_with(out_error, "目标与起点重合，但没有生成有效路线");
    }
    const navmesh::BaseNavZone* zone = ctx->mesh->pack.findZoneByName(state->navmesh_zone);
    if (zone == nullptr) {
        return fail_with(out_error, "找不到当前区域的 navmesh");
    }
    const double snap_radius = std::max(ctx->param->navmesh_snap_radius, kBlindTargetFallbackSnapRadius);
    const float snap_floor =
        goal_floor_y > navmesh::kBaseNavFloorYValidMin ? goal_floor_y : ctx->mesh->pack.floorYForZoneName(state->current_zone);

    // 探针剩余缺口至少是 offset - snap_radius, 超出这个上限的探针必然过不了缺口检查
    const double probe_limit = std::min(total, kBlindTargetMaxExtension + snap_radius);

    navmesh::BaseNavRouteResult approach;
    NavmeshRouteDiagnostic accepted_diagnostic;
    bool found = false;
    double blind_gap = 0.0;
    const auto probe_started_at = std::chrono::steady_clock::now();
    for (double offset = 0.0; offset <= probe_limit + 1e-6; offset += kBlindTargetProbeStep) {
        if (ctx->maa_context != nullptr && MaaTaskerStopping(MaaContextGetTasker(ctx->maa_context))) {
            return fail_with(out_error, "规划已取消");
        }
        const int64_t probe_elapsed_ms = ElapsedMs(probe_started_at);
        if (probe_elapsed_ms > kBlindTargetProbeBudgetMs) {
            LogWarn << "NAVMESH blind-target fallback gave up: probe budget exhausted." << VAR(state->navmesh_zone)
                    << VAR(state->current_zone) << VAR(target.x) << VAR(target.y) << VAR(offset) << VAR(probe_limit)
                    << VAR(probe_elapsed_ms);
            return fail_with(out_error, std::format("目标附近探测超过 {} ms 预算", kBlindTargetProbeBudgetMs));
        }
        const double t = std::min(offset / total, 1.0);
        const navmesh::WorldPoint probe {
            .x = target.x + (start.x - target.x) * t,
            .y = target.y + (start.y - target.y) * t,
        };
        const auto entry = ctx->mesh->planner.snap(zone->zone_id, probe, snap_radius, snap_floor);
        if (!entry) {
            continue;
        }
        const auto request = leg_request(*ctx, *state, entry->point, goal_floor_y, std::nullopt);
        NavmeshRouteDiagnostic diagnostic;
        const auto route = plan_corridor_route(*ctx->mesh, request, ctx->maa_context, ctx->diagnostics == nullptr ? nullptr : &diagnostic);
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
        return fail_with(out_error, "目标附近没有与起点连通的网格接近点");
    }
    if (blind_gap > kBlindTargetMaxExtension) {
        LogWarn << "NAVMESH blind-target fallback rejected: residual gap too large." << VAR(state->navmesh_zone) << VAR(state->current_zone)
                << VAR(target.x) << VAR(target.y) << VAR(blind_gap) << VAR(kBlindTargetMaxExtension);
        return fail_with(out_error, std::format("剩余盲走距离 {:.2f}px 超过 {:.2f}px 上限", blind_gap, kBlindTargetMaxExtension));
    }

    if (!append_generated_waypoints(out_path, approach.path, {}, &ctx->mesh->planner, approach.path.zone_id)) {
        return fail_with(out_error, "网格接近路线没有生成可执行路点");
    }
    if (ctx->diagnostics != nullptr) {
        ctx->diagnostics->push_back(std::move(accepted_diagnostic));
    }
    if (blind_gap > kWaypointArrivalSlack) {
        out_path->push_back(Waypoint { .x = target.x, .y = target.y, .action = ActionType::RUN });
        out_path->back().strict_arrival = false;
    }
    move_route_start(state, target);
    LogInfo << "NAVMESH blind-target fallback applied." << VAR(state->navmesh_zone) << VAR(state->current_zone) << VAR(target.x)
            << VAR(target.y) << VAR(blind_gap) << VAR(approach.path.points.back().x) << VAR(approach.path.points.back().y);
    return true;
}

bool append_start_recovery(
    LegState* state,
    std::vector<Waypoint>* out_path,
    std::string* out_error,
    const ExpansionContext& ctx,
    const navmesh::BaseNavRouteRequest& request)
{
    const auto entry = find_start_recovery_entry(*ctx.param, *ctx.mesh, state->navmesh_zone, state->current_zone, request, out_error);
    if (!entry) {
        return false;
    }

    out_path->push_back(Waypoint { .x = entry->point.x, .y = entry->point.y, .action = ActionType::RUN });
    out_path->back().strict_arrival = false;
    move_route_start(state, entry->point);
    LogInfo << "NAVMESH start off mesh; walking onto the nearest mesh point first." << VAR(state->navmesh_zone) << VAR(state->current_zone)
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

// walking_path 非空时滑索只在严格更省时顶替走路, 为空时用来桥接步行不连通的两侧
bool try_append_zipline_leg(
    ExpansionContext* ctx,
    LegState* state,
    std::vector<Waypoint>* out_path,
    const ProjectedTarget& target,
    const navmesh::WorldPath* walking_path)
{
    auto route = PlanZiplineRoute(
        *ctx->param,
        {
            .locator_zone = state->current_zone,
            .navmesh_zone = state->navmesh_zone,
            .start = state->route_start,
            .goal = target.point,
            .walking_path = walking_path,
            .goal_floor_y = target.floor_y,
            .goal_deck_y = target.deck_y,
            .start_floor_y = state->route_start_floor_y,
            .start_deck_y = state->route_start_deck_y,
            .maa_context = ctx->maa_context,
            .capture_diagnostics = ctx->diagnostics != nullptr,
            .no_go = ctx->no_go,
        });
    if (!route || route->approach.points.empty() || route->departure.points.empty() || route->towers.size() < 2) {
        return false;
    }

    const size_t insert_index = out_path->size();
    // 上索点在下面自己补, 起点已在索下时通用追加一个点都不会产出
    append_generated_waypoints(out_path, route->approach, { .include_goal = false }, &ctx->mesh->planner, route->approach.zone_id);
    const navmesh::WorldPoint mount = route->approach.points.back();
    // 架子坐标是角格锚点, 走到跟前常碰不到设备, 所以头一跳瞄第一个站位
    const navmesh::WorldPoint mount_spot = route->mount_spots.empty() ? mount : route->mount_spots.front();
    for (size_t hop = 0; hop + 1 < route->towers.size(); ++hop) {
        const zipline::ZiplineNode& from = route->towers[hop];
        const zipline::ZiplineNode& to = route->towers[hop + 1];
        const ZiplineNodeRef from_ref = ToNodeRef(from);
        const ZiplineNodeRef to_ref = ToNodeRef(to);

        out_path->push_back(
            Waypoint {
                .x = hop == 0 ? mount_spot.x : from.x,
                .y = hop == 0 ? mount_spot.y : from.y,
                .action = ActionType::ZIPLINE,
            });
        out_path->back().strict_arrival = true;
        out_path->back().target_deck_y = from.height;

        ZiplineHopPlan hop_plan;
        hop_plan.mount = from_ref;
        hop_plan.landing = to_ref;
        hop_plan.planned_elevation_deg = CalcElevation(from_ref, to_ref);
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
        out_path->back().zipline_hop = hop_plan;
    }

    // landing 是下索段末端, 这一跳的落点是 dismount
    const size_t last_hop_index = out_path->size() - 1;
    const ZiplineNodeRef dismount = (*out_path)[last_hop_index].zipline_hop->landing;
    const size_t departure_index = out_path->size();
    const navmesh::WorldPoint landing = route->departure.points.back();
    append_generated_waypoints(out_path, route->departure, {}, &ctx->mesh->planner, route->departure.zone_id);
    if (out_path->size() == departure_index) {
        out_path->push_back(Waypoint { .x = landing.x, .y = landing.y, .action = ActionType::RUN });
        out_path->back().strict_arrival = true;
    }
    (*out_path)[last_hop_index].zipline_hop->exit_aim = pick_exit_aim_point(*out_path, departure_index, dismount);
    if (target.deck_y) {
        out_path->back().target_deck_y = target.deck_y;
    }

    move_route_start(state, landing);
    if (ctx->diagnostics != nullptr) {
        ctx->diagnostics->insert(
            ctx->diagnostics->end(),
            std::make_move_iterator(route->diagnostics.begin()),
            std::make_move_iterator(route->diagnostics.end()));
    }
    const bool walking_baseline_available = walking_path != nullptr;
    LogInfo << "Expanded NAVMESH waypoint via zipline." << VAR(state->navmesh_zone) << VAR(state->current_zone)
            << VAR(walking_baseline_available) << VAR(route->cost) << VAR(insert_index) << VAR(out_path->size() - insert_index)
            << VAR(route->towers.size()) << VAR(mount_spot.x) << VAR(mount_spot.y) << VAR(route->mount_spots.size())
            << VAR(route->towers.back().x) << VAR(route->towers.back().y);
    return true;
}

struct NavmeshLegPlan
{
    ProjectedTarget target {};
    navmesh::BaseNavRouteRequest request {};
    navmesh::BaseNavRouteResult route_result {};
    NavmeshRouteDiagnostic route_diagnostic {};
    bool start_recovered = false;
    std::string start_recovery_error {};
    int64_t plan_ms = 0;
};

NavmeshRouteDiagnostic* diagnostic_sink(const ExpansionContext& ctx, NavmeshRouteDiagnostic* diagnostic)
{
    return ctx.diagnostics == nullptr ? nullptr : diagnostic;
}

void plan_leg_with_start_recovery(NavmeshLegPlan* leg, LegState* state, std::vector<Waypoint>* out_path, const ExpansionContext& ctx)
{
    const auto plan_started_at = std::chrono::steady_clock::now();
    leg->route_result = plan_corridor_route(*ctx.mesh, leg->request, ctx.maa_context, diagnostic_sink(ctx, &leg->route_diagnostic));
    if (!leg->route_result.ok() && leg->route_result.status != navmesh::BaseNavRouteStatus::NoGo) {
        if (append_start_recovery(state, out_path, &leg->start_recovery_error, ctx, leg->request)) {
            leg->request.start = state->route_start;
            leg->route_diagnostic = {};
            leg->route_result = plan_corridor_route(*ctx.mesh, leg->request, ctx.maa_context, diagnostic_sink(ctx, &leg->route_diagnostic));
            leg->start_recovered = true;
        }
    }
    leg->plan_ms = ElapsedMs(plan_started_at);
}

bool handle_unreachable_leg(
    ExpansionContext* ctx,
    LegState* state,
    std::vector<Waypoint>* out_path,
    const NavmeshLegPlan& leg,
    const Waypoint& waypoint,
    std::optional<size_t> authored_index)
{
    // 下面的兜底都不看可走面, 禁区必须先拦下, 否则会穿过禁区
    if (leg.route_result.status == navmesh::BaseNavRouteStatus::NoGo) {
        LogError << "NAVMESH waypoint rejected by a virtual no-go zone." << VAR(state->navmesh_zone) << VAR(state->current_zone)
                 << VAR(leg.target.point.x) << VAR(leg.target.point.y) << VAR(leg.route_result.error);
        fail_waypoint(
            ctx,
            "route_no_go",
            " 无法规划：" + leg.route_result.error,
            waypoint,
            authored_index,
            *state,
            &leg.route_result,
            leg.target.point);
        return false;
    }
    // 滑索能桥接步行不连通的两侧, 优先于盲走和作者提示回退
    if (try_append_zipline_leg(ctx, state, out_path, leg.target, nullptr)) {
        return true;
    }
    if (cancel_if_stopped(ctx, *state)) {
        return false;
    }
    // 盲走兜底只看二维, 有面声明时用它会把错层当成到达
    if (leg.target.deck_y) {
        LogError << "Failed to plan NAVMESH waypoint on the declared deck." << VAR(state->navmesh_zone) << VAR(state->current_zone)
                 << VAR(leg.target.point.x) << VAR(leg.target.point.y) << VAR(*leg.target.deck_y)
                 << VAR(navmesh::ToString(leg.route_result.status));
        const std::string route_error = route_error_text(leg.route_result);
        const bool disconnected = leg.route_result.gap_start.has_value() && leg.route_result.gap_goal.has_value();
        fail_waypoint(
            ctx,
            disconnected ? "route_disconnected" : "target_deck_unreachable",
            disconnected ? std::format(" 的指定目标面与起点不连通（target_deck_y={:.2f}）：{}", *leg.target.deck_y, route_error)
                         : std::format(" 无法抵达指定目标面（target_deck_y={:.2f}）：{}", *leg.target.deck_y, route_error),
            waypoint,
            authored_index,
            *state,
            &leg.route_result,
            leg.target.point);
        return false;
    }
    LogWarn << "NAVMESH waypoint not directly reachable; attempting blind-target fallback." << VAR(state->navmesh_zone)
            << VAR(state->current_zone) << VAR(leg.target.point.x) << VAR(leg.target.point.y)
            << VAR(navmesh::ToString(leg.route_result.status));
    std::string blind_fallback_error;
    if (append_blind_target_fallback(ctx, state, out_path, &blind_fallback_error, leg.target.point, leg.target.floor_y)) {
        return true;
    }
    if (cancel_if_stopped(ctx, *state)) {
        return false;
    }
    LogError << "Failed to plan NAVMESH waypoint." << VAR(state->navmesh_zone) << VAR(state->current_zone) << VAR(leg.target.point.x)
             << VAR(leg.target.point.y) << VAR(navmesh::ToString(leg.route_result.status));
    std::string detail = " 无法规划：" + route_error_text(leg.route_result);
    if (!leg.start_recovery_error.empty()) {
        detail += "；起点恢复失败：" + leg.start_recovery_error;
    }
    if (!blind_fallback_error.empty()) {
        detail += "；盲走兜底失败：" + blind_fallback_error;
    }
    fail_waypoint(ctx, "route_unreachable", std::move(detail), waypoint, authored_index, *state, &leg.route_result, leg.target.point);
    return false;
}

bool commit_walking_leg(
    ExpansionContext* ctx,
    LegState* state,
    std::vector<Waypoint>* out_path,
    NavmeshLegPlan* leg,
    const Waypoint& waypoint,
    std::optional<size_t> authored_index,
    TimePoint expand_started_at)
{
    log_generated_navmesh_path(*state, leg->request, leg->route_result);
    const size_t insert_index = out_path->size();
    if (!append_generated_waypoints(out_path, leg->route_result.path, {}, &ctx->mesh->planner, leg->route_result.path.zone_id)) {
        LogError << "NAVMESH planning returned an empty path." << VAR(state->navmesh_zone);
        fail_waypoint(
            ctx,
            "empty_route",
            " 的规划结果没有可执行路点",
            waypoint,
            authored_index,
            *state,
            &leg->route_result,
            leg->target.point);
        return false;
    }

    // 这一腿没追加点时末点属于上一腿, 不能盖
    if (leg->target.deck_y && out_path->size() > insert_index) {
        out_path->back().target_deck_y = leg->target.deck_y;
    }
    move_route_start(state, leg->route_result.path.points.back());
    if (ctx->diagnostics != nullptr) {
        ctx->diagnostics->push_back(std::move(leg->route_diagnostic));
    }
    const size_t path_point_count = leg->route_result.path.points.size();
    const size_t appended_waypoints = out_path->size() - insert_index;
    const size_t route_waypoints = out_path->size();
    const int64_t expand_ms = ElapsedMs(expand_started_at);
    LogInfo << "Expanded NAVMESH waypoint." << VAR(state->navmesh_zone) << VAR(state->current_zone) << VAR(path_point_count)
            << VAR(insert_index) << VAR(appended_waypoints) << VAR(route_waypoints) << VAR(leg->start_recovered) << VAR(leg->plan_ms)
            << VAR(expand_ms);
    return true;
}

bool append_navmesh_leg(
    ExpansionContext* ctx,
    LegState* state,
    std::vector<Waypoint>* out_path,
    const Waypoint& waypoint,
    std::optional<size_t> authored_index)
{
    const TimePoint expand_started_at = std::chrono::steady_clock::now();
    NavmeshLegPlan leg;
    leg.target = resolve_projected_target(ctx->mesh->pack, waypoint);
    leg.request = leg_request(*ctx, *state, leg.target.point, leg.target.floor_y, leg.target.deck_y);
    plan_leg_with_start_recovery(&leg, state, out_path, *ctx);
    if (!leg.route_result.ok()) {
        return handle_unreachable_leg(ctx, state, out_path, leg, waypoint, authored_index);
    }
    return commit_walking_leg(ctx, state, out_path, &leg, waypoint, authored_index, expand_started_at);
}

bool append_authored_waypoint(
    ExpansionContext* ctx,
    LegState* state,
    std::vector<Waypoint>* out_path,
    const Waypoint& waypoint,
    size_t authored_index)
{
    if (is_navmesh_waypoint(waypoint)) {
        return append_navmesh_leg(ctx, state, out_path, waypoint, authored_index);
    }
    const auto projected_waypoint = project_authored_target(*ctx, waypoint);
    if (!projected_waypoint) {
        fail_waypoint(ctx, "unknown_target_tier", " 的坐标层级不存在", waypoint, authored_index, *state);
        return false;
    }
    out_path->push_back(*projected_waypoint);
    update_state_from_regular_waypoint(state, *projected_waypoint);
    return true;
}

void stamp_authored_group(std::vector<Waypoint>* out_path, size_t output_begin, size_t group_begin)
{
    for (size_t output = output_begin; output < out_path->size(); ++output) {
        (*out_path)[output].authored_group_begin = group_begin;
    }
}

bool append_stamped_authored_waypoint(ExpansionContext* ctx, LegState* state, std::vector<Waypoint>* out_path, size_t authored_index)
{
    const size_t output_begin = out_path->size();
    if (!append_authored_waypoint(ctx, state, out_path, ctx->param->path[authored_index], authored_index)) {
        return false;
    }
    stamp_authored_group(out_path, output_begin, authored_index);
    return true;
}

// 不跨 ZONE, 止于第一个边界点并把它算在段内
size_t replaceable_span_end(const std::vector<Waypoint>& path, size_t begin)
{
    size_t end = begin;
    while (end < path.size() && !IsZoneDeclaration(path[end])) {
        if (EndsReplaceableSpan(path[end++])) {
            break;
        }
    }
    return end;
}

std::optional<size_t> last_positioned_index(const std::vector<Waypoint>& path, size_t begin, size_t end)
{
    for (size_t index = end; index > begin; --index) {
        if (path[index - 1].has_position) {
            return index - 1;
        }
    }
    return std::nullopt;
}

navmesh::WorldPath baseline_walk_path(const navmesh::WorldPoint& start, const std::vector<Waypoint>& out_path, size_t output_begin)
{
    navmesh::WorldPath walk;
    walk.points.push_back(start);
    for (size_t index = output_begin; index < out_path.size(); ++index) {
        if (out_path[index].has_position) {
            walk.points.push_back({ .x = out_path[index].x, .y = out_path[index].y });
        }
    }
    return walk;
}

// 整段都没经过规划时为空
std::optional<double> baseline_arrival_height(const std::vector<Waypoint>& out_path, size_t output_begin)
{
    for (size_t index = out_path.size(); index > output_begin; --index) {
        const Waypoint& waypoint = out_path[index - 1];
        if (waypoint.target_deck_y) {
            return waypoint.target_deck_y;
        }
        if (waypoint.route_floor_y) {
            return waypoint.route_floor_y;
        }
    }
    return std::nullopt;
}

bool rides_zipline(const std::vector<Waypoint>& out_path, size_t output_begin)
{
    return std::any_of(out_path.begin() + static_cast<std::ptrdiff_t>(output_begin), out_path.end(), [](const Waypoint& waypoint) {
        return waypoint.action == ActionType::ZIPLINE;
    });
}

// 先照作者路线展开当基线, 滑索比基线省够了才把这段走路整段换掉, 段尾的动作照常保留
bool append_replaceable_span(ExpansionContext* ctx, LegState* state, std::vector<Waypoint>* out_path, size_t begin, size_t end)
{
    const std::vector<Waypoint>& path = ctx->param->path;
    const LegState span_start = *state;
    const size_t output_begin = out_path->size();
    const size_t diagnostics_begin = ctx->diagnostics == nullptr ? 0 : ctx->diagnostics->size();
    for (size_t index = begin; index < end; ++index) {
        if (cancel_if_stopped(ctx, *state)) {
            return false;
        }
        if (!append_stamped_authored_waypoint(ctx, state, out_path, index)) {
            return false;
        }
    }

    const std::optional<size_t> target_index = last_positioned_index(path, begin, end);
    // 基线里已有的滑索是步行不通时的桥, 不再拿别的滑索去比
    if (!target_index || rides_zipline(*out_path, output_begin)) {
        return true;
    }

    // 叠层处按基线到达的那层落地, 只是偏好, 那层到不了时照常落最近的面
    ProjectedTarget target = resolve_projected_target(ctx->mesh->pack, path[*target_index]);
    if (target.floor_y <= navmesh::kBaseNavFloorYValidMin) {
        if (const std::optional<double> height = baseline_arrival_height(*out_path, output_begin)) {
            target.floor_y = static_cast<float>(*height);
        }
    }
    const navmesh::WorldPath baseline = baseline_walk_path(span_start.route_start, *out_path, output_begin);
    const size_t diagnostics_end = ctx->diagnostics == nullptr ? 0 : ctx->diagnostics->size();
    LegState shortcut_state = span_start;
    std::vector<Waypoint> shortcut;
    if (!try_append_zipline_leg(ctx, &shortcut_state, &shortcut, target, &baseline)) {
        return true;
    }

    out_path->resize(output_begin);
    out_path->insert(out_path->end(), std::make_move_iterator(shortcut.begin()), std::make_move_iterator(shortcut.end()));
    // 滑索失败后要从段首重新展开整段
    stamp_authored_group(out_path, output_begin, begin);
    *state = shortcut_state;
    if (ctx->diagnostics != nullptr) {
        const auto first = ctx->diagnostics->begin();
        ctx->diagnostics->erase(
            first + static_cast<std::ptrdiff_t>(diagnostics_begin),
            first + static_cast<std::ptrdiff_t>(diagnostics_end));
    }
    LogInfo << "Zipline replaced the walking of an authored span." << VAR(begin) << VAR(end) << VAR_AS(target_index, *target_index);
    for (size_t index = *target_index; index < end; ++index) {
        if (index == *target_index && IsMovementPoint(path[index])) {
            continue;
        }
        if (!append_stamped_authored_waypoint(ctx, state, out_path, index)) {
            return false;
        }
    }
    return true;
}

void append_zone_declaration(LegState* state, std::vector<Waypoint>* out_path, const Waypoint& waypoint)
{
    state->current_zone = waypoint.zone_id;
    out_path->push_back(waypoint);
}

bool append_authored_route(ExpansionContext* ctx, LegState* state, std::vector<Waypoint>* out_path)
{
    const std::vector<Waypoint>& path = ctx->param->path;
    for (size_t index = 0; index < path.size(); ++index) {
        if (cancel_if_stopped(ctx, *state)) {
            return false;
        }
        const size_t output_begin = out_path->size();
        if (IsZoneDeclaration(path[index])) {
            append_zone_declaration(state, out_path, path[index]);
        }
        else if (!append_authored_waypoint(ctx, state, out_path, path[index], index)) {
            return false;
        }
        stamp_authored_group(out_path, output_begin, index);
    }
    return true;
}

bool append_route_with_zipline_shortcuts(ExpansionContext* ctx, LegState* state, std::vector<Waypoint>* out_path)
{
    const std::vector<Waypoint>& path = ctx->param->path;
    for (size_t index = 0; index < path.size();) {
        if (cancel_if_stopped(ctx, *state)) {
            return false;
        }
        if (IsZoneDeclaration(path[index])) {
            append_zone_declaration(state, out_path, path[index]);
            stamp_authored_group(out_path, out_path->size() - 1, index);
            ++index;
            continue;
        }
        const size_t span_end = replaceable_span_end(path, index);
        if (!append_replaceable_span(ctx, state, out_path, index, span_end)) {
            return false;
        }
        index = span_end;
    }
    return true;
}

std::optional<std::string> infer_preload_navmesh_zone(const NaviParam& param)
{
    std::string current_zone = param.map_name;
    for (const Waypoint& waypoint : param.path) {
        if (IsZoneDeclaration(waypoint)) {
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
    std::vector<Waypoint>* out_path,
    NavmeshExpansionFailure* out_failure,
    const NaviParam& param,
    const NaviPosition& initial_pos,
    MaaContext* maa_context,
    std::vector<NavmeshRouteDiagnostic>* out_diagnostics,
    const std::vector<VirtualNoGoDisc>* no_go,
    std::optional<double> start_deck_y)
{
    NavmeshExpansionFailure discarded_failure;
    NavmeshExpansionFailure* failure = out_failure != nullptr ? out_failure : &discarded_failure;
    *failure = {};
    if (out_diagnostics != nullptr) {
        out_diagnostics->clear();
    }
    const bool contains_navmesh = contains_navmesh_waypoint(param.path);
    const bool plans_ziplines = param.zipline_enabled && contains_positioned_waypoint(param.path);
    const bool needs_regular_projection =
        param.normalize_position_via_navmesh && std::any_of(param.path.begin(), param.path.end(), has_explicit_coordinate_frame);
    if (!contains_navmesh && !plans_ziplines && !needs_regular_projection) {
        *out_path = param.path;
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
        *failure = zone_failure(
            "zone_unresolved",
            std::format("无法根据当前位置区域 {} 与 map_name {} 推断 navmesh 区域", state.current_zone, param.map_name),
            state);
        if (!contains_navmesh && !needs_regular_projection) {
            *out_path = param.path;
            LogInfo << "Zipline planning unavailable; keeping the authored path.";
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
            *out_path = param.path;
            LogInfo << "Zipline navmesh unavailable; keeping the authored path." << VAR_AS(state->navmesh_zone, state.navmesh_zone);
            return true;
        }
        *failure = zone_failure(
            "navmesh_load_failed",
            std::format("无法加载区域 {} 的 navmesh 数据（{}）", state.navmesh_zone, MAA_NS::path_to_utf8_string(navmesh_path)),
            state);
        return false;
    }

    out_path->clear();
    ExpansionContext ctx {
        .param = &param,
        .mesh = mesh.get(),
        .maa_context = maa_context,
        .diagnostics = out_diagnostics,
        .no_go = no_go,
        .rope_off = param.rope_interact_text.empty() ? nullptr : &param.rope_off,
        .failure = failure,
    };
    const bool expanded =
        plans_ziplines ? append_route_with_zipline_shortcuts(&ctx, &state, out_path) : append_authored_route(&ctx, &state, out_path);
    if (!expanded) {
        if (out_diagnostics != nullptr) {
            out_diagnostics->clear();
        }
        if (failure->message.empty()) {
            *failure = zone_failure("expansion_failed", "路线展开失败，未返回具体原因", state);
        }
        return false;
    }
    size_t rope_rides = 0;
    for (Waypoint& waypoint : *out_path) {
        if (waypoint.rope_ride) {
            waypoint.interact_text = param.rope_interact_text;
            ++rope_rides;
        }
    }
    const int64_t expand_total_ms = ElapsedMs(expand_started_at);
    const size_t authored_waypoints = param.path.size();
    const size_t expanded_waypoints = out_path->size();
    LogInfo << "NAVMESH route expansion finished." << VAR_AS(state->navmesh_zone, state.navmesh_zone) << VAR(authored_waypoints)
            << VAR(expanded_waypoints) << VAR(rope_rides) << VAR(navmesh_load_ms) << VAR(expand_total_ms);
    return true;
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
    auto route_result = plan_corridor_route(*mesh, request, nullptr, out_diagnostic);
    // 禁区判掉的不接, 盲走会径直穿过禁区; 入口搜索半径可能超出盲走预算, 所以再卡一次距离
    if (!route_result.ok() && out_start_recovery_entry != nullptr && route_result.status != navmesh::BaseNavRouteStatus::NoGo) {
        const auto entry = find_start_recovery_entry(param, *mesh, navmesh_zone, locator_zone, request, nullptr);
        if (entry && entry->distance <= kStartRecoveryMaxBlindWalk) {
            navmesh::BaseNavRouteRequest recovered = request;
            recovered.start = entry->point;
            auto retry = plan_corridor_route(*mesh, recovered, nullptr, out_diagnostic);
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
    std::vector<Waypoint>* out_path,
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::BaseNavRouteResult& route,
    const GeneratedWaypointOptions& options)
{
    const ZoneNavmesh zone = LoadZoneNavmesh(param, locator_zone);
    if (!zone.mesh) {
        LogWarn << "Generated navmesh waypoints emitted without a drivability check." << VAR(locator_zone)
                << VAR_AS(navmesh_zone, zone.navmesh_zone);
        return append_generated_waypoints(out_path, route.path, options);
    }
    return append_generated_waypoints(out_path, route.path, options, &zone.mesh->planner, route.path.zone_id);
}

} // namespace mapnavigator
