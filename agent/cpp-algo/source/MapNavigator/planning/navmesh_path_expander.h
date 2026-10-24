#pragma once

#include <algorithm>
#include <cmath>
#include <cstddef>
#include <cstdint>
#include <filesystem>
#include <functional>
#include <optional>
#include <string>
#include <vector>

#include "../../Navmesh/BaseNavPlanner.h"
#include "../../Navmesh/OccluderPack.h"
#include "../navi_domain_types.h"
#include "navmesh_diagnostics.h"

namespace mapnavigator
{

struct NaviParam;

// 按线程记录, ExpandNavmeshWaypoints 开头清空, 返回 false 后立即读取
struct NavmeshExpansionFailure
{
    std::string code;
    std::string message;
    std::optional<size_t> authored_index;
    std::string zone_id;
    std::optional<navmesh::WorldPoint> segment_start;
    std::optional<navmesh::WorldPoint> segment_goal;
    std::optional<navmesh::WorldPoint> gap_start;
    std::optional<navmesh::WorldPoint> gap_goal;
    std::optional<double> gap_distance;
    std::optional<navmesh::WorldPoint> target;
    std::string target_tier;
    std::optional<double> target_deck_y;
    std::string route_status;
    std::string route_error;
};

// 无引导时最多盲走多远, navmesh 不含水面, 终点要外延才够得着岸边目标
inline constexpr double kStartRecoveryMaxBlindWalk = 32.0;
inline constexpr double kBlindTargetMaxExtension = 30.0;

std::filesystem::path ResolveNavmeshFilePath(const std::string& configured_path = {});
// 相对 exe 定位: <exe>/../data/MapNavigator/nogo_zones.json
std::filesystem::path NoGoTablePath();
std::string InitialExpectedZone(const NaviParam& param);
// 原地把 tier 模板像素坐标换算到 navmesh 底图像素系, 其他 zone 原样不动
void NormalizeLivePositionToBase(const NaviParam& param, NaviPosition& pos);
void PreloadNavmeshWaypoints(const NaviParam& param);
// should_stop 在路点之间轮询, 状态机还没建好, 规划期间只有这里能响应停止
bool ExpandNavmeshWaypoints(
    const NaviParam& param,
    const NaviPosition& initial_pos,
    const std::function<bool()>& should_stop,
    std::vector<Waypoint>& out_path,
    std::vector<NavmeshRouteDiagnostic>* out_diagnostics = nullptr,
    const std::vector<VirtualNoGoDisc>* no_go = nullptr,
    std::optional<double> start_deck_y = std::nullopt);
NavmeshExpansionFailure CurrentNavmeshExpansionFailure();
// 给了 out_start_recovery_entry 时, 起点锚不上网格就改从盲走范围内最近的网格点规划, 该点由此传回
std::optional<navmesh::BaseNavRouteResult> PlanNavmeshRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    std::optional<double> goal_deck_y = std::nullopt,
    std::optional<double> start_floor_y = std::nullopt,
    NavmeshRouteDiagnostic* out_diagnostic = nullptr,
    const std::vector<VirtualNoGoDisc>* no_go = nullptr,
    std::optional<double> start_deck_y = std::nullopt,
    std::optional<navmesh::WorldPoint>* out_start_recovery_entry = nullptr);

// distance 是平面距离, height 与 BaseNavRouteRequest::floor_y 同一量纲
struct NavmeshSnap
{
    double distance = 0.0;
    double height = 0.0;
};

// zone 解析不了时为空, 命中点可能远在 radius 之外, 要看返回的 distance
std::optional<NavmeshSnap> NavmeshSnapAt(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& point,
    double radius,
    std::optional<double> floor_y = std::nullopt);

// 单位米, 与遮挡包同一坐标系
struct NavmeshAirLine
{
    navmesh::OccluderPoint a;
    navmesh::OccluderPoint b;
};

// 空结果表示有线通畅或无从判断, 调用方一律当通过, 被挡的组每条线给一个离起点最近的命中
std::vector<std::vector<navmesh::OccluderHit>>
    NavmeshLineGroupBlocks(const NaviParam& param, const std::string& locator_zone, const std::vector<std::vector<NavmeshAirLine>>& groups);

// 结果与 bases 同形, 无从判断时保留原 y
std::vector<std::vector<double>> NavmeshGroundHeights(
    const NaviParam& param,
    const std::string& locator_zone,
    const std::vector<std::vector<navmesh::OccluderPoint>>& bases);

// 两点的连通类集合不相交就必然规划不通, 空集表示无从判断, 应当允许尝试
std::vector<std::vector<uint32_t>>
    NavmeshRegionsNear(const NaviParam& param, const std::string& locator_zone, const std::vector<navmesh::WorldPoint>& points);
float NavmeshFloorYForZone(const NaviParam& param, const std::string& locator_zone);
bool NavmeshZonesShareGeometry(const NaviParam& param, const std::string& zone_a, const std::string& zone_b);

template <typename Fn>
void ForEachResampledPoint(const std::vector<navmesh::WorldPoint>& poly, double step, Fn&& fn)
{
    if (poly.empty()) {
        return;
    }
    const double safe_step = std::max(step, 0.1);
    fn(poly.front());
    for (size_t i = 0; i + 1 < poly.size(); ++i) {
        const navmesh::WorldPoint& a = poly[i];
        const navmesh::WorldPoint& b = poly[i + 1];
        const double seg = std::hypot(b.x - a.x, b.y - a.y);
        const int steps = static_cast<int>(std::ceil(seg / safe_step));
        for (int k = 1; k <= steps; ++k) {
            const double t = static_cast<double>(k) / static_cast<double>(steps);
            fn(navmesh::WorldPoint { .x = a.x + (b.x - a.x) * t, .y = a.y + (b.y - a.y) * t });
        }
    }
}

// 不在 navmesh 上的采样比例, zone 解析不了或线为空时返回 0
double NavmeshOffMeshFraction(
    const NaviParam& param,
    const std::string& locator_zone,
    const std::vector<navmesh::WorldPoint>& polyline,
    double step);
std::optional<navmesh::WorldPoint> PlanUnstickTarget(
    const NaviParam& param,
    const NaviPosition& position,
    double stuck_heading,
    int attempt_index,
    double* out_distance = nullptr);
bool AppendGeneratedNavmeshWaypoints(
    const navmesh::WorldPath& world_path,
    std::vector<Waypoint>& out_path,
    bool include_goal,
    bool emit_interior_corners = false,
    const navmesh::BaseNavPlanner* drivability_planner = nullptr,
    uint16_t drivable_zone_id = 0,
    bool strict_segment_breaks = true);
bool AppendGeneratedNavmeshWaypoints(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::BaseNavRouteResult& route,
    std::vector<Waypoint>& out_path,
    bool include_goal,
    bool emit_interior_corners,
    bool strict_segment_breaks);

} // namespace mapnavigator
