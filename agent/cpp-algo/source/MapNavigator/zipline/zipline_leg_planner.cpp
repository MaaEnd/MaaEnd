#include "zipline_leg_planner.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <limits>
#include <memory>
#include <mutex>
#include <optional>
#include <queue>
#include <system_error>
#include <unordered_map>
#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../../Zipline/ZiplineStore.h"
#include "../navi_log.h"
#include "../navi_param.h"
#include "../planning/navmesh_path_expander.h"
#include "../planning/navmesh_queries.h"

namespace mapnavigator
{

namespace
{

// 逐条腿累加, 不能覆盖
thread_local ZiplineOutcome g_outcome;

constexpr size_t kMaxExtraPlans = 12;

// 供电结构离架子比这近才会抢走交互面板
constexpr double kMountPoleClearPx = 5.0;
// 让开量加上判定圈仍要够得着架子, 所以只让开判定圈的零头
constexpr double kMountStandOffsetPx = 0.75;
constexpr double kMountStandSnapRadiusPx = 2.0;
constexpr double kMountStandSnapTolPx = 1.0;

struct ZiplineData
{
    zipline::ZiplineFrames frames;
    zipline::ZiplineStore store;
    bool ok = false;
};

std::filesystem::file_time_type file_stamp(const std::filesystem::path& path)
{
    std::error_code ec;
    const auto stamp = std::filesystem::last_write_time(path, ec);
    return ec ? std::filesystem::file_time_type {} : stamp;
}

// 导入会在运行中改写文件, 所以按 mtime 重读; 交出的是快照, 重载只换指针
std::shared_ptr<const ZiplineData> shared_data()
{
    static std::mutex mutex;
    static std::shared_ptr<const ZiplineData> cached;
    static std::filesystem::file_time_type frames_stamp {};
    static std::filesystem::file_time_type store_stamp {};

    const std::filesystem::path frames_path = zipline::ZiplineFrames::DefaultPath();
    const std::filesystem::path store_path = zipline::ZiplineStore::DefaultPath();
    const auto frames_now = file_stamp(frames_path);
    const auto store_now = file_stamp(store_path);

    std::lock_guard<std::mutex> lock(mutex);
    if (cached && frames_now == frames_stamp && store_now == store_stamp) {
        return cached;
    }

    auto reloaded = std::make_shared<ZiplineData>();
    const bool frames_ok = reloaded->frames.load(frames_path);
    const bool store_ok = reloaded->store.load(store_path);
    reloaded->ok = frames_ok && store_ok;
    cached = std::move(reloaded);
    frames_stamp = frames_now;
    store_stamp = store_now;
    return cached;
}

double planar_distance(const navmesh::WorldPoint& a, const navmesh::WorldPoint& b)
{
    return std::hypot(b.x - a.x, b.y - a.y);
}

// 类号集合有序, 任一边为空表示无从判断, 当作可能连通
bool may_connect(const std::vector<uint32_t>& a, const std::vector<uint32_t>& b)
{
    if (a.empty() || b.empty()) {
        return true;
    }
    for (size_t i = 0, j = 0; i < a.size() && j < b.size();) {
        if (a[i] == b[j]) {
            return true;
        }
        if (a[i] < b[j]) {
            ++i;
        }
        else {
            ++j;
        }
    }
    return false;
}

struct SupplyPoint
{
    double x = 0.0;
    double z = 0.0;
    double radius = 0.0;
    std::array<int, 2> footprint { 1, 1 };
    std::array<int, 2> coverage_size { 0, 0 };
};

constexpr double absolute_value(double value)
{
    return value < 0.0 ? -value : value;
}

constexpr double grid_half_span(int size)
{
    return static_cast<double>((size - 1) / 2);
}

constexpr bool grid_areas_may_overlap(
    double delta_x,
    double delta_z,
    const std::array<int, 2>& coverage_size,
    const std::array<int, 2>& supply_footprint,
    const std::array<int, 2>& tower_footprint)
{
    const double center_reach_x = grid_half_span(coverage_size[0]) + grid_half_span(tower_footprint[0]);
    const double center_reach_z = grid_half_span(coverage_size[1]) + grid_half_span(tower_footprint[1]);
    const double anchor_uncertainty_x = grid_half_span(supply_footprint[0]) + grid_half_span(tower_footprint[0]);
    const double anchor_uncertainty_z = grid_half_span(supply_footprint[1]) + grid_half_span(tower_footprint[1]);
    const double reach_x = center_reach_x + anchor_uncertainty_x;
    const double reach_z = center_reach_z + anchor_uncertainty_z;
    return absolute_value(delta_x) <= reach_x && absolute_value(delta_z) <= reach_z;
}

static_assert(grid_areas_may_overlap(5.0, 2.0, { 7, 7 }, { 3, 3 }, { 3, 3 }));
static_assert(grid_areas_may_overlap(6.0, 6.0, { 7, 7 }, { 3, 3 }, { 3, 3 }));
static_assert(!grid_areas_may_overlap(0.0, 7.0, { 7, 7 }, { 3, 3 }, { 3, 3 }));

// 只量原始世界坐标的水平 x/z, 高低差不进判据
bool is_powered(const zipline::ZiplineMark& tower, const std::array<int, 2>& tower_footprint, const std::vector<SupplyPoint>& supplies)
{
    return std::any_of(supplies.begin(), supplies.end(), [&](const SupplyPoint& supply) {
        if (supply.coverage_size[0] > 0 && supply.coverage_size[1] > 0) {
            return grid_areas_may_overlap(supply.x - tower.x, supply.z - tower.z, supply.coverage_size, supply.footprint, tower_footprint);
        }
        return std::hypot(supply.x - tower.x, supply.z - tower.z) <= supply.radius;
    });
}

navmesh::WorldPoint to_world(const zipline::ZiplineNode& node)
{
    return navmesh::WorldPoint { .x = node.x, .y = node.y };
}

// 标记坐标落在占地的某个角格上, 每个角都算一个可能的塔心, 原始世界坐标 x/z
std::vector<std::array<double, 2>> tower_centers(const zipline::ZiplineNode& node, const std::array<int, 2>& footprint)
{
    const double half_x = grid_half_span(footprint[0]);
    const double half_z = grid_half_span(footprint[1]);
    const std::array<double, 2> signs { 1.0, -1.0 };
    const size_t x_count = half_x > 0.0 ? signs.size() : 1;
    const size_t z_count = half_z > 0.0 ? signs.size() : 1;
    std::vector<std::array<double, 2>> centers;
    for (size_t xi = 0; xi < x_count; ++xi) {
        for (size_t zi = 0; zi < z_count; ++zi) {
            centers.push_back({ node.world_x + 0.5 + signs[xi] * half_x, node.world_z + 0.5 + signs[zi] * half_z });
        }
    }
    return centers;
}

// 偏移在原始世界坐标里按格算再投影, 地图比例不进这里
std::vector<navmesh::WorldPoint>
    center_spots(const zipline::ZiplineFrame& frame, const zipline::ZiplineNode& tower, const std::array<int, 2>& footprint)
{
    std::vector<navmesh::WorldPoint> spots;
    for (const auto& [x, z] : tower_centers(tower, footprint)) {
        const zipline::ZiplineMark shifted {
            .template_id = tower.template_id,
            .level_id = tower.level_id,
            .x = x,
            .y = tower.world_y,
            .z = z,
        };
        spots.push_back(to_world(frame.project(shifted)));
    }
    return spots;
}

bool snaps_onto_tower_floor(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& point,
    const zipline::ZiplineNode& tower)
{
    const auto snap = NavmeshSnapAt(param, locator_zone, point, kMountStandSnapRadiusPx, tower.height);
    if (!snap || snap->distance > kMountStandSnapTolPx || std::abs(snap->height - tower.height) > navmesh::kBaseNavFloorBand) {
        return false;
    }
    return true;
}

// 沿供电结构到架子的方向往外挪, 让架子重新成为离身位最近的设备
std::optional<navmesh::WorldPoint> mount_stand_point(
    const NaviParam& param,
    const std::string& locator_zone,
    const zipline::ZiplineNode& tower,
    const std::vector<navmesh::WorldPoint>& supplies)
{
    const navmesh::WorldPoint here = to_world(tower);
    const navmesh::WorldPoint* nearest = nullptr;
    double nearest_distance = kMountPoleClearPx;
    for (const navmesh::WorldPoint& supply : supplies) {
        const double distance = planar_distance(here, supply);
        if (distance < nearest_distance) {
            nearest_distance = distance;
            nearest = &supply;
        }
    }
    if (nearest == nullptr || nearest_distance < 1e-3) {
        return std::nullopt;
    }
    const navmesh::WorldPoint stand {
        .x = here.x + (here.x - nearest->x) / nearest_distance * kMountStandOffsetPx,
        .y = here.y + (here.y - nearest->y) / nearest_distance * kMountStandOffsetPx,
    };
    if (!snaps_onto_tower_floor(param, locator_zone, stand, tower)) {
        return std::nullopt;
    }
    LogDebug << "ZiplineRoute: keeping a spot clear of the power structure next to the mount tower" << VAR(tower.x) << VAR(tower.y)
             << VAR(stand.x) << VAR(stand.y) << VAR(nearest_distance);
    return stand;
}

// 只取中心格, 往模型里推得越深, 被挡住那一刻离航点越远, 越容易落在预筛窗外
std::vector<navmesh::WorldPoint> mount_spots(
    const NaviParam& param,
    const std::string& locator_zone,
    const zipline::ZiplineFrame& frame,
    const zipline::ZiplineNode& tower,
    const std::array<int, 2>& footprint,
    const navmesh::WorldPoint& approach_from,
    const std::vector<navmesh::WorldPoint>& supplies)
{
    const navmesh::WorldPoint anchor = to_world(tower);
    const double half_x = grid_half_span(footprint[0]);
    const double half_z = grid_half_span(footprint[1]);
    const double toward_x = anchor.x - approach_from.x;
    const double toward_y = anchor.y - approach_from.y;
    std::vector<std::pair<double, navmesh::WorldPoint>> ranked;
    for (const navmesh::WorldPoint& spot : center_spots(frame, tower, footprint)) {
        if (!snaps_onto_tower_floor(param, locator_zone, spot, tower)) {
            continue;
        }
        ranked.emplace_back((spot.x - anchor.x) * toward_x + (spot.y - anchor.y) * toward_y, spot);
    }
    std::stable_sort(ranked.begin(), ranked.end(), [](const auto& lhs, const auto& rhs) { return lhs.first > rhs.first; });
    std::vector<navmesh::WorldPoint> spots;
    for (const auto& entry : ranked) {
        spots.push_back(entry.second);
    }
    if (spots.empty()) {
        spots.push_back(anchor);
    }
    if (const std::optional<navmesh::WorldPoint> stand = mount_stand_point(param, locator_zone, tower, supplies)) {
        spots.push_back(*stand);
    }
    LogDebug << "ZiplineRoute: stand points to try at the mount tower" << VAR(spots.size()) << VAR(anchor.x) << VAR(anchor.y) << VAR(half_x)
             << VAR(half_z);
    return spots;
}

constexpr double minimum_possible_world_span_squared(
    double anchor_delta_x,
    double delta_y,
    double anchor_delta_z,
    const std::array<int, 2>& footprint_a,
    const std::array<int, 2>& footprint_b)
{
    const double uncertainty_x = grid_half_span(footprint_a[0]) + grid_half_span(footprint_b[0]);
    const double uncertainty_z = grid_half_span(footprint_a[1]) + grid_half_span(footprint_b[1]);
    const double dx = std::max(absolute_value(anchor_delta_x) - uncertainty_x, 0.0);
    const double dz = std::max(absolute_value(anchor_delta_z) - uncertainty_z, 0.0);
    return dx * dx + delta_y * delta_y + dz * dz;
}

// BaseNavRouteResult::cost 是搜索代价, 和几何长度不能混用
double polyline_length(const navmesh::WorldPath& path)
{
    double total = 0.0;
    for (size_t i = 0; i + 1 < path.points.size(); ++i) {
        total += planar_distance(path.points[i], path.points[i + 1]);
    }
    return total;
}

constexpr size_t kNoTower = std::numeric_limits<size_t>::max();

// 中段架子到索线的三维垂距在此以内时, 这条直索只算不确定边
constexpr double kRopeInterceptLateralWu = 15.0;

// 中段架子与索线的竖直落差超过此值时不算拦路
constexpr double kRopeInterceptDropWu = 11.5;

// 绳端在塔底上方的高度, 判索撞不撞地要按抬升后的两端连线量
constexpr double kRopeEndHeightWu = 5.72;

std::vector<navmesh::OccluderPoint> tower_base_points(const zipline::ZiplineNode& node, const std::array<int, 2>& footprint)
{
    std::vector<navmesh::OccluderPoint> bases;
    for (const auto& [x, z] : tower_centers(node, footprint)) {
        bases.push_back(navmesh::OccluderPoint { .x = x, .y = node.world_y, .z = z });
    }
    return bases;
}

// 从塔落地后的地面高度起算, 记录里的塔底是整数格高, 差几厘米就会判错贴着梁底过的绳
std::vector<navmesh::OccluderPoint> rope_end_points(const std::vector<navmesh::OccluderPoint>& bases, const std::vector<double>& grounds)
{
    std::vector<navmesh::OccluderPoint> ends;
    for (size_t k = 0; k < bases.size(); ++k) {
        ends.push_back(navmesh::OccluderPoint { .x = bases[k].x, .y = grounds[k] + kRopeEndHeightWu, .z = bases[k].z });
    }
    return ends;
}

struct RopeCandidate
{
    size_t a = 0;
    size_t b = 0;
    std::vector<NavmeshAirLine> lines;
};

// certain 是 all 的子集, 要求中段无拦路架且有一组连线不撞实体, 任一条不成立只降档不删边
struct ZipLinkGraph
{
    std::vector<std::vector<size_t>> all;
    std::vector<std::vector<size_t>> certain;
    // 没被拦路架拦下、还要问遮挡体才能进 certain 的边
    std::vector<RopeCandidate> ropes;
};

void drop_edge(std::vector<std::vector<size_t>>& links, size_t a, size_t b)
{
    const auto drop = [](std::vector<size_t>& outgoing, size_t target) {
        outgoing.erase(std::remove(outgoing.begin(), outgoing.end(), target), outgoing.end());
    };
    drop(links[a], b);
    drop(links[b], a);
}

// 返回拦路架到索线的垂距, nodes 须已做过通电筛选
std::optional<double> rope_intercepted(
    const std::vector<zipline::ZiplineNode>& nodes,
    const std::vector<size_t>& node_record,
    size_t from,
    size_t to,
    const std::vector<double>& span_limit,
    const std::vector<std::array<int, 2>>& footprints)
{
    const double dx = nodes[to].world_x - nodes[from].world_x;
    const double dy = nodes[to].world_y - nodes[from].world_y;
    const double dz = nodes[to].world_z - nodes[from].world_z;
    const double span = std::hypot(dx, dy, dz);
    if (span <= 2.0 * kRopeInterceptLateralWu) {
        return std::nullopt;
    }
    const double planar_squared = dx * dx + dz * dz;
    for (size_t k = 0; k < nodes.size(); ++k) {
        if (k == from || k == to || node_record[k] != node_record[from]) {
            continue;
        }
        const double offset_x = nodes[k].world_x - nodes[from].world_x;
        const double offset_y = nodes[k].world_y - nodes[from].world_y;
        const double offset_z = nodes[k].world_z - nodes[from].world_z;
        const double along = (offset_x * dx + offset_y * dy + offset_z * dz) / span;
        if (along <= kRopeInterceptLateralWu || along >= span - kRopeInterceptLateralWu) {
            continue;
        }
        const double lateral_x = offset_x - dx * along / span;
        const double lateral_y = offset_y - dy * along / span;
        const double lateral_z = offset_z - dz * along / span;
        const double lateral = std::hypot(lateral_x, lateral_y, lateral_z);
        if (lateral > kRopeInterceptLateralWu) {
            continue;
        }
        if (planar_squared > 0.0) {
            const double drop = offset_y - dy * (offset_x * dx + offset_z * dz) / planar_squared;
            if (std::abs(drop) > kRopeInterceptDropWu) {
                continue;
            }
        }
        const double limit = std::min(span_limit[from], span_limit[k]);
        if (minimum_possible_world_span_squared(offset_x, offset_y, offset_z, footprints[from], footprints[k]) > limit * limit) {
            continue;
        }
        return lateral;
    }
    return std::nullopt;
}

ZipLinkGraph build_links(
    const std::vector<zipline::ZiplineNode>& nodes,
    const std::vector<size_t>& node_record,
    const std::vector<double>& span_limit,
    const std::vector<std::array<int, 2>>& footprints,
    const std::vector<std::vector<navmesh::OccluderPoint>>& rope_ends)
{
    ZipLinkGraph graph;
    graph.all.resize(nodes.size());
    graph.certain.resize(nodes.size());
    std::vector<double> demoted_lateral;
    for (size_t i = 0; i < nodes.size(); ++i) {
        if (span_limit[i] <= 0.0) {
            continue;
        }
        for (size_t j = i + 1; j < nodes.size(); ++j) {
            if (node_record[i] != node_record[j] || span_limit[j] <= 0.0) {
                continue;
            }
            const double limit = std::min(span_limit[i], span_limit[j]);
            const double limit_squared = limit * limit;
            const double span_squared = minimum_possible_world_span_squared(
                nodes[j].world_x - nodes[i].world_x,
                nodes[j].world_y - nodes[i].world_y,
                nodes[j].world_z - nodes[i].world_z,
                footprints[i],
                footprints[j]);
            if (span_squared > limit_squared) {
                continue;
            }
            // 绳长高差按记录里的塔底量, 和上面的下界用同一组高度
            std::vector<NavmeshAirLine> lines;
            const double dy = nodes[j].world_y - nodes[i].world_y;
            for (const navmesh::OccluderPoint& end_a : rope_ends[i]) {
                for (const navmesh::OccluderPoint& end_b : rope_ends[j]) {
                    const double dx = end_b.x - end_a.x;
                    const double dz = end_b.z - end_a.z;
                    if (dx * dx + dy * dy + dz * dz <= limit_squared) {
                        lines.push_back(NavmeshAirLine { .a = end_a, .b = end_b });
                    }
                }
            }
            if (lines.empty()) {
                continue;
            }
            graph.all[i].push_back(j);
            graph.all[j].push_back(i);
            if (const std::optional<double> lateral = rope_intercepted(nodes, node_record, i, j, span_limit, footprints)) {
                demoted_lateral.push_back(*lateral);
                continue;
            }
            graph.ropes.push_back(RopeCandidate { .a = i, .b = j, .lines = std::move(lines) });
        }
    }
    if (!demoted_lateral.empty()) {
        LogDebug << "ZiplineRoute: demoted the straight ropes intercepted by another tower." << VAR(demoted_lateral);
    }
    return graph;
}

// 到不了的留 infinity, 边权每跳含一次换乘开销, 链头那次由调用方减回去
void solve_zip_chains(
    const std::vector<zipline::ZiplineNode>& nodes,
    const std::vector<std::vector<size_t>>& links,
    const zipline::ZiplineCostModel& cost,
    size_t source,
    std::vector<double>* dist,
    std::vector<size_t>* prev)
{
    dist->assign(nodes.size(), std::numeric_limits<double>::infinity());
    prev->assign(nodes.size(), kNoTower);
    (*dist)[source] = 0.0;

    using Entry = std::pair<double, size_t>;
    std::priority_queue<Entry, std::vector<Entry>, std::greater<Entry>> queue;
    queue.emplace(0.0, source);
    while (!queue.empty()) {
        const auto [reached_at, at] = queue.top();
        queue.pop();
        if (reached_at > (*dist)[at]) {
            continue;
        }
        for (const size_t next : links[at]) {
            // 代价用像素距离, 要和 navmesh 路径长度相加比较
            const double hop = planar_distance(to_world(nodes[at]), to_world(nodes[next])) * cost.speed_ratio + cost.transfer_penalty;
            if (reached_at + hop >= (*dist)[next]) {
                continue;
            }
            (*dist)[next] = reached_at + hop;
            (*prev)[next] = at;
            queue.emplace((*dist)[next], next);
        }
    }
}

struct PlannedLeg
{
    navmesh::WorldPath path;
    double length = 0.0;
    NavmeshRouteDiagnostic diagnostic;
};

std::optional<PlannedLeg> to_planned_leg(const std::optional<navmesh::BaseNavRouteResult>& route, NavmeshRouteDiagnostic diagnostic)
{
    if (!route || !route->ok()) {
        return std::nullopt;
    }
    return PlannedLeg {
        .path = route->path,
        .length = polyline_length(route->path),
        .diagnostic = std::move(diagnostic),
    };
}

enum class LegLookup
{
    Planned,
    Unplannable,
    OutOfBudget,
};

// 接近段和离开段两个缓存共用
struct LegBudget
{
    size_t remaining = kMaxExtraPlans;
};

// 缓存里存 nullopt 表示这段规划不出来, 下次不必再试
class LegCache
{
public:
    using Planner = std::function<std::optional<PlannedLeg>(size_t)>;

    LegCache(Planner planner, LegBudget& budget)
        : planner_(std::move(planner))
        , budget_(budget)
    {
    }

    // 返回 Planned 时 *leg 指向缓存里的结果
    LegLookup get(size_t index, const PlannedLeg** leg)
    {
        auto it = cache_.find(index);
        if (it == cache_.end()) {
            if (budget_.remaining == 0) {
                return LegLookup::OutOfBudget;
            }
            --budget_.remaining;
            it = cache_.emplace(index, planner_(index)).first;
        }
        if (!it->second) {
            return LegLookup::Unplannable;
        }
        *leg = &*it->second;
        return LegLookup::Planned;
    }

private:
    Planner planner_;
    LegBudget& budget_;
    std::unordered_map<size_t, std::optional<PlannedLeg>> cache_;
};

struct PlanInputs
{
    const NaviParam& param;
    const ZiplineLegRequest& request;
    const zipline::ZiplineFrames& frames;
    const zipline::ZiplineFrame& frame;
    const zipline::ZiplineCostModel& cost;
    double gain_threshold = 0.0;
};

struct TowerSites
{
    std::vector<zipline::ZiplineNode> nodes;
    // 每根架子来自哪份记录, 不同记录的架子之间挂不上索
    std::vector<size_t> node_record;
    // 像素平面坐标, 通电判定另用世界坐标
    std::vector<navmesh::WorldPoint> supply_points;
    std::vector<std::array<int, 2>> footprints;
    std::vector<navmesh::WorldPoint> spots;
    std::vector<size_t> spot_tower;
    std::vector<std::vector<size_t>> tower_spots;
    std::vector<std::vector<size_t>> board_spots;
    std::vector<std::vector<size_t>> land_spots;
    std::vector<double> lb_from_start;
    std::vector<double> lb_to_goal;
};

TowerSites collect_powered_towers(const NaviParam& param, const ZiplineData& data, const zipline::ZiplineFrame& frame)
{
    const bool require_power = data.frames.hasPowerSources();

    std::vector<zipline::ZiplineNode> nodes;
    std::vector<size_t> node_record;
    std::vector<navmesh::WorldPoint> supply_points;
    size_t unpowered = 0;
    const std::vector<zipline::ZiplineMapRecord>& records = data.store.maps();
    for (size_t record_index = 0; record_index < records.size(); ++record_index) {
        const zipline::ZiplineMapRecord& record = records[record_index];
        if (record.account_id != param.zipline_account_id) {
            continue;
        }
        if (!frame.map_id.empty() && record.map_id != frame.map_id) {
            continue;
        }

        std::vector<SupplyPoint> supplies;
        for (const auto& mark : record.marks) {
            const zipline::ZiplinePowerSource* source = data.frames.powerSource(mark.template_id);
            if (source != nullptr) {
                supplies.push_back(SupplyPoint {
                    .x = mark.x,
                    .z = mark.z,
                    .radius = source->radius,
                    .footprint = source->footprint,
                    .coverage_size = source->coverage_size,
                });
                supply_points.push_back(to_world(frame.project(mark)));
            }
        }
        if (require_power && supplies.empty()) {
            LogWarn << "ZiplineRoute: no power structures on record, every zipline here counts as unpowered" << VAR(record.map_id);
        }

        for (const auto& mark : record.marks) {
            if (!frame.accepts(mark)) {
                continue;
            }
            if (require_power && !is_powered(mark, data.frames.footprint(mark.template_id), supplies)) {
                ++unpowered;
                continue;
            }
            nodes.push_back(frame.project(mark));
            node_record.push_back(record_index);
        }
    }
    if (unpowered != 0) {
        LogDebug << "ZiplineRoute: left out the ziplines no power reaches" << VAR(unpowered) << VAR(nodes.size());
    }
    return TowerSites {
        .nodes = std::move(nodes),
        .node_record = std::move(node_record),
        .supply_points = std::move(supply_points),
    };
}

// 平面距离和高度都要判, 可走面会在同一平面坐标上摞好几层
void find_boarding_spots(const PlanInputs& in, TowerSites& sites)
{
    sites.footprints.resize(sites.nodes.size());
    sites.tower_spots.resize(sites.nodes.size());
    size_t out_of_reach = 0;
    size_t wrong_floor = 0;
    for (size_t i = 0; i < sites.nodes.size(); ++i) {
        const zipline::ZiplineNode& node = sites.nodes[i];
        sites.footprints[i] = in.frames.footprint(node.template_id);
        const navmesh::WorldPoint anchor = to_world(node);
        std::vector<navmesh::WorldPoint> tries { anchor };
        for (const navmesh::WorldPoint& center : center_spots(in.frame, node, sites.footprints[i])) {
            if (center.x != anchor.x || center.y != anchor.y) {
                tries.push_back(center);
            }
        }
        bool in_reach = false;
        for (const navmesh::WorldPoint& point : tries) {
            const auto snap = NavmeshSnapAt(in.param, in.request.locator_zone, point, in.cost.reach_radius, node.height);
            if (!snap || snap->distance > in.cost.reach_radius) {
                continue;
            }
            in_reach = true;
            if (std::abs(snap->height - node.height) > navmesh::kBaseNavFloorBand) {
                continue;
            }
            sites.tower_spots[i].push_back(sites.spots.size());
            sites.spots.push_back(point);
            sites.spot_tower.push_back(i);
        }
        if (sites.tower_spots[i].empty()) {
            ++(in_reach ? wrong_floor : out_of_reach);
        }
    }
    if (out_of_reach != 0 || wrong_floor != 0) {
        LogDebug << "ZiplineRoute: these ziplines can be ridden through but not boarded" << VAR(out_of_reach) << VAR(wrong_floor)
                 << VAR_AS(nodes.size(), sites.nodes.size());
    }
}

void split_by_region(const PlanInputs& in, TowerSites& sites)
{
    std::vector<navmesh::WorldPoint> probes = sites.spots;
    probes.push_back(in.request.start);
    probes.push_back(in.request.goal);
    const std::vector<std::vector<uint32_t>> regions = NavmeshRegionsNear(in.param, in.request.locator_zone, probes);
    const std::vector<uint32_t>& start_regions = regions[sites.spots.size()];
    const std::vector<uint32_t>& goal_regions = regions[sites.spots.size() + 1];
    sites.board_spots.resize(sites.nodes.size());
    sites.land_spots.resize(sites.nodes.size());
    size_t cut_off = 0;
    for (size_t i = 0; i < sites.nodes.size(); ++i) {
        for (const size_t spot : sites.tower_spots[i]) {
            if (may_connect(regions[spot], start_regions)) {
                sites.board_spots[i].push_back(spot);
            }
            if (may_connect(regions[spot], goal_regions)) {
                sites.land_spots[i].push_back(spot);
            }
        }
        if (!sites.tower_spots[i].empty() && sites.board_spots[i].empty() && sites.land_spots[i].empty()) {
            ++cut_off;
        }
    }
    if (cut_off != 0) {
        LogDebug << "ZiplineRoute: these ziplines share no walkable ground with either end" << VAR(cut_off)
                 << VAR_AS(nodes.size(), sites.nodes.size());
    }
}

// 上不去的架子只取消上索资格, 仍可当落点
void drop_unboardable(const NaviParam& param, TowerSites& sites)
{
    size_t unboardable = 0;
    for (const ZiplineHopRecord& entry : param.zipline_ledger) {
        if (entry.outcome != HopOutcome::Unboardable) {
            continue;
        }
        for (size_t i = 0; i < sites.nodes.size(); ++i) {
            if (!sites.board_spots[i].empty() && ToNodeRef(sites.nodes[i]).sameTower(entry.plan.mount)) {
                sites.board_spots[i].clear();
                ++unboardable;
            }
        }
    }
    if (unboardable != 0) {
        LogInfo << "ZiplineRoute: left out the towers the runtime never managed to board." << VAR(unboardable)
                << VAR_AS(nodes.size(), sites.nodes.size());
    }
}

bool none_usable(const std::vector<std::vector<size_t>>& per_tower)
{
    return std::all_of(per_tower.begin(), per_tower.end(), [](const std::vector<size_t>& v) { return v.empty(); });
}

void walk_bounds(const PlanInputs& in, TowerSites& sites)
{
    sites.lb_from_start.assign(sites.nodes.size(), std::numeric_limits<double>::infinity());
    sites.lb_to_goal.assign(sites.nodes.size(), std::numeric_limits<double>::infinity());
    for (size_t i = 0; i < sites.nodes.size(); ++i) {
        for (const size_t spot : sites.board_spots[i]) {
            sites.lb_from_start[i] = std::min(sites.lb_from_start[i], planar_distance(in.request.start, sites.spots[spot]));
        }
        for (const size_t spot : sites.land_spots[i]) {
            sites.lb_to_goal[i] = std::min(sites.lb_to_goal[i], planar_distance(sites.spots[spot], in.request.goal));
        }
    }
}

ZipLinkGraph build_rope_graph(const PlanInputs& in, const TowerSites& sites)
{
    // 未登记的架子类型索长上限为 0, 不参与配对
    std::vector<double> span_limit(sites.nodes.size());
    std::vector<std::vector<navmesh::OccluderPoint>> tower_bases(sites.nodes.size());
    for (size_t i = 0; i < sites.nodes.size(); ++i) {
        span_limit[i] = in.frames.maxSpan(sites.nodes[i].template_id);
        tower_bases[i] = tower_base_points(sites.nodes[i], sites.footprints[i]);
    }
    const std::vector<std::vector<double>> grounds = NavmeshGroundHeights(in.param, in.request.locator_zone, tower_bases);
    std::vector<std::vector<navmesh::OccluderPoint>> rope_ends(sites.nodes.size());
    for (size_t i = 0; i < sites.nodes.size(); ++i) {
        rope_ends[i] = rope_end_points(tower_bases[i], grounds[i]);
    }
    ZipLinkGraph graph = build_links(sites.nodes, sites.node_record, span_limit, sites.footprints, rope_ends);

    std::vector<std::vector<NavmeshAirLine>> groups;
    groups.reserve(graph.ropes.size());
    for (RopeCandidate& rope : graph.ropes) {
        groups.push_back(std::move(rope.lines));
    }
    const std::vector<std::vector<navmesh::OccluderHit>> blocks = NavmeshLineGroupBlocks(in.param, in.request.locator_zone, groups);
    std::vector<double> demoted_first_hits;
    for (size_t index = 0; index < graph.ropes.size(); ++index) {
        const RopeCandidate& rope = graph.ropes[index];
        if (blocks[index].empty()) {
            graph.certain[rope.a].push_back(rope.b);
            graph.certain[rope.b].push_back(rope.a);
            continue;
        }
        double latest = 0.0;
        for (size_t k = 0; k < blocks[index].size(); ++k) {
            const NavmeshAirLine& line = groups[index][k];
            const double length = std::hypot(line.b.x - line.a.x, line.b.y - line.a.y, line.b.z - line.a.z);
            latest = std::max(latest, blocks[index][k].s * length);
        }
        demoted_first_hits.push_back(latest);
    }
    if (!demoted_first_hits.empty()) {
        LogDebug << "ZiplineRoute: demoted the ropes blocked by solids." << VAR(demoted_first_hits) << VAR(graph.ropes.size());
    }
    return graph;
}

// 索不分上下行, 失败的跳两个方向一起封
void ban_failed_hops(const NaviParam& param, const std::vector<zipline::ZiplineNode>& nodes, ZipLinkGraph& graph)
{
    if (param.zipline_ledger.empty()) {
        return;
    }
    const auto near_tower = [&nodes](size_t tower, const ZiplineNodeRef& ref) {
        return std::hypot(nodes[tower].x - ref.x, nodes[tower].y - ref.y) <= kZiplineHopBanMatchWu;
    };
    std::vector<std::pair<size_t, size_t>> banned;
    for (size_t i = 0; i < graph.all.size(); ++i) {
        for (const size_t j : graph.all[i]) {
            if (j <= i) {
                continue;
            }
            int32_t aim_failures = 0;
            for (const ZiplineHopRecord& entry : param.zipline_ledger) {
                const bool rope_failure = IsZiplineRopeFailure(entry.outcome);
                if (!rope_failure && entry.outcome != HopOutcome::AimFailed) {
                    continue;
                }
                const ZiplineNodeRef& from = entry.plan.mount;
                const ZiplineNodeRef& to = entry.plan.landing;
                if (!((near_tower(i, from) && near_tower(j, to)) || (near_tower(i, to) && near_tower(j, from)))) {
                    continue;
                }
                if (rope_failure || ++aim_failures >= kZiplineAimFailBanCount) {
                    banned.emplace_back(i, j);
                    break;
                }
            }
        }
    }
    for (const auto& [a, b] : banned) {
        drop_edge(graph.all, a, b);
        drop_edge(graph.certain, a, b);
    }
    LogInfo << "ZiplineRoute: dropped the hops the runtime already gave up on." << VAR(param.zipline_ledger.size()) << VAR(banned.size());
}

struct Candidate
{
    size_t mount = 0;
    size_t dismount = 0;
    // 含上索和沿途每次换乘
    double zip_cost = 0.0;
    double lower_bound = 0.0;
    // 链解自确定边子图, 还原链和列举备选架子须用同一张图
    bool certain = false;
};

// 按下界升序, LegPicker 靠这个顺序剪枝
std::vector<Candidate> collect_candidates(const PlanInputs& in, const TowerSites& sites, const ZipLinkGraph& graph)
{
    std::vector<Candidate> candidates;
    std::vector<double> chain_cost;
    std::vector<size_t> chain_prev;
    std::vector<double> certain_cost;
    std::vector<size_t> certain_prev;
    for (size_t i = 0; i < sites.nodes.size(); ++i) {
        if (sites.board_spots[i].empty() || graph.all[i].empty() || sites.lb_from_start[i] + in.cost.mount_penalty >= in.gain_threshold) {
            continue;
        }

        solve_zip_chains(sites.nodes, graph.certain, in.cost, i, &certain_cost, &certain_prev);
        solve_zip_chains(sites.nodes, graph.all, in.cost, i, &chain_cost, &chain_prev);
        for (size_t j = 0; j < sites.nodes.size(); ++j) {
            if (j == i || sites.land_spots[j].empty()) {
                continue;
            }
            const auto offer = [&](double chain, bool certain) {
                if (!std::isfinite(chain)) {
                    return;
                }
                const double zip_cost = in.cost.mount_penalty - in.cost.transfer_penalty + chain;
                const double lower_bound = sites.lb_from_start[i] + zip_cost + sites.lb_to_goal[j];
                if (lower_bound >= in.gain_threshold) {
                    return;
                }
                candidates.push_back(
                    Candidate { .mount = i, .dismount = j, .zip_cost = zip_cost, .lower_bound = lower_bound, .certain = certain });
            };
            offer(certain_cost[j], true);
            if (chain_cost[j] < certain_cost[j]) {
                offer(chain_cost[j], false);
            }
        }
    }
    std::stable_sort(candidates.begin(), candidates.end(), [](const Candidate& a, const Candidate& b) {
        return a.lower_bound < b.lower_bound;
    });
    return candidates;
}

std::optional<PlannedLeg> plan_approach(const PlanInputs& in, const TowerSites& sites, size_t spot)
{
    NavmeshRouteDiagnostic diagnostic;
    auto route = PlanNavmeshRoute(
        in.param,
        in.request.locator_zone,
        {
            .start = in.request.start,
            .start_floor_y = in.request.start_floor_y,
            .start_deck_y = in.request.start_deck_y,
            .goal = sites.spots[spot],
            .goal_deck_y = sites.nodes[sites.spot_tower[spot]].height,
        },
        in.request.capture_diagnostics ? &diagnostic : nullptr);
    return to_planned_leg(route, std::move(diagnostic));
}

std::optional<PlannedLeg> plan_departure(const PlanInputs& in, const TowerSites& sites, size_t spot)
{
    NavmeshRouteDiagnostic diagnostic;
    auto route = PlanNavmeshRoute(
        in.param,
        in.request.locator_zone,
        {
            .start = sites.spots[spot],
            .start_floor_y = sites.nodes[sites.spot_tower[spot]].height,
            .goal = in.request.goal,
            .goal_deck_y = in.request.goal_deck_y,
        },
        in.request.capture_diagnostics ? &diagnostic : nullptr);
    return to_planned_leg(route, std::move(diagnostic));
}

// 规划通一个站点或预算用完就停
LegLookup first_leg(LegCache& cache, const std::vector<size_t>& spots, const PlannedLeg** leg)
{
    LegLookup lookup = LegLookup::OutOfBudget;
    for (const size_t spot : spots) {
        lookup = cache.get(spot, leg);
        if (lookup != LegLookup::Unplannable) {
            break;
        }
    }
    return lookup;
}

// 两段都指向 LegPicker 的缓存
struct PickedLegs
{
    Candidate candidate;
    const PlannedLeg* approach = nullptr;
    const PlannedLeg* departure = nullptr;
    double cost = 0.0;
};

class LegPicker
{
public:
    LegPicker(const PlanInputs& in, const TowerSites& sites)
        : in_(in)
        , sites_(sites)
        , approach_cache_([&in, &sites](size_t spot) { return plan_approach(in, sites, spot); }, budget_)
        , departure_cache_([&in, &sites](size_t spot) { return plan_departure(in, sites, spot); }, budget_)
        , best_cost_(in.gain_threshold)
    {
    }

    LegPicker(const LegPicker&) = delete;
    LegPicker& operator=(const LegPicker&) = delete;

    // 先只比确定边候选, 都走不通才比不确定的, 混在一起比会让便宜的不确定直索赢
    void pick(const std::vector<Candidate>& candidates)
    {
        evaluate(candidates, true);
        if (!best_ && !interrupted_) {
            evaluate(candidates, false);
        }
    }

    const std::optional<PickedLegs>& best() const { return best_; }

    bool truncated() const { return truncated_; }

    bool interrupted() const { return interrupted_; }

private:
    void evaluate(const std::vector<Candidate>& candidates, bool want_certain)
    {
        const std::function<bool()>& should_stop = in_.request.should_stop;
        for (const auto& candidate : candidates) {
            if (candidate.certain != want_certain) {
                continue;
            }
            if (should_stop && should_stop()) {
                interrupted_ = true;
                break;
            }
            if (candidate.lower_bound >= best_cost_) {
                break;
            }

            const PlannedLeg* approach = nullptr;
            const LegLookup approach_lookup = first_leg(approach_cache_, sites_.board_spots[candidate.mount], &approach);
            if (approach_lookup == LegLookup::OutOfBudget) {
                truncated_ = true;
                break;
            }
            if (approach_lookup == LegLookup::Unplannable) {
                continue;
            }

            if (approach->length + candidate.zip_cost + sites_.lb_to_goal[candidate.dismount] >= best_cost_) {
                continue;
            }

            const PlannedLeg* departure = nullptr;
            const LegLookup departure_lookup = first_leg(departure_cache_, sites_.land_spots[candidate.dismount], &departure);
            if (departure_lookup == LegLookup::OutOfBudget) {
                truncated_ = true;
                break;
            }
            if (departure_lookup == LegLookup::Unplannable) {
                continue;
            }

            const double total = approach->length + candidate.zip_cost + departure->length;
            if (total >= best_cost_) {
                continue;
            }

            best_cost_ = total;
            best_ = PickedLegs { .candidate = candidate, .approach = approach, .departure = departure, .cost = total };
        }
    }

    const PlanInputs& in_;
    const TowerSites& sites_;
    LegBudget budget_;
    LegCache approach_cache_;
    LegCache departure_cache_;
    double best_cost_ = 0.0;
    std::optional<PickedLegs> best_;
    bool truncated_ = false;
    bool interrupted_ = false;
};

ZiplineRoute assemble_route(const PlanInputs& in, const TowerSites& sites, const ZipLinkGraph& graph, const PickedLegs& picked)
{
    ZiplineRoute route {
        .approach = picked.approach->path,
        .departure = picked.departure->path,
        .cost = picked.cost,
    };
    const std::vector<std::vector<size_t>>& links = picked.candidate.certain ? graph.certain : graph.all;
    std::vector<double> chain_cost;
    std::vector<size_t> chain_prev;
    solve_zip_chains(sites.nodes, links, in.cost, picked.candidate.mount, &chain_cost, &chain_prev);
    std::vector<size_t> chain;
    for (size_t at = picked.candidate.dismount; at != kNoTower; at = chain_prev[at]) {
        chain.push_back(at);
    }
    std::reverse(chain.begin(), chain.end());
    for (size_t hop = 0; hop < chain.size(); ++hop) {
        route.towers.push_back(sites.nodes[chain[hop]]);
        if (hop + 1 < chain.size()) {
            std::vector<zipline::ZiplineNode> alternates;
            for (const size_t other : links[chain[hop]]) {
                if (other != chain[hop + 1]) {
                    alternates.push_back(sites.nodes[other]);
                }
            }
            route.hop_alternates.push_back(std::move(alternates));
        }
    }
    if (in.request.capture_diagnostics) {
        route.diagnostics.push_back(picked.approach->diagnostic);
        route.diagnostics.push_back(picked.departure->diagnostic);
    }
    const navmesh::WorldPoint& approach_from =
        route.approach.points.size() >= 2 ? route.approach.points[route.approach.points.size() - 2] : in.request.start;
    route.mount_spots = mount_spots(
        in.param,
        in.request.locator_zone,
        in.frame,
        route.towers.front(),
        in.frames.footprint(route.towers.front().template_id),
        approach_from,
        sites.supply_points);
    return route;
}

} // namespace

ZiplineNodeRef ToNodeRef(const zipline::ZiplineNode& node)
{
    ZiplineNodeRef ref;
    ref.level_id = node.level_id;
    ref.world_x = node.world_x;
    ref.world_y = node.world_y;
    ref.world_z = node.world_z;
    ref.has_world = true;
    ref.x = node.x;
    ref.y = node.y;
    ref.height = node.height;
    return ref;
}

void ResetZiplineOutcome()
{
    g_outcome = {};
}

ZiplineOutcome CurrentZiplineOutcome()
{
    return g_outcome;
}

std::optional<ZiplineRoute> PlanZiplineRoute(const NaviParam& param, const ZiplineLegRequest& request)
{
    if (!param.zipline_enabled) {
        return std::nullopt;
    }
    if (param.zipline_account_id.empty()) {
        g_outcome.account_unknown = true;
        return std::nullopt;
    }

    const bool walking_baseline_available = request.walking_path != nullptr && !request.walking_path->points.empty();

    // outcome 是这条腿记进哪本账, nullptr 表示不记账
    const auto no_zipline = [&](const char* why, bool ZiplineOutcome::* outcome) -> std::optional<ZiplineRoute> {
        if (walking_baseline_available) {
            LogInfo << "ZiplineRoute: walking this leg instead." << VAR(why) << VAR_AS(navmesh_zone, request.navmesh_zone);
        }
        else {
            LogInfo << "ZiplineRoute: no bridge for the disconnected walking leg." << VAR(why)
                    << VAR_AS(navmesh_zone, request.navmesh_zone);
        }
        if (outcome) {
            g_outcome.*outcome = true;
        }
        return std::nullopt;
    };

    const std::shared_ptr<const ZiplineData> data = shared_data();
    if (!data->ok || data->frames.empty()) {
        return no_zipline("no zipline calibration on disk", &ZiplineOutcome::no_data);
    }

    const zipline::ZiplineFrame* frame = data->frames.findByZone(request.navmesh_zone);
    if (!frame) {
        return no_zipline("this zone is not calibrated", nullptr);
    }

    TowerSites sites = collect_powered_towers(param, *data, *frame);
    if (sites.nodes.size() < 2) {
        return no_zipline("no powered ziplines recorded in this zone", &ZiplineOutcome::no_data);
    }

    const zipline::ZiplineCostModel& cost = data->frames.cost();
    const double baseline_length = walking_baseline_available ? polyline_length(*request.walking_path) : 0.0;
    const double gain_threshold = walking_baseline_available ? baseline_length - cost.min_gain : std::numeric_limits<double>::infinity();
    if (walking_baseline_available && gain_threshold <= cost.mount_penalty) {
        return no_zipline("the walk is too short for any zipline to pay off", &ZiplineOutcome::not_chosen);
    }

    const PlanInputs in {
        .param = param,
        .request = request,
        .frames = data->frames,
        .frame = *frame,
        .cost = cost,
        .gain_threshold = gain_threshold,
    };
    find_boarding_spots(in, sites);
    if (sites.spots.empty()) {
        return no_zipline("not one zipline here can be walked up to", &ZiplineOutcome::not_chosen);
    }

    split_by_region(in, sites);
    drop_unboardable(param, sites);
    if (none_usable(sites.board_spots) || none_usable(sites.land_spots)) {
        return no_zipline("no zipline can be boarded from here, or none of them lands near the destination", &ZiplineOutcome::not_chosen);
    }

    walk_bounds(in, sites);
    ZipLinkGraph graph = build_rope_graph(in, sites);
    ban_failed_hops(param, sites.nodes, graph);

    const std::vector<Candidate> candidates = collect_candidates(in, sites, graph);
    if (candidates.empty()) {
        return no_zipline("no reachable pair of ziplines leads anywhere useful", &ZiplineOutcome::not_chosen);
    }

    LegPicker picker(in, sites);
    picker.pick(candidates);
    if (picker.truncated()) {
        LogWarn << "ZiplineRoute: plan budget exhausted, decided on the candidates evaluated so far" << VAR(kMaxExtraPlans)
                << VAR(candidates.size());
    }
    if (!picker.best()) {
        return no_zipline(
            walking_baseline_available ? "no zipline route beats walking" : "no feasible zipline bridge to target",
            picker.truncated() || picker.interrupted() ? nullptr : &ZiplineOutcome::not_chosen);
    }

    std::optional<ZiplineRoute> best = assemble_route(in, sites, graph, *picker.best());
    LogInfo << "ZiplineRoute: picked" << VAR(walking_baseline_available) << VAR(baseline_length) << VAR(best->cost)
            << VAR(best->towers.size()) << VAR(best->towers.front().x) << VAR(best->towers.front().y) << VAR(best->towers.back().x)
            << VAR(best->towers.back().y);
    g_outcome.used = true;
    return best;
}

} // namespace mapnavigator
