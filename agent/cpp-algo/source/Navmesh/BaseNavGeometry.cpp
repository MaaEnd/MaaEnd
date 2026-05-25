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

double TriangleHeuristic(const BaseNavTriangle& lhs, const BaseNavTriangle& rhs)
{
    return std::hypot(static_cast<double>(lhs.center_u - rhs.center_u), static_cast<double>(lhs.center_v - rhs.center_v));
}

WorldPoint TriangleCenter(const BaseNavTriangle& triangle)
{
    return WorldPoint { .x = triangle.center_u, .y = triangle.center_v };
}

std::optional<std::array<WorldPoint, 2>>
    OverlappingSegmentPortal(const WorldPoint& a, const WorldPoint& b, const WorldPoint& c, const WorldPoint& d)
{
    constexpr double kEpsilon = 1e-3;
    const double ab_x = b.x - a.x;
    const double ab_y = b.y - a.y;
    const double length_sq = ab_x * ab_x + ab_y * ab_y;
    if (length_sq <= kEpsilon * kEpsilon) {
        return std::nullopt;
    }
    const double length = std::sqrt(length_sq);
    const auto line_distance = [&](const WorldPoint& point) {
        return std::abs(ab_x * (point.y - a.y) - ab_y * (point.x - a.x)) / length;
    };
    if (line_distance(c) > kEpsilon || line_distance(d) > kEpsilon) {
        return std::nullopt;
    }

    const auto segment_t = [&](const WorldPoint& point) {
        return ((point.x - a.x) * ab_x + (point.y - a.y) * ab_y) / length_sq;
    };
    const double c_t = segment_t(c);
    const double d_t = segment_t(d);
    const double overlap_left = std::max(0.0, std::min(c_t, d_t));
    const double overlap_right = std::min(1.0, std::max(c_t, d_t));
    if (overlap_right - overlap_left <= kEpsilon) {
        return std::nullopt;
    }
    return std::array {
        WorldPoint { .x = a.x + ab_x * overlap_left, .y = a.y + ab_y * overlap_left },
        WorldPoint { .x = a.x + ab_x * overlap_right, .y = a.y + ab_y * overlap_right },
    };
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
