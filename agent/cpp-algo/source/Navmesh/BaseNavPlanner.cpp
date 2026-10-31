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
constexpr double kRoutePullSampleStep = 0.5; // 拉直判据沿捷径采样的步长 px

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
constexpr double kIndexBinSize = 4.0;          // 空间分箱的格边长 px

constexpr double kSegmentWalkSnapRadius = 1.0; // 点查询取候选三角的邻域半径 px

}

BaseNavPlanner::BaseNavPlanner(const BaseNavPack& pack)
    : pack_(pack)
    , triangle_zones_(pack.triangles().size(), 0)
    , neighbor_link_bits_(pack.triangles().size(), 0)
{
    for (const auto& zone : pack_.zones()) {
        const uint32_t end = zone.first_triangle + zone.triangle_count;
        for (uint32_t index = zone.first_triangle; index < end && index < triangle_zones_.size(); ++index) {
            triangle_zones_[index] = zone.zone_id;
        }
    }
    // 两段只写各自的成员且不读对方产物, 所以可以并行
    if (NavWorkerCount(static_cast<int64_t>(pack_.triangles().size())) <= 1) {
        buildIndex();
        buildSpatialIndex();
        return;
    }
    // buildIndex 抛出时 jthread 析构会自动 join
    std::jthread bins([this] { buildSpatialIndex(); });
    buildIndex();
    bins.join();
}

void BaseNavPlanner::buildIndex()
{
    buildNaturalComponents();

    // 按 uint64_t 字切块并行, 相邻块不共用一个字
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

    const auto& triangles = pack_.triangles();
    for (size_t index = 0; index < links.size(); ++index) {
        if (((passed[index >> 6] >> (index & 63)) & 1) == 0) {
            continue;
        }
        const BaseNavLink& link = links[index];
        const auto& neighbors = triangles[link.source].neighbors;
        bool slotted = false;
        for (size_t slot = 0; slot < neighbors.size(); ++slot) {
            if (neighbors[slot] >= 0 && static_cast<uint32_t>(neighbors[slot]) == link.target) {
                neighbor_link_bits_[link.source] |= static_cast<uint8_t>(1U << slot);
                slotted = true;
            }
        }
        if (!slotted) {
            bridge_links_.push_back((static_cast<uint64_t>(link.source) << 32U) | link.target);
        }
    }
    std::sort(bridge_links_.begin(), bridge_links_.end());
    bridge_links_.shrink_to_fit();
}

bool BaseNavPlanner::hasLink(uint32_t source, uint32_t target) const
{
    if (source >= neighbor_link_bits_.size()) {
        return false;
    }
    const auto& neighbors = pack_.triangles()[source].neighbors;
    for (size_t slot = 0; slot < neighbors.size(); ++slot) {
        if (neighbors[slot] >= 0 && static_cast<uint32_t>(neighbors[slot]) == target) {
            return ((neighbor_link_bits_[source] >> slot) & 1U) != 0;
        }
    }
    return std::binary_search(bridge_links_.begin(), bridge_links_.end(), (static_cast<uint64_t>(source) << 32U) | target);
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

    struct BinRect
    {
        int32_t x0, x1, y0, y1;
    };

    const auto bin_rect = [this](uint32_t triangle_index) -> BinRect {
        const auto points = trianglePoints(triangle_index);
        const double left = std::min({ points[0].x, points[1].x, points[2].x });
        const double right = std::max({ points[0].x, points[1].x, points[2].x });
        const double top = std::min({ points[0].y, points[1].y, points[2].y });
        const double bottom = std::max({ points[0].y, points[1].y, points[2].y });
        return { static_cast<int32_t>(std::floor(left / kIndexBinSize)),
                 static_cast<int32_t>(std::floor(right / kIndexBinSize)),
                 static_cast<int32_t>(std::floor(top / kIndexBinSize)),
                 static_cast<int32_t>(std::floor(bottom / kIndexBinSize)) };
    };
    bin_grids_.clear();
    std::vector<int32_t> zone_slot(size_t { 1 } << 16, -1);
    for (const BaseNavZone& zone : pack_.zones()) {
        if (zone.zone_id != 0 && zone.triangle_count != 0 && zone_slot[zone.zone_id] < 0) {
            zone_slot[zone.zone_id] = static_cast<int32_t>(bin_grids_.size());
            bin_grids_.push_back(BinGrid { .zone_id = zone.zone_id });
        }
    }
    const size_t nslots = bin_grids_.size();
    const int64_t n = static_cast<int64_t>(triangles.size());
    const size_t workers = NavWorkerCount(n);
    // 块序即三角序, 所以格内三角升序且与线程数无关
    const auto for_each_tri = [&](auto&& fn) {
        ParallelChunks(n, workers, [&](size_t w, int64_t begin, int64_t end) {
            for (int64_t t = begin; t < end; ++t) {
                const uint16_t zone_id = static_cast<size_t>(t) < triangle_zones_.size() ? triangle_zones_[static_cast<size_t>(t)] : 0;
                const int32_t slot = zone_slot[zone_id];
                if (slot < 0) {
                    continue;
                }
                fn(w, static_cast<size_t>(slot), bin_rect(static_cast<uint32_t>(t)), static_cast<uint32_t>(t));
            }
        });
    };
    std::vector<std::vector<BinRect>> bounds(
        workers,
        std::vector<BinRect>(
            nslots,
            { std::numeric_limits<int32_t>::max(),
              std::numeric_limits<int32_t>::min(),
              std::numeric_limits<int32_t>::max(),
              std::numeric_limits<int32_t>::min() }));
    for_each_tri([&bounds](size_t w, size_t slot, const BinRect& r, uint32_t) {
        BinRect& b = bounds[w][slot];
        b.x0 = std::min(b.x0, r.x0);
        b.x1 = std::max(b.x1, r.x1);
        b.y0 = std::min(b.y0, r.y0);
        b.y1 = std::max(b.y1, r.y1);
    });
    for (size_t slot = 0; slot < nslots; ++slot) {
        BinRect b = bounds[0][slot];
        for (size_t w = 1; w < workers; ++w) {
            b.x0 = std::min(b.x0, bounds[w][slot].x0);
            b.x1 = std::max(b.x1, bounds[w][slot].x1);
            b.y0 = std::min(b.y0, bounds[w][slot].y0);
            b.y1 = std::max(b.y1, bounds[w][slot].y1);
        }
        BinGrid& grid = bin_grids_[slot];
        if (b.x0 > b.x1) {
            continue;
        }
        grid.bx0 = b.x0;
        grid.by0 = b.y0;
        grid.nx = b.x1 - b.x0 + 1;
        grid.ny = b.y1 - b.y0 + 1;
        grid.offsets.assign(static_cast<size_t>(grid.nx) * static_cast<size_t>(grid.ny) + 1, 0);
    }
    const auto cell = [](const BinGrid& grid, int32_t bin_x, int32_t bin_y) -> size_t {
        return static_cast<size_t>(bin_x - grid.bx0) * static_cast<size_t>(grid.ny) + static_cast<size_t>(bin_y - grid.by0);
    };
    // counts[w][slot][c] 先是第 w 块落进格 c 的三角数, 前缀和后变成写入游标
    std::vector<std::vector<std::vector<uint32_t>>> counts(workers, std::vector<std::vector<uint32_t>>(nslots));
    for (size_t w = 0; w < workers; ++w) {
        for (size_t slot = 0; slot < nslots; ++slot) {
            counts[w][slot].assign(bin_grids_[slot].offsets.empty() ? 0 : bin_grids_[slot].offsets.size() - 1, 0);
        }
    }
    for_each_tri([&](size_t w, size_t slot, const BinRect& r, uint32_t) {
        const BinGrid& grid = bin_grids_[slot];
        std::vector<uint32_t>& cnt = counts[w][slot];
        for (int32_t bin_x = r.x0; bin_x <= r.x1; ++bin_x) {
            for (int32_t bin_y = r.y0; bin_y <= r.y1; ++bin_y) {
                ++cnt[cell(grid, bin_x, bin_y)];
            }
        }
    });
    for (size_t slot = 0; slot < nslots; ++slot) {
        BinGrid& grid = bin_grids_[slot];
        uint32_t running = 0;
        for (size_t c = 0; c + 1 < grid.offsets.size(); ++c) {
            grid.offsets[c] = running;
            for (size_t w = 0; w < workers; ++w) {
                const uint32_t here = counts[w][slot][c];
                counts[w][slot][c] = running;
                running += here;
            }
        }
        if (!grid.offsets.empty()) {
            grid.offsets.back() = running;
        }
        grid.triangles.assign(running, 0);
    }
    for_each_tri([&](size_t w, size_t slot, const BinRect& r, uint32_t triangle_index) {
        BinGrid& grid = bin_grids_[slot];
        std::vector<uint32_t>& next = counts[w][slot];
        for (int32_t bin_x = r.x0; bin_x <= r.x1; ++bin_x) {
            for (int32_t bin_y = r.y0; bin_y <= r.y1; ++bin_y) {
                grid.triangles[next[cell(grid, bin_x, bin_y)]++] = triangle_index;
            }
        }
    });
}

std::vector<uint32_t> BaseNavPlanner::candidateTriangles(uint16_t zone_id, const WorldPoint& point, double radius) const
{
    std::vector<uint32_t> result;
    const double query_radius = std::max(0.0, radius);
    const int32_t bin_x0 = static_cast<int32_t>(std::floor((point.x - query_radius) / kIndexBinSize));
    const int32_t bin_x1 = static_cast<int32_t>(std::floor((point.x + query_radius) / kIndexBinSize));
    const int32_t bin_y0 = static_cast<int32_t>(std::floor((point.y - query_radius) / kIndexBinSize));
    const int32_t bin_y1 = static_cast<int32_t>(std::floor((point.y + query_radius) / kIndexBinSize));
    const BinGrid* grid = nullptr;
    for (const BinGrid& g : bin_grids_) {
        if (g.zone_id == zone_id) {
            grid = &g;
            break;
        }
    }
    if (grid == nullptr || grid->offsets.empty()) {
        return result;
    }
    for (int32_t bin_x = std::max(bin_x0, grid->bx0); bin_x <= std::min(bin_x1, grid->bx0 + grid->nx - 1); ++bin_x) {
        for (int32_t bin_y = std::max(bin_y0, grid->by0); bin_y <= std::min(bin_y1, grid->by0 + grid->ny - 1); ++bin_y) {
            const size_t c =
                static_cast<size_t>(bin_x - grid->bx0) * static_cast<size_t>(grid->ny) + static_cast<size_t>(bin_y - grid->by0);
            result.insert(result.end(), grid->triangles.begin() + grid->offsets[c], grid->triangles.begin() + grid->offsets[c + 1]);
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
        const double height = triangleHeight(triangle_index);
        if (!best) {
            best = height;
            out_triangle = triangle_index;
        }
        else if (!reference) {
            if (height < *best) {
                best = height;
                out_triangle = triangle_index;
            }
        }
        else if (std::abs(height - *reference) < std::abs(*best - *reference)) {
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
    uint32_t cached = kInvalidTriangle;
    for (int index = 0; index <= samples; ++index) {
        const double t = static_cast<double>(index) / samples;
        const WorldPoint point { .x = a.x + (b.x - a.x) * t, .y = a.y + (b.y - a.y) * t };
        if (previous && cached != kInvalidTriangle) {
            if (detail::PointInTriangle(point, trianglePoints(cached))) {
                continue;
            }
        }
        const std::optional<double> height = groundHeightNearIndexed(zone_id, point, previous, cached);
        if (!height) {
            return false;
        }
        if (previous && std::abs(*height - *previous) > kBridgeMaxHeightDelta) {
            return false;
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

    const double query_radius = std::max(0.0, radius);
    std::vector<uint32_t> candidates = candidateTriangles(zone_id, point, query_radius);
    if (candidates.empty() && query_radius < kIndexBinSize) {
        // 放宽只为取候选, 命中仍受 radius 剔除
        candidates = candidateTriangles(zone_id, point, kIndexBinSize);
    }

    // 小碎片常摞在真实路面上方, 吸上去会让 A* 误报不可达, 所以排在后面
    const auto is_small_island = [&](uint32_t triangle_index) {
        return natural_component_sizes_[natural_component_ids_[triangle_index]] <= kSmallBridgeComponentMaxTriangles;
    };

    if (floor_y > kBaseNavFloorYValidMin) {
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
            const double delta = std::abs(triangleHeight(triangle_index) - static_cast<double>(floor_y));
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
        // 同一 (u,v) 摞着几层时距离都为 0, 取最上层, 俯视底图看见的就是它
        const std::tuple<int, double, double, uint32_t> key { is_small_island(triangle_index) ? 1 : 0,
                                                              distance,
                                                              -triangleHeight(triangle_index),
                                                              triangle_index };
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
    // 两侧边线两端各内缩一个半宽, 端点常贴墙, 不内缩会否掉走廊里所有捷径
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
    const auto& triangle = pack_.triangles()[triangle_index];
    const auto& vertices = pack_.vertices();
    return (static_cast<double>(vertices[triangle.vertices[0]].height) + static_cast<double>(vertices[triangle.vertices[1]].height)
            + static_cast<double>(vertices[triangle.vertices[2]].height))
           / 3.0;
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
    case BaseNavRouteStatus::NoGo:
        return "no_go";
    }
    return "unknown";
}

} // namespace navmesh
