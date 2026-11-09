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
};

DisjointSet make_disjoint_set(size_t count)
{
    DisjointSet set { .parent = std::vector<uint32_t>(count, 0) };
    std::iota(set.parent.begin(), set.parent.end(), 0U);
    return set;
}

uint32_t disjoint_find(DisjointSet* set, uint32_t value)
{
    while (set->parent[value] != value) {
        set->parent[value] = set->parent[set->parent[value]];
        value = set->parent[value];
    }
    return value;
}

void disjoint_unite(DisjointSet* set, uint32_t lhs, uint32_t rhs)
{
    const uint32_t lhs_root = disjoint_find(set, lhs);
    const uint32_t rhs_root = disjoint_find(set, rhs);
    if (lhs_root != rhs_root) {
        set->parent[rhs_root] = lhs_root;
    }
}

constexpr uint32_t kInvalidTriangle = std::numeric_limits<uint32_t>::max();
constexpr double kIndexBinSize = 4.0;          // 空间分箱的格边长 px

constexpr double kSegmentWalkSnapRadius = 1.0; // 点查询取候选三角的邻域半径 px

bool is_natural_neighbor(const BaseNavPack& pack, uint32_t lhs, uint32_t rhs)
{
    for (int32_t neighbor : pack.triangles[lhs].neighbors) {
        if (neighbor >= 0 && static_cast<uint32_t>(neighbor) == rhs) {
            return true;
        }
    }
    return false;
}

bool is_traversable_link(const BaseNavPlanner& planner, const BaseNavPack& pack, uint32_t lhs, uint32_t rhs)
{
    if (lhs >= planner.triangle_zones.size() || rhs >= planner.triangle_zones.size() || planner.triangle_zones[lhs] == 0
        || planner.triangle_zones[lhs] != planner.triangle_zones[rhs]) {
        return false;
    }
    if (is_natural_neighbor(pack, lhs, rhs)) {
        return true;
    }

    const uint32_t lhs_component = planner.natural_component_ids[lhs];
    const uint32_t rhs_component = planner.natural_component_ids[rhs];
    const uint32_t min_component_size =
        std::min(planner.natural_component_sizes[lhs_component], planner.natural_component_sizes[rhs_component]);
    if (min_component_size > kSmallBridgeComponentMaxTriangles) {
        return true;
    }

    const auto bridge_points = ClosestEdgeBridgePoints(pack, lhs, rhs);
    return bridge_points && detail::Distance((*bridge_points)[0], (*bridge_points)[1]) <= kSmallBridgeMaxGap;
}

void build_natural_components(BaseNavPlanner* planner, const BaseNavPack& pack)
{
    const auto& triangles = pack.triangles;
    DisjointSet components = make_disjoint_set(triangles.size());
    for (uint32_t triangle_index = 0; triangle_index < triangles.size(); ++triangle_index) {
        for (int32_t neighbor : triangles[triangle_index].neighbors) {
            if (neighbor < 0) {
                continue;
            }
            const uint32_t next = static_cast<uint32_t>(neighbor);
            if (next < triangles.size() && planner->triangle_zones[next] == planner->triangle_zones[triangle_index]) {
                disjoint_unite(&components, triangle_index, next);
            }
        }
    }

    constexpr uint32_t kInvalidComponent = std::numeric_limits<uint32_t>::max();
    std::vector<uint32_t> root_to_component(triangles.size(), kInvalidComponent);
    planner->natural_component_ids.assign(triangles.size(), kInvalidComponent);
    planner->natural_component_sizes.clear();
    for (uint32_t triangle_index = 0; triangle_index < triangles.size(); ++triangle_index) {
        const uint32_t root = disjoint_find(&components, triangle_index);
        uint32_t& component_id = root_to_component[root];
        if (component_id == kInvalidComponent) {
            component_id = static_cast<uint32_t>(planner->natural_component_sizes.size());
            planner->natural_component_sizes.push_back(0);
        }
        planner->natural_component_ids[triangle_index] = component_id;
        ++planner->natural_component_sizes[component_id];
    }
}

void build_index(BaseNavPlanner* planner, const BaseNavPack& pack, const std::vector<BaseNavLink>& links)
{
    build_natural_components(planner, pack);

    // 按 uint64_t 字切块并行, 相邻块不共用一个字
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
                    if (is_traversable_link(*planner, pack, link.source, link.target)) {
                        bits |= uint64_t { 1 } << (index & 63);
                    }
                }
                passed[static_cast<size_t>(word)] = bits;
            }
        });

    const auto& triangles = pack.triangles;
    for (size_t index = 0; index < links.size(); ++index) {
        if (((passed[index >> 6] >> (index & 63)) & 1) == 0) {
            continue;
        }
        const BaseNavLink& link = links[index];
        const auto& neighbors = triangles[link.source].neighbors;
        bool slotted = false;
        for (size_t slot = 0; slot < neighbors.size(); ++slot) {
            if (neighbors[slot] >= 0 && static_cast<uint32_t>(neighbors[slot]) == link.target) {
                planner->neighbor_link_bits[link.source] |= static_cast<uint8_t>(1U << slot);
                slotted = true;
            }
        }
        if (!slotted) {
            planner->bridge_links.push_back((static_cast<uint64_t>(link.source) << 32U) | link.target);
        }
    }
    std::sort(planner->bridge_links.begin(), planner->bridge_links.end());
    planner->bridge_links.shrink_to_fit();
}

void build_spatial_index(std::vector<BaseNavBinGrid>* bin_grids, const std::vector<uint16_t>& triangle_zones, const BaseNavPack& pack)
{
    const auto& triangles = pack.triangles;

    struct BinRect
    {
        int32_t x0, x1, y0, y1;
    };

    const auto bin_rect = [&pack](uint32_t triangle_index) -> BinRect {
        const auto points = TrianglePoints(pack, triangle_index);
        const double left = std::min({ points[0].x, points[1].x, points[2].x });
        const double right = std::max({ points[0].x, points[1].x, points[2].x });
        const double top = std::min({ points[0].y, points[1].y, points[2].y });
        const double bottom = std::max({ points[0].y, points[1].y, points[2].y });
        return { static_cast<int32_t>(std::floor(left / kIndexBinSize)),
                 static_cast<int32_t>(std::floor(right / kIndexBinSize)),
                 static_cast<int32_t>(std::floor(top / kIndexBinSize)),
                 static_cast<int32_t>(std::floor(bottom / kIndexBinSize)) };
    };
    bin_grids->clear();
    std::vector<int32_t> zone_slot(size_t { 1 } << 16, -1);
    for (const BaseNavZone& zone : pack.zones) {
        if (zone.zone_id != 0 && zone.triangle_count != 0 && zone_slot[zone.zone_id] < 0) {
            zone_slot[zone.zone_id] = static_cast<int32_t>(bin_grids->size());
            bin_grids->push_back(BaseNavBinGrid { .zone_id = zone.zone_id });
        }
    }
    const size_t nslots = bin_grids->size();
    const int64_t n = static_cast<int64_t>(triangles.size());
    const size_t workers = NavWorkerCount(n);
    // 块序即三角序, 所以格内三角升序且与线程数无关
    const auto for_each_tri = [&](auto&& fn) {
        ParallelChunks(n, workers, [&](size_t w, int64_t begin, int64_t end) {
            for (int64_t t = begin; t < end; ++t) {
                const uint16_t zone_id = static_cast<size_t>(t) < triangle_zones.size() ? triangle_zones[static_cast<size_t>(t)] : 0;
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
        BaseNavBinGrid& grid = (*bin_grids)[slot];
        if (b.x0 > b.x1) {
            continue;
        }
        grid.bx0 = b.x0;
        grid.by0 = b.y0;
        grid.nx = b.x1 - b.x0 + 1;
        grid.ny = b.y1 - b.y0 + 1;
        grid.offsets.assign(static_cast<size_t>(grid.nx) * static_cast<size_t>(grid.ny) + 1, 0);
    }
    const auto cell = [](const BaseNavBinGrid& grid, int32_t bin_x, int32_t bin_y) -> size_t {
        return static_cast<size_t>(bin_x - grid.bx0) * static_cast<size_t>(grid.ny) + static_cast<size_t>(bin_y - grid.by0);
    };
    // counts[w][slot][c] 先是第 w 块落进格 c 的三角数, 前缀和后变成写入游标
    std::vector<std::vector<std::vector<uint32_t>>> counts(workers, std::vector<std::vector<uint32_t>>(nslots));
    for (size_t w = 0; w < workers; ++w) {
        for (size_t slot = 0; slot < nslots; ++slot) {
            counts[w][slot].assign((*bin_grids)[slot].offsets.empty() ? 0 : (*bin_grids)[slot].offsets.size() - 1, 0);
        }
    }
    for_each_tri([&](size_t w, size_t slot, const BinRect& r, uint32_t) {
        const BaseNavBinGrid& grid = (*bin_grids)[slot];
        std::vector<uint32_t>& cnt = counts[w][slot];
        for (int32_t bin_x = r.x0; bin_x <= r.x1; ++bin_x) {
            for (int32_t bin_y = r.y0; bin_y <= r.y1; ++bin_y) {
                ++cnt[cell(grid, bin_x, bin_y)];
            }
        }
    });
    for (size_t slot = 0; slot < nslots; ++slot) {
        BaseNavBinGrid& grid = (*bin_grids)[slot];
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
        BaseNavBinGrid& grid = (*bin_grids)[slot];
        std::vector<uint32_t>& next = counts[w][slot];
        for (int32_t bin_x = r.x0; bin_x <= r.x1; ++bin_x) {
            for (int32_t bin_y = r.y0; bin_y <= r.y1; ++bin_y) {
                grid.triangles[next[cell(grid, bin_x, bin_y)]++] = triangle_index;
            }
        }
    });
}

// 取包含 point 的三角中高度最接近 reference 的, reference 为空时取最低
std::optional<double> ground_height_near_indexed(
    uint32_t* out_triangle,
    const BaseNavPlanner& planner,
    const BaseNavPack& pack,
    uint16_t zone_id,
    const WorldPoint& point,
    std::optional<double> reference)
{
    std::optional<double> best;
    *out_triangle = kInvalidTriangle;
    for (const uint32_t triangle_index : CandidateTriangles(planner, zone_id, point, kSegmentWalkSnapRadius)) {
        if (planner.triangle_zones[triangle_index] != zone_id) {
            continue;
        }
        if (!detail::PointInTriangle(point, TrianglePoints(pack, triangle_index))) {
            continue;
        }
        const double height = TriangleHeight(pack, triangle_index);
        if (!best) {
            best = height;
            *out_triangle = triangle_index;
        }
        else if (!reference) {
            if (height < *best) {
                best = height;
                *out_triangle = triangle_index;
            }
        }
        else if (std::abs(height - *reference) < std::abs(*best - *reference)) {
            best = height;
            *out_triangle = triangle_index;
        }
    }
    return best;
}

bool segment_height_walkable(
    const BaseNavPlanner& planner,
    const BaseNavPack& pack,
    uint16_t zone_id,
    const WorldPoint& a,
    const WorldPoint& b,
    std::optional<double> seed_height)
{
    if (FindBaseNavZone(pack, zone_id) == nullptr) {
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
            if (detail::PointInTriangle(point, TrianglePoints(pack, cached))) {
                continue;
            }
        }
        const std::optional<double> height = ground_height_near_indexed(&cached, planner, pack, zone_id, point, previous);
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

}

bool HasLink(const BaseNavPlanner& planner, const BaseNavPack& pack, uint32_t source, uint32_t target)
{
    if (source >= planner.neighbor_link_bits.size()) {
        return false;
    }
    const auto& neighbors = pack.triangles[source].neighbors;
    for (size_t slot = 0; slot < neighbors.size(); ++slot) {
        if (neighbors[slot] >= 0 && static_cast<uint32_t>(neighbors[slot]) == target) {
            return ((planner.neighbor_link_bits[source] >> slot) & 1U) != 0;
        }
    }
    return std::binary_search(planner.bridge_links.begin(), planner.bridge_links.end(), (static_cast<uint64_t>(source) << 32U) | target);
}

std::vector<uint32_t> CandidateTriangles(const BaseNavPlanner& planner, uint16_t zone_id, const WorldPoint& point, double radius)
{
    std::vector<uint32_t> result;
    const double query_radius = std::max(0.0, radius);
    const int32_t bin_x0 = static_cast<int32_t>(std::floor((point.x - query_radius) / kIndexBinSize));
    const int32_t bin_x1 = static_cast<int32_t>(std::floor((point.x + query_radius) / kIndexBinSize));
    const int32_t bin_y0 = static_cast<int32_t>(std::floor((point.y - query_radius) / kIndexBinSize));
    const int32_t bin_y1 = static_cast<int32_t>(std::floor((point.y + query_radius) / kIndexBinSize));
    const BaseNavBinGrid* grid = nullptr;
    for (const BaseNavBinGrid& g : planner.bin_grids) {
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

bool PointOnMesh(const BaseNavPlanner& planner, const BaseNavPack& pack, uint16_t zone_id, const WorldPoint& point)
{
    if (FindBaseNavZone(pack, zone_id) == nullptr) {
        return false;
    }
    for (const uint32_t triangle_index : CandidateTriangles(planner, zone_id, point, kSegmentWalkSnapRadius)) {
        if (planner.triangle_zones[triangle_index] != zone_id) {
            continue;
        }
        if (detail::PointInTriangle(point, TrianglePoints(pack, triangle_index))) {
            return true;
        }
    }
    return false;
}

std::optional<BaseNavSnapResult> SnapToMesh(
    const BaseNavPlanner& planner,
    const BaseNavPack& pack,
    uint16_t zone_id,
    const WorldPoint& point,
    double radius,
    float floor_y)
{
    const BaseNavZone* zone = FindBaseNavZone(pack, zone_id);
    if (zone == nullptr) {
        return std::nullopt;
    }

    const double query_radius = std::max(0.0, radius);
    std::vector<uint32_t> candidates = CandidateTriangles(planner, zone_id, point, query_radius);
    if (candidates.empty() && query_radius < kIndexBinSize) {
        // 放宽只为取候选, 命中仍受 radius 剔除
        candidates = CandidateTriangles(planner, zone_id, point, kIndexBinSize);
    }

    // 小碎片常摞在真实路面上方, 吸上去会让 A* 误报不可达, 所以排序时排在后面
    if (floor_y > kBaseNavFloorYValidMin) {
        std::optional<std::tuple<int, int, double, double>> best_key;
        std::optional<BaseNavSnapResult> best_floor;
        for (const uint32_t triangle_index : candidates) {
            if (planner.triangle_zones[triangle_index] != zone_id) {
                continue;
            }
            const auto points = TrianglePoints(pack, triangle_index);
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
            const double delta = std::abs(TriangleHeight(pack, triangle_index) - static_cast<double>(floor_y));
            const std::tuple<int, int, double, double> key { delta <= static_cast<double>(kBaseNavFloorBand) ? 0 : 1,
                                                             IsSmallIslandTriangle(planner, triangle_index) ? 1 : 0,
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
        if (planner.triangle_zones[triangle_index] != zone_id) {
            continue;
        }
        const auto points = TrianglePoints(pack, triangle_index);
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
        const std::tuple<int, double, double, uint32_t> key { IsSmallIslandTriangle(planner, triangle_index) ? 1 : 0,
                                                              distance,
                                                              -TriangleHeight(pack, triangle_index),
                                                              triangle_index };
        if (!best_key || key < *best_key) {
            best_key = key;
            best = BaseNavSnapResult { .triangle = triangle_index, .point = snapped, .distance = distance };
        }
    }
    return best;
}

bool IsRouteSegmentDrivable(
    const BaseNavPlanner& planner,
    const BaseNavPack& pack,
    uint16_t zone_id,
    const WorldPoint& a,
    const WorldPoint& b,
    double half_width,
    std::optional<double> seed_height)
{
    if (!segment_height_walkable(planner, pack, zone_id, a, b, seed_height)) {
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
        if (!segment_height_walkable(
                planner,
                pack,
                zone_id,
                { .x = head.x + ox, .y = head.y + oy },
                { .x = tail.x + ox, .y = tail.y + oy },
                seed_height)) {
            return false;
        }
    }
    return true;
}

bool IsSmallIslandTriangle(const BaseNavPlanner& planner, uint32_t triangle_index)
{
    return planner.natural_component_sizes[planner.natural_component_ids[triangle_index]] <= kSmallBridgeComponentMaxTriangles;
}

std::array<WorldPoint, 3> TrianglePoints(const BaseNavPack& pack, uint32_t triangle_index)
{
    const BaseNavTriangle& triangle = pack.triangles[triangle_index];
    const auto& vertices = pack.vertices;
    return {
        WorldPoint { .x = vertices[triangle.vertices[0]].u, .y = vertices[triangle.vertices[0]].v },
        WorldPoint { .x = vertices[triangle.vertices[1]].u, .y = vertices[triangle.vertices[1]].v },
        WorldPoint { .x = vertices[triangle.vertices[2]].u, .y = vertices[triangle.vertices[2]].v },
    };
}

double TriangleHeight(const BaseNavPack& pack, uint32_t triangle_index)
{
    const auto& triangle = pack.triangles[triangle_index];
    const auto& vertices = pack.vertices;
    return (static_cast<double>(vertices[triangle.vertices[0]].height) + static_cast<double>(vertices[triangle.vertices[1]].height)
            + static_cast<double>(vertices[triangle.vertices[2]].height))
           / 3.0;
}

uint32_t ComponentId(const BaseNavPlanner& planner, uint32_t triangle_index)
{
    return triangle_index < planner.natural_component_ids.size() ? planner.natural_component_ids[triangle_index]
                                                                 : std::numeric_limits<uint32_t>::max();
}

uint32_t ComponentSize(const BaseNavPlanner& planner, uint32_t triangle_index)
{
    const uint32_t id = ComponentId(planner, triangle_index);
    return id < planner.natural_component_sizes.size() ? planner.natural_component_sizes[id] : 0;
}

std::optional<std::array<WorldPoint, 2>> ClosestEdgeBridgePoints(const BaseNavPack& pack, uint32_t lhs, uint32_t rhs)
{
    const auto lhs_points = TrianglePoints(pack, lhs);
    const auto rhs_points = TrianglePoints(pack, rhs);
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

BaseNavPlanner BuildBaseNavPlanner(const BaseNavPack& pack, std::vector<BaseNavLink> links)
{
    BaseNavPlanner planner {
        .triangle_zones = std::vector<uint16_t>(pack.triangles.size(), 0),
        .neighbor_link_bits = std::vector<uint8_t>(pack.triangles.size(), 0),
    };
    for (const auto& zone : pack.zones) {
        const uint32_t end = zone.first_triangle + zone.triangle_count;
        for (uint32_t index = zone.first_triangle; index < end && index < planner.triangle_zones.size(); ++index) {
            planner.triangle_zones[index] = zone.zone_id;
        }
    }
    // 两段只写各自的字段且不读对方产物, 所以可以并行
    if (NavWorkerCount(static_cast<int64_t>(pack.triangles.size())) <= 1) {
        build_index(&planner, pack, links);
        build_spatial_index(&planner.bin_grids, planner.triangle_zones, pack);
        return planner;
    }
    // build_index 抛出时 jthread 析构会自动 join
    std::jthread bins([&planner, &pack] { build_spatial_index(&planner.bin_grids, planner.triangle_zones, pack); });
    build_index(&planner, pack, links);
    bins.join();
    return planner;
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
