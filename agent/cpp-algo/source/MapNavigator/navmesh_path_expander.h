#pragma once

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
std::optional<navmesh::BaseNavRouteResult> PlanNavmeshDetourRoute(
    const NaviParam& param,
    const NaviPosition& position,
    const Waypoint& anchor,
    double route_heading,
    navmesh::WorldPoint* out_detour_vertex = nullptr);
bool AppendGeneratedNavmeshWaypoints(const navmesh::WorldPath& world_path, std::vector<Waypoint>& out_path, bool include_goal);

} // namespace mapnavigator
