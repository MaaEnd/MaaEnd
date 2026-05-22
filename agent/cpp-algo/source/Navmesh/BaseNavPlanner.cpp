#include <algorithm>
#include <array>
#include <cmath>
#include <cstddef>
#include <cstdint>
#include <limits>
#include <queue>
#include <tuple>
#include <unordered_set>
#include <utility>

#include "BaseNavGeometry.h"
#include "BaseNavPlanner.h"
#include "BaseNavRoutePostProcess.h"

namespace navmesh
{

namespace
{

constexpr double kIndexBinSize = 96.0;
constexpr double kBridgeFixedCost = 12.0;
constexpr double kBridgeGapCostFactor = 3.0;
constexpr double kBridgeHeightCostFactor = 40.0;
constexpr double kBridgeMaxHeightDelta = 3.0;

struct QueueNode
{
    uint32_t triangle = 0;
    double priority = 0.0;

    bool operator<(const QueueNode& rhs) const { return priority > rhs.priority; }
};

}

BaseNavPlanner::BaseNavPlanner(const BaseNavPack& pack)
    : pack_(pack)
    , triangle_zones_(pack.triangles().size(), 0)
    , adjacency_(pack.triangles().size())
    , triangle_bounds_(pack.triangles().size())
    , triangle_heights_(pack.triangles().size(), 0.0)
{
    buildIndex();
}

size_t BaseNavPlanner::BinKeyHash::operator()(const BinKey& key) const
{
    uint64_t value = static_cast<uint64_t>(key.zone_id);
    value = value * 1'099'511'628'211ULL ^ static_cast<uint32_t>(key.x);
    value = value * 1'099'511'628'211ULL ^ static_cast<uint32_t>(key.y);
    return static_cast<size_t>(value);
}

void BaseNavPlanner::buildIndex()
{
    for (const auto& zone : pack_.zones()) {
        const uint32_t end = zone.first_triangle + zone.triangle_count;
        for (uint32_t index = zone.first_triangle; index < end && index < triangle_zones_.size(); ++index) {
            triangle_zones_[index] = zone.zone_id;
        }
    }
    for (const BaseNavLink& link : pack_.links()) {
        if (link.source < adjacency_.size() && link.target < adjacency_.size()) {
            adjacency_[link.source].push_back(link.target);
        }
    }

    for (uint32_t triangle_index = 0; triangle_index < pack_.triangles().size(); ++triangle_index) {
        triangle_heights_[triangle_index] = triangleAverageHeight(triangle_index);
        const uint16_t zone_id = triangle_zones_[triangle_index];
        if (zone_id == 0) {
            continue;
        }
        const auto points = trianglePoints(triangle_index);
        const double left = std::min({ points[0].x, points[1].x, points[2].x });
        const double right = std::max({ points[0].x, points[1].x, points[2].x });
        const double top = std::min({ points[0].y, points[1].y, points[2].y });
        const double bottom = std::max({ points[0].y, points[1].y, points[2].y });
        triangle_bounds_[triangle_index] = { left, top, right, bottom };
        const int32_t left_bin = static_cast<int32_t>(std::floor(left / kIndexBinSize));
        const int32_t right_bin = static_cast<int32_t>(std::floor(right / kIndexBinSize));
        const int32_t top_bin = static_cast<int32_t>(std::floor(top / kIndexBinSize));
        const int32_t bottom_bin = static_cast<int32_t>(std::floor(bottom / kIndexBinSize));
        for (int32_t bin_x = left_bin; bin_x <= right_bin; ++bin_x) {
            for (int32_t bin_y = top_bin; bin_y <= bottom_bin; ++bin_y) {
                bins_[BinKey { .zone_id = zone_id, .x = bin_x, .y = bin_y }].push_back(triangle_index);
            }
        }
    }
}

double BaseNavPlanner::triangleAverageHeight(uint32_t triangle_index) const
{
    const auto& triangle = pack_.triangles()[triangle_index];
    const auto& vertices = pack_.vertices();
    return (static_cast<double>(vertices[triangle.vertices[0]].height) + static_cast<double>(vertices[triangle.vertices[1]].height)
            + static_cast<double>(vertices[triangle.vertices[2]].height))
           / 3.0;
}

BaseNavRouteResult BaseNavPlanner::findPath(const BaseNavRouteRequest& request) const
{
    const BaseNavZone* zone = request.zone_id != 0 ? pack_.findZone(request.zone_id) : pack_.findZoneByName(request.zone_name);
    if (zone == nullptr) {
        BaseNavRouteResult result;
        result.status = BaseNavRouteStatus::ZoneNotFound;
        return result;
    }

    const auto start = snap(zone->zone_id, request.start, request.snap_radius);
    if (!start) {
        BaseNavRouteResult result;
        result.status = BaseNavRouteStatus::StartNotWalkable;
        return result;
    }
    const auto goal = snap(zone->zone_id, request.goal, request.snap_radius);
    if (!goal) {
        BaseNavRouteResult result;
        result.status = BaseNavRouteStatus::GoalNotWalkable;
        return result;
    }
    const auto& triangles = pack_.triangles();
    std::priority_queue<QueueNode> open;
    std::vector<double> g_score(triangles.size(), std::numeric_limits<double>::infinity());
    std::vector<int32_t> parents(triangles.size(), -1);
    std::vector<uint8_t> closed(triangles.size(), 0);
    g_score[start->triangle] = 0.0;
    open.push(
        { .triangle = start->triangle, .priority = detail::TriangleHeuristic(triangles[start->triangle], triangles[goal->triangle]) });

    while (!open.empty()) {
        const uint32_t current = open.top().triangle;
        open.pop();
        if (closed[current] != 0) {
            continue;
        }
        if (current == goal->triangle) {
            BaseNavRouteResult result;
            result.status = BaseNavRouteStatus::Success;
            result.triangles = reconstructPath(parents, start->triangle, goal->triangle);
            result.path.zone_id = zone->zone_id;
            result.path.zone_name = zone->name;
            result.path.points = buildWaypoints(result.triangles, start->point, goal->point, result.path.segment_breaks);
            result.cost = g_score[current];
            return result;
        }
        closed[current] = 1;
        for (uint32_t next : adjacency_[current]) {
            if (next >= triangles.size() || triangle_zones_[next] != zone->zone_id) {
                continue;
            }
            const double tentative = g_score[current] + transitionCost(current, next);
            if (request.max_cost > 0.0 && tentative > request.max_cost) {
                continue;
            }
            if (tentative >= g_score[next]) {
                continue;
            }
            parents[next] = static_cast<int32_t>(current);
            g_score[next] = tentative;
            open.push({ .triangle = next, .priority = tentative + detail::TriangleHeuristic(triangles[next], triangles[goal->triangle]) });
        }
    }

    BaseNavRouteResult result;
    result.status = BaseNavRouteStatus::Unreachable;
    return result;
}

std::optional<BaseNavSnapResult> BaseNavPlanner::snap(uint16_t zone_id, const WorldPoint& point, double radius) const
{
    const auto candidates = candidateTriangles(zone_id, point, radius);
    std::optional<BaseNavSnapResult> best;
    for (uint32_t triangle_index : candidates) {
        const auto points = trianglePoints(triangle_index);
        const WorldPoint snapped = detail::ClosestPointOnTriangle(point, points);
        const double distance = detail::Distance(snapped, point);
        if (distance > radius && !detail::PointInTriangle(point, points)) {
            continue;
        }
        if (!best || distance < best->distance) {
            best = BaseNavSnapResult { .triangle = triangle_index, .point = snapped, .distance = distance };
        }
    }
    return best;
}

std::vector<uint32_t> BaseNavPlanner::candidateTriangles(uint16_t zone_id, const WorldPoint& point, double radius) const
{
    const BaseNavZone* zone = pack_.findZone(zone_id);
    if (zone == nullptr) {
        return {};
    }
    std::vector<uint32_t> result;
    std::unordered_set<uint32_t> seen;
    const int32_t left_bin = static_cast<int32_t>(std::floor((point.x - radius) / kIndexBinSize));
    const int32_t right_bin = static_cast<int32_t>(std::floor((point.x + radius) / kIndexBinSize));
    const int32_t top_bin = static_cast<int32_t>(std::floor((point.y - radius) / kIndexBinSize));
    const int32_t bottom_bin = static_cast<int32_t>(std::floor((point.y + radius) / kIndexBinSize));
    for (int32_t bin_x = left_bin; bin_x <= right_bin; ++bin_x) {
        for (int32_t bin_y = top_bin; bin_y <= bottom_bin; ++bin_y) {
            const auto iter = bins_.find(BinKey { .zone_id = zone_id, .x = bin_x, .y = bin_y });
            if (iter == bins_.end()) {
                continue;
            }
            for (uint32_t triangle_index : iter->second) {
                if (!seen.insert(triangle_index).second) {
                    continue;
                }
                const auto& bounds = triangle_bounds_[triangle_index];
                if (bounds[0] - radius <= point.x && point.x <= bounds[2] + radius && bounds[1] - radius <= point.y
                    && point.y <= bounds[3] + radius) {
                    result.push_back(triangle_index);
                }
            }
        }
    }
    if (result.empty() && radius < kIndexBinSize) {
        return candidateTriangles(zone_id, point, kIndexBinSize);
    }
    return result;
}

std::array<WorldPoint, 3> BaseNavPlanner::trianglePoints(uint32_t triangle_index) const
{
    const BaseNavTriangle& triangle = pack_.triangles()[triangle_index];
    const auto& vertices = pack_.vertices();
    return {
        WorldPoint { .x = vertices[triangle.vertices[0]].u, .y = vertices[triangle.vertices[0]].v },
        WorldPoint { .x = vertices[triangle.vertices[1]].u, .y = vertices[triangle.vertices[1]].v },
        WorldPoint { .x = vertices[triangle.vertices[2]].u, .y = vertices[triangle.vertices[2]].v },
    };
}

std::optional<std::array<WorldPoint, 2>> BaseNavPlanner::sharedEdgePortal(uint32_t lhs, uint32_t rhs) const
{
    std::array<uint32_t, 2> shared { 0, 0 };
    size_t count = 0;
    for (uint32_t left_vertex : pack_.triangles()[lhs].vertices) {
        for (uint32_t right_vertex : pack_.triangles()[rhs].vertices) {
            if (left_vertex == right_vertex && count < shared.size()) {
                shared[count++] = left_vertex;
            }
        }
    }
    if (count != 2) {
        return overlappingEdgePortal(lhs, rhs);
    }
    const auto& vertices = pack_.vertices();
    return std::array {
        WorldPoint { .x = vertices[shared[0]].u, .y = vertices[shared[0]].v },
        WorldPoint { .x = vertices[shared[1]].u, .y = vertices[shared[1]].v },
    };
}

std::optional<WorldPoint> BaseNavPlanner::sharedEdgeMidpoint(uint32_t lhs, uint32_t rhs) const
{
    const auto portal = sharedEdgePortal(lhs, rhs);
    if (!portal) {
        return std::nullopt;
    }
    return WorldPoint {
        .x = ((*portal)[0].x + (*portal)[1].x) * 0.5,
        .y = ((*portal)[0].y + (*portal)[1].y) * 0.5,
    };
}

std::optional<std::array<WorldPoint, 2>> BaseNavPlanner::overlappingEdgePortal(uint32_t lhs, uint32_t rhs) const
{
    const auto lhs_points = trianglePoints(lhs);
    const auto rhs_points = trianglePoints(rhs);
    const std::array<std::array<WorldPoint, 2>, 3> lhs_edges {
        std::array<WorldPoint, 2> { lhs_points[0], lhs_points[1] },
        std::array<WorldPoint, 2> { lhs_points[1], lhs_points[2] },
        std::array<WorldPoint, 2> { lhs_points[2], lhs_points[0] },
    };
    const std::array<std::array<WorldPoint, 2>, 3> rhs_edges {
        std::array<WorldPoint, 2> { rhs_points[0], rhs_points[1] },
        std::array<WorldPoint, 2> { rhs_points[1], rhs_points[2] },
        std::array<WorldPoint, 2> { rhs_points[2], rhs_points[0] },
    };
    for (const auto& lhs_edge : lhs_edges) {
        for (const auto& rhs_edge : rhs_edges) {
            if (const auto portal = detail::OverlappingSegmentPortal(lhs_edge[0], lhs_edge[1], rhs_edge[0], rhs_edge[1]); portal) {
                return portal;
            }
        }
    }
    return std::nullopt;
}

std::optional<std::array<WorldPoint, 2>> BaseNavPlanner::closestEdgeBridgePoints(uint32_t lhs, uint32_t rhs) const
{
    const auto lhs_points = trianglePoints(lhs);
    const auto rhs_points = trianglePoints(rhs);
    const std::array<std::array<WorldPoint, 2>, 3> lhs_edges {
        std::array<WorldPoint, 2> { lhs_points[0], lhs_points[1] },
        std::array<WorldPoint, 2> { lhs_points[1], lhs_points[2] },
        std::array<WorldPoint, 2> { lhs_points[2], lhs_points[0] },
    };
    const std::array<std::array<WorldPoint, 2>, 3> rhs_edges {
        std::array<WorldPoint, 2> { rhs_points[0], rhs_points[1] },
        std::array<WorldPoint, 2> { rhs_points[1], rhs_points[2] },
        std::array<WorldPoint, 2> { rhs_points[2], rhs_points[0] },
    };

    std::optional<std::tuple<double, WorldPoint, WorldPoint>> best;
    for (const auto& lhs_edge : lhs_edges) {
        for (const auto& rhs_edge : rhs_edges) {
            const auto candidate = detail::ClosestSegmentPoints(lhs_edge[0], lhs_edge[1], rhs_edge[0], rhs_edge[1]);
            if (!best || std::get<0>(candidate) < std::get<0>(*best)) {
                best = candidate;
            }
        }
    }
    if (!best) {
        return std::nullopt;
    }
    return std::array<WorldPoint, 2> { std::get<1>(*best), std::get<2>(*best) };
}

double BaseNavPlanner::transitionCost(uint32_t lhs, uint32_t rhs) const
{
    const auto& triangles = pack_.triangles();
    const WorldPoint lhs_center = detail::TriangleCenter(triangles[lhs]);
    const WorldPoint rhs_center = detail::TriangleCenter(triangles[rhs]);
    if (const auto midpoint = sharedEdgeMidpoint(lhs, rhs); midpoint) {
        return detail::Distance(lhs_center, *midpoint) + detail::Distance(*midpoint, rhs_center);
    }
    const auto bridge_points = closestEdgeBridgePoints(lhs, rhs);
    const double height_delta = std::abs(triangle_heights_[lhs] - triangle_heights_[rhs]);
    if (height_delta > kBridgeMaxHeightDelta) {
        return std::numeric_limits<double>::infinity();
    }
    if (!bridge_points) {
        return detail::TriangleHeuristic(triangles[lhs], triangles[rhs]) + kBridgeFixedCost + height_delta * kBridgeHeightCostFactor;
    }
    const double gap = detail::Distance((*bridge_points)[0], (*bridge_points)[1]);
    return detail::Distance(lhs_center, (*bridge_points)[0]) + gap + detail::Distance((*bridge_points)[1], rhs_center) + kBridgeFixedCost
           + gap * kBridgeGapCostFactor + height_delta * kBridgeHeightCostFactor;
}

std::vector<uint32_t> BaseNavPlanner::reconstructPath(const std::vector<int32_t>& parents, uint32_t start, uint32_t goal) const
{
    std::vector<uint32_t> path;
    uint32_t cursor = goal;
    path.push_back(goal);
    while (cursor != start) {
        if (cursor >= parents.size() || parents[cursor] < 0) {
            return {};
        }
        cursor = static_cast<uint32_t>(parents[cursor]);
        path.push_back(cursor);
    }
    std::reverse(path.begin(), path.end());
    return path;
}

std::vector<WorldPoint> BaseNavPlanner::buildWaypoints(
    const std::vector<uint32_t>& triangles,
    const WorldPoint& start,
    const WorldPoint& goal,
    std::vector<size_t>& segment_breaks) const
{
    std::vector<WorldPoint> points;
    std::vector<size_t> raw_segment_breaks;
    segment_breaks.clear();
    points.push_back(start);
    for (size_t index = 1; index < triangles.size(); ++index) {
        const uint32_t lhs = triangles[index - 1];
        const uint32_t rhs = triangles[index];
        const auto midpoint = sharedEdgeMidpoint(lhs, rhs);
        if (midpoint) {
            points.push_back(*midpoint);
            continue;
        }
        if (const auto bridge_points = closestEdgeBridgePoints(lhs, rhs); bridge_points) {
            points.push_back((*bridge_points)[0]);
            raw_segment_breaks.push_back(points.size());
            points.push_back((*bridge_points)[1]);
        }
    }
    points.push_back(goal);
    auto route = detail::PostProcessRoutePoints(points, raw_segment_breaks);
    segment_breaks = std::move(route.segment_breaks);
    return std::move(route.points);
}

const char* ToString(BaseNavRouteStatus status)
{
    switch (status) {
    case BaseNavRouteStatus::Success:
        return "success";
    case BaseNavRouteStatus::ZoneNotFound:
        return "zone_not_found";
    case BaseNavRouteStatus::StartNotWalkable:
        return "start_not_walkable";
    case BaseNavRouteStatus::GoalNotWalkable:
        return "goal_not_walkable";
    case BaseNavRouteStatus::Unreachable:
        return "unreachable";
    }
    return "unknown";
}

}
