#include "navmesh_queries.h"

#include <filesystem>
#include <memory>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_param.h"
#include "navmesh_cache.h"

namespace mapnavigator
{

namespace
{

navmesh::WorldPoint offset_point(const NaviPosition& position, double heading, double distance)
{
    const double radians = NaviMath::normalizeHeading(heading) * kPi / 180.0;
    return { .x = position.x + std::sin(radians) * distance, .y = position.y - std::cos(radians) * distance };
}

} // namespace

void NormalizeLivePositionToBase(const NaviParam& param, NaviPosition& pos)
{
    if (pos.zone_id.empty()) {
        return;
    }
    const ZoneNavmesh zone = LoadZoneNavmesh(param, pos.zone_id);
    if (!zone.mesh) {
        return;
    }
    const auto projection = zone.mesh->pack.projectToBase(pos.zone_id, pos.x, pos.y);
    if (projection && projection->was_tier) {
        // zone_id 保持定位器命名, 区域校验要拿它比对
        pos.x = projection->x;
        pos.y = projection->y;
    }
}

float NavmeshFloorYForZone(const NaviParam& param, const std::string& locator_zone)
{
    if (locator_zone.empty()) {
        return navmesh::kBaseNavFloorYNone;
    }
    const ZoneNavmesh zone = LoadZoneNavmesh(param, locator_zone);
    if (!zone.mesh) {
        return navmesh::kBaseNavFloorYNone;
    }
    return zone.mesh->pack.floorYForZoneName(locator_zone);
}

bool NavmeshZonesShareGeometry(const NaviParam& param, const std::string& zone_a, const std::string& zone_b)
{
    if (zone_a.empty() || zone_b.empty()) {
        return false;
    }
    if (zone_a == zone_b) {
        return true;
    }
    const ZoneNavmesh zone = LoadZoneNavmesh(param, zone_a);
    if (!zone.mesh) {
        return false;
    }
    const navmesh::BaseNavPack& pack = zone.mesh->pack;
    const auto geometry_id = [&pack](const std::string& name) -> int {
        const navmesh::BaseNavZone* found = pack.findZoneByName(name);
        return found == nullptr ? -1 : static_cast<int>(pack.geometryZoneId(found->zone_id));
    };
    const int geom_a = geometry_id(zone_a);
    return geom_a >= 0 && geom_a == geometry_id(zone_b);
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
    const auto proj = zone.mesh->pack.projectToBase(zone.navmesh_zone, point.x, point.y);
    if (!proj || proj->geometry_zone == nullptr) {
        return std::nullopt;
    }
    const auto entry = zone.mesh->planner.snap(
        proj->geometry_zone->zone_id,
        { .x = proj->x, .y = proj->y },
        radius,
        floor_y ? static_cast<float>(*floor_y) : navmesh::kBaseNavFloorYNone);
    if (!entry) {
        return std::nullopt;
    }
    return NavmeshSnap { .distance = entry->distance, .height = zone.mesh->planner.triangleHeight(entry->triangle) };
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
            const std::vector<navmesh::OccluderHit> line_hits = scene->lineHits(line.a, line.b);
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
            heights[index][k] = scene->groundHeight(bases[index][k]);
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
    return zone.mesh->engine.regionsNear(zone.navmesh_zone, points);
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
    const auto sample = [&](const navmesh::WorldPoint& p) {
        const auto proj = zone.mesh->pack.projectToBase(zone.navmesh_zone, p.x, p.y);
        if (!proj || proj->geometry_zone == nullptr) {
            return;
        }
        ++total;
        if (!zone.mesh->planner.pointOnMesh(proj->geometry_zone->zone_id, { .x = proj->x, .y = proj->y })) {
            ++off;
        }
    };

    ForEachResampledPoint(polyline, step, sample);
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
    const auto on_mesh = [&](const navmesh::WorldPoint& p) {
        const auto proj = zone.mesh->pack.projectToBase(zone.navmesh_zone, p.x, p.y);
        if (!proj || proj->geometry_zone == nullptr) {
            return true;
        }
        return zone.mesh->planner.pointOnMesh(proj->geometry_zone->zone_id, { .x = proj->x, .y = proj->y });
    };

    static constexpr double kFan[] = { 180.0, -135.0, 135.0, -90.0, 90.0 };
    constexpr int kFanCount = static_cast<int>(sizeof(kFan) / sizeof(kFan[0]));
    const int rot = ((attempt_index % kFanCount) + kFanCount) % kFanCount;

    for (int i = 0; i < kFanCount; ++i) {
        const double bearing = NaviMath::normalizeHeading(stuck_heading + kFan[(i + rot) % kFanCount]);
        double run = 0.0;
        double longest = 0.0;
        double solid_start = -1.0; // 末尾连续在网格上那段的起始距离, -1 表示没有
        for (double d = kUnstickSampleStepM; d <= kUnstickMaxDistanceM + 1e-9; d += kUnstickSampleStepM) {
            if (on_mesh(offset_point(position, bearing, d))) {
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
