#pragma once

#include <cstddef>
#include <functional>
#include <optional>
#include <vector>

#include "NavmeshTypes.h"

namespace navmesh::detail
{

struct RoutePointsWithBreaks
{
    std::vector<WorldPoint> points;
    std::vector<size_t> segment_breaks;
};

// True when the straight segment a->b stays on walkable mesh; supplied by the planner.
using SegmentWalkableFn = std::function<bool(const WorldPoint& a, const WorldPoint& b)>;

// True when a point lies on walkable mesh (point-in-any-triangle). Centering uses this rather than
// the marching SegmentWalkableFn, which underestimates clearance on overlapping/fragmented meshes.
using PointOnMeshFn = std::function<bool(const WorldPoint& point)>;

// Ground height at a point (nullopt = off mesh). The water-edge decentering pass uses it to tell a
// dangerous water/cliff edge (ground drops away just past the tight side) from a harmless wall edge.
using GroundHeightFn = std::function<std::optional<double>(const WorldPoint& point)>;

// Thinning keeps a point only at structural corners; centering then shifts straight runs onto the
// corridor centreline; two decentering passes (clearance relax + water-edge block shift) then peel
// the route off water/cliff edges. Keep this mirrored with tools/MapNavigator/basenav_preview.py.
// Any callback may be empty to skip the corresponding pass.
RoutePointsWithBreaks PostProcessRoutePoints(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const SegmentWalkableFn& is_segment_walkable = {},
    const PointOnMeshFn& point_on_mesh = {},
    const GroundHeightFn& ground_height = {});

}
