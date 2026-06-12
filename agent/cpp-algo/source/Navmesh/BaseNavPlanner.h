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
    std::vector<uint32_t> blocked_triangles;
    // Dominant-floor height of the floor being navigated (from the locator/tier zone). Lets snap resolve
    // onto the right floor of a multi-floor base. kBaseNavFloorYNone (default) keeps the floor-blind path.
    float floor_y = kBaseNavFloorYNone;
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
    // `floor_y` re-ranks the snap onto the correct floor of a multi-floor base: surfaces within
    // kBaseNavFloorBand of it are preferred, off-band ones are a graceful fallback (never gated to
    // nullopt). kBaseNavFloorYNone (the default) keeps the legacy floor-blind behavior byte-for-byte.
    // Mirrors basenav_preview.py BaseNavField.snap.
    std::optional<BaseNavSnapResult> snap(
        uint16_t zone_id, const WorldPoint& point, double radius, float floor_y = kBaseNavFloorYNone) const;

    // Navmesh raycast: true when the straight segment a->b stays on walkable mesh within `zone_id`.
    // Fails closed on any ambiguity.
    bool isSegmentWalkable(uint16_t zone_id, const WorldPoint& a, const WorldPoint& b) const;

    // Point-containment test: true when `point` lies in any triangle of `zone_id`. Unlike the marching
    // isSegmentWalkable it ignores adjacency, so it does not misjudge on overlapping/fragmented meshes.
    bool pointOnMesh(uint16_t zone_id, const WorldPoint& point) const;

private:
    const BaseNavPack& pack_;
    std::vector<uint16_t> triangle_zones_;
    std::vector<uint32_t> adjacency_offsets_;
    std::vector<uint32_t> adjacency_links_;
    std::vector<double> triangle_heights_;
    std::vector<uint32_t> natural_component_ids_;
    std::vector<uint32_t> natural_component_sizes_;
    // 空间分箱索引:(zone_id, bin_x, bin_y) → 该格覆盖的三角形下标。使 snap/pointOnMesh 从全区线性
    // 扫描降为 O(邻近候选);剔除条件不变,结果与线性扫描完全一致。对齐 Python basenav_lib 的 bins。
    std::unordered_map<uint64_t, std::vector<uint32_t>> spatial_bins_;

    void buildIndex();
    void buildNaturalComponents();
    void buildSpatialIndex();
    // 返回 point±radius 覆盖的格内全部三角形(可能跨格重复,不影响结果)。
    std::vector<uint32_t> candidateTriangles(uint16_t zone_id, const WorldPoint& point, double radius) const;
    void computeTriangleHeights();
    double triangleAverageHeight(uint32_t triangle_index) const;
    bool isNaturalNeighbor(uint32_t lhs, uint32_t rhs) const;
    bool isTraversableLink(uint32_t lhs, uint32_t rhs) const;
    // point 处的地面高度:取包含 point 的候选三角形中高度与 reference 最接近者(高度连续性,跨重叠缝
    // 选回脚下路面而非墙体)。reference 为空时取最低高度;无三角形包含时返回 nullopt。out_triangle 回传
    // 选中三角形供 segmentHeightWalkable 缓存复用。
    std::optional<double> groundHeightNearIndexed(
        uint16_t zone_id,
        const WorldPoint& point,
        std::optional<double> reference,
        uint32_t& out_triangle) const;
    // LOS 拉直的可行性判据,取代抽稀中的 march:沿 a→b 采样,要求每点在网格内、且相邻采样的地面高度
    // 跳变不超过 kBridgeMaxHeightDelta。共面捷径全程平坦判可走(被拉直至中线),绕墙捷径因踩墙跳变判
    // 不可走(直角得以保留)。march 在共面重叠缝处误判不可走、使抽稀拉不直,故改用此高度连续性判据。
    // blocked 非空(绕障查询)时,直线踩入任一被封堵三角形即判不可走——即直线穿回障碍本身;反之直线
    // 全程绕开封堵集,才作为两端真实可衔接的可达性证明被接受。
    bool segmentHeightWalkable(
        uint16_t zone_id,
        const WorldPoint& a,
        const WorldPoint& b,
        const std::vector<uint8_t>* blocked = nullptr) const;
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
