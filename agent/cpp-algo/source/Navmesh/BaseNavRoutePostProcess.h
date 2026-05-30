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

// When `is_segment_walkable` is set, simplification re-inserts any corner whose shortcut would
// cross unwalkable mesh. An empty validator keeps the previous purely-geometric behaviour.
RoutePointsWithBreaks PostProcessRoutePoints(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const SegmentWalkableFn& is_segment_walkable = {});

}
