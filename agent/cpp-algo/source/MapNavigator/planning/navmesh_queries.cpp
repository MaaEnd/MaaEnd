#include "navmesh_queries.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <filesystem>
#include <memory>
#include <optional>
#include <string>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_param.h"
#include "navmesh_cache.h"

namespace mapnavigator
{

namespace
{

constexpr std::array<float, 4> kIdentityTierTransform { 1.0F, 0.0F, 1.0F, 0.0F };

// 同一地图的层都归到坐标与底图一致的那个区名, 包里查不到的区原样返回
std::string base_zone_name(const navmesh::BaseNavPack& pack, const std::string& zone_id)
{
    const navmesh::BaseNavZone* zone = navmesh::FindBaseNavZoneByName(pack, zone_id);
    if (zone == nullptr) {
        return zone_id;
    }
    const uint16_t geometry = navmesh::GeometryZoneId(pack, zone->zone_id);
    for (const navmesh::BaseNavZone& candidate : pack.zones) {
        if (navmesh::IsTierZone(candidate) && navmesh::GeometryZoneId(pack, candidate.zone_id) == geometry
            && candidate.transform == kIdentityTierTransform) {
            return candidate.name;
        }
    }
    return zone_id;
}

std::optional<double> zone_main_floor(const navmesh::BaseNavPack& pack, const std::string& zone_id)
{
    const float floor_y = navmesh::FloorYForZoneName(pack, zone_id);
    if (floor_y <= navmesh::kBaseNavFloorYValidMin) {
        return std::nullopt;
    }
    return floor_y;
}

navmesh::WorldPoint offset_point(const NaviPosition& position, double heading, double distance)
{
    const double radians = NormalizeHeading(heading) * kPi / 180.0;
    return { .x = position.x + std::sin(radians) * distance, .y = position.y - std::cos(radians) * distance };
}

std::vector<navmesh::WorldPoint> resample_polyline(const std::vector<navmesh::WorldPoint>& poly, double step)
{
    std::vector<navmesh::WorldPoint> points;
    if (poly.empty()) {
        return points;
    }
    const double safe_step = std::max(step, 0.1);
    points.push_back(poly.front());
    for (size_t i = 0; i + 1 < poly.size(); ++i) {
        const navmesh::WorldPoint& a = poly[i];
        const navmesh::WorldPoint& b = poly[i + 1];
        const double seg = std::hypot(b.x - a.x, b.y - a.y);
        const int steps = static_cast<int>(std::ceil(seg / safe_step));
        for (int k = 1; k <= steps; ++k) {
            const double t = static_cast<double>(k) / static_cast<double>(steps);
            points.push_back(navmesh::WorldPoint { .x = a.x + (b.x - a.x) * t, .y = a.y + (b.y - a.y) * t });
        }
    }
    return points;
}

// 投影不到底图时为空
std::optional<bool> point_on_mesh(const ZoneNavmesh& zone, const navmesh::WorldPoint& p)
{
    const auto proj = navmesh::ProjectToBase(zone.mesh->pack, zone.navmesh_zone, p.x, p.y);
    if (!proj || proj->geometry_zone == nullptr) {
        return std::nullopt;
    }
    return navmesh::PointOnMesh(zone.mesh->planner, zone.mesh->pack, proj->geometry_zone->zone_id, { .x = proj->x, .y = proj->y });
}

} // namespace

void NormalizeLivePositionToBase(NaviPosition* pos, const NaviParam& param)
{
    pos->zone_floor_y.reset();
    if (pos->zone_id.empty()) {
        return;
    }
    const ZoneNavmesh zone = LoadZoneNavmesh(param, pos->zone_id);
    if (!zone.mesh) {
        return;
    }
    const navmesh::BaseNavPack& pack = zone.mesh->pack;
    const auto projection = navmesh::ProjectToBase(pack, pos->zone_id, pos->x, pos->y);
    if (!projection || !projection->was_tier) {
        return;
    }
    pos->x = projection->x;
    pos->y = projection->y;
    pos->zone_floor_y = zone_main_floor(pack, pos->zone_id);
    pos->zone_id = base_zone_name(pack, pos->zone_id);
}

std::vector<std::string> ZonesOnSameMap(const NaviParam& param, const std::string& zone_id)
{
    std::vector<std::string> zones;
    if (zone_id.empty()) {
        return zones;
    }
    const ZoneNavmesh zone = LoadZoneNavmesh(param, zone_id);
    if (!zone.mesh) {
        return zones;
    }
    const navmesh::BaseNavPack& pack = zone.mesh->pack;
    const navmesh::BaseNavZone* found = navmesh::FindBaseNavZoneByName(pack, zone_id);
    if (found == nullptr) {
        return zones;
    }
    const uint16_t geometry = navmesh::GeometryZoneId(pack, found->zone_id);
    for (const navmesh::BaseNavZone& candidate : pack.zones) {
        if (navmesh::GeometryZoneId(pack, candidate.zone_id) == geometry) {
            zones.push_back(candidate.name);
        }
    }
    return zones;
}

void FlattenWaypointZones(std::vector<Waypoint>* path, const NaviParam& param)
{
    for (Waypoint& waypoint : *path) {
        if (waypoint.zone_id.empty()) {
            continue;
        }
        const ZoneNavmesh zone = LoadZoneNavmesh(param, waypoint.zone_id);
        if (zone.mesh) {
            waypoint.zone_floor_y = zone_main_floor(zone.mesh->pack, waypoint.zone_id);
            waypoint.zone_id = base_zone_name(zone.mesh->pack, waypoint.zone_id);
        }
    }
}

std::optional<NavmeshSnap> NavmeshSnapAt(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& point,
    double radius,
    std::optional<double> floor_y)
{
    const ZoneNavmesh zone = LoadZoneNavmesh(param, locator_zone);
    if (!zone.mesh) {
        return std::nullopt;
    }
    const auto proj = navmesh::ProjectToBase(zone.mesh->pack, zone.navmesh_zone, point.x, point.y);
    if (!proj || proj->geometry_zone == nullptr) {
        return std::nullopt;
    }
    const auto entry = navmesh::SnapToMesh(
        zone.mesh->planner,
        zone.mesh->pack,
        proj->geometry_zone->zone_id,
        { .x = proj->x, .y = proj->y },
        radius,
        floor_y ? static_cast<float>(*floor_y) : navmesh::kBaseNavFloorYNone);
    if (!entry) {
        return std::nullopt;
    }
    return NavmeshSnap { .distance = entry->distance, .height = navmesh::TriangleHeight(zone.mesh->pack, entry->triangle) };
}

std::vector<std::vector<navmesh::OccluderHit>>
    NavmeshLineGroupBlocks(const NaviParam& param, const std::string& locator_zone, const std::vector<std::vector<NavmeshAirLine>>& groups)
{
    std::vector<std::vector<navmesh::OccluderHit>> blocks(groups.size());
    if (groups.empty()) {
        return blocks;
    }
    const std::string navmesh_zone = InferBaseNavZone(locator_zone, param.map_name);
    if (navmesh_zone.empty()) {
        return blocks;
    }
    const std::filesystem::path occluder_path = navmesh::OccluderSidecarPath(ResolveNavmeshFilePath(param.navmesh_file));
    const auto scene = navmesh::LoadOccluderScene(occluder_path, navmesh_zone);
    if (!scene) {
        LogWarn << "No occluder scene, every air line passes." << VAR(occluder_path) << VAR(navmesh_zone) << VAR(groups.size());
        return blocks;
    }
    for (size_t index = 0; index < groups.size(); ++index) {
        std::vector<navmesh::OccluderHit> hits;
        for (const NavmeshAirLine& line : groups[index]) {
            const std::vector<navmesh::OccluderHit> line_hits = navmesh::OccluderLineHits(*scene, line.a, line.b);
            if (line_hits.empty()) {
                hits.clear();
                break;
            }
            hits.push_back(line_hits.front());
        }
        blocks[index] = std::move(hits);
    }
    return blocks;
}

std::vector<std::vector<double>> NavmeshGroundHeights(
    const NaviParam& param,
    const std::string& locator_zone,
    const std::vector<std::vector<navmesh::OccluderPoint>>& bases)
{
    std::vector<std::vector<double>> heights(bases.size());
    for (size_t index = 0; index < bases.size(); ++index) {
        for (const navmesh::OccluderPoint& base : bases[index]) {
            heights[index].push_back(base.y);
        }
    }
    const std::string navmesh_zone = InferBaseNavZone(locator_zone, param.map_name);
    if (navmesh_zone.empty()) {
        return heights;
    }
    const auto scene = navmesh::LoadOccluderScene(navmesh::OccluderSidecarPath(ResolveNavmeshFilePath(param.navmesh_file)), navmesh_zone);
    if (!scene) {
        return heights;
    }
    for (size_t index = 0; index < bases.size(); ++index) {
        for (size_t k = 0; k < bases[index].size(); ++k) {
            heights[index][k] = navmesh::OccluderGroundHeight(*scene, bases[index][k]);
        }
    }
    return heights;
}

std::vector<std::vector<uint32_t>>
    NavmeshRegionsNear(const NaviParam& param, const std::string& locator_zone, const std::vector<navmesh::WorldPoint>& points)
{
    const ZoneNavmesh zone = LoadZoneNavmesh(param, locator_zone);
    if (!zone.mesh) {
        return std::vector<std::vector<uint32_t>>(points.size());
    }
    return navmesh::recast::RecastRegionsNear(&zone.mesh->engine, zone.navmesh_zone, points);
}

double NavmeshOffMeshFraction(
    const NaviParam& param,
    const std::string& locator_zone,
    const std::vector<navmesh::WorldPoint>& polyline,
    double step)
{
    if (polyline.size() < 2) {
        return 0.0;
    }
    const ZoneNavmesh zone = LoadZoneNavmesh(param, locator_zone);
    if (!zone.mesh) {
        return 0.0;
    }

    size_t total = 0;
    size_t off = 0;
    for (const navmesh::WorldPoint& p : resample_polyline(polyline, step)) {
        const std::optional<bool> on_mesh = point_on_mesh(zone, p);
        if (!on_mesh) {
            continue;
        }
        ++total;
        if (!*on_mesh) {
            ++off;
        }
    }
    if (total == 0) {
        return 0.0;
    }
    return static_cast<double>(off) / static_cast<double>(total);
}

std::optional<navmesh::WorldPoint>
    PlanUnstickTarget(const NaviParam& param, const NaviPosition& position, double stuck_heading, int attempt_index, double* out_distance)
{
    const ZoneNavmesh zone = LoadZoneNavmesh(param, position.zone_id);
    if (!zone.mesh) {
        return std::nullopt;
    }
    static constexpr double kFan[] = { 180.0, -135.0, 135.0, -90.0, 90.0 };
    constexpr int kFanCount = static_cast<int>(sizeof(kFan) / sizeof(kFan[0]));
    const int rot = ((attempt_index % kFanCount) + kFanCount) % kFanCount;

    for (int i = 0; i < kFanCount; ++i) {
        const double bearing = NormalizeHeading(stuck_heading + kFan[(i + rot) % kFanCount]);
        double run = 0.0;
        double longest = 0.0;
        double solid_start = -1.0; // 末尾连续在网格上那段的起始距离, -1 表示没有
        for (double d = kUnstickSampleStepM; d <= kUnstickMaxDistanceM + 1e-9; d += kUnstickSampleStepM) {
            if (point_on_mesh(zone, offset_point(position, bearing, d)).value_or(true)) {
                if (solid_start < 0.0) {
                    solid_start = d;
                }
                run = 0.0;
            }
            else {
                solid_start = -1.0;
                run += kUnstickSampleStepM;
                longest = std::max(longest, run);
            }
        }
        if (solid_start >= 0.0 && longest <= kUnstickMaxRockCrossingM) {
            const double dist = std::clamp(solid_start + kUnstickMeshMarginM, kUnstickMinDistanceM, kUnstickMaxDistanceM);
            if (out_distance != nullptr) {
                *out_distance = dist;
            }
            return offset_point(position, bearing, dist);
        }
    }
    return std::nullopt;
}

} // namespace mapnavigator
