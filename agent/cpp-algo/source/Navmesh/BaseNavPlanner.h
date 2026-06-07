#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <string>
#include <vector>

#include "BaseNavPack.h"
#include "NavmeshTypes.h"

namespace navmesh
{

struct BaseNavSnapResult
{
    uint32_t triangle = 0;
    WorldPoint point;
    double distance = 0.0;
};

struct BaseNavRouteRequest
{
    uint16_t zone_id = 0;
    std::string zone_name;
    WorldPoint start;
    WorldPoint goal;
    double snap_radius = 5.0;
    double max_cost = 0.0;
};

enum class BaseNavRouteStatus
{
    Success,
    ZoneNotFound,
    StartNotWalkable,
    GoalNotWalkable,
    Unreachable,
};

struct BaseNavRouteResult
{
    BaseNavRouteStatus status = BaseNavRouteStatus::Unreachable;
    WorldPath path;
    std::vector<uint32_t> triangles;
    double cost = 0.0;

    bool ok() const { return status == BaseNavRouteStatus::Success; }
};

class BaseNavPlanner
{
public:
    explicit BaseNavPlanner(const BaseNavPack& pack);

    BaseNavRouteResult findPath(const BaseNavRouteRequest& request) const;
    std::optional<BaseNavSnapResult> snap(uint16_t zone_id, const WorldPoint& point, double radius) const;

private:
    const BaseNavPack& pack_;
    std::vector<uint16_t> triangle_zones_;
    std::vector<uint32_t> adjacency_offsets_;
    std::vector<uint32_t> adjacency_links_;
    std::vector<double> triangle_heights_;
    std::vector<uint32_t> natural_component_ids_;
    std::vector<uint32_t> natural_component_sizes_;

    void buildIndex();
    void buildNaturalComponents();
    void computeTriangleHeights();
    double triangleAverageHeight(uint32_t triangle_index) const;
    bool isNaturalNeighbor(uint32_t lhs, uint32_t rhs) const;
    bool isTraversableLink(uint32_t lhs, uint32_t rhs) const;
    std::array<WorldPoint, 3> trianglePoints(uint32_t triangle_index) const;
    std::optional<std::array<WorldPoint, 2>> sharedEdgePortal(uint32_t lhs, uint32_t rhs) const;
    std::optional<WorldPoint> sharedEdgeMidpoint(uint32_t lhs, uint32_t rhs) const;
    std::optional<std::array<WorldPoint, 2>> overlappingEdgePortal(uint32_t lhs, uint32_t rhs) const;
    std::optional<std::array<WorldPoint, 2>> closestEdgeBridgePoints(uint32_t lhs, uint32_t rhs) const;
    double transitionCost(uint32_t lhs, uint32_t rhs) const;
    std::vector<uint32_t> reconstructPath(const std::vector<int32_t>& parents, uint32_t start, uint32_t goal) const;
    std::vector<WorldPoint> buildWaypoints(
        const std::vector<uint32_t>& triangles,
        const WorldPoint& start,
        const WorldPoint& goal,
        std::vector<size_t>& segment_breaks) const;
};

const char* ToString(BaseNavRouteStatus status);

}
