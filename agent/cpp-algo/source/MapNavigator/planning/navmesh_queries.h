#pragma once

#include <cstddef>
#include <cstdint>
#include <optional>
#include <string>
#include <vector>

#include "../../Navmesh/NavmeshTypes.h"
#include "../../Navmesh/OccluderPack.h"
#include "../navi_domain_types.h"

namespace navmesh
{
struct BaseNavPack;
}

namespace mapnavigator
{

struct NaviParam;

// 同一地图的层都归到坐标与底图一致的那个区名, 包里查不到的区原样返回
std::string BaseZoneName(const navmesh::BaseNavPack& pack, const std::string& zone_id);

// 原地把层上的位置换成所在地图底图的坐标和区名, 层的主楼面高记进 zone_floor_y, 其他 zone 原样不动
void NormalizeLivePositionToBase(NaviPosition* pos, const NaviParam& param);

// 与 zone_id 同在一张地图上的全部区名, 包里查不到时为空
std::vector<std::string> ZonesOnSameMap(const NaviParam& param, const std::string& zone_id);

// 原地把路点的区名换成所在地图底图的区名, 原层的主楼面高记进 zone_floor_y
void FlattenWaypointZones(std::vector<Waypoint>* path, const NaviParam& param);

// zone_id 那张图上的点换成所在地图底图的坐标, 包里查不到时为空
std::optional<navmesh::WorldPoint> ProjectToMapBase(const NaviParam& param, const std::string& zone_id, double x, double y);

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
