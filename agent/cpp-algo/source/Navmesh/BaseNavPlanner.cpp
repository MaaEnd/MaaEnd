#include <algorithm>
#include <array>
#include <cmath>
#include <cstddef>
#include <cstdint>
#include <limits>
#include <numeric>
#include <queue>
#include <tuple>
#include <utility>

#include "BaseNavGeometry.h"
#include "BaseNavPlanner.h"
#include "BaseNavRoutePostProcess.h"

namespace navmesh
{

namespace
{

constexpr double kBridgeFixedCost = 12.0;
constexpr double kBridgeGapCostFactor = 3.0;
constexpr double kBridgeHeightCostFactor = 40.0;
constexpr double kBridgeMaxHeightDelta = 3.0;
constexpr uint32_t kSmallBridgeComponentMaxTriangles = 512;
constexpr double kSmallBridgeMaxGap = 4.0;
constexpr double kRoutePullSampleStep = 0.5; // 拉直判据沿捷径采样的步长(像素),与 Python ROUTE_PULL_SAMPLE_STEP 对齐
constexpr double kRouteBoundaryDistanceLimit = 8.0;
constexpr double kRouteBoundaryPenaltyFactor = 2.0;

struct QueueNode
{
    uint32_t triangle = 0;
    double priority = 0.0;

    bool operator<(const QueueNode& rhs) const { return priority > rhs.priority; }
};

struct DisjointSet
{
    std::vector<uint32_t> parent;

    explicit DisjointSet(size_t count)
        : parent(count, 0)
    {
        std::iota(parent.begin(), parent.end(), 0U);
    }

    uint32_t find(uint32_t value)
    {
        while (parent[value] != value) {
            parent[value] = parent[parent[value]];
            value = parent[value];
        }
        return value;
    }

    void unite(uint32_t lhs, uint32_t rhs)
    {
        const uint32_t lhs_root = find(lhs);
        const uint32_t rhs_root = find(rhs);
        if (lhs_root != rhs_root) {
            parent[rhs_root] = lhs_root;
        }
    }
};

constexpr uint32_t kInvalidTriangle = std::numeric_limits<uint32_t>::max();
constexpr double kIndexBinSize = 4.0; // 空间分箱的格边长(px),与 Python basenav_lib INDEX_BIN_SIZE 对齐。

// tier 细分后三角形极小,8px 每桶堆 ~200 个 → 每次 pointOnMesh 候选遍历是后处理热点;4px 每桶 ~12 个,查询 ~4x 快
// (空间索引仅加速查询、不改判定结果)。建索引略增但 C++ 编译版快,可忽略。

uint64_t PackBinKey(uint16_t zone_id, int32_t bin_x, int32_t bin_y)
{
    const uint64_t zone = static_cast<uint64_t>(zone_id);
    const uint64_t packed_x = static_cast<uint64_t>(static_cast<uint32_t>(bin_x)) & 0xFFFFFFu;
    const uint64_t packed_y = static_cast<uint64_t>(static_cast<uint32_t>(bin_y)) & 0xFFFFFFu;
    return (zone << 48) | (packed_x << 24) | packed_y;
}

constexpr double kSegmentWalkSnapRadius = 1.0;    // snap radius for locating the segment origin on the mesh
constexpr double kSegmentWalkEpsilon = 1e-6;      // tolerance on the t/s intersection fractions
constexpr double kSegmentParallelEpsilon = 1e-12; // |determinant| below this => segments treated as parallel

// Intersection parameters of segment a->b with c->d; false if (near) parallel. `t` is the fraction
// along a->b, `s` along c->d.
bool SegmentIntersectParams(const WorldPoint& a, const WorldPoint& b, const WorldPoint& c, const WorldPoint& d, double& t, double& s)
{
    const double rx = b.x - a.x;
    const double ry = b.y - a.y;
    const double sx = d.x - c.x;
    const double sy = d.y - c.y;
    const double denom = rx * sy - ry * sx;
    if (std::abs(denom) < kSegmentParallelEpsilon) {
        return false;
    }
    const double qpx = c.x - a.x;
    const double qpy = c.y - a.y;
    t = (qpx * sy - qpy * sx) / denom;
    s = (qpx * ry - qpy * rx) / denom;
    return true;
}

// SSF 漏斗辅助函数 -----------------------------------------------------------

static double FunnelTriArea2(const WorldPoint& a, const WorldPoint& b, const WorldPoint& c)
{
    return (b.x - a.x) * (c.y - a.y) - (c.x - a.x) * (b.y - a.y);
}

static bool WptEqual(const WorldPoint& a, const WorldPoint& b)
{
    return std::abs(a.x - b.x) < 1e-7 && std::abs(a.y - b.y) < 1e-7;
}

// Mononen Simple Stupid Funnel.
static std::vector<WorldPoint> SsfFunnel(const std::vector<std::pair<WorldPoint, WorldPoint>>& portals)
{
    std::vector<WorldPoint> pts;
    if (portals.empty()) {
        return pts;
    }
    pts.push_back(portals[0].first);
    WorldPoint apex = portals[0].first;
    WorldPoint pl = portals[0].first;
    WorldPoint pr = portals[0].second;
    size_t ai = 0, li = 0, ri = 0;
    size_t i = 1;
    while (i < portals.size()) {
        const auto& [left, right] = portals[i];
        if (FunnelTriArea2(apex, pr, right) <= 0.0) {
            if (WptEqual(apex, pr) || FunnelTriArea2(apex, pl, right) > 0.0) {
                pr = right;
                ri = i;
            }
            else {
                pts.push_back(pl);
                apex = pl;
                ai = li;
                pl = pr = apex;
                li = ri = ai;
                i = ai + 1;
                continue;
            }
        }
        if (FunnelTriArea2(apex, pl, left) >= 0.0) {
            if (WptEqual(apex, pl) || FunnelTriArea2(apex, pr, left) < 0.0) {
                pl = left;
                li = i;
            }
            else {
                pts.push_back(pr);
                apex = pr;
                ai = ri;
                pl = pr = apex;
                li = ri = ai;
                i = ai + 1;
                continue;
            }
        }
        ++i;
    }
    pts.push_back(portals.back().first);
    return pts;
}

}

BaseNavPlanner::BaseNavPlanner(const BaseNavPack& pack)
    : pack_(pack)
    , triangle_zones_(pack.triangles().size(), 0)
    , adjacency_offsets_(pack.triangles().size() + 1, 0)
    , triangle_heights_(pack.triangles().size(), 0.0)
    , triangle_boundary_distances_(pack.triangles().size(), std::numeric_limits<double>::infinity())
{
    buildIndex();
    computeTriangleBoundaryDistances();
    buildSpatialIndex();
    computeTriangleHeights();
}

void BaseNavPlanner::buildIndex()
{
    for (const auto& zone : pack_.zones()) {
        const uint32_t end = zone.first_triangle + zone.triangle_count;
        for (uint32_t index = zone.first_triangle; index < end && index < triangle_zones_.size(); ++index) {
            triangle_zones_[index] = zone.zone_id;
        }
    }
    buildNaturalComponents();

    size_t valid_link_count = 0;
    for (const BaseNavLink& link : pack_.links()) {
        if (isTraversableLink(link.source, link.target)) {
            ++adjacency_offsets_[link.source + 1];
            ++valid_link_count;
        }
    }
    for (size_t index = 1; index < adjacency_offsets_.size(); ++index) {
        adjacency_offsets_[index] += adjacency_offsets_[index - 1];
    }

    adjacency_links_.resize(valid_link_count);
    std::vector<uint32_t> next_offsets = adjacency_offsets_;
    for (const BaseNavLink& link : pack_.links()) {
        if (isTraversableLink(link.source, link.target)) {
            adjacency_links_[next_offsets[link.source]++] = link.target;
        }
    }
}

void BaseNavPlanner::buildNaturalComponents()
{
    const auto& triangles = pack_.triangles();
    DisjointSet components(triangles.size());
    for (uint32_t triangle_index = 0; triangle_index < triangles.size(); ++triangle_index) {
        for (int32_t neighbor : triangles[triangle_index].neighbors) {
            if (neighbor < 0) {
                continue;
            }
            const uint32_t next = static_cast<uint32_t>(neighbor);
            if (next < triangles.size() && triangle_zones_[next] == triangle_zones_[triangle_index]) {
                components.unite(triangle_index, next);
            }
        }
    }

    constexpr uint32_t kInvalidComponent = std::numeric_limits<uint32_t>::max();
    std::vector<uint32_t> root_to_component(triangles.size(), kInvalidComponent);
    natural_component_ids_.assign(triangles.size(), kInvalidComponent);
    natural_component_sizes_.clear();
    for (uint32_t triangle_index = 0; triangle_index < triangles.size(); ++triangle_index) {
        const uint32_t root = components.find(triangle_index);
        uint32_t& component_id = root_to_component[root];
        if (component_id == kInvalidComponent) {
            component_id = static_cast<uint32_t>(natural_component_sizes_.size());
            natural_component_sizes_.push_back(0);
        }
        natural_component_ids_[triangle_index] = component_id;
        ++natural_component_sizes_[component_id];
    }
}

void BaseNavPlanner::buildSpatialIndex()
{
    const auto& triangles = pack_.triangles();
    for (uint32_t triangle_index = 0; triangle_index < triangles.size(); ++triangle_index) {
        const uint16_t zone_id = triangle_index < triangle_zones_.size() ? triangle_zones_[triangle_index] : 0;
        if (zone_id == 0) {
            continue; // 区外三角形不入索引(与 Python _build_index 一致)
        }
        const auto points = trianglePoints(triangle_index);
        const double left = std::min({ points[0].x, points[1].x, points[2].x });
        const double right = std::max({ points[0].x, points[1].x, points[2].x });
        const double top = std::min({ points[0].y, points[1].y, points[2].y });
        const double bottom = std::max({ points[0].y, points[1].y, points[2].y });
        const int32_t bin_x0 = static_cast<int32_t>(std::floor(left / kIndexBinSize));
        const int32_t bin_x1 = static_cast<int32_t>(std::floor(right / kIndexBinSize));
        const int32_t bin_y0 = static_cast<int32_t>(std::floor(top / kIndexBinSize));
        const int32_t bin_y1 = static_cast<int32_t>(std::floor(bottom / kIndexBinSize));
        for (int32_t bin_x = bin_x0; bin_x <= bin_x1; ++bin_x) {
            for (int32_t bin_y = bin_y0; bin_y <= bin_y1; ++bin_y) {
                spatial_bins_[PackBinKey(zone_id, bin_x, bin_y)].push_back(triangle_index);
            }
        }
    }
}

bool BaseNavPlanner::triangleHasBoundaryEdge(uint32_t triangle_index) const
{
    if (triangle_index >= pack_.triangles().size() || triangle_index >= triangle_zones_.size()) {
        return false;
    }
    const uint16_t zone_id = triangle_zones_[triangle_index];
    if (zone_id == 0) {
        return false;
    }
    const BaseNavTriangle& triangle = pack_.triangles()[triangle_index];
    for (int32_t neighbor : triangle.neighbors) {
        if (neighbor < 0 || static_cast<uint32_t>(neighbor) >= triangle_zones_.size()
            || triangle_zones_[static_cast<uint32_t>(neighbor)] != zone_id) {
            return true;
        }
    }
    return false;
}

void BaseNavPlanner::computeTriangleBoundaryDistances()
{
    const auto& triangles = pack_.triangles();
    std::priority_queue<QueueNode> open;
    triangle_boundary_distances_.assign(triangles.size(), std::numeric_limits<double>::infinity());

    for (uint32_t triangle_index = 0; triangle_index < triangles.size(); ++triangle_index) {
        if (triangleHasBoundaryEdge(triangle_index)) {
            triangle_boundary_distances_[triangle_index] = 0.0;
            open.push({ .triangle = triangle_index, .priority = 0.0 });
        }
    }

    while (!open.empty()) {
        const QueueNode node = open.top();
        open.pop();
        const uint32_t current = node.triangle;
        if (current >= triangles.size() || node.priority > triangle_boundary_distances_[current]) {
            continue;
        }
        if (node.priority >= kRouteBoundaryDistanceLimit) {
            continue;
        }

        const WorldPoint current_center = detail::TriangleCenter(triangles[current]);
        for (uint32_t adjacency_index = adjacency_offsets_[current]; adjacency_index < adjacency_offsets_[current + 1]; ++adjacency_index) {
            const uint32_t next = adjacency_links_[adjacency_index];
            if (next >= triangles.size() || triangle_zones_[next] != triangle_zones_[current]) {
                continue;
            }
            const double step = detail::Distance(current_center, detail::TriangleCenter(triangles[next]));
            const double candidate = node.priority + step;
            if (candidate >= triangle_boundary_distances_[next] || candidate > kRouteBoundaryDistanceLimit) {
                continue;
            }
            triangle_boundary_distances_[next] = candidate;
            open.push({ .triangle = next, .priority = candidate });
        }
    }
}

std::vector<uint32_t> BaseNavPlanner::candidateTriangles(uint16_t zone_id, const WorldPoint& point, double radius) const
{
    std::vector<uint32_t> result;
    const double query_radius = std::max(0.0, radius);
    const int32_t bin_x0 = static_cast<int32_t>(std::floor((point.x - query_radius) / kIndexBinSize));
    const int32_t bin_x1 = static_cast<int32_t>(std::floor((point.x + query_radius) / kIndexBinSize));
    const int32_t bin_y0 = static_cast<int32_t>(std::floor((point.y - query_radius) / kIndexBinSize));
    const int32_t bin_y1 = static_cast<int32_t>(std::floor((point.y + query_radius) / kIndexBinSize));
    for (int32_t bin_x = bin_x0; bin_x <= bin_x1; ++bin_x) {
        for (int32_t bin_y = bin_y0; bin_y <= bin_y1; ++bin_y) {
            const auto found = spatial_bins_.find(PackBinKey(zone_id, bin_x, bin_y));
            if (found == spatial_bins_.end()) {
                continue;
            }
            result.insert(result.end(), found->second.begin(), found->second.end());
        }
    }
    return result;
}

bool BaseNavPlanner::pointOnMesh(uint16_t zone_id, const WorldPoint& point) const
{
    if (pack_.findZone(zone_id) == nullptr) {
        return false;
    }
    for (const uint32_t triangle_index : candidateTriangles(zone_id, point, kSegmentWalkSnapRadius)) {
        if (triangle_zones_[triangle_index] != zone_id) {
            continue;
        }
        if (detail::PointInTriangle(point, trianglePoints(triangle_index))) {
            return true;
        }
    }
    return false;
}

void BaseNavPlanner::computeTriangleHeights()
{
    const auto& triangles = pack_.triangles();
    const auto& vertices = pack_.vertices();
    for (size_t index = 0; index < triangles.size(); ++index) {
        const auto& triangle = triangles[index];
        triangle_heights_[index] =
            (static_cast<double>(vertices[triangle.vertices[0]].height) + static_cast<double>(vertices[triangle.vertices[1]].height)
             + static_cast<double>(vertices[triangle.vertices[2]].height))
            / 3.0;
    }
}

double BaseNavPlanner::triangleAverageHeight(uint32_t triangle_index) const
{
    return triangle_heights_[triangle_index];
}

double BaseNavPlanner::boundaryAwareTransitionCost(uint32_t lhs, uint32_t rhs, double base_cost) const
{
    const auto penalty_ratio = [this](uint32_t triangle_index) {
        if (triangle_index >= triangle_boundary_distances_.size()) {
            return 0.0;
        }
        const double distance = triangle_boundary_distances_[triangle_index];
        if (!std::isfinite(distance) || distance >= kRouteBoundaryDistanceLimit) {
            return 0.0;
        }
        return (kRouteBoundaryDistanceLimit - distance) / kRouteBoundaryDistanceLimit;
    };
    const double ratio = (penalty_ratio(lhs) + penalty_ratio(rhs)) * 0.5;
    return base_cost * (1.0 + ratio * kRouteBoundaryPenaltyFactor);
}

std::optional<double> BaseNavPlanner::groundHeightNearIndexed(
    uint16_t zone_id,
    const WorldPoint& point,
    std::optional<double> reference,
    uint32_t& out_triangle) const
{
    std::optional<double> best;
    out_triangle = kInvalidTriangle;
    for (const uint32_t triangle_index : candidateTriangles(zone_id, point, kSegmentWalkSnapRadius)) {
        if (triangle_zones_[triangle_index] != zone_id) {
            continue;
        }
        if (!detail::PointInTriangle(point, trianglePoints(triangle_index))) {
            continue;
        }
        const double height = triangle_heights_[triangle_index];
        if (!best) {
            best = height;
            out_triangle = triangle_index;
        }
        else if (!reference) {
            if (height < *best) { // 无参考(直线起点):取最低瓦片,即路面而非恰好重叠其上的墙体
                best = height;
                out_triangle = triangle_index;
            }
        }
        else if (std::abs(height - *reference) < std::abs(*best - *reference)) { // 取与上一采样高度最接近者,保持脚下地面连续
            best = height;
            out_triangle = triangle_index;
        }
    }
    return best;
}

bool BaseNavPlanner::segmentHeightWalkable(uint16_t zone_id, const WorldPoint& a, const WorldPoint& b, const std::vector<uint8_t>* blocked)
    const
{
    if (pack_.findZone(zone_id) == nullptr) {
        return false;
    }
    const double length = std::hypot(b.x - a.x, b.y - a.y);
    const int samples = std::max(1, static_cast<int>(length / kRoutePullSampleStep));
    std::optional<double> previous;
    // 缓存上一采样点命中的三角形:相邻采样多落在同一三角形内,命中则复用其高度,省去 candidateTriangles
    // 扫描,且结果与完整扫描等价。
    uint32_t cached = kInvalidTriangle;
    for (int index = 0; index <= samples; ++index) {
        const double t = static_cast<double>(index) / samples;
        const WorldPoint point { .x = a.x + (b.x - a.x) * t, .y = a.y + (b.y - a.y) * t };
        if (previous && cached != kInvalidTriangle) {
            if (detail::PointInTriangle(point, trianglePoints(cached))) {
                continue; // 命中缓存:高度等于 previous、三角形未变(已判过阻挡),直接进入下一采样点
            }
        }
        const std::optional<double> height = groundHeightNearIndexed(zone_id, point, previous, cached);
        if (!height) {
            return false; // 采样点离开网格,判定捷径不可走
        }
        // 直线踩入被封堵三角形即等于直穿障碍本身,拒绝;反之全程绕开封堵集,方为真实可衔接的可达性证明。
        if (blocked != nullptr && cached < blocked->size() && (*blocked)[cached] != 0) {
            return false;
        }
        if (previous && std::abs(*height - *previous) > kBridgeMaxHeightDelta) {
            return false; // 地面高度突跳(踩入墙体或跌落台面),为结构性拐角,捷径不可走
        }
        previous = height;
    }
    return true;
}

bool BaseNavPlanner::isNaturalNeighbor(uint32_t lhs, uint32_t rhs) const
{
    for (int32_t neighbor : pack_.triangles()[lhs].neighbors) {
        if (neighbor >= 0 && static_cast<uint32_t>(neighbor) == rhs) {
            return true;
        }
    }
    return false;
}

bool BaseNavPlanner::isTraversableLink(uint32_t lhs, uint32_t rhs) const
{
    if (lhs >= triangle_zones_.size() || rhs >= triangle_zones_.size() || triangle_zones_[lhs] == 0
        || triangle_zones_[lhs] != triangle_zones_[rhs]) {
        return false;
    }
    if (isNaturalNeighbor(lhs, rhs)) {
        return true;
    }

    const uint32_t lhs_component = natural_component_ids_[lhs];
    const uint32_t rhs_component = natural_component_ids_[rhs];
    const uint32_t min_component_size = std::min(natural_component_sizes_[lhs_component], natural_component_sizes_[rhs_component]);
    if (min_component_size > kSmallBridgeComponentMaxTriangles) {
        return true;
    }

    const auto bridge_points = closestEdgeBridgePoints(lhs, rhs);
    return bridge_points && detail::Distance((*bridge_points)[0], (*bridge_points)[1]) <= kSmallBridgeMaxGap;
}

BaseNavRouteResult BaseNavPlanner::findPath(const BaseNavRouteRequest& request) const
{
    const BaseNavZone* zone = request.zone_id != 0 ? pack_.findZone(request.zone_id) : pack_.findZoneByName(request.zone_name);
    if (zone == nullptr) {
        BaseNavRouteResult result;
        result.status = BaseNavRouteStatus::ZoneNotFound;
        return result;
    }

    // A tier zone carries no triangles — it is just the affine onto its parent geometry zone. Mirror the
    // python tool (is_tier -> geometry_zone_id + base = s*tier + t): map the query points through the
    // tier's OWN baked affine and route on the parent's triangles. Current callers pass the already-inferred
    // geometry zone, so this branch is inert for them; it is the planner's self-sufficient fallback so a tier
    // zone_id never has to be resolved through the external MapTracker transforms.
    WorldPoint start_point = request.start;
    WorldPoint goal_point = request.goal;
    // Resolve the floor to snap onto. The request's floor_y (set by runtime callers from the locator/tier
    // zone) takes precedence; otherwise fall back to the queried zone's own baked floor, captured BEFORE any
    // tier->parent reassignment so the planner's self-sufficient tier-zone branch stays floor-aware. A
    // geometry query with no request floor yields the sentinel -> legacy floor-blind snap, byte-for-byte.
    const float base_floor = request.floor_y > kBaseNavFloorYValidMin ? request.floor_y : zone->floor_y;
    const float start_floor = request.start_floor_y > kBaseNavFloorYValidMin ? request.start_floor_y : base_floor;
    const float goal_floor = request.goal_floor_y > kBaseNavFloorYValidMin ? request.goal_floor_y : base_floor;
    if (IsTierZone(*zone)) {
        const BaseNavZone* parent = pack_.findZone(static_cast<uint16_t>(zone->component_count));
        if (parent == nullptr) {
            BaseNavRouteResult result;
            result.status = BaseNavRouteStatus::ZoneNotFound;
            return result;
        }
        const std::array<float, 4>& t = zone->transform;
        start_point = { static_cast<double>(t[0]) * start_point.x + static_cast<double>(t[1]),
                        static_cast<double>(t[2]) * start_point.y + static_cast<double>(t[3]) };
        goal_point = { static_cast<double>(t[0]) * goal_point.x + static_cast<double>(t[1]),
                       static_cast<double>(t[2]) * goal_point.y + static_cast<double>(t[3]) };
        zone = parent;
    }

    const auto start = snap(zone->zone_id, start_point, request.snap_radius, start_floor);
    if (!start) {
        BaseNavRouteResult result;
        result.status = BaseNavRouteStatus::StartNotWalkable;
        return result;
    }
    const auto goal = snap(zone->zone_id, goal_point, request.snap_radius, goal_floor);
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
    std::vector<uint8_t> blocked(triangles.size(), 0);
    for (uint32_t triangle : request.blocked_triangles) {
        if (triangle < blocked.size() && triangle != start->triangle && triangle != goal->triangle) {
            blocked[triangle] = 1;
        }
    }
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
        for (uint32_t adjacency_index = adjacency_offsets_[current]; adjacency_index < adjacency_offsets_[current + 1]; ++adjacency_index) {
            const uint32_t next = adjacency_links_[adjacency_index];
            if (next >= triangles.size() || triangle_zones_[next] != zone->zone_id) {
                continue;
            }
            if (blocked[next] != 0) {
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

    // A* over the triangle adjacency graph reports Unreachable on fragmented / overlapping meshes where
    // the goal sits in a tiny disconnected component, or just past a height step the bridge-cost cutoff
    // (kBridgeMaxHeightDelta) rejects — even when the goal is a short, flat walk away. Both endpoints have
    // already snapped onto the mesh here, so when the straight segment between them stays on walkable mesh
    // with continuous ground height it is a sound proof the goal is reachable; accept it as a direct path.
    // For obstacle-detour queries (blocked triangles set) the same proof holds as long as the straight
    // segment never steps onto a blocked triangle — passing the blocked mask makes segmentHeightWalkable
    // reject exactly that case. This is the difference between refusing a truly impassable crossing and
    // refusing two positions that are plainly connectable around the obstacle: the former still fails (the
    // straight line walks back through the obstacle and trips the mask, or hits a height step), while the
    // latter is now accepted instead of being lost to a fragmented-mesh A* false Unreachable.
    const std::vector<uint8_t>* blocked_mask = request.blocked_triangles.empty() ? nullptr : &blocked;
    if (segmentHeightWalkable(zone->zone_id, start->point, goal->point, blocked_mask)) {
        BaseNavRouteResult result;
        result.status = BaseNavRouteStatus::Success;
        result.triangles.push_back(start->triangle);
        if (goal->triangle != start->triangle) {
            result.triangles.push_back(goal->triangle);
        }
        result.path.zone_id = zone->zone_id;
        result.path.zone_name = zone->name;
        result.path.points = { start->point, goal->point };
        result.cost = detail::Distance(start->point, goal->point);
        return result;
    }

    BaseNavRouteResult result;
    result.status = BaseNavRouteStatus::Unreachable;
    return result;
}

std::optional<BaseNavSnapResult> BaseNavPlanner::snap(uint16_t zone_id, const WorldPoint& point, double radius, float floor_y) const
{
    const BaseNavZone* zone = pack_.findZone(zone_id);
    if (zone == nullptr) {
        return std::nullopt;
    }

    // 仅取邻近格内的候选三角形,替代对整区的线性扫描;经下方相同的剔除后结果与线性扫描一致。
    const double query_radius = std::max(0.0, radius);
    std::vector<uint32_t> candidates = candidateTriangles(zone_id, point, query_radius);
    if (candidates.empty() && query_radius < kIndexBinSize) {
        // 半径不足一格时邻域可能为空,放宽到整格再取候选(命中仍受 radius 距离剔除约束)。
        candidates = candidateTriangles(zone_id, point, kIndexBinSize);
    }

    // A (u,v) can stack a tiny disconnected fragment (baked wall-top / ledge, never the real walkable floor)
    // right over the dominant surface. Snapping onto it strands the endpoint in a micro-component and A* then
    // false-reports Unreachable. Demote such a fragment so a non-island candidate always wins — the same
    // size cutoff the bridge logic treats as a stitchable island. Only re-ranks when both compete; the common
    // single-surface (u,v) is untouched.
    const auto is_small_island = [&](uint32_t triangle_index) {
        return natural_component_sizes_[natural_component_ids_[triangle_index]] <= kSmallBridgeComponentMaxTriangles;
    };

    if (floor_y > kBaseNavFloorYValidMin) {
        // Floor-aware path: a click in a multi-floor base projects onto several STACKED triangles
        // (other floors / walls overlap this (u,v)). Rank so an in-band surface (|height-floor_y| <=
        // kBaseNavFloorBand) always beats an off-band one, then by snap distance, then by height
        // proximity to floor_y. The band is a PREFERENCE — if nothing lands in-band we still return the
        // nearest surface (never nullopt), so floor_y only re-ranks onto the right floor, never gates it
        // out. Mirrors basenav_preview.py BaseNavField.snap (floor-aware branch). The zone + bbox culls
        // match the legacy path below so the effective candidate set equals the python tool's.
        std::optional<std::tuple<int, int, double, double>> best_key;
        std::optional<BaseNavSnapResult> best_floor;
        for (const uint32_t triangle_index : candidates) {
            if (triangle_zones_[triangle_index] != zone_id) {
                continue;
            }
            const auto points = trianglePoints(triangle_index);
            const double left = std::min({ points[0].x, points[1].x, points[2].x });
            const double right = std::max({ points[0].x, points[1].x, points[2].x });
            const double top = std::min({ points[0].y, points[1].y, points[2].y });
            const double bottom = std::max({ points[0].y, points[1].y, points[2].y });
            if (point.x < left - radius || point.x > right + radius || point.y < top - radius || point.y > bottom + radius) {
                continue;
            }
            WorldPoint snapped = point;
            double distance = 0.0;
            if (!detail::PointInTriangle(point, points)) {
                snapped = detail::ClosestPointOnTriangle(point, points);
                distance = detail::Distance(snapped, point);
                if (distance > radius) {
                    continue;
                }
            }
            const double delta = std::abs(triangle_heights_[triangle_index] - static_cast<double>(floor_y));
            const std::tuple<int, int, double, double> key { delta <= static_cast<double>(kBaseNavFloorBand) ? 0 : 1,
                                                             is_small_island(triangle_index) ? 1 : 0,
                                                             distance,
                                                             delta };
            if (!best_key || key < *best_key) {
                best_key = key;
                best_floor = BaseNavSnapResult { .triangle = triangle_index, .point = snapped, .distance = distance };
            }
        }
        return best_floor;
    }

    // Floor-blind path: rank by (non-island first, then snap distance, then smallest index). With no island
    // in play this is exactly the legacy order — containing surfaces (distance 0) win, ties by smallest index —
    // so the golden-hash parity holds; it only diverges to skip a micro-component when a real surface competes.
    std::optional<std::tuple<int, double, uint32_t>> best_key;
    std::optional<BaseNavSnapResult> best;
    for (const uint32_t triangle_index : candidates) {
        if (triangle_zones_[triangle_index] != zone_id) {
            continue;
        }
        const auto points = trianglePoints(triangle_index);
        const double left = std::min({ points[0].x, points[1].x, points[2].x });
        const double right = std::max({ points[0].x, points[1].x, points[2].x });
        const double top = std::min({ points[0].y, points[1].y, points[2].y });
        const double bottom = std::max({ points[0].y, points[1].y, points[2].y });
        if (point.x < left - radius || point.x > right + radius || point.y < top - radius || point.y > bottom + radius) {
            continue;
        }
        WorldPoint snapped = point;
        double distance = 0.0;
        if (!detail::PointInTriangle(point, points)) {
            snapped = detail::ClosestPointOnTriangle(point, points);
            distance = detail::Distance(snapped, point);
            if (distance > radius) {
                continue;
            }
        }
        const std::tuple<int, double, uint32_t> key { is_small_island(triangle_index) ? 1 : 0, distance, triangle_index };
        if (!best_key || key < *best_key) {
            best_key = key;
            best = BaseNavSnapResult { .triangle = triangle_index, .point = snapped, .distance = distance };
        }
    }
    return best;
}

bool BaseNavPlanner::isRouteSegmentDrivable(uint16_t zone_id, const WorldPoint& a, const WorldPoint& b) const
{
    return segmentHeightWalkable(zone_id, a, b, nullptr);
}

uint16_t BaseNavPlanner::triangleZone(uint32_t triangle_index) const
{
    return triangle_index < triangle_zones_.size() ? triangle_zones_[triangle_index] : 0;
}

bool BaseNavPlanner::isSegmentWalkable(uint16_t zone_id, const WorldPoint& a, const WorldPoint& b) const
{
    if (pack_.findZone(zone_id) == nullptr) {
        return false;
    }
    if (detail::Distance(a, b) < kSegmentWalkEpsilon) {
        return true;
    }

    const auto start = snap(zone_id, a, kSegmentWalkSnapRadius);
    if (!start) {
        return false; // origin not on the mesh; fail closed
    }

    const auto& triangles = pack_.triangles();
    uint32_t current = start->triangle;
    // 沿 a→b 穿越三角形,要求出边交点参数 t 单调向前,截断重叠/共面三角形的横向往复,使不可达射线在
    // 墙处快速失败而非遍历整张网格。仅加速 False 路径,判定结果与原算法一致。entry_t 初值取负(非 0):
    // 起点常落在 portal 共享边上,起始三角形的真实出边可能 t≈0,否则会被单调过滤误剔除。
    double entry_t = -1.0;
    const size_t max_steps = triangles.size() + 4;
    for (size_t step = 0; step < max_steps; ++step) {
        const auto points = trianglePoints(current);
        if (detail::PointInTriangle(b, points)) {
            return true;
        }

        // Exit edge = forward-most crossing (largest t in (0, 1]).
        const BaseNavTriangle& triangle = triangles[current];
        double best_t = entry_t;
        uint32_t exit_va = 0;
        uint32_t exit_vb = 0;
        bool has_exit = false;
        for (int edge = 0; edge < 3; ++edge) {
            const WorldPoint& p0 = points[edge];
            const WorldPoint& p1 = points[(edge + 1) % 3];
            double t = 0.0;
            double s = 0.0;
            if (!SegmentIntersectParams(a, b, p0, p1, t, s)) {
                continue;
            }
            if (t <= entry_t + kSegmentWalkEpsilon || t > 1.0 + kSegmentWalkEpsilon) {
                continue;
            }
            if (s < -kSegmentWalkEpsilon || s > 1.0 + kSegmentWalkEpsilon) {
                continue;
            }
            if (t > best_t) {
                best_t = t;
                exit_va = triangle.vertices[edge];
                exit_vb = triangle.vertices[(edge + 1) % 3];
                has_exit = true;
            }
        }
        if (!has_exit) {
            return false; // numeric edge case; fail closed
        }

        // Neighbour sharing the exit edge's two vertices; absence => wall.
        uint32_t next = kInvalidTriangle;
        for (int32_t neighbor : triangle.neighbors) {
            if (neighbor < 0) {
                continue;
            }
            const auto& candidate = triangles[static_cast<uint32_t>(neighbor)].vertices;
            const bool has_va = candidate[0] == exit_va || candidate[1] == exit_va || candidate[2] == exit_va;
            const bool has_vb = candidate[0] == exit_vb || candidate[1] == exit_vb || candidate[2] == exit_vb;
            if (has_va && has_vb) {
                next = static_cast<uint32_t>(neighbor);
                break;
            }
        }
        if (next == kInvalidTriangle || next >= triangles.size() || triangle_zones_[next] != zone_id) {
            return false; // wall edge or zone boundary
        }
        entry_t = best_t; // 记录进入下一三角形的参数,强制单调向前
        current = next;
    }
    return false;
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

std::optional<std::pair<std::vector<WorldPoint>, std::vector<size_t>>> BaseNavPlanner::funnelRoutePoints(
    const std::vector<uint32_t>& triangles,
    const WorldPoint& start,
    const WorldPoint& goal,
    uint16_t zone_id) const
{
    if (triangles.size() <= 1) {
        return std::nullopt;
    }
    const auto& pack_triangles = pack_.triangles();
    const auto& pack_vertices = pack_.vertices();

    // 预处理每条走廊边:正规 2 共享顶点 → portal;退化 → bridge/pinch.
    struct PortalEdge
    {
        enum class Kind
        {
            Portal,
            Bridge,
            Pinch
        } kind;
        uint32_t u_idx = 0, v_idx = 0;      // Portal: CCW 有向出边 u→v; left=pu, right=pv
        WorldPoint exit_pt {}, entry_pt {}; // Bridge: 出口/入口
        WorldPoint pinch_pt {};             // Pinch:  收缩孔
    };

    std::vector<PortalEdge> edges;
    edges.reserve(triangles.size() - 1);
    std::vector<std::pair<WorldPoint, WorldPoint>> bridge_pairs; // (exit, entry) for break reconstruction

    for (size_t idx = 1; idx < triangles.size(); ++idx) {
        const uint32_t tri_a = triangles[idx - 1];
        const uint32_t tri_b = triangles[idx];
        const BaseNavTriangle& ta = pack_triangles[tri_a];

        // CCW 绕序:面积负数则 v1/v2 互换
        std::array<uint32_t, 3> va = ta.vertices;
        {
            const WorldPoint p0 { pack_vertices[va[0]].u, pack_vertices[va[0]].v };
            const WorldPoint p1 { pack_vertices[va[1]].u, pack_vertices[va[1]].v };
            const WorldPoint p2 { pack_vertices[va[2]].u, pack_vertices[va[2]].v };
            if (FunnelTriArea2(p0, p1, p2) < 0.0) {
                std::swap(va[1], va[2]);
            }
        }

        // 与 tri_b 的共享顶点
        const BaseNavTriangle& tb = pack_triangles[tri_b];
        std::array<uint32_t, 2> shared {};
        int shared_count = 0;
        for (uint32_t vi : va) {
            for (uint32_t vj : tb.vertices) {
                if (vi == vj && shared_count < 2) {
                    shared[shared_count++] = vi;
                }
            }
        }

        if (shared_count == 2) {
            // 找 CCW 有向出边 u→v: (pos[u]+1)%3 == pos[v]
            int pos0 = 0, pos1 = 0;
            for (int k = 0; k < 3; ++k) {
                if (va[k] == shared[0]) {
                    pos0 = k;
                }
                if (va[k] == shared[1]) {
                    pos1 = k;
                }
            }
            PortalEdge e;
            e.kind = PortalEdge::Kind::Portal;
            if ((pos0 + 1) % 3 == pos1) {
                e.u_idx = shared[0];
                e.v_idx = shared[1];
            }
            else {
                e.u_idx = shared[1];
                e.v_idx = shared[0];
            }
            edges.push_back(e);
        }
        else {
            const auto bridge = closestEdgeBridgePoints(tri_a, tri_b);
            if (bridge) {
                PortalEdge e;
                e.kind = PortalEdge::Kind::Bridge;
                e.exit_pt = (*bridge)[0];
                e.entry_pt = (*bridge)[1];
                edges.push_back(e);
                if (!WptEqual(e.exit_pt, e.entry_pt)) {
                    bridge_pairs.push_back({ e.exit_pt, e.entry_pt });
                }
            }
            else {
                if (const auto m = sharedEdgeMidpoint(tri_a, tri_b)) {
                    PortalEdge e;
                    e.kind = PortalEdge::Kind::Pinch;
                    e.pinch_pt = *m;
                    edges.push_back(e);
                }
                // m==nullopt: 完全退化，跳过(与 Python 对齐:不追加 portal)
            }
        }
    }

    // 试双向握手;取在网格上且路径总长更短的(正确握手 = 走廊内最短 = 路径最短)
    using FunnelResult = std::pair<std::vector<WorldPoint>, std::vector<size_t>>;
    std::optional<FunnelResult> best;
    double best_len = std::numeric_limits<double>::max();

    for (int swap = 0; swap <= 1; ++swap) {
        // 构造 portal 列表
        std::vector<std::pair<WorldPoint, WorldPoint>> portals;
        portals.reserve(edges.size() + 2);
        portals.push_back({ start, start });

        for (const auto& e : edges) {
            if (e.kind == PortalEdge::Kind::Portal) {
                const WorldPoint pu { pack_vertices[e.u_idx].u, pack_vertices[e.u_idx].v };
                const WorldPoint pv { pack_vertices[e.v_idx].u, pack_vertices[e.v_idx].v };
                portals.push_back(swap == 0 ? std::make_pair(pu, pv) : std::make_pair(pv, pu));
            }
            else if (e.kind == PortalEdge::Kind::Bridge) {
                portals.push_back({ e.exit_pt, e.exit_pt });
            }
            else {
                portals.push_back({ e.pinch_pt, e.pinch_pt });
            }
        }
        portals.push_back({ goal, goal });

        // 运行 SSF
        auto raw_pts = SsfFunnel(portals);

        // 去重相邻重复点
        std::vector<WorldPoint> clean;
        clean.reserve(raw_pts.size());
        clean.push_back(raw_pts[0]);
        for (size_t k = 1; k < raw_pts.size(); ++k) {
            if (!WptEqual(raw_pts[k], clean.back())) {
                clean.push_back(raw_pts[k]);
            }
        }

        // 重建 bridge 分段断点
        std::vector<WorldPoint> route_pts;
        std::vector<size_t> route_brk;
        route_pts.reserve(clean.size() + bridge_pairs.size());
        for (const auto& pt : clean) {
            route_pts.push_back(pt);
            for (const auto& [ex, en] : bridge_pairs) {
                if (WptEqual(pt, ex)) {
                    route_brk.push_back(route_pts.size());
                    route_pts.push_back(en);
                    break;
                }
            }
        }

        // 在网格验证:非 bridge 段按步长 1.0 采样
        const auto is_break = [&](size_t k) {
            return std::find(route_brk.begin(), route_brk.end(), k) != route_brk.end();
        };
        bool valid = true;
        for (size_t k = 0; k + 1 < route_pts.size() && valid; ++k) {
            if (is_break(k + 1)) {
                continue;
            }
            const WorldPoint& a = route_pts[k];
            const WorldPoint& b = route_pts[k + 1];
            const double seg_len = detail::Distance(a, b);
            const int steps = std::max(1, static_cast<int>(seg_len / 1.0));
            for (int j = 0; j <= steps && valid; ++j) {
                const double t = static_cast<double>(j) / steps;
                const WorldPoint pt { a.x + (b.x - a.x) * t, a.y + (b.y - a.y) * t };
                if (!pointOnMesh(zone_id, pt)) {
                    valid = false;
                }
            }
        }
        if (!valid) {
            continue;
        }

        // 计算路径总长度(不含 bridge 跳段;更短 = 更直 = 正确握手方向)
        double total_len = 0.0;
        for (size_t k = 0; k + 1 < route_pts.size(); ++k) {
            if (!is_break(k + 1)) {
                total_len += detail::Distance(route_pts[k], route_pts[k + 1]);
            }
        }

        if (total_len < best_len) {
            best_len = total_len;
            best = FunnelResult { std::move(route_pts), std::move(route_brk) };
        }
    }

    return best;
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
        const double base_cost = detail::Distance(lhs_center, *midpoint) + detail::Distance(*midpoint, rhs_center);
        return boundaryAwareTransitionCost(lhs, rhs, base_cost);
    }
    const auto bridge_points = closestEdgeBridgePoints(lhs, rhs);
    const double height_delta = std::abs(triangleAverageHeight(lhs) - triangleAverageHeight(rhs));
    if (height_delta > kBridgeMaxHeightDelta) {
        return std::numeric_limits<double>::infinity();
    }
    if (!bridge_points) {
        const double base_cost =
            detail::TriangleHeuristic(triangles[lhs], triangles[rhs]) + kBridgeFixedCost + height_delta * kBridgeHeightCostFactor;
        return boundaryAwareTransitionCost(lhs, rhs, base_cost);
    }
    const double gap = detail::Distance((*bridge_points)[0], (*bridge_points)[1]);
    const double base_cost = detail::Distance(lhs_center, (*bridge_points)[0]) + gap + detail::Distance((*bridge_points)[1], rhs_center)
                             + kBridgeFixedCost + gap * kBridgeGapCostFactor + height_delta * kBridgeHeightCostFactor;
    return boundaryAwareTransitionCost(lhs, rhs, base_cost);
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
    // 走廊单 zone,取首个三角形的 zone。
    const uint16_t zone_id = triangles.empty() ? 0 : triangle_zones_[triangles.front()];

    // SSF 漏斗:直接在三角形走廊内求最短折线,替代中点+thin 拉直.
    // 内部试双向握手、取在网格上且路径更短(更直)的那个;两向均离网格才回退到 thin.
    if (auto funnel = funnelRoutePoints(triangles, start, goal, zone_id); funnel) {
        const detail::SegmentWalkableFn validator = [this, zone_id](const WorldPoint& a, const WorldPoint& b) {
            return segmentHeightWalkable(zone_id, a, b);
        };
        const detail::PointOnMeshFn on_mesh = [this, zone_id](const WorldPoint& p) {
            return pointOnMesh(zone_id, p);
        };
        const detail::GroundHeightFn ground = [this, zone_id](const WorldPoint& p) {
            uint32_t triangle = 0;
            return groundHeightNearIndexed(zone_id, p, std::nullopt, triangle);
        };
        auto route = detail::PostProcessRoutePoints(funnel->first, funnel->second, validator, on_mesh, ground);
        segment_breaks = std::move(route.segment_breaks);
        return std::move(route.points);
    }

    // 回退:传统中点路径 + PostProcessRoutePoints(含 thin 拉直)
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

    // LOS 拉直的可行性判据:按地面高度连续性判定捷径(改用高度连续性,因逐三角行进在共面重叠缝处误判
    // 不可走、使路线贴墙)。
    const detail::SegmentWalkableFn validator = [this, zone_id](const WorldPoint& a, const WorldPoint& b) {
        return segmentHeightWalkable(zone_id, a, b);
    };
    // 点包含判据:居中据此还原走廊真实宽度,避免逐三角行进在重叠网格上低估横向余量。
    const detail::PointOnMeshFn on_mesh = [this, zone_id](const WorldPoint& p) {
        return pointOnMesh(zone_id, p);
    };
    // 地面高度 oracle:离水让边据此分辨"危险水/坎边"(紧边外侧地面骤降或离开网格)与"无害墙边"。
    const detail::GroundHeightFn ground = [this, zone_id](const WorldPoint& p) {
        uint32_t triangle = 0;
        return groundHeightNearIndexed(zone_id, p, std::nullopt, triangle);
    };
    auto route = detail::PostProcessRoutePoints(points, raw_segment_breaks, validator, on_mesh, ground);
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

} // namespace navmesh
