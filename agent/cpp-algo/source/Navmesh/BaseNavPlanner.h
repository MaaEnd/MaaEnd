#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <string>
#include <unordered_map>
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
    struct BinKey
    {
        uint16_t zone_id = 0;
        int32_t x = 0;
        int32_t y = 0;

        bool operator==(const BinKey& rhs) const { return zone_id == rhs.zone_id && x == rhs.x && y == rhs.y; }
    };

    struct BinKeyHash
    {
        size_t operator()(const BinKey& key) const;
    };

    const BaseNavPack& pack_;
    std::vector<uint16_t> triangle_zones_;
    std::vector<std::vector<uint32_t>> adjacency_;
    std::vector<std::array<double, 4>> triangle_bounds_;
    std::vector<double> triangle_heights_;
    std::unordered_map<BinKey, std::vector<uint32_t>, BinKeyHash> bins_;

    void buildIndex();
    std::vector<uint32_t> candidateTriangles(uint16_t zone_id, const WorldPoint& point, double radius) const;
    double triangleAverageHeight(uint32_t triangle_index) const;
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
