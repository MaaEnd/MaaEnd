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

#include "../Zipline/ZiplineStore.h"
#include "navi_controller.h"
#include "navmesh_path_expander.h"

namespace mapnavigator
{

namespace
{

// 这次寻路里滑索的去向，由 no_zipline 与选中分支写入。逐条腿累加而不是覆盖：
// 只留最后一条腿的结论会把前面用上滑索的那些说没。清零、规划、取用同在寻路入口那一次
// 调用里跑完，thread_local 就把并发请求各自的账分开了。
thread_local bool g_zipline_used = false;
thread_local bool g_zipline_account_unknown = false;
thread_local bool g_zipline_no_data = false;
thread_local bool g_zipline_not_chosen = false;

// 一次请求里最多额外跑几条 navmesh 规划。候选是成对的，不设上限的话，滑索密集的地图
// 会把规划耗时抬高一个量级；触顶后只拿已经算出来的候选做决策，并在日志里说明截断。
constexpr size_t kMaxExtraPlans = 12;

// 供电结构离架子这么近才谈得上抢走交互面板, 更远的没必要给它让位。
constexpr double kMountPoleClearPx = 5.0;
// 让开量。面板要架子是离身位最近的那台设备, 也要人还够得着架子 —— 让开量加上判定圈得留在
// 够得着的那一圈里, 所以只让开判定圈的零头; 让多了就是把人推出去, 面板一样不出来。
constexpr double kMountStandOffsetPx = 0.75;
// 让开后的落脚点自己也得贴着同一层的可走面才算数: 规划只验过架子周围那一圈有面。
constexpr double kMountStandSnapRadiusPx = 2.0;
constexpr double kMountStandSnapTolPx = 1.0;

struct ZiplineData
{
    zipline::ZiplineFrames frames;
    zipline::ZiplineStore store;
    bool ok = false;
};

// 文件不存在时返回零值，与「读得到但是空的」区分不开也没关系：两者都不该触发重载。
std::filesystem::file_time_type FileStamp(const std::filesystem::path& path)
{
    std::error_code ec;
    const auto stamp = std::filesystem::last_write_time(path, ec);
    return ec ? std::filesystem::file_time_type {} : stamp;
}

// 标定与滑索记录都是只读的，但导入动作会在同一次运行里改写它们，所以按 mtime 决定重不重读：
// 用户导完立刻试路线是必然的用法，缓存住第一次的空记录会让人以为滑索没生效。
// 交出的是快照指针，重载只换指针，调用方手里那份不会被改到。
std::shared_ptr<const ZiplineData> SharedData()
{
    static std::mutex mutex;
    static std::shared_ptr<const ZiplineData> cached;
    static std::filesystem::file_time_type frames_stamp {};
    static std::filesystem::file_time_type store_stamp {};

    const std::filesystem::path frames_path = zipline::ZiplineFrames::DefaultPath();
    const std::filesystem::path store_path = zipline::ZiplineStore::DefaultPath();
    const auto frames_now = FileStamp(frames_path);
    const auto store_now = FileStamp(store_path);

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

double Distance(const navmesh::WorldPoint& a, const navmesh::WorldPoint& b)
{
    return std::hypot(b.x - a.x, b.y - a.y);
}

// 两点之间还有没有可能连通。类号集合都是有序的，扫一遍就够。任一边空着是「问不出来」
// 而不是「不连通」，这种时候一律当作可能连通，让规划自己去判。
bool MayConnect(const std::vector<uint32_t>& a, const std::vector<uint32_t>& b)
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

// 一个供电结构的供电范围。规则按 templateId 查一次就够，别放进逐根架子的内层循环。
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

// 森空岛 saveMarks 的 pos 不是建筑中心，而是会随朝向落在不同角格上的锚点；响应又不带
// 朝向。实测同一台 3x3 中继器原地旋转后，pos 会沿一轴跳 2 格，正好等于占地边长减一。
// 因此单个标记无法唯一还原中心，只能把双方所有可能的中心位置都纳入判定：
//
//   真实中心允许的轴差 = 供电覆盖半宽 + 架子占地半宽
//   标记到真实中心的不确定性 = 供电结构占地半宽 + 架子占地半宽
//
// 两项相加得到标记点允许的轴差。这里选择“任一可能朝向能重合就保留”，是为了避免把实际
// 通电的滑索提前挡在规划图外；在森空岛提供朝向前，代价是边界上可能保留少量未通电架子。
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

// 7x7 中继器与 3x3 滑索的中心重合边界是 4；双方角格锚点各引入 1 格不确定性，
// 所以标记点边界是 6。实测误判的 (5, 2) 必须保留，任一轴到 7 才能确定不重合。
static_assert(grid_areas_may_overlap(5.0, 2.0, { 7, 7 }, { 3, 3 }, { 3, 3 }));
static_assert(grid_areas_may_overlap(6.0, 6.0, { 7, 7 }, { 3, 3 }, { 3, 3 }));
static_assert(!grid_areas_may_overlap(0.0, 7.0, { 7, 7 }, { 3, 3 }, { 3, 3 }));

// 这根架子通不通电。只量原始世界坐标的水平 x/z；架子与供电结构的高低差不进判据。
bool IsPowered(const zipline::ZiplineMark& tower, const std::array<int, 2>& tower_footprint, const std::vector<SupplyPoint>& supplies)
{
    return std::any_of(supplies.begin(), supplies.end(), [&](const SupplyPoint& supply) {
        if (supply.coverage_size[0] > 0 && supply.coverage_size[1] > 0) {
            return grid_areas_may_overlap(supply.x - tower.x, supply.z - tower.z, supply.coverage_size, supply.footprint, tower_footprint);
        }
        return std::hypot(supply.x - tower.x, supply.z - tower.z) <= supply.radius;
    });
}

navmesh::WorldPoint ToWorld(const zipline::ZiplineNode& node)
{
    return navmesh::WorldPoint { .x = node.x, .y = node.y };
}

// 架子可能的塔心，原始世界坐标的 x/z。锚点落在随朝向变化的角格上，塔心在每条水平轴上可能偏出占地
// 半宽，朝向不在记录里，所以每个角都算一个塔心。只占一格时锚点就是塔心。
std::vector<std::array<double, 2>> TowerCenters(const zipline::ZiplineNode& node, const std::array<int, 2>& footprint)
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

// 各个可能的塔心投到像素平面。偏移按格在原始世界坐标里算，再走投影：地图比例不进这里。
std::vector<navmesh::WorldPoint>
    CenterSpots(const zipline::ZiplineFrame& frame, const zipline::ZiplineNode& tower, const std::array<int, 2>& footprint)
{
    std::vector<navmesh::WorldPoint> spots;
    for (const auto& [x, z] : TowerCenters(tower, footprint)) {
        const zipline::ZiplineMark shifted {
            .template_id = tower.template_id,
            .level_id = tower.level_id,
            .x = x,
            .y = tower.world_y,
            .z = z,
        };
        spots.push_back(ToWorld(frame.project(shifted)));
    }
    return spots;
}

// 上索认不出提示时改站哪。沿「供电结构 → 架子」把落脚点往外挪一点点, 让架子重新成为离身位
// 最近的那台设备; 挪出去的点贴不住同一层的面, 或者旁边压根没有供电结构, 就不给这个备选。
std::optional<navmesh::WorldPoint> MountStandPoint(
    const NaviParam& param,
    const std::string& locator_zone,
    const zipline::ZiplineNode& tower,
    const std::vector<navmesh::WorldPoint>& supplies)
{
    const navmesh::WorldPoint here = ToWorld(tower);
    const navmesh::WorldPoint* nearest = nullptr;
    double nearest_distance = kMountPoleClearPx;
    for (const navmesh::WorldPoint& supply : supplies) {
        const double distance = Distance(here, supply);
        if (distance < nearest_distance) {
            nearest_distance = distance;
            nearest = &supply;
        }
    }
    // 正好重合时没有方向可推
    if (nearest == nullptr || nearest_distance < 1e-3) {
        return std::nullopt;
    }
    const navmesh::WorldPoint stand {
        .x = here.x + (here.x - nearest->x) / nearest_distance * kMountStandOffsetPx,
        .y = here.y + (here.y - nearest->y) / nearest_distance * kMountStandOffsetPx,
    };
    const auto snap = NavmeshSnapAt(param, locator_zone, stand, kMountStandSnapRadiusPx, tower.height);
    if (!snap || snap->distance > kMountStandSnapTolPx || std::abs(snap->height - tower.height) > navmesh::kBaseNavFloorBand) {
        return std::nullopt;
    }
    LogDebug << "ZiplineRoute: keeping a spot clear of the power structure next to the mount tower" << VAR(tower.x) << VAR(tower.y)
             << VAR(stand.x) << VAR(stand.y) << VAR(nearest_distance);
    return stand;
}

// 上索依次走到哪。坐标记的是随朝向变化的角格锚点, 设备模型占着锚点四周哪一格未知; 互动要人面朝
// 设备且身位与模型有交集, 走到锚点上两条都不一定成立, 所以把各个可能的中心格排成候选逐个试。
// 取中心格而不取更深处: 行进中的提示预筛按到航点的距离开窗, 目标越往模型里推, 身位被挡住那一刻
// 离航点越远, 越容易落在窗外。顺着进场方向的那个排前面, 少一次掉头。
std::vector<navmesh::WorldPoint> MountSpots(
    const NaviParam& param,
    const std::string& locator_zone,
    const zipline::ZiplineFrame& frame,
    const zipline::ZiplineNode& tower,
    const std::array<int, 2>& footprint,
    const navmesh::WorldPoint& approach_from,
    const std::vector<navmesh::WorldPoint>& supplies)
{
    const navmesh::WorldPoint anchor = ToWorld(tower);
    const double half_x = grid_half_span(footprint[0]);
    const double half_z = grid_half_span(footprint[1]);
    // 进场方向取最后一段走路; 起点已经站在架子跟前时这两个分量都是零, 排序退化成固定顺序
    const double toward_x = anchor.x - approach_from.x;
    const double toward_y = anchor.y - approach_from.y;
    std::vector<std::pair<double, navmesh::WorldPoint>> ranked;
    for (const navmesh::WorldPoint& spot : CenterSpots(frame, tower, footprint)) {
        const auto snap = NavmeshSnapAt(param, locator_zone, spot, kMountStandSnapRadiusPx, tower.height);
        if (!snap || snap->distance > kMountStandSnapTolPx || std::abs(snap->height - tower.height) > navmesh::kBaseNavFloorBand) {
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
        // 只占一格时锚点就是中心; 几个中心格全贴不住同一层的面时也只剩它
        spots.push_back(anchor);
    }
    if (const std::optional<navmesh::WorldPoint> stand = MountStandPoint(param, locator_zone, tower, supplies)) {
        spots.push_back(*stand);
    }
    LogDebug << "ZiplineRoute: stand points to try at the mount tower" << VAR(spots.size()) << VAR(anchor.x) << VAR(anchor.y) << VAR(half_x)
             << VAR(half_z);
    return spots;
}

// 森空岛只给随朝向变化的角格锚点，双方中心在每条水平轴上都可能朝彼此靠近各自的
// 占地半宽。高度坐标不受朝向影响，原样计入三维距离。返回平方值供配对内层循环直接比较。
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

// 折线自身的长度。BaseNavRouteResult::cost 是搜索代价，不是几何长度，两者不能混用。
double PolylineLength(const navmesh::WorldPath& path)
{
    double total = 0.0;
    for (size_t i = 0; i + 1 < path.points.size(); ++i) {
        total += Distance(path.points[i], path.points[i + 1]);
    }
    return total;
}

constexpr size_t kNoTower = std::numeric_limits<size_t>::max();

// 索线中段横向近到这个距离还有另一根通电架子，这条直索就只算不确定边：滑行可能终止在中间
// 那根上。三次实测终止在中间架子，拦路架横向 10.14 / 11.23 / 14.99；另有一次中段立着横向 4.55
// 的架子，人却整条滑完了。横向距离分不开这四例，架子朝向又不在记录里，所以这条规则只降档不删边，
// 取值取到盖住已知被拦的 14.99 为止。
constexpr double kRopeInterceptLateralWu = 15.0;

// 架子把人从脚下地面抬到绳端高度，滑到那头也保持这个高度。判索挂不挂得住只能按抬升后的两端连
// 线量：按塔底量的是贴地那条线，几乎每根索都会被判成撞地。参与规划的滑索架与长距滑索架，绳端都
// 在塔底上方 5.72 m；绳从梁底下几厘米钻过去的线要按这个高度才判得通。
constexpr double kRopeEndHeightWu = 5.72;

// 这根架子的绳端可能在哪：每个可能的塔心正上方绳端高度处。
std::vector<navmesh::OccluderPoint> RopeEnds(const zipline::ZiplineNode& node, const std::array<int, 2>& footprint)
{
    std::vector<navmesh::OccluderPoint> ends;
    for (const auto& [x, z] : TowerCenters(node, footprint)) {
        ends.push_back(navmesh::OccluderPoint { .x = x, .y = node.world_y + kRopeEndHeightWu, .z = z });
    }
    return ends;
}

// 一根候选索两端各取可能的塔心，索长够得着的那几组绳端连线。
struct RopeCandidate
{
    size_t a = 0;
    size_t b = 0;
    std::vector<NavmeshAirLine> lines;
};

// 候选图分两档。all 收下两端可能的塔心之间有一组连线在索长以内的边；certain 只收两件事同时成立的：
// 中段没有拦路架、索长以内的那几组连线里有一组中途不撞实体。任一条不成立只降一档，边仍留在 all 里，
// 所以没有哪条规则能把一根索从图里抹掉，阈值取错只改变偏好顺序。
struct ZipLinkGraph
{
    std::vector<std::vector<size_t>> all;
    std::vector<std::vector<size_t>> certain;
    // 没被拦路架拦下、还要问遮挡体才能进 certain 的边
    std::vector<RopeCandidate> ropes;
};

void DropEdge(std::vector<std::vector<size_t>>& links, size_t a, size_t b)
{
    const auto drop = [](std::vector<size_t>& outgoing, size_t target) {
        outgoing.erase(std::remove(outgoing.begin(), outgoing.end(), target), outgoing.end());
    };
    drop(links[a], b);
    drop(links[b], a);
}

// from→to 的索中段拦着另一根架子时返回它的横向距离。拦路架需离两端各超过一个拦截半径，更贴近
// 端点的即端点自身；它与 from 之间也要挂得上索，从 from 够不到的架子接不住滑过来的人。
// nodes 已完成通电筛选，不承载索的架子不构成拦截。
std::optional<double> RopeIntercepted(
    const std::vector<zipline::ZiplineNode>& nodes,
    const std::vector<size_t>& node_map,
    size_t from,
    size_t to,
    const std::vector<double>& span_limit,
    const std::vector<std::array<int, 2>>& footprints)
{
    const double dx = nodes[to].world_x - nodes[from].world_x;
    const double dz = nodes[to].world_z - nodes[from].world_z;
    const double span = std::hypot(dx, dz);
    if (span <= 2.0 * kRopeInterceptLateralWu) {
        return std::nullopt;
    }
    for (size_t k = 0; k < nodes.size(); ++k) {
        if (k == from || k == to || node_map[k] != node_map[from]) {
            continue;
        }
        const double offset_x = nodes[k].world_x - nodes[from].world_x;
        const double offset_z = nodes[k].world_z - nodes[from].world_z;
        const double along = (offset_x * dx + offset_z * dz) / span;
        if (along <= kRopeInterceptLateralWu || along >= span - kRopeInterceptLateralWu) {
            continue;
        }
        const double lateral_x = offset_x - dx * along / span;
        const double lateral_z = offset_z - dz * along / span;
        const double lateral = std::hypot(lateral_x, lateral_z);
        if (lateral > kRopeInterceptLateralWu) {
            continue;
        }
        const double limit = std::min(span_limit[from], span_limit[k]);
        const double offset_y = nodes[k].world_y - nodes[from].world_y;
        if (minimum_possible_world_span_squared(offset_x, offset_y, offset_z, footprints[from], footprints[k]) > limit * limit) {
            continue;
        }
        return lateral;
    }
    return std::nullopt;
}

// 哪两根架子之间挂着索，记录本身没说，这里按几何推断：同一张图上两根架子任一可能中心的
// 三维距离不超过两者索长上限里较小的那个，就当它们之间有一条候选索，不看架子类型、也不看在哪个区。
// 索不分上下行，所以两个方向都算。位置含糊时优先保留候选；推错后执行侧会封掉失败边并重新规划，
// 推漏则整条连续链都无法发现。
//
// 中段站着另一根架子的直索降到不确定档：滑行可能终止在中间那根上，这条边的实际形态是经过它的
// 两跳链。实测里同样的几何有滑完整条的，所以只降不删，确定边够用时自然轮不到它。
ZipLinkGraph BuildLinks(
    const std::vector<zipline::ZiplineNode>& nodes,
    const std::vector<size_t>& node_map,
    const std::vector<double>& span_limit,
    const std::vector<std::array<int, 2>>& footprints,
    const std::vector<std::vector<navmesh::OccluderPoint>>& rope_ends)
{
    ZipLinkGraph graph;
    graph.all.resize(nodes.size());
    graph.certain.resize(nodes.size());
    // 逐条记下拦路架的横向距离：kRopeInterceptLateralWu 的依据只有四条实测索，实机日志里攒起来的
    // 这个分布才是后续调它的证据。
    std::vector<double> demoted_lateral;
    for (size_t i = 0; i < nodes.size(); ++i) {
        if (span_limit[i] <= 0.0) {
            continue;
        }
        for (size_t j = i + 1; j < nodes.size(); ++j) {
            if (node_map[i] != node_map[j] || span_limit[j] <= 0.0) {
                continue;
            }
            const double limit = std::min(span_limit[i], span_limit[j]);
            const double limit_squared = limit * limit;
            // 各轴取最近那一对塔心都够不着的，逐组量也够不着，省下内层的逐组循环
            const double span_squared = minimum_possible_world_span_squared(
                nodes[j].world_x - nodes[i].world_x,
                nodes[j].world_y - nodes[i].world_y,
                nodes[j].world_z - nodes[i].world_z,
                footprints[i],
                footprints[j]);
            if (span_squared > limit_squared) {
                continue;
            }
            // 两端各取每个可能的塔心逐组量绳长，有一组在索长以内这根索就进候选；够得着的那几组留给遮挡体问
            std::vector<NavmeshAirLine> lines;
            for (const navmesh::OccluderPoint& end_a : rope_ends[i]) {
                for (const navmesh::OccluderPoint& end_b : rope_ends[j]) {
                    const double dx = end_b.x - end_a.x;
                    const double dy = end_b.y - end_a.y;
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
            if (const std::optional<double> lateral = RopeIntercepted(nodes, node_map, i, j, span_limit, footprints)) {
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

// 从 source 出发，沿索能到的每一根架子和到它最省的滑法。到不了的架子留 infinity。
// 边权里每一跳都摊了一次换乘开销，链头那一次不该收，由调用方减回去。
void SolveZipChains(
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
            // 判连通用世界距离，算代价用像素距离：后者要和 navmesh 的路径长度加在一起比较。
            const double hop = Distance(ToWorld(nodes[at]), ToWorld(nodes[next])) * cost.speed_ratio + cost.transfer_penalty;
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

// 同一个滑索点会被多个候选对共用，按下标缓存，避免同一段路重复规划。
// 缓存里存 nullopt 表示这段确实规划不出来，下次不必再试。
class LegCache
{
public:
    using Planner = std::function<std::optional<PlannedLeg>(size_t)>;

    LegCache(Planner planner, size_t& budget)
        : planner_(std::move(planner))
        , budget_(budget)
    {
    }

    // 返回 nullopt 表示这段不可达或预算已经用完。
    const std::optional<PlannedLeg>* get(size_t index)
    {
        auto it = cache_.find(index);
        if (it != cache_.end()) {
            return &it->second;
        }
        if (budget_ == 0) {
            return nullptr;
        }
        --budget_;
        auto [inserted, ok] = cache_.emplace(index, planner_(index));
        return &inserted->second;
    }

private:
    Planner planner_;
    size_t& budget_;
    std::unordered_map<size_t, std::optional<PlannedLeg>> cache_;
};

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
    g_zipline_used = false;
    g_zipline_account_unknown = false;
    g_zipline_no_data = false;
    g_zipline_not_chosen = false;
}

ZiplineOutcome CurrentZiplineOutcome()
{
    return ZiplineOutcome {
        .used = g_zipline_used,
        .account_unknown = g_zipline_account_unknown,
        .no_data = g_zipline_no_data,
        .not_chosen = g_zipline_not_chosen,
    };
}

std::optional<ZiplineRoute> PlanZiplineRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const std::string& navmesh_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    const navmesh::WorldPath* walking_path,
    std::optional<double> goal_deck_y,
    std::optional<double> start_floor_y,
    const std::function<bool()>& should_stop,
    bool capture_diagnostics)
{
    // 没写 zip 的请求在这里就走完了：不读标定、不读记录、不多跑一条规划。
    if (!param.zipline_enabled) {
        return std::nullopt;
    }
    if (param.zipline_account_id.empty()) {
        g_zipline_account_unknown = true;
        return std::nullopt;
    }

    const bool walking_baseline_available = walking_path != nullptr && !walking_path->points.empty();

    // 要了滑索却没用上时，说清楚是输给了步行，还是没能桥接原本不连通的两端。
    // outcome 指向这条腿该记进哪本账：「这里没滑索可用」和「有滑索但没选中」指向的操作不同，
    // 混成一句会把用户带偏。传 nullptr 表示这条腿不记账，理由见各调用点。
    const auto no_zipline = [&](const char* why, bool* outcome) -> std::optional<ZiplineRoute> {
        if (walking_baseline_available) {
            LogInfo << "ZiplineRoute: walking this leg instead." << VAR(why) << VAR(navmesh_zone);
        }
        else {
            LogInfo << "ZiplineRoute: no bridge for the disconnected walking leg." << VAR(why) << VAR(navmesh_zone);
        }
        if (outcome) {
            *outcome = true;
        }
        return std::nullopt;
    };

    const std::shared_ptr<const ZiplineData> data = SharedData();
    if (!data->ok || data->frames.empty()) {
        return no_zipline("no zipline calibration on disk", &g_zipline_no_data);
    }

    const zipline::ZiplineFrame* frame = data->frames.findByZone(navmesh_zone);
    if (!frame) {
        // 没标定的区按「本来就没滑索」处理，不记账也就不提示：叫用户去导入坐标救不了这里，
        // 已导入的人还会被引去做一次白工。
        return no_zipline("this zone is not calibrated", nullptr);
    }

    // 不通电的滑索架在游戏里走不了，规划前先按供电范围把它们挡掉。
    const bool require_power = data->frames.hasPowerSources();

    // map_id 留空表示这个区还没绑定到具体哪张森空岛地图，此时已导入的标记全部纳入候选：
    // 坐标对不上的那些接不上网格，在规划预算之内就被淘汰掉。
    std::vector<zipline::ZiplineNode> nodes;
    // 每根架子出自哪一份记录。一份记录就是一张图，不同图的架子之间挂不上索
    std::vector<size_t> node_map;
    // 供电结构的落点也投一份到像素平面: 通电判定用的是世界坐标, 而让位算的是人站在哪
    std::vector<navmesh::WorldPoint> supply_points;
    size_t unpowered = 0;
    const std::vector<zipline::ZiplineMapRecord>& records = data->store.maps();
    for (size_t record_index = 0; record_index < records.size(); ++record_index) {
        const zipline::ZiplineMapRecord& record = records[record_index];
        if (record.account_id != param.zipline_account_id) {
            continue;
        }
        if (!frame->map_id.empty() && record.map_id != frame->map_id) {
            continue;
        }

        // 供电结构和滑索架在同一份记录里，先把这张图的电网收齐，再逐根架子判。
        std::vector<SupplyPoint> supplies;
        for (const auto& mark : record.marks) {
            const zipline::ZiplinePowerSource* source = data->frames.powerSource(mark.template_id);
            if (source != nullptr) {
                supplies.push_back(SupplyPoint {
                    .x = mark.x,
                    .z = mark.z,
                    .radius = source->radius,
                    .footprint = source->footprint,
                    .coverage_size = source->coverage_size,
                });
                supply_points.push_back(ToWorld(frame->project(mark)));
            }
        }
        if (require_power && supplies.empty()) {
            LogWarn << "ZiplineRoute: no power structures on record, every zipline here counts as unpowered" << VAR(record.map_id);
        }

        for (const auto& mark : record.marks) {
            if (!frame->accepts(mark)) {
                continue;
            }
            if (require_power && !IsPowered(mark, data->frames.footprint(mark.template_id), supplies)) {
                ++unpowered;
                continue;
            }
            nodes.push_back(frame->project(mark));
            node_map.push_back(record_index);
        }
    }
    if (unpowered != 0) {
        LogDebug << "ZiplineRoute: left out the ziplines no power reaches" << VAR(unpowered) << VAR(nodes.size());
    }

    if (nodes.size() < 2) {
        return no_zipline("no powered ziplines recorded in this zone", &g_zipline_no_data);
    }

    // 有步行基线时，只有省下 min_gain 以上才算有收益：省得比一次上索的开销还少时，这点
    // 便宜落在代价模型自身的误差里，换来的却是实打实的多一次交互。纯步行不连通时没有
    // 可比较的基线，任何能把起终两侧可走面接起来的连续链都优先于盲走和作者路径回退。
    const zipline::ZiplineCostModel& cost = data->frames.cost();
    const double baseline_length = walking_baseline_available ? PolylineLength(*walking_path) : 0.0;
    const double gain_threshold = walking_baseline_available ? baseline_length - cost.min_gain : std::numeric_limits<double>::infinity();
    // 走路短到白送一整段滑行都追不平上索的开销时，下面的吸附和规划都不必做了。
    if (walking_baseline_available && gain_threshold <= cost.mount_penalty) {
        return no_zipline("the walk is too short for any zipline to pay off", &g_zipline_not_chosen);
    }

    // 只筛两端：上索点和下索点得让人走到跟前，链中间那些是从索上落到下一根架子上的，脚下有没有
    // 可走面都不影响。平面距离和高度两道一起判——可走面在同一片平面坐标上能摞好几层，只比平面
    // 距离的话，架在楼顶而脚下那层在楼底也算「够得着」，人走过去才发现头顶上什么都没有。
    // 锚点只是占地里的一个角格，架子贴着台沿时锚点可能悬在台外、塔心却在台上，所以锚点和每个可能的
    // 塔心都当一个站点逐个吸附，贴得住一个就算够得着。锚点排第一，锚点接得上时规划次数与只看锚点一样。
    std::vector<std::array<int, 2>> footprints(nodes.size());
    std::vector<navmesh::WorldPoint> spots;
    std::vector<size_t> spot_tower;
    std::vector<std::vector<size_t>> tower_spots(nodes.size());
    size_t out_of_reach = 0;
    size_t wrong_floor = 0;
    for (size_t i = 0; i < nodes.size(); ++i) {
        footprints[i] = data->frames.footprint(nodes[i].template_id);
        const navmesh::WorldPoint anchor = ToWorld(nodes[i]);
        std::vector<navmesh::WorldPoint> tries { anchor };
        for (const navmesh::WorldPoint& center : CenterSpots(*frame, nodes[i], footprints[i])) {
            if (center.x != anchor.x || center.y != anchor.y) {
                tries.push_back(center);
            }
        }
        bool in_reach = false;
        for (const navmesh::WorldPoint& point : tries) {
            const auto snap = NavmeshSnapAt(param, locator_zone, point, cost.reach_radius, nodes[i].height);
            if (!snap || snap->distance > cost.reach_radius) {
                continue;
            }
            in_reach = true;
            if (std::abs(snap->height - nodes[i].height) > navmesh::kBaseNavFloorBand) {
                continue;
            }
            tower_spots[i].push_back(spots.size());
            spots.push_back(point);
            spot_tower.push_back(i);
        }
        if (tower_spots[i].empty()) {
            ++(in_reach ? wrong_floor : out_of_reach);
        }
    }
    if (out_of_reach != 0 || wrong_floor != 0) {
        LogDebug << "ZiplineRoute: these ziplines can be ridden through but not boarded" << VAR(out_of_reach) << VAR(wrong_floor)
                 << VAR(nodes.size());
    }
    // 一根都上不去时后面的配对必然是空的，早一步收场；原因也要说成「上不去」而不是「不划算」，
    // 前者多半是标定或高度对不上，后者才是真的不划算。
    if (out_of_reach + wrong_floor == nodes.size()) {
        return no_zipline("not one zipline here can be walked up to", &g_zipline_not_chosen);
    }

    // 脚下有面不等于走得到。一条路线只在单一连通类里搜，所以上索点得跟角色同类、下索点
    // 得跟送货点同类；不同类的架子规划必败，却照样按直线距离排在前头，把预算烧个精光。
    // 判据只排除已知必败的组合：类号问不出来时按可能连通处理，宁可白跑一条也不误杀。
    // 逐个站点判，同一根架子只要有一个站点跟起点同类就能上索，有一个跟终点同类就能落地。
    std::vector<navmesh::WorldPoint> probes = spots;
    probes.push_back(start);
    probes.push_back(goal);
    const std::vector<std::vector<uint32_t>> regions = NavmeshRegionsNear(param, locator_zone, probes);
    std::vector<std::vector<size_t>> board_spots(nodes.size());
    std::vector<std::vector<size_t>> land_spots(nodes.size());
    size_t cut_off = 0;
    for (size_t i = 0; i < nodes.size(); ++i) {
        for (const size_t spot : tower_spots[i]) {
            if (MayConnect(regions[spot], regions[spots.size()])) {
                board_spots[i].push_back(spot);
            }
            if (MayConnect(regions[spot], regions[spots.size() + 1])) {
                land_spots[i].push_back(spot);
            }
        }
        if (!tower_spots[i].empty() && board_spots[i].empty() && land_spots[i].empty()) {
            ++cut_off;
        }
    }
    if (cut_off != 0) {
        LogDebug << "ZiplineRoute: these ziplines share no walkable ground with either end" << VAR(cut_off) << VAR(nodes.size());
    }
    // 执行侧每个站位都走到过、一次上索提示都没出来的架子, 这一趟不再当上索点。当落点照旧:
    // 人是从索上落到架子上的, 上不去跟够不着是两件事
    size_t unboardable = 0;
    for (const ZiplineHopRecord& record : param.zipline_ledger) {
        if (record.outcome != HopOutcome::Unboardable) {
            continue;
        }
        for (size_t i = 0; i < nodes.size(); ++i) {
            if (!board_spots[i].empty() && ToNodeRef(nodes[i]).SameTower(record.plan.mount)) {
                board_spots[i].clear();
                ++unboardable;
            }
        }
    }
    if (unboardable != 0) {
        LogInfo << "ZiplineRoute: left out the towers the runtime never managed to board." << VAR(unboardable) << VAR(nodes.size());
    }
    // 一头都接不上时后面配对必然是空的。链中间的架子仍然全留着，那些是从索上落下去的。
    const auto none_usable = [](const std::vector<std::vector<size_t>>& per_tower) {
        return std::all_of(per_tower.begin(), per_tower.end(), [](const std::vector<size_t>& v) { return v.empty(); });
    };
    if (none_usable(board_spots) || none_usable(land_spots)) {
        return no_zipline("no zipline can be boarded from here, or none of them lands near the destination", &g_zipline_not_chosen);
    }

    // 索长上限逐点查一次就够：配对是 O(n²) 的，放进内层循环等于把字符串查表也乘上 n²。
    // 查不到的类型上限为 0，下面直接跳过——没登记过物理属性的架子不参与配对。
    // 两个下界取各站点里离得最近的那个，规划落在哪个站点都不会比它短。
    std::vector<double> span_limit(nodes.size());
    std::vector<double> lb_from_start(nodes.size(), std::numeric_limits<double>::infinity());
    std::vector<double> lb_to_goal(nodes.size(), std::numeric_limits<double>::infinity());
    for (size_t i = 0; i < nodes.size(); ++i) {
        span_limit[i] = data->frames.maxSpan(nodes[i].template_id);
        for (const size_t spot : board_spots[i]) {
            lb_from_start[i] = std::min(lb_from_start[i], Distance(start, spots[spot]));
        }
        for (const size_t spot : land_spots[i]) {
            lb_to_goal[i] = std::min(lb_to_goal[i], Distance(spots[spot], goal));
        }
    }

    struct Candidate
    {
        size_t mount = 0;
        size_t dismount = 0;
        // 上索到落地这一整段，已经含上索和沿途每一次换乘。
        double zip_cost = 0.0;
        double lower_bound = 0.0;
        // 该链解自确定边子图。还原整条链与列举备选架子时须使用同一张图。
        bool certain = false;
    };

    // 一条路线用几条索由代价决定，不设跳数上限：换乘要收钱，划不来的长链自己就被淘汰了。
    std::vector<std::vector<navmesh::OccluderPoint>> rope_ends(nodes.size());
    for (size_t i = 0; i < nodes.size(); ++i) {
        rope_ends[i] = RopeEnds(nodes[i], footprints[i]);
    }
    ZipLinkGraph graph = BuildLinks(nodes, node_map, span_limit, footprints, rope_ends);

    const auto drop_from_graph = [&graph](size_t a, size_t b) {
        DropEdge(graph.all, a, b);
        DropEdge(graph.certain, a, b);
    };

    // 两根架子之间隔着实体时挂不住索，这条边留在不确定档。每条边只量一次，两个方向一起定。
    // 索长以内的那几组塔心连线逐组问：有一组不撞就进确定档，问到它就停；全撞才留在不确定档。
    {
        std::vector<std::vector<NavmeshAirLine>> groups;
        groups.reserve(graph.ropes.size());
        for (RopeCandidate& rope : graph.ropes) {
            groups.push_back(std::move(rope.lines));
        }
        // 只判两个绳端之间那一条直线。架子本身不在这份数据里，挡不着自己的绳。
        const std::vector<std::vector<navmesh::OccluderHit>> blocks = NavmeshLineGroupBlocks(param, locator_zone, groups);
        // 逐条记下沿绳第一处碰撞离起端多少米，取各组塔心里撞得最晚的那条：现场排查「这根索为什么被降档」只有这一行。
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
    }

    // 执行侧账本里滑不动、滑错、落地丢了的跳直接从连通图里拿掉。索不分上下行, 一根滑不动的索
    // 反着大概率也滑不动, 两个方向一起封。
    if (!param.zipline_ledger.empty()) {
        const auto near_tower = [&nodes](size_t tower, const ZiplineNodeRef& ref) {
            return std::hypot(nodes[tower].x - ref.x, nodes[tower].y - ref.y) <= kZiplineHopBanMatchWu;
        };
        std::vector<std::pair<size_t, size_t>> banned;
        for (size_t i = 0; i < graph.all.size(); ++i) {
            for (const size_t j : graph.all[i]) {
                if (j <= i) {
                    continue;
                }
                for (const ZiplineHopRecord& record : param.zipline_ledger) {
                    if (!IsZiplineRopeFailure(record.outcome)) {
                        continue;
                    }
                    const ZiplineNodeRef& from = record.plan.mount;
                    const ZiplineNodeRef& to = record.plan.landing;
                    if ((near_tower(i, from) && near_tower(j, to)) || (near_tower(i, to) && near_tower(j, from))) {
                        banned.emplace_back(i, j);
                        break;
                    }
                }
            }
        }
        for (const auto& edge : banned) {
            drop_from_graph(edge.first, edge.second);
        }
        LogInfo << "ZiplineRoute: dropped the hops the runtime already gave up on." << VAR(param.zipline_ledger.size())
                << VAR(banned.size());
    }

    std::vector<Candidate> candidates;
    std::vector<double> chain_cost;
    std::vector<size_t> chain_prev;
    std::vector<double> certain_cost;
    std::vector<size_t> certain_prev;
    for (size_t i = 0; i < nodes.size(); ++i) {
        // 这根架子连白送一整段滑行都够不着收益门槛，从它起头的所有链就都不必算了。
        if (board_spots[i].empty() || graph.all[i].empty() || lb_from_start[i] + cost.mount_penalty >= gain_threshold) {
            continue;
        }

        SolveZipChains(nodes, graph.certain, cost, i, &certain_cost, &certain_prev);
        SolveZipChains(nodes, graph.all, cost, i, &chain_cost, &chain_prev);
        for (size_t j = 0; j < nodes.size(); ++j) {
            if (j == i || land_spots[j].empty()) {
                continue;
            }
            // 两档各记一个候选，不确定那档只在确实更便宜时才多记一条。哪档先用由下面的两趟
            // 评估决定：确定边能到达就用确定边，即使它比不确定的直索多几跳、代价更高。多跳可达
            // 优于一条可能不存在的索，推断错误要整段重新规划，代价高于多出的那几跳。
            const auto offer = [&](double chain, bool certain) {
                if (!std::isfinite(chain)) {
                    return;
                }
                const double zip_cost = cost.mount_penalty - cost.transfer_penalty + chain;
                const double lower_bound = lb_from_start[i] + zip_cost + lb_to_goal[j];
                if (lower_bound >= gain_threshold) {
                    return;
                }
                candidates.push_back(
                    Candidate { .mount = i, .dismount = j, .zip_cost = zip_cost, .lower_bound = lower_bound, .certain = certain });
            };
            offer(certain_cost[j], true);
            // 全图含确定图，所以松量链只在确实更便宜时才值得多记一条。
            if (chain_cost[j] < certain_cost[j]) {
                offer(chain_cost[j], false);
            }
        }
    }

    if (candidates.empty()) {
        // 摸得到、挂得上、还顺路的那一根不存在，三件事在这里合成一个结果。
        return no_zipline("no reachable pair of ziplines leads anywhere useful", &g_zipline_not_chosen);
    }
    std::sort(candidates.begin(), candidates.end(), [](const Candidate& a, const Candidate& b) { return a.lower_bound < b.lower_bound; });

    size_t plan_budget = kMaxExtraPlans;

    // 两段腿都按站点下标缓存、按站点规划。上索点的高度是导入数据里带来的逐点真值，直接钉住终点所在的那一层。
    LegCache approach_cache(
        [&](size_t index) -> std::optional<PlannedLeg> {
            NavmeshRouteDiagnostic diagnostic;
            auto route = PlanNavmeshRoute(
                param,
                locator_zone,
                start,
                spots[index],
                nodes[spot_tower[index]].height,
                start_floor_y,
                capture_diagnostics ? &diagnostic : nullptr);
            if (!route || !route->ok()) {
                return std::nullopt;
            }
            return PlannedLeg {
                .path = route->path,
                .length = PolylineLength(route->path),
                .diagnostic = std::move(diagnostic),
            };
        },
        plan_budget);

    // 下索点同理：角色是从索上落下来的，站在哪一层是已知的，不必让吸附去猜。
    // 终点面用调用方给的那个，与纯走路方案完全一致——换走法不换目的地。
    LegCache departure_cache(
        [&](size_t index) -> std::optional<PlannedLeg> {
            NavmeshRouteDiagnostic diagnostic;
            auto route = PlanNavmeshRoute(
                param,
                locator_zone,
                spots[index],
                goal,
                goal_deck_y,
                nodes[spot_tower[index]].height,
                capture_diagnostics ? &diagnostic : nullptr);
            if (!route || !route->ok()) {
                return std::nullopt;
            }
            return PlannedLeg {
                .path = route->path,
                .length = PolylineLength(route->path),
                .diagnostic = std::move(diagnostic),
            };
        },
        plan_budget);

    // 一根架子的站点按顺序规划，规划通一个就用它，后面的不再规划。每个站点各花一次预算，预算用完返回 nullptr。
    const auto first_leg = [](LegCache& cache, const std::vector<size_t>& candidates_of_tower, size_t* used_spot) {
        const std::optional<PlannedLeg>* leg = nullptr;
        for (const size_t spot : candidates_of_tower) {
            leg = cache.get(spot);
            *used_spot = spot;
            if (leg == nullptr || leg->has_value()) {
                break;
            }
        }
        return leg;
    };

    double best_cost = gain_threshold;
    std::optional<ZiplineRoute> best;
    std::optional<Candidate> best_candidate;
    size_t best_approach_spot = 0;
    size_t best_departure_spot = 0;
    bool truncated = false;
    bool interrupted = false;

    // 分两趟评估：先只比确定边的候选，一条都走不通才比带不确定边的。代价比较会让便宜的不确定
    // 直索赢过确定链，单靠排序换不来确定优先，得把两档隔开比。同一趟内候选仍按下界升序。
    const auto evaluate = [&](bool want_certain) {
        for (const auto& candidate : candidates) {
            if (candidate.certain != want_certain) {
                continue;
            }
            if (should_stop && should_stop()) {
                interrupted = true;
                break;
            }
            // 候选按下界升序，当前下界都追不上最好成绩时，后面的更追不上。
            if (candidate.lower_bound >= best_cost) {
                break;
            }

            size_t approach_spot = 0;
            const std::optional<PlannedLeg>* approach = first_leg(approach_cache, board_spots[candidate.mount], &approach_spot);
            if (!approach) {
                truncated = true;
                break;
            }
            if (!approach->has_value()) {
                continue;
            }

            // 上索段的真实长度到手，用它换掉欧氏下界再剪一次，省下终点段的规划。
            if ((*approach)->length + candidate.zip_cost + lb_to_goal[candidate.dismount] >= best_cost) {
                continue;
            }

            size_t departure_spot = 0;
            const std::optional<PlannedLeg>* departure = first_leg(departure_cache, land_spots[candidate.dismount], &departure_spot);
            if (!departure) {
                truncated = true;
                break;
            }
            if (!departure->has_value()) {
                continue;
            }

            const double total = (*approach)->length + candidate.zip_cost + (*departure)->length;
            if (total >= best_cost) {
                continue;
            }

            best_cost = total;
            best_candidate = candidate;
            best_approach_spot = approach_spot;
            best_departure_spot = departure_spot;
            best = ZiplineRoute {
                .approach = (*approach)->path,
                .departure = (*departure)->path,
                .cost = total,
            };
        }
    };
    evaluate(true);
    // 第一趟把预算烧光也算一次失败，第二趟照样要跑：缓存里已经规划好的腿是白拿的，碰到第一个
    // 没缓存的架子照旧记 truncated 退出，多跑这一趟不额外花预算。
    if (!best && !interrupted) {
        evaluate(false);
    }

    if (truncated) {
        LogWarn << "ZiplineRoute: plan budget exhausted, decided on the candidates evaluated so far" << VAR(kMaxExtraPlans)
                << VAR(candidates.size());
    }

    if (!best) {
        // 候选没评完就跑不出「都不划算」这个结论：预算耗尽或请求被叫停时空着手回去，
        // 免得把没比过的那些一起判了。
        return no_zipline(
            walking_baseline_available ? "no zipline route beats walking" : "no feasible zipline bridge to target",
            truncated || interrupted ? nullptr : &g_zipline_not_chosen);
    }

    // 中间经过哪几根架子到这时才还原：候选是成对枚举出来的，逐个存下整条链纯属浪费。
    const std::vector<std::vector<size_t>>& links = best_candidate->certain ? graph.certain : graph.all;
    SolveZipChains(nodes, links, cost, best_candidate->mount, &chain_cost, &chain_prev);
    std::vector<size_t> chain;
    for (size_t at = best_candidate->dismount; at != kNoTower; at = chain_prev[at]) {
        chain.push_back(at);
    }
    std::reverse(chain.begin(), chain.end());
    for (size_t hop = 0; hop < chain.size(); ++hop) {
        best->towers.push_back(nodes[chain[hop]]);
        if (hop + 1 < chain.size()) {
            std::vector<zipline::ZiplineNode> alternates;
            for (const size_t other : links[chain[hop]]) {
                if (other != chain[hop + 1]) {
                    alternates.push_back(nodes[other]);
                }
            }
            best->hop_alternates.push_back(std::move(alternates));
        }
    }
    if (capture_diagnostics) {
        const std::optional<PlannedLeg>* approach = approach_cache.get(best_approach_spot);
        const std::optional<PlannedLeg>* departure = departure_cache.get(best_departure_spot);
        if (approach != nullptr && approach->has_value()) {
            best->diagnostics.push_back((*approach)->diagnostic);
        }
        if (departure != nullptr && departure->has_value()) {
            best->diagnostics.push_back((*departure)->diagnostic);
        }
    }
    // 只有链首那一根要按提示上索, 中途都是从索上落到下一根架子上的
    const navmesh::WorldPoint& approach_from =
        best->approach.points.size() >= 2 ? best->approach.points[best->approach.points.size() - 2] : start;
    best->mount_spots = MountSpots(
        param,
        locator_zone,
        *frame,
        best->towers.front(),
        data->frames.footprint(best->towers.front().template_id),
        approach_from,
        supply_points);

    LogInfo << "ZiplineRoute: picked" << VAR(walking_baseline_available) << VAR(baseline_length) << VAR(best->cost)
            << VAR(best->towers.size()) << VAR(best->towers.front().x) << VAR(best->towers.front().y) << VAR(best->towers.back().x)
            << VAR(best->towers.back().y);
    g_zipline_used = true;
    return best;
}

} // namespace mapnavigator
