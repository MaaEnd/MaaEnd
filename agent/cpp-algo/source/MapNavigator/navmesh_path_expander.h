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

#include "../Navmesh/BaseNavPlanner.h"
#include "navi_domain_types.h"
#include "navmesh_diagnostics.h"

namespace mapnavigator
{

struct NaviParam;

// Final reason a complete authored-route expansion failed. The record is scoped to the calling
// thread and reset by ExpandNavmeshWaypoints, so the WebUI query can read it immediately after a
// false return without mixing concurrent navigation requests.
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

// How far the runtime will walk with no guidance. Start recovery covers the whole off-mesh band the
// blind walk out of the base drops us in; the goal extension exists because navmesh omits water, so a
// target a human can reach would otherwise be reported unreachable.
inline constexpr double kStartRecoveryMaxBlindWalk = 32.0;
inline constexpr double kBlindTargetMaxExtension = 30.0;

std::filesystem::path ResolveNavmeshFilePath(const std::string& configured_path = {});
std::string InitialExpectedZone(const NaviParam& param);
// Maps a live locator fix onto the navmesh base-pixel frame using the navmesh's OWN baked tier affine
// (the same is_tier / base = s*tier + t the python tool uses), in place. A geometry / base-matched /
// unknown zone projects to identity, so this is a no-op there — only a tier-template-pixel fix is
// rewritten.
void NormalizeLivePositionToBase(const NaviParam& param, NaviPosition& pos);
void PreloadNavmeshWaypoints(const NaviParam& param);
// `should_stop` is polled between waypoints and between fallback probes: expansion runs before the
// state machine exists, so it is the only place a stop request can be honored during planning.
bool ExpandNavmeshWaypoints(
    const NaviParam& param,
    const NaviPosition& initial_pos,
    const std::function<bool()>& should_stop,
    std::vector<Waypoint>& out_path,
    std::vector<NavmeshRouteDiagnostic>* out_diagnostics = nullptr);
NavmeshExpansionFailure CurrentNavmeshExpansionFailure();
// The goal deck pins which overlapping walkable surface the route must stop on; unset keeps the full span
// set. `start_floor_y` overrides which floor the start snaps onto, for the rare caller that actually knows
// the height it is standing at (a zipline dismount); unset keeps the zone's dominant floor, unchanged.
std::optional<navmesh::BaseNavRouteResult> PlanNavmeshRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    std::optional<double> goal_deck_y = std::nullopt,
    std::optional<double> start_floor_y = std::nullopt,
    NavmeshRouteDiagnostic* out_diagnostic = nullptr);

// Which walkable surface `point` lands on: the planar distance to it, and its height on the same scale
// as BaseNavRouteRequest::floor_y. Height matters as much as distance — a point directly above or below
// a floor is a plane away from it and no distance test will say so.
struct NavmeshSnap
{
    double distance = 0.0;
    double height = 0.0;
};

// Nullopt when the zone can't resolve. The returned distance is the answer, not whether it returned:
// snap falls back to a wider search on a small radius, so a hit can still be far outside `radius`, and
// `floor_y` only ranks the candidates. Resolves the zone like PlanNavmeshRoute.
std::optional<NavmeshSnap> NavmeshSnapAt(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& point,
    double radius,
    std::optional<double> floor_y = std::nullopt);
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
