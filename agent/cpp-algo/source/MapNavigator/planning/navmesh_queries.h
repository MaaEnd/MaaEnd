#pragma once

#include <algorithm>
#include <cmath>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <string>
#include <vector>

#include "../../Navmesh/NavmeshTypes.h"
#include "../../Navmesh/OccluderPack.h"
#include "../navi_domain_types.h"

namespace mapnavigator
{

struct NaviParam;

// 原地把 tier 模板像素坐标换算到 navmesh 底图像素系, 其他 zone 原样不动
void NormalizeLivePositionToBase(const NaviParam& param, NaviPosition& pos);

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

} // namespace mapnavigator
