#include <algorithm>
#include <array>
#include <cmath>
#include <cstddef>
#include <cstdint>
#include <limits>
#include <numeric>
#include <thread>
#include <tuple>

#include "BaseNavGeometry.h"
#include "BaseNavPlanner.h"
#include "NavParallel.h"

namespace navmesh
{

namespace
{

constexpr double kBridgeMaxHeightDelta = 3.0;
constexpr uint32_t kSmallBridgeComponentMaxTriangles = 512;
constexpr double kSmallBridgeMaxGap = 4.0;
constexpr double kRoutePullSampleStep = 0.5; // 拉直判据沿捷径采样的步长(像素),与 Python ROUTE_PULL_SAMPLE_STEP 对齐

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

constexpr double kSegmentWalkSnapRadius = 1.0; // 点查询(pointOnMesh/groundHeightNearIndexed)取候选三角形的邻域半径(px)

}

BaseNavPlanner::BaseNavPlanner(const BaseNavPack& pack)
    : pack_(pack)
    , triangle_zones_(pack.triangles().size(), 0)
    , adjacency_offsets_(pack.triangles().size() + 1, 0)
    , triangle_heights_(pack.triangles().size(), 0.0)
{
    for (const auto& zone : pack_.zones()) {
        const uint32_t end = zone.first_triangle + zone.triangle_count;
        for (uint32_t index = zone.first_triangle; index < end && index < triangle_zones_.size(); ++index) {
            triangle_zones_[index] = zone.zone_id;
        }
    }
    // 三段各写各的成员(连通分量与邻接表 / 空间桶 / 三角高度), 读的只有 pack_ 与刚填好的分区表,
    // 谁都看不见另外两段的产物, 于是并起来跑与顺着跑逐位相同。
    if (NavWorkerCount(static_cast<int64_t>(pack_.triangles().size())) <= 1) {
        buildIndex();
        buildSpatialIndex();
        computeTriangleHeights();
        return;
    }
    // jthread: buildIndex 抛出时(分配失败是唯一的来路)这两个还在跑, 析构自己接回来。
    std::jthread bins([this] { buildSpatialIndex(); });
    std::jthread heights([this] { computeTriangleHeights(); });
    buildIndex();
    bins.join();
    heights.join();
}

void BaseNavPlanner::buildIndex()
{
    buildNaturalComponents();

    // 位图按每条边一比特存下通行判据, 换掉填充趟的整串重算: 两趟之间只做了前缀和与 resize,
    // 谓词读的分区表、自然连通分量与三角几何一个字都没写, 第二趟必然复现第一趟的答案。
    // 按字切块并行, 相邻块碰不到同一个 uint64_t, 于是位图与线程数无关。
    const auto& links = pack_.links();
    const size_t word_count = links.size() / 64 + 1;
    std::vector<uint64_t> passed(word_count, 0);
    ParallelChunks(
        static_cast<int64_t>(word_count),
        NavWorkerCount(static_cast<int64_t>(links.size())),
        [&](size_t, int64_t begin, int64_t end) {
            for (int64_t word = begin; word < end; ++word) {
                const size_t first = static_cast<size_t>(word) << 6;
                const size_t last = std::min(first + 64, links.size());
                uint64_t bits = 0;
                for (size_t index = first; index < last; ++index) {
                    const BaseNavLink& link = links[index];
                    if (isTraversableLink(link.source, link.target)) {
                        bits |= uint64_t { 1 } << (index & 63);
                    }
                }
                passed[static_cast<size_t>(word)] = bits;
            }
        });

    size_t valid_link_count = 0;
    for (size_t index = 0; index < links.size(); ++index) {
        if (((passed[index >> 6] >> (index & 63)) & 1) != 0) {
            ++adjacency_offsets_[links[index].source + 1];
            ++valid_link_count;
        }
    }
    for (size_t index = 1; index < adjacency_offsets_.size(); ++index) {
        adjacency_offsets_[index] += adjacency_offsets_[index - 1];
    }

    adjacency_links_.resize(valid_link_count);
    std::vector<uint32_t> next_offsets = adjacency_offsets_;
    for (size_t index = 0; index < links.size(); ++index) {
        if (((passed[index >> 6] >> (index & 63)) & 1) != 0) {
            const BaseNavLink& link = links[index];
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

bool BaseNavPlanner::segmentHeightWalkable(uint16_t zone_id, const WorldPoint& a, const WorldPoint& b, std::optional<double> seed_height)
    const
{
    if (pack_.findZone(zone_id) == nullptr) {
        return false;
    }
    const double length = std::hypot(b.x - a.x, b.y - a.y);
    const int samples = std::max(1, static_cast<int>(length / kRoutePullSampleStep));
    std::optional<double> previous = seed_height;
    // 缓存上一采样点命中的三角形:相邻采样多落在同一三角形内,命中则复用其高度,省去 candidateTriangles
    // 扫描,且结果与完整扫描等价。
    uint32_t cached = kInvalidTriangle;
    for (int index = 0; index <= samples; ++index) {
        const double t = static_cast<double>(index) / samples;
        const WorldPoint point { .x = a.x + (b.x - a.x) * t, .y = a.y + (b.y - a.y) * t };
        if (previous && cached != kInvalidTriangle) {
            if (detail::PointInTriangle(point, trianglePoints(cached))) {
                continue; // 命中缓存:高度等于 previous、三角形未变,直接进入下一采样点
            }
        }
        const std::optional<double> height = groundHeightNearIndexed(zone_id, point, previous, cached);
        if (!height) {
            return false; // 采样点离开网格,判定捷径不可走
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
        // out. The zone + bbox culls match the floor-blind path below, so both see the same candidates.
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

    // Floor-blind path: rank by (non-island first, then snap distance, then highest surface, then smallest
    // index). Containing surfaces (distance 0) still win; it only diverges to skip a micro-component when a
    // real surface competes, and to pick the top layer when several floors stack on the same (u,v).
    std::optional<std::tuple<int, double, double, uint32_t>> best_key;
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
        // 距离打平只会发生在同一 (u,v) 上摞着好几层地面时(都含点 ⇒ 都是 0)。此时按高度降序:
        // 底图像素是俯视图上的一点,那点看得见的就是最上面那层。原来的「取最小三角号」在这里
        // 是任意的 —— 重烘一次三角顺序一换,起点就可能吸到桥下/水下那层,与终点分属两个分量,
        // A* 直接报不连通。非打平的候选不受影响,distance 仍排在高度前面。
        const std::tuple<int, double, double, uint32_t> key {
            is_small_island(triangle_index) ? 1 : 0, distance, -triangle_heights_[triangle_index], triangle_index
        };
        if (!best_key || key < *best_key) {
            best_key = key;
            best = BaseNavSnapResult { .triangle = triangle_index, .point = snapped, .distance = distance };
        }
    }
    return best;
}

bool BaseNavPlanner::isRouteSegmentDrivable(
    uint16_t zone_id,
    const WorldPoint& a,
    const WorldPoint& b,
    double half_width,
    std::optional<double> seed_height) const
{
    if (!segmentHeightWalkable(zone_id, a, b, seed_height)) {
        return false;
    }
    const double dx = b.x - a.x;
    const double dy = b.y - a.y;
    const double length = std::hypot(dx, dy);
    if (half_width <= 0.0 || length <= std::numeric_limits<double>::epsilon()) {
        return true;
    }
    // Walk both flanks of the same leg. They are pulled in by one half-width at each end so the test never
    // asks about the ground beside the anchors themselves — those sit wherever the route already put them,
    // frequently hard against a wall, and would veto every shortcut in a corridor.
    const double ux = dx / length;
    const double uy = dy / length;
    const double inset = std::min(half_width, length * 0.5);
    const WorldPoint head { .x = a.x + ux * inset, .y = a.y + uy * inset };
    const WorldPoint tail { .x = b.x - ux * inset, .y = b.y - uy * inset };
    for (const double side : { 1.0, -1.0 }) {
        const double ox = -uy * half_width * side;
        const double oy = ux * half_width * side;
        if (!segmentHeightWalkable(zone_id, { .x = head.x + ox, .y = head.y + oy }, { .x = tail.x + ox, .y = tail.y + oy }, seed_height)) {
            return false;
        }
    }
    return true;
}

bool BaseNavPlanner::isSmallIslandTriangle(uint32_t triangle_index) const
{
    return natural_component_sizes_[natural_component_ids_[triangle_index]] <= kSmallBridgeComponentMaxTriangles;
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

double BaseNavPlanner::triangleHeight(uint32_t triangle_index) const
{
    return triangle_heights_[triangle_index];
}

uint32_t BaseNavPlanner::componentId(uint32_t triangle_index) const
{
    return triangle_index < natural_component_ids_.size() ? natural_component_ids_[triangle_index] : std::numeric_limits<uint32_t>::max();
}

uint32_t BaseNavPlanner::componentSize(uint32_t triangle_index) const
{
    const uint32_t id = componentId(triangle_index);
    return id < natural_component_sizes_.size() ? natural_component_sizes_[id] : 0;
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

const char* ToString(BaseNavRouteStatus status)
{
    switch (status) {
    case BaseNavRouteStatus::Success:
        return "success";
    case BaseNavRouteStatus::ZoneNotFound:
        return "zone_not_found";
    case BaseNavRouteStatus::Unreachable:
        return "unreachable";
    }
    return "unknown";
}

} // namespace navmesh
