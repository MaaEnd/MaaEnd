#pragma once

#include <array>
#include <tuple>

#include "BaseNavPack.h"
#include "NavmeshTypes.h"

namespace navmesh::detail
{

double Distance(const WorldPoint& lhs, const WorldPoint& rhs);
bool PointInTriangle(const WorldPoint& point, const std::array<WorldPoint, 3>& triangle);
WorldPoint ClosestPointOnSegment(const WorldPoint& point, const WorldPoint& a, const WorldPoint& b);
WorldPoint ClosestPointOnTriangle(const WorldPoint& point, const std::array<WorldPoint, 3>& triangle);
std::tuple<double, WorldPoint, WorldPoint>
    ClosestSegmentPoints(const WorldPoint& a, const WorldPoint& b, const WorldPoint& c, const WorldPoint& d);

}
