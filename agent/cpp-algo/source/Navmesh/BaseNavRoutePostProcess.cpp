#include <algorithm>
#include <cmath>
#include <unordered_set>
#include <utility>

#include "BaseNavGeometry.h"
#include "BaseNavRoutePostProcess.h"

namespace navmesh::detail
{

namespace
{

constexpr double kDedupePointEpsilon = 0.25;
constexpr double kCollinearEpsilon = 1e-3;
constexpr double kRouteMinPointDistance = 6.0;
constexpr double kRouteSimplifyEpsilon = 3.0;
constexpr double kRouteMaxPointDistance = 4.0;

std::vector<size_t> SortedUniqueBreaks(std::vector<size_t> breaks)
{
    std::sort(breaks.begin(), breaks.end());
    breaks.erase(std::unique(breaks.begin(), breaks.end()), breaks.end());
    return breaks;
}

RoutePointsWithBreaks DedupePointsWithBreaks(const std::vector<WorldPoint>& points, const std::vector<size_t>& segment_breaks)
{
    RoutePointsWithBreaks result;
    const std::unordered_set<size_t> break_set(segment_breaks.begin(), segment_breaks.end());
    for (size_t index = 0; index < points.size(); ++index) {
        if (!result.points.empty() && Distance(points[index], result.points.back()) <= kDedupePointEpsilon) {
            if (break_set.contains(index)) {
                result.segment_breaks.push_back(result.points.size());
            }
            continue;
        }
        if (break_set.contains(index)) {
            result.segment_breaks.push_back(result.points.size());
        }
        result.points.push_back(points[index]);
    }
    result.segment_breaks = SortedUniqueBreaks(std::move(result.segment_breaks));
    return result;
}

RoutePointsWithBreaks RemoveCollinearWithBreaks(const std::vector<WorldPoint>& points, const std::vector<size_t>& segment_breaks)
{
    if (points.size() <= 2) {
        return RoutePointsWithBreaks { .points = points, .segment_breaks = segment_breaks };
    }

    RoutePointsWithBreaks result;
    result.points.push_back(points.front());
    const std::unordered_set<size_t> break_set(segment_breaks.begin(), segment_breaks.end());
    for (size_t index = 1; index + 1 < points.size(); ++index) {
        if (break_set.contains(index)) {
            result.segment_breaks.push_back(result.points.size());
            result.points.push_back(points[index]);
            continue;
        }
        const WorldPoint& a = result.points.back();
        const WorldPoint& b = points[index];
        const WorldPoint& c = points[index + 1];
        const double area = std::abs((b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x));
        const double length = Distance(a, c);
        if (length > kCollinearEpsilon && area / length <= kCollinearEpsilon) {
            continue;
        }
        result.points.push_back(points[index]);
    }
    result.points.push_back(points.back());
    result.segment_breaks = SortedUniqueBreaks(std::move(result.segment_breaks));
    return result;
}

double PointLineDistance(const WorldPoint& point, const WorldPoint& start, const WorldPoint& end)
{
    const double dx = end.x - start.x;
    const double dy = end.y - start.y;
    const double length_sq = dx * dx + dy * dy;
    if (length_sq <= 1e-9) {
        return Distance(point, start);
    }
    const double t = std::clamp(((point.x - start.x) * dx + (point.y - start.y) * dy) / length_sq, 0.0, 1.0);
    const WorldPoint snapped { .x = start.x + dx * t, .y = start.y + dy * t };
    return Distance(point, snapped);
}

std::vector<size_t> RdpKeepIndices(const std::vector<WorldPoint>& points, size_t start, size_t end, double epsilon)
{
    std::unordered_set<size_t> keep { start, end };
    std::vector<std::pair<size_t, size_t>> stack { { start, end } };
    while (!stack.empty()) {
        const auto [left, right] = stack.back();
        stack.pop_back();
        double best_distance = -1.0;
        size_t best_index = 0;
        for (size_t index = left + 1; index < right; ++index) {
            const double distance = PointLineDistance(points[index], points[left], points[right]);
            if (distance > best_distance) {
                best_distance = distance;
                best_index = index;
            }
        }
        if (best_distance > epsilon && best_index > 0) {
            keep.insert(best_index);
            stack.emplace_back(left, best_index);
            stack.emplace_back(best_index, right);
        }
    }
    std::vector<size_t> result(keep.begin(), keep.end());
    std::sort(result.begin(), result.end());
    return result;
}

std::vector<size_t>
    ThinContinuousSegment(const std::vector<WorldPoint>& points, size_t start, size_t end, double min_distance, double simplify_epsilon)
{
    if (end - start <= 2) {
        std::vector<size_t> result;
        for (size_t index = start; index < end; ++index) {
            result.push_back(index);
        }
        return result;
    }

    const std::vector<size_t> critical_indices = RdpKeepIndices(points, start, end - 1, simplify_epsilon);
    const std::unordered_set<size_t> critical_set(critical_indices.begin(), critical_indices.end());
    std::vector<size_t> kept { start };
    double distance_since_kept = 0.0;
    for (size_t index = start + 1; index + 1 < end; ++index) {
        distance_since_kept += Distance(points[index - 1], points[index]);
        if (critical_set.contains(index) || distance_since_kept >= min_distance) {
            kept.push_back(index);
            distance_since_kept = 0.0;
        }
    }
    kept.push_back(end - 1);
    return kept;
}

RoutePointsWithBreaks ThinRoutePointsWithBreaks(const std::vector<WorldPoint>& points, const std::vector<size_t>& segment_breaks)
{
    if (points.size() <= 2) {
        return RoutePointsWithBreaks { .points = points, .segment_breaks = segment_breaks };
    }

    std::vector<size_t> valid_breaks;
    for (size_t break_index : segment_breaks) {
        if (break_index > 0 && break_index < points.size()) {
            valid_breaks.push_back(break_index);
        }
    }
    valid_breaks = SortedUniqueBreaks(std::move(valid_breaks));

    std::vector<size_t> segment_starts { 0 };
    segment_starts.insert(segment_starts.end(), valid_breaks.begin(), valid_breaks.end());
    std::vector<size_t> segment_ends(valid_breaks.begin(), valid_breaks.end());
    segment_ends.push_back(points.size());

    RoutePointsWithBreaks result;
    for (size_t segment_index = 0; segment_index < segment_starts.size(); ++segment_index) {
        if (segment_index > 0) {
            result.segment_breaks.push_back(result.points.size());
        }
        const std::vector<size_t> kept_indices = ThinContinuousSegment(
            points,
            segment_starts[segment_index],
            segment_ends[segment_index],
            kRouteMinPointDistance,
            kRouteSimplifyEpsilon);
        for (size_t index : kept_indices) {
            result.points.push_back(points[index]);
        }
    }
    result.segment_breaks = SortedUniqueBreaks(std::move(result.segment_breaks));
    return result;
}

std::vector<WorldPoint> DensifyContinuousSegment(const std::vector<WorldPoint>& points, size_t start, size_t end, double max_distance)
{
    if (start >= end) {
        return {};
    }
    const double safe_max_distance = std::max(max_distance, 0.25);
    std::vector<WorldPoint> result { points[start] };
    for (size_t index = start + 1; index < end; ++index) {
        const WorldPoint from_point = points[index - 1];
        const WorldPoint to_point = points[index];
        const double distance = Distance(from_point, to_point);
        if (distance <= 1e-6) {
            continue;
        }
        const int step_count = std::max(1, static_cast<int>(std::ceil(distance / safe_max_distance)));
        for (int step = 1; step < step_count; ++step) {
            const double t = static_cast<double>(step) / static_cast<double>(step_count);
            result.push_back(
                WorldPoint {
                    .x = from_point.x + (to_point.x - from_point.x) * t,
                    .y = from_point.y + (to_point.y - from_point.y) * t,
                });
        }
        result.push_back(to_point);
    }
    return result;
}

RoutePointsWithBreaks DensifyRoutePointsWithBreaks(const std::vector<WorldPoint>& points, const std::vector<size_t>& segment_breaks)
{
    if (points.size() <= 1) {
        return RoutePointsWithBreaks { .points = points, .segment_breaks = segment_breaks };
    }

    std::vector<size_t> valid_breaks;
    for (size_t break_index : segment_breaks) {
        if (break_index > 0 && break_index < points.size()) {
            valid_breaks.push_back(break_index);
        }
    }
    valid_breaks = SortedUniqueBreaks(std::move(valid_breaks));

    std::vector<size_t> segment_starts { 0 };
    segment_starts.insert(segment_starts.end(), valid_breaks.begin(), valid_breaks.end());
    std::vector<size_t> segment_ends(valid_breaks.begin(), valid_breaks.end());
    segment_ends.push_back(points.size());

    RoutePointsWithBreaks result;
    for (size_t segment_index = 0; segment_index < segment_starts.size(); ++segment_index) {
        if (segment_index > 0) {
            result.segment_breaks.push_back(result.points.size());
        }
        std::vector<WorldPoint> segment =
            DensifyContinuousSegment(points, segment_starts[segment_index], segment_ends[segment_index], kRouteMaxPointDistance);
        result.points.insert(result.points.end(), segment.begin(), segment.end());
    }
    result.segment_breaks = SortedUniqueBreaks(std::move(result.segment_breaks));
    return result;
}

}

RoutePointsWithBreaks PostProcessRoutePoints(const std::vector<WorldPoint>& points, const std::vector<size_t>& segment_breaks)
{
    const auto deduped = DedupePointsWithBreaks(points, segment_breaks);
    auto simplified = RemoveCollinearWithBreaks(deduped.points, deduped.segment_breaks);
    auto thinned = ThinRoutePointsWithBreaks(simplified.points, simplified.segment_breaks);
    return DensifyRoutePointsWithBreaks(thinned.points, thinned.segment_breaks);
}

}
