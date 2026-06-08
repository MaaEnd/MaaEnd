#pragma once

#include <cstddef>
#include <functional>
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

// Thinning keeps a point only at structural corners; centering then shifts straight runs onto the
// corridor centreline. Keep this mirrored with tools/MapNavigator/basenav_preview.py. Either
// callback may be empty to skip the corresponding pass.
RoutePointsWithBreaks PostProcessRoutePoints(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const SegmentWalkableFn& is_segment_walkable = {},
    const PointOnMeshFn& point_on_mesh = {});

}
