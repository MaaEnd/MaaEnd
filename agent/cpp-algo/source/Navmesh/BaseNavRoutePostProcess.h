#pragma once

#include <cstddef>
#include <vector>

#include "NavmeshTypes.h"

namespace navmesh::detail
{

struct RoutePointsWithBreaks
{
    std::vector<WorldPoint> points;
    std::vector<size_t> segment_breaks;
};

RoutePointsWithBreaks PostProcessRoutePoints(const std::vector<WorldPoint>& points, const std::vector<size_t>& segment_breaks);

}
