#pragma once

#include <algorithm>
#include <cmath>
#include <cstddef>
#include <cstdint>
#include <filesystem>
#include <optional>
#include <string>
#include <vector>

#include "../Navmesh/BaseNavPlanner.h"
#include "navi_domain_types.h"

namespace mapnavigator
{

struct NaviParam;

std::filesystem::path ResolveNavmeshFilePath(const std::string& configured_path = {});
std::string InitialExpectedZone(const NaviParam& param);
// Maps a live locator fix onto the navmesh base-pixel frame using the navmesh's OWN baked tier affine
// (the same is_tier / base = s*tier + t the python tool uses), in place. A geometry / base-matched /
// unknown zone projects to identity, so this is a no-op there — only a tier-template-pixel fix is
// rewritten. Never consults the external MapTracker transforms.
void NormalizeLivePositionToBase(const NaviParam& param, NaviPosition& pos);
void PreloadNavmeshWaypoints(const NaviParam& param);
bool ExpandNavmeshWaypoints(const NaviParam& param, const NaviPosition& initial_pos, std::vector<Waypoint>& out_path);
std::optional<navmesh::BaseNavRouteResult> PlanNavmeshRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    const std::vector<uint32_t>& blocked_triangles = {});
float NavmeshFloorYForZone(const NaviParam& param, const std::string& locator_zone);
bool NavmeshZonesShareGeometry(const NaviParam& param, const std::string& zone_a, const std::string& zone_b);
// Resample `poly` at ~`step` world units (clamped to >=0.1) and invoke `fn` on the leading vertex and
// every resampled point. Used by NavmeshOffMeshFraction. Caller guards poly.size() >= 2 for a
// meaningful result.
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

// Fraction of `polyline` (resampled every ~`step` world units, vertices included) lying OFF the
// navmesh — the signature of water the game omits. Resolves the zone like PlanNavmeshRoute. Returns
// 0.0 when the zone can't resolve or the line is empty (fail-safe on-mesh).
double NavmeshOffMeshFraction(
    const NaviParam& param,
    const std::string& locator_zone,
    const std::vector<navmesh::WorldPoint>& polyline,
    double step);
std::optional<navmesh::BaseNavRouteResult> PlanNavmeshDetourRoute(
    const NaviParam& param,
    const NaviPosition& position,
    const Waypoint& anchor,
    double route_heading,
    navmesh::WorldPoint* out_detour_vertex = nullptr);
std::optional<navmesh::WorldPoint> PlanUnstickTarget(
    const NaviParam& param,
    const NaviPosition& position,
    double stuck_heading,
    int attempt_index,
    double* out_distance = nullptr);
bool AppendGeneratedNavmeshWaypoints(
    const navmesh::WorldPath& world_path, std::vector<Waypoint>& out_path, bool include_goal,
    bool emit_interior_corners = false, const navmesh::BaseNavPlanner* drivability_planner = nullptr,
    uint16_t drivable_zone_id = 0);

} // namespace mapnavigator
