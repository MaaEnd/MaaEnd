#include <algorithm>
#include <cmath>

#include "BaseNavGeometry.h"

namespace navmesh::detail
{

double Distance(const WorldPoint& lhs, const WorldPoint& rhs)
{
    return std::hypot(lhs.x - rhs.x, lhs.y - rhs.y);
}

bool PointInTriangle(const WorldPoint& point, const std::array<WorldPoint, 3>& triangle)
{
    constexpr double kEpsilon = 1e-5;
    const auto cross = [](const WorldPoint& p, const WorldPoint& a, const WorldPoint& b) {
        return (p.x - b.x) * (a.y - b.y) - (a.x - b.x) * (p.y - b.y);
    };
    const double d1 = cross(point, triangle[0], triangle[1]);
    const double d2 = cross(point, triangle[1], triangle[2]);
    const double d3 = cross(point, triangle[2], triangle[0]);
    const bool has_neg = d1 < -kEpsilon || d2 < -kEpsilon || d3 < -kEpsilon;
    const bool has_pos = d1 > kEpsilon || d2 > kEpsilon || d3 > kEpsilon;
    return !(has_neg && has_pos);
}

WorldPoint ClosestPointOnSegment(const WorldPoint& point, const WorldPoint& a, const WorldPoint& b)
{
    const double ab_x = b.x - a.x;
    const double ab_y = b.y - a.y;
    const double denom = ab_x * ab_x + ab_y * ab_y;
    if (denom <= 1e-12) {
        return a;
    }
    const double t = std::clamp(((point.x - a.x) * ab_x + (point.y - a.y) * ab_y) / denom, 0.0, 1.0);
    return { .x = a.x + ab_x * t, .y = a.y + ab_y * t };
}

WorldPoint ClosestPointOnTriangle(const WorldPoint& point, const std::array<WorldPoint, 3>& triangle)
{
    if (PointInTriangle(point, triangle)) {
        return point;
    }
    std::array candidates {
        ClosestPointOnSegment(point, triangle[0], triangle[1]),
        ClosestPointOnSegment(point, triangle[1], triangle[2]),
        ClosestPointOnSegment(point, triangle[2], triangle[0]),
    };
    return *std::min_element(candidates.begin(), candidates.end(), [&](const WorldPoint& lhs, const WorldPoint& rhs) {
        return Distance(lhs, point) < Distance(rhs, point);
    });
}

std::tuple<double, WorldPoint, WorldPoint>
    ClosestSegmentPoints(const WorldPoint& a, const WorldPoint& b, const WorldPoint& c, const WorldPoint& d)
{
    std::array<std::tuple<double, WorldPoint, WorldPoint>, 4> candidates {
        std::tuple { Distance(a, ClosestPointOnSegment(a, c, d)), a, ClosestPointOnSegment(a, c, d) },
        std::tuple { Distance(b, ClosestPointOnSegment(b, c, d)), b, ClosestPointOnSegment(b, c, d) },
        std::tuple { Distance(c, ClosestPointOnSegment(c, a, b)), ClosestPointOnSegment(c, a, b), c },
        std::tuple { Distance(d, ClosestPointOnSegment(d, a, b)), ClosestPointOnSegment(d, a, b), d },
    };
    return *std::min_element(candidates.begin(), candidates.end(), [](const auto& lhs, const auto& rhs) {
        return std::get<0>(lhs) < std::get<0>(rhs);
    });
}

}
