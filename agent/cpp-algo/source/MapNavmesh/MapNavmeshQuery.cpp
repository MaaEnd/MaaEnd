#include "MapNavmeshQuery.h"

#include <algorithm>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <exception>
#include <filesystem>
#include <fstream>
#include <memory>
#include <mutex>
#include <optional>
#include <string>
#include <unordered_map>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>

#include "../MapNavigator/navi_param_parser.h"
#include "../MapNavigator/navmesh_path_expander.h"
#include "../MapNavigator/zipline_leg_planner.h"
#include "../Navmesh/BaseNavGeometry.h"
#include "../Navmesh/BaseNavPlanner.h"
#include "../Navmesh/BaseNavReader.h"
#include "../Navmesh/RecastNavGrid.h"
#include "../Navmesh/RecastNavRoute.h"

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace mapnavmesh
{

namespace
{

// 探针往外找多远才算"最近的网格点": 取运行时两个盲走预算里更大的那个, 再远也已经无解。
constexpr double kOffMeshSearchRadius = std::max(mapnavigator::kStartRecoveryMaxBlindWalk, mapnavigator::kBlindTargetMaxExtension);
// 路线预览拼接相邻片段时，只消除计算噪声造成的同点重复。
constexpr double kPreviewPointMergeEpsilon = 1e-6;

struct QueryParam
{
    std::string op;
    std::string navmesh_file;
    int zone_id = -1;
    std::vector<std::vector<double>> points;
    std::vector<double> point;
    std::vector<double> start;
    std::vector<double> goal;
    double snap_radius = 5.0;
    // 空 = 不限层 / 不限面，meojson 没有 optional，用空槽表达。
    std::vector<double> floor_y;
    std::vector<double> goal_deck_y;
    std::vector<double> position;
    std::string position_zone;
    json::value custom_action_param = json::object {};
    std::string out_file;

    MEO_JSONIZATION(
        op,
        MEO_OPT navmesh_file,
        MEO_OPT zone_id,
        MEO_OPT points,
        MEO_OPT point,
        MEO_OPT start,
        MEO_OPT goal,
        MEO_OPT snap_radius,
        MEO_OPT floor_y,
        MEO_OPT goal_deck_y,
        MEO_OPT position,
        MEO_OPT position_zone,
        MEO_OPT custom_action_param,
        MEO_OPT out_file)
};

// 整包常驻：几何区元信息要全，路线又要按区取网格，一份全量加载同时喂得起两边。
struct QueryContext
{
    navmesh::BaseNavPack pack;
    std::optional<navmesh::BaseNavPlanner> planner;
    std::optional<navmesh::recast::RecastNavEngine> engine;
};

std::mutex g_contexts_mutex;
std::unordered_map<std::string, std::unique_ptr<QueryContext>> g_contexts;

json::object Fail(const std::string& message)
{
    return json::object { { "ok", false }, { "error", message } };
}

// 建好就不再销毁，指针长期有效；调用方只在建的时候持锁。
QueryContext* AcquireContext(const std::string& configured_path, std::string& error)
{
    std::error_code ec;
    const std::filesystem::path resolved = mapnavigator::ResolveNavmeshFilePath(configured_path);
    std::filesystem::path absolute = std::filesystem::absolute(resolved, ec);
    if (ec) {
        absolute = resolved;
    }
    const std::string key = absolute.lexically_normal().string();

    const std::lock_guard<std::mutex> lock(g_contexts_mutex);
    const auto found = g_contexts.find(key);
    if (found != g_contexts.end()) {
        return found->second.get();
    }

    auto loaded = navmesh::LoadBaseNavPack(key, {});
    if (!loaded.ok()) {
        error = loaded.message.empty() ? navmesh::ToString(loaded.status) : loaded.message;
        LogError << "load navmesh failed" << VAR(key) << VAR(error);
        return nullptr;
    }

    auto context = std::make_unique<QueryContext>();
    context->pack = std::move(*loaded.pack);
    context->planner.emplace(context->pack);
    context->engine.emplace(context->pack, *context->planner);
    context->pack.releaseLinks();
    QueryContext* raw = context.get();
    g_contexts.emplace(key, std::move(context));
    LogInfo << "navmesh loaded" << VAR(key);
    return raw;
}

float FloorOrNone(const std::vector<double>& slot)
{
    return slot.empty() ? navmesh::kBaseNavFloorYNone : static_cast<float>(slot.front());
}

json::object BuildZones(const navmesh::BaseNavPack& pack)
{
    json::array zones;
    for (const navmesh::BaseNavZone& zone : pack.zones()) {
        json::object item {
            { "zone_id", zone.zone_id },
            { "name", zone.name },
            { "is_tier", navmesh::IsTierZone(zone) },
            { "geometry_zone_id", pack.geometryZoneId(zone.zone_id) },
            { "width", static_cast<double>(zone.width) },
            { "height", static_cast<double>(zone.height) },
            { "transform",
              json::array { static_cast<double>(zone.transform[0]),
                            static_cast<double>(zone.transform[1]),
                            static_cast<double>(zone.transform[2]),
                            static_cast<double>(zone.transform[3]) } },
            { "floor_y", zone.floor_y > navmesh::kBaseNavFloorYValidMin ? json::value(static_cast<double>(zone.floor_y)) : json::value() },
            { "triangle_count", zone.triangle_count },
        };
        zones.emplace_back(std::move(item));
    }
    return json::object { { "ok", true }, { "zones", std::move(zones) } };
}

// 顶点按被引用的全局下标升序去重后重编号，与前端读的 NMSH 布局一致。
json::object BuildMesh(const QueryContext& context, uint16_t zone_id, const std::string& out_file)
{
    if (out_file.empty()) {
        return Fail("mesh 需要 out_file");
    }
    const navmesh::BaseNavZone* zone = context.pack.findZone(context.pack.geometryZoneId(zone_id));
    if (zone == nullptr || zone->triangle_count == 0) {
        return Fail("该区无三角面");
    }

    const auto& triangles = context.pack.triangles();
    const auto& vertices = context.pack.vertices();
    const uint32_t first = zone->first_triangle;
    const uint32_t count = zone->triangle_count;

    std::vector<uint32_t> refs;
    refs.reserve(static_cast<size_t>(count) * 3);
    for (uint32_t i = 0; i < count; ++i) {
        const auto& tri = triangles[first + i];
        refs.push_back(tri.vertices[0]);
        refs.push_back(tri.vertices[1]);
        refs.push_back(tri.vertices[2]);
    }
    std::vector<uint32_t> unique_global = refs;
    std::sort(unique_global.begin(), unique_global.end());
    unique_global.erase(std::unique(unique_global.begin(), unique_global.end()), unique_global.end());

    const uint32_t vertex_count = static_cast<uint32_t>(unique_global.size());
    std::vector<uint8_t> blob;
    blob.resize(16 + static_cast<size_t>(vertex_count) * 12 + refs.size() * 4);
    uint8_t* cursor = blob.data();
    // 前端读的 mesh 线格式(gl/renderer.js setMesh): 小端 "NMSH" + u32 版本 + u32 顶点数 + u32 三角数,
    // magic 与版本都被前端校验, 对不上直接 throw。
    std::memcpy(cursor, "NMSH", 4);
    cursor += 4;
    for (const uint32_t field : { 1U, vertex_count, count }) {
        std::memcpy(cursor, &field, 4);
        cursor += 4;
    }
    for (const uint32_t global : unique_global) {
        const navmesh::BaseNavVertex& vertex = vertices[global];
        const float xyz[3] = { vertex.u, vertex.v, vertex.height };
        std::memcpy(cursor, xyz, 12);
        cursor += 12;
    }
    for (const uint32_t global : refs) {
        const uint32_t local =
            static_cast<uint32_t>(std::lower_bound(unique_global.begin(), unique_global.end(), global) - unique_global.begin());
        std::memcpy(cursor, &local, 4);
        cursor += 4;
    }

    std::ofstream out(out_file, std::ios::binary);
    if (!out.is_open()) {
        return Fail("写不开 " + out_file);
    }
    out.write(reinterpret_cast<const char*>(blob.data()), static_cast<std::streamsize>(blob.size()));
    out.close();
    if (!out) {
        return Fail("写入失败 " + out_file);
    }

    return json::object { { "ok", true },
                          { "path", out_file },
                          { "bytes", blob.size() },
                          { "vertex_count", vertex_count },
                          { "triangle_count", count } };
}

// 吸得上就什么都不报（运行时会悄悄吸过去）；吸不上才给最近网格点，让调用方标出这个点掉在网格外。
json::value ProbeOffMesh(
    const navmesh::BaseNavPlanner& planner,
    uint16_t geom_zone_id,
    const navmesh::WorldPoint& point,
    double snap_radius,
    float floor_y,
    const json::value& budget)
{
    if (planner.snap(geom_zone_id, point, snap_radius, floor_y).has_value()) {
        return {};
    }
    const auto nearest = planner.snap(geom_zone_id, point, kOffMeshSearchRadius, floor_y);
    if (!nearest) {
        return json::object { { "distance", json::value() }, { "nearest", json::value() }, { "budget", budget } };
    }
    return json::object { { "distance", nearest->distance },
                          { "nearest", json::array { nearest->point.x, nearest->point.y } },
                          { "budget", budget } };
}

json::object BuildOffMeshProbe(const QueryContext& context, const QueryParam& param)
{
    const uint16_t geom = context.pack.geometryZoneId(static_cast<uint16_t>(param.zone_id));
    const float floor_y = FloorOrNone(param.floor_y);
    json::array results;
    for (const std::vector<double>& raw : param.points) {
        if (raw.size() < 2) {
            results.emplace_back(json::value());
            continue;
        }
        const navmesh::WorldPoint point { .x = raw[0], .y = raw[1] };
        results.emplace_back(ProbeOffMesh(*context.planner, geom, point, param.snap_radius, floor_y, json::value()));
    }
    return json::object { { "ok", true }, { "results", std::move(results) } };
}

// 同一个二维坐标底下可能压着走廊/天桥/屋顶好几层，按高度归并成可选面。
json::object BuildDeckProbe(const QueryContext& context, const QueryParam& param)
{
    if (param.point.size() < 2) {
        return Fail("point 需为 [x, y]");
    }
    const uint16_t geom = context.pack.geometryZoneId(static_cast<uint16_t>(param.zone_id));
    const navmesh::WorldPoint point { .x = param.point[0], .y = param.point[1] };
    const navmesh::BaseNavPlanner& planner = *context.planner;

    struct Hit
    {
        double height = 0.0;
        double distance = 0.0;
        uint32_t triangle = 0;
    };

    std::vector<Hit> found;
    for (const uint32_t triangle : planner.candidateTriangles(geom, point, navmesh::recast::kCS)) {
        const auto vertices = planner.trianglePoints(triangle);
        double distance = 0.0;
        if (!navmesh::detail::PointInTriangle(point, vertices)) {
            const navmesh::WorldPoint closest = navmesh::detail::ClosestPointOnTriangle(point, vertices);
            distance = navmesh::detail::Distance(closest, point);
            if (distance > navmesh::recast::kCS) {
                continue;
            }
        }
        found.push_back({ .height = planner.triangleHeight(triangle), .distance = distance, .triangle = triangle });
    }
    std::sort(found.begin(), found.end(), [](const Hit& lhs, const Hit& rhs) {
        return lhs.distance != rhs.distance ? lhs.distance < rhs.distance : lhs.triangle < rhs.triangle;
    });

    std::vector<Hit> decks;
    for (const Hit& hit : found) {
        const bool merged = std::any_of(decks.begin(), decks.end(), [&](const Hit& kept) {
            return std::abs(kept.height - hit.height) <= navmesh::recast::kDeckBand;
        });
        if (!merged) {
            decks.push_back(hit);
        }
    }
    std::stable_sort(decks.begin(), decks.end(), [](const Hit& lhs, const Hit& rhs) { return lhs.height > rhs.height; });

    json::array out;
    for (const Hit& deck : decks) {
        out.emplace_back(
            json::object { { "height", deck.height },
                           // 寻路认这张面的高度带, 前端拿它高亮同层三角形
                           { "band", json::array { deck.height - navmesh::recast::kDeckBand, deck.height + navmesh::recast::kDeckBand } },
                           { "on_surface", deck.distance == 0.0 },
                           // 烘焙出来的墙顶/檐口薄片, 不是真地面
                           { "thin", planner.isSmallIslandTriangle(deck.triangle) } });
    }
    return json::object { { "ok", true }, { "decks", std::move(out) } };
}

json::object BuildRoute(QueryContext& context, const QueryParam& param)
{
    if (param.start.size() < 2 || param.goal.size() < 2) {
        return Fail("start/goal 需为 [x, y]");
    }
    const uint16_t geom = context.pack.geometryZoneId(static_cast<uint16_t>(param.zone_id));
    const navmesh::BaseNavZone* zone = context.pack.findZone(geom);
    if (zone == nullptr) {
        return Fail("未知 zone: " + std::to_string(param.zone_id));
    }

    const navmesh::WorldPoint start { .x = param.start[0], .y = param.start[1] };
    const navmesh::WorldPoint goal { .x = param.goal[0], .y = param.goal[1] };
    const float floor_y = FloorOrNone(param.floor_y);
    const auto plan = context.engine->plan(zone->name, start, goal, floor_y, floor_y, FloorOrNone(param.goal_deck_y), {}, {}, {});

    if (!plan.ok) {
        // 失败时带上两端的离网探针，调用方才能标出是哪个点掉在网格外。
        return json::object {
            { "ok", false },
            { "error", plan.error },
            { "off_mesh",
              json::object {
                  { "start",
                    ProbeOffMesh(
                        *context.planner,
                        geom,
                        start,
                        param.snap_radius,
                        floor_y,
                        json::value(mapnavigator::kStartRecoveryMaxBlindWalk)) },
                  { "goal",
                    ProbeOffMesh(
                        *context.planner,
                        geom,
                        goal,
                        param.snap_radius,
                        floor_y,
                        json::value(mapnavigator::kBlindTargetMaxExtension)) },
              } },
        };
    }

    json::array points;
    for (const navmesh::WorldPoint& p : plan.points) {
        points.emplace_back(json::array { p.x, p.y });
    }
    json::object debug {
        { "window",
          json::object { { "x0", plan.debug.x0 },
                         { "y0", plan.debug.y0 },
                         { "nx", plan.debug.nx },
                         { "ny", plan.debug.ny },
                         { "cell_size", plan.debug.cell_size } } },
        { "topology_cells", json::array() },
        { "topology_heights", json::array() },
        { "taut_points", json::array() },
        { "pulled_points", json::array() },
        { "assembled_points", json::array() },
        { "planned_points", json::array() },
        { "warnings", json::array() },
    };
    for (const auto& p : plan.debug.topology_cells) {
        debug["topology_cells"].as_array().emplace_back(json::array { p.x, p.y });
    }
    for (const double height : plan.debug.topology_heights) {
        debug["topology_heights"].as_array().emplace_back(height);
    }
    for (const auto& p : plan.debug.taut_points) {
        debug["taut_points"].as_array().emplace_back(json::array { p.x, p.y });
    }
    for (const auto& p : plan.debug.pulled_points) {
        debug["pulled_points"].as_array().emplace_back(json::array { p.x, p.y });
    }
    for (const auto& p : plan.debug.assembled_points) {
        debug["assembled_points"].as_array().emplace_back(json::array { p.x, p.y });
    }
    for (const auto& p : plan.debug.planned_points) {
        debug["planned_points"].as_array().emplace_back(json::array { p.x, p.y });
    }
    for (const auto& warning : plan.debug.warnings) {
        debug["warnings"].as_array().emplace_back(warning);
    }
    const auto ss = context.planner->snap(geom, start, param.snap_radius, floor_y);
    const auto sg = context.planner->snap(geom, goal, param.snap_radius, floor_y);
    json::object snap {
        { "start",
          ss ? json::object { { "point", json::array { ss->point.x, ss->point.y } },
                              { "triangle", ss->triangle },
                              { "distance", ss->distance },
                              { "component", context.planner->componentId(ss->triangle) },
                              { "component_size", context.planner->componentSize(ss->triangle) } }
             : json::value() },
        { "goal",
          sg ? json::object { { "point", json::array { sg->point.x, sg->point.y } },
                              { "triangle", sg->triangle },
                              { "distance", sg->distance },
                              { "component", context.planner->componentId(sg->triangle) },
                              { "component_size", context.planner->componentSize(sg->triangle) } }
             : json::value() },
    };
    debug["snap"] = std::move(snap);
    debug["connectivity"] =
        json::object { { "same_component",
                         ss && sg && context.planner->componentId(ss->triangle) == context.planner->componentId(sg->triangle) },
                       { "reachable", true } };
    return json::object { { "ok", true }, { "points", std::move(points) }, { "cost", plan.length }, { "debug", std::move(debug) } };
}

bool SamePoint(const navmesh::WorldPoint& lhs, const navmesh::WorldPoint& rhs)
{
    return std::hypot(lhs.x - rhs.x, lhs.y - rhs.y) < kPreviewPointMergeEpsilon;
}

void AppendDistinct(std::vector<navmesh::WorldPoint>& points, const navmesh::WorldPoint& point)
{
    if (points.empty() || !SamePoint(points.back(), point)) {
        points.push_back(point);
    }
}

json::array PointsToJson(const std::vector<navmesh::WorldPoint>& points)
{
    json::array out;
    for (const navmesh::WorldPoint& point : points) {
        out.emplace_back(json::array { point.x, point.y });
    }
    return out;
}

json::array DiagnosticPointsToJson(
    const std::vector<navmesh::WorldPoint>& points,
    const std::vector<navmesh::WorldPoint>& cells,
    const std::vector<double>& heights)
{
    json::array out;
    for (const navmesh::WorldPoint& point : points) {
        double height = 0.0;
        bool have_height = false;
        const size_t count = std::min(cells.size(), heights.size());
        double best_distance = 0.0;
        for (size_t i = 0; i < count; ++i) {
            const double distance = std::hypot(point.x - cells[i].x, point.y - cells[i].y);
            if (!have_height || distance < best_distance) {
                have_height = true;
                best_distance = distance;
                height = heights[i];
            }
        }
        if (have_height) {
            out.emplace_back(json::array { point.x, point.y, height });
        }
        else {
            out.emplace_back(json::array { point.x, point.y });
        }
    }
    return out;
}

json::object DiagnosticToJson(const mapnavigator::NavmeshRouteDiagnostic& diagnostic)
{
    json::array warnings;
    for (const std::string& warning : diagnostic.warnings) {
        warnings.emplace_back(warning);
    }
    return json::object {
        { "window",
          json::object {
              { "x0", diagnostic.x0 },
              { "y0", diagnostic.y0 },
              { "nx", diagnostic.nx },
              { "ny", diagnostic.ny },
              { "cell_size", diagnostic.cell_size },
          } },
        { "start", json::array { diagnostic.start.x, diagnostic.start.y } },
        { "goal", json::array { diagnostic.goal.x, diagnostic.goal.y } },
        { "timing_ms",
          json::object {
              { "window", diagnostic.timing.window_ms },
              { "topology", diagnostic.timing.topology_ms },
              { "geometry", diagnostic.timing.geometry_ms },
              { "pull", diagnostic.timing.pull_ms },
              { "assemble", diagnostic.timing.assemble_ms },
              { "lift", diagnostic.timing.lift_ms },
              { "total", diagnostic.timing.total_ms },
          } },
        { "topology_cells", DiagnosticPointsToJson(diagnostic.topology_cells, diagnostic.topology_cells, diagnostic.topology_heights) },
        { "taut_points", DiagnosticPointsToJson(diagnostic.taut_points, diagnostic.topology_cells, diagnostic.topology_heights) },
        { "pulled_points", DiagnosticPointsToJson(diagnostic.pulled_points, diagnostic.topology_cells, diagnostic.topology_heights) },
        { "assembled_points",
          DiagnosticPointsToJson(diagnostic.assembled_points, diagnostic.topology_cells, diagnostic.topology_heights) },
        { "planned_points", DiagnosticPointsToJson(diagnostic.planned_points, diagnostic.topology_cells, diagnostic.topology_heights) },
        { "warnings", std::move(warnings) },
    };
}

json::object BuildRoutePreview(const QueryParam& query)
{
    if (query.position.size() < 2 || query.position_zone.empty()) {
        return Fail("route_preview 需要 position=[x,y] 与 position_zone");
    }
    if (!query.custom_action_param.is_object()) {
        return Fail("route_preview 的 custom_action_param 必须是对象");
    }

    mapnavigator::NaviParam param;
    if (!mapnavigator::TryParseNaviParam(query.custom_action_param, param, "MapNavmeshQuery route_preview")) {
        return Fail("custom_action_param 解析失败");
    }
    if (param.path.empty()) {
        return Fail("route_preview 的 path 不能为空");
    }
    param.navmesh_file = query.navmesh_file;
    param.normalize_position_via_navmesh = true;

    mapnavigator::NaviPosition position {
        .x = query.position[0],
        .y = query.position[1],
        // 预览里点了哪一层就按哪一层吸起点；空槽等同实机，落回区的主层。
        .floor_y = query.floor_y.empty() ? std::optional<double> {} : std::optional<double> { query.floor_y.front() },
        .zone_id = query.position_zone,
    };
    mapnavigator::NormalizeLivePositionToBase(param, position);

    mapnavigator::ResetZiplineOutcome();
    std::vector<mapnavigator::Waypoint> expanded;
    std::vector<mapnavigator::NavmeshRouteDiagnostic> diagnostics;
    if (!mapnavigator::ExpandNavmeshWaypoints(
            param,
            position,
            [] { return false; },
            expanded,
            &diagnostics)) {
        const mapnavigator::NavmeshExpansionFailure failure = mapnavigator::CurrentNavmeshExpansionFailure();
        json::object result = Fail(failure.message.empty() ? "路线展开失败" : failure.message);
        json::object detail {
            { "code", failure.code },
            { "message", failure.message },
            { "zone_id", failure.zone_id },
            { "target_tier", failure.target_tier },
            { "route_status", failure.route_status },
            { "route_error", failure.route_error },
        };
        if (failure.authored_index) {
            detail.emplace("authored_index", *failure.authored_index);
        }
        if (failure.segment_start) {
            detail.emplace("segment_start", json::array { failure.segment_start->x, failure.segment_start->y });
        }
        if (failure.segment_goal) {
            detail.emplace("segment_goal", json::array { failure.segment_goal->x, failure.segment_goal->y });
        }
        if (failure.gap_start) {
            detail.emplace("gap_start", json::array { failure.gap_start->x, failure.gap_start->y });
        }
        if (failure.gap_goal) {
            detail.emplace("gap_goal", json::array { failure.gap_goal->x, failure.gap_goal->y });
        }
        if (failure.gap_distance) {
            detail.emplace("gap_distance", *failure.gap_distance);
        }
        if (failure.target) {
            detail.emplace("target", json::array { failure.target->x, failure.target->y });
        }
        if (failure.target_deck_y) {
            detail.emplace("target_deck_y", *failure.target_deck_y);
        }
        result.emplace("failure", std::move(detail));
        return result;
    }

    std::vector<navmesh::WorldPoint> all_points;
    std::vector<navmesh::WorldPoint> current_walk;
    json::array walk_segments;
    json::array zipline_segments;
    const navmesh::WorldPoint start { .x = position.x, .y = position.y };
    AppendDistinct(all_points, start);
    AppendDistinct(current_walk, start);

    const auto flush_walk = [&]() {
        if (current_walk.size() >= 2) {
            walk_segments.emplace_back(PointsToJson(current_walk));
        }
        current_walk.clear();
    };

    for (const mapnavigator::Waypoint& waypoint : expanded) {
        if (!waypoint.HasPosition()) {
            continue;
        }
        const navmesh::WorldPoint point { .x = waypoint.x, .y = waypoint.y };
        AppendDistinct(all_points, point);
        AppendDistinct(current_walk, point);

        if (waypoint.action != mapnavigator::ActionType::ZIPLINE) {
            continue;
        }
        if (!waypoint.zipline_target) {
            return Fail("滑索展开结果缺少下索点");
        }

        flush_walk();
        const mapnavigator::ZiplineTarget& target = *waypoint.zipline_target;
        const navmesh::WorldPoint landing { .x = target.x, .y = target.y };
        json::object segment {
            { "from", json::array { point.x, point.y } },
            { "to", json::array { landing.x, landing.y } },
            { "from_height", waypoint.target_deck_y ? json::value(*waypoint.target_deck_y) : json::value() },
            { "to_height", target.height },
            { "elevation_deg", target.elevation_deg },
            { "authored_group_begin", waypoint.authored_group_begin },
        };
        if (waypoint.mount_restand) {
            segment.emplace("mount_restand", json::array { waypoint.mount_restand->x, waypoint.mount_restand->y });
        }
        zipline_segments.emplace_back(std::move(segment));
        AppendDistinct(all_points, landing);
        AppendDistinct(current_walk, landing);
    }
    flush_walk();

    json::array diagnostic_items;
    for (const mapnavigator::NavmeshRouteDiagnostic& diagnostic : diagnostics) {
        diagnostic_items.emplace_back(DiagnosticToJson(diagnostic));
    }

    const mapnavigator::ZiplineOutcome outcome = mapnavigator::CurrentZiplineOutcome();
    return json::object {
        { "ok", true },
        { "points", PointsToJson(all_points) },
        { "walk_segments", std::move(walk_segments) },
        { "zipline_segments", std::move(zipline_segments) },
        { "diagnostics", std::move(diagnostic_items) },
        { "expanded_waypoints", expanded.size() },
        { "zipline",
          json::object {
              { "requested", param.zipline_enabled },
              { "used", outcome.used },
              { "no_data", outcome.no_data },
              { "not_chosen", outcome.not_chosen },
          } },
    };
}

json::object BuildWarm(QueryContext& context, const QueryParam& param)
{
    const navmesh::BaseNavZone* zone = context.pack.findZone(context.pack.geometryZoneId(static_cast<uint16_t>(param.zone_id)));
    if (zone == nullptr || zone->triangle_count == 0) {
        return Fail("该区无三角面");
    }
    context.engine->warm(zone->name);
    return json::object { { "ok", true }, { "zone", zone->name } };
}

json::object Dispatch(const QueryParam& param)
{
    if (param.op.empty()) {
        return Fail("缺少 op");
    }
    if (param.op == "route_preview") {
        return BuildRoutePreview(param);
    }
    std::string error;
    QueryContext* context = AcquireContext(param.navmesh_file, error);
    if (context == nullptr) {
        return Fail("navmesh 加载失败: " + error);
    }

    if (param.op == "zones") {
        return BuildZones(context->pack);
    }
    if (param.op == "mesh") {
        return BuildMesh(*context, static_cast<uint16_t>(param.zone_id), param.out_file);
    }
    if (param.op == "offmesh_probe") {
        return BuildOffMeshProbe(*context, param);
    }
    if (param.op == "deck_probe") {
        return BuildDeckProbe(*context, param);
    }
    if (param.op == "route") {
        return BuildRoute(*context, param);
    }
    if (param.op == "warm") {
        return BuildWarm(*context, param);
    }
    return Fail("未知 op: " + param.op);
}

std::optional<QueryParam> ParseParam(const char* custom_recognition_param)
{
    if (custom_recognition_param == nullptr || *custom_recognition_param == '\0') {
        return std::nullopt;
    }
    const auto parsed = json::parse(custom_recognition_param);
    if (!parsed) {
        return std::nullopt;
    }
    QueryParam value {};
    if (!value.from_json(*parsed)) {
        // from_json 中途失败会留下写了一半的字段，整个丢掉。
        return std::nullopt;
    }
    return value;
}

}

MaaBool MAA_CALL MapNavmeshQueryRun(
    [[maybe_unused]] MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    [[maybe_unused]] const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    constexpr char kExceptionDetail[] = R"({"ok":false,"error":"navmesh 查询异常"})";
    try {
        const auto param = ParseParam(custom_recognition_param);
        if (!param) {
            LogWarn << "invalid param" << VAR(custom_recognition_param);
        }
        const json::object result = param ? Dispatch(*param) : Fail("param 解析失败");

        if (out_detail != nullptr) {
            const std::string detail = json::value(result).dumps();
            if (MaaStringBufferSet(out_detail, detail.c_str()) == MAA_FALSE) {
                LogError << "failed to write navmesh query detail";
                return MAA_FALSE;
            }
        }
        if (out_box != nullptr) {
            *out_box = { 0, 0, 1, 1 };
        }
        // 恒命中：查不出来也要把原因带回调用方，返回 false 会让 detail 一并丢掉。
        return MAA_TRUE;
    }
    catch (const std::exception& error) {
        LogError << "navmesh query threw an exception" << VAR(error.what());
    }
    catch (...) {
        LogError << "navmesh query threw an unknown exception";
    }

    if (out_detail != nullptr && MaaStringBufferSet(out_detail, kExceptionDetail) == MAA_FALSE) {
        LogError << "failed to write navmesh query exception detail";
        return MAA_FALSE;
    }
    if (out_box != nullptr) {
        *out_box = { 0, 0, 1, 1 };
    }
    return MAA_TRUE;
}

}
