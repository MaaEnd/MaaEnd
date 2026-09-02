#include "zipline_leg_planner.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <limits>
#include <memory>
#include <mutex>
#include <queue>
#include <system_error>
#include <unordered_map>
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

// 哪两根架子之间挂着索，记录本身没说，这里按几何推断：同一种架子、同一层、任一可能中心的
// 世界距离不超过这种架子的索长上限，就当它们之间有一条候选索。索不分上下行，所以两个方向
// 都算。位置含糊时优先保留候选；推错后执行侧会封掉失败边并重新规划，推漏则整条连续链都无法发现。
std::vector<std::vector<size_t>> BuildLinks(
    const std::vector<zipline::ZiplineNode>& nodes,
    const std::vector<double>& span_limit,
    const std::vector<std::array<int, 2>>& footprints)
{
    std::vector<std::vector<size_t>> links(nodes.size());
    for (size_t i = 0; i < nodes.size(); ++i) {
        if (span_limit[i] <= 0.0) {
            continue;
        }
        for (size_t j = i + 1; j < nodes.size(); ++j) {
            if (nodes[i].template_id != nodes[j].template_id || nodes[i].level_id != nodes[j].level_id) {
                continue;
            }
            const double span_squared = minimum_possible_world_span_squared(
                nodes[j].world_x - nodes[i].world_x,
                nodes[j].world_y - nodes[i].world_y,
                nodes[j].world_z - nodes[i].world_z,
                footprints[i],
                footprints[j]);
            if (span_squared > span_limit[i] * span_limit[i]) {
                continue;
            }
            links[i].push_back(j);
            links[j].push_back(i);
        }
    }
    return links;
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

void ResetZiplineOutcome()
{
    g_zipline_used = false;
    g_zipline_no_data = false;
    g_zipline_not_chosen = false;
}

ZiplineOutcome CurrentZiplineOutcome()
{
    return ZiplineOutcome {
        .used = g_zipline_used,
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
    const std::function<bool()>& should_stop,
    bool capture_diagnostics)
{
    // 没写 zip 的请求在这里就走完了：不读标定、不读记录、不多跑一条规划。
    if (!param.zipline_enabled) {
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
    // 供电结构的落点也投一份到像素平面: 通电判定用的是世界坐标, 而让位算的是人站在哪
    std::vector<navmesh::WorldPoint> supply_points;
    size_t unpowered = 0;
    for (const auto& record : data->store.maps()) {
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
    std::vector<bool> reachable(nodes.size(), false);
    size_t out_of_reach = 0;
    size_t wrong_floor = 0;
    for (size_t i = 0; i < nodes.size(); ++i) {
        const auto snap = NavmeshSnapAt(param, locator_zone, ToWorld(nodes[i]), cost.reach_radius, nodes[i].height);
        if (!snap || snap->distance > cost.reach_radius) {
            ++out_of_reach;
            continue;
        }
        if (std::abs(snap->height - nodes[i].height) > navmesh::kBaseNavFloorBand) {
            ++wrong_floor;
            continue;
        }
        reachable[i] = true;
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

    // 索长上限逐点查一次就够：配对是 O(n²) 的，放进内层循环等于把字符串查表也乘上 n²。
    // 查不到的类型上限为 0，下面直接跳过——没登记过物理属性的架子不参与配对。
    std::vector<double> span_limit(nodes.size());
    std::vector<std::array<int, 2>> footprints(nodes.size());
    std::vector<double> lb_from_start(nodes.size());
    std::vector<double> lb_to_goal(nodes.size());
    for (size_t i = 0; i < nodes.size(); ++i) {
        span_limit[i] = data->frames.maxSpan(nodes[i].template_id);
        footprints[i] = data->frames.footprint(nodes[i].template_id);
        lb_from_start[i] = Distance(start, ToWorld(nodes[i]));
        lb_to_goal[i] = Distance(ToWorld(nodes[i]), goal);
    }

    struct Candidate
    {
        size_t mount = 0;
        size_t dismount = 0;
        // 上索到落地这一整段，已经含上索和沿途每一次换乘。
        double zip_cost = 0.0;
        double lower_bound = 0.0;
    };

    // 一条路线用几条索由代价决定，不设跳数上限：换乘要收钱，划不来的长链自己就被淘汰了。
    std::vector<std::vector<size_t>> links = BuildLinks(nodes, span_limit, footprints);

    // 执行侧判死过的跳直接从连通图里拿掉。索不分上下行, 一根滑不动的索反着大概率也滑不动,
    // 两个方向一起封。
    if (!param.banned_zipline_hops.empty()) {
        const auto near_tower = [&nodes](size_t tower, double x, double y) {
            return std::hypot(nodes[tower].x - x, nodes[tower].y - y) <= kZiplineHopBanMatchWu;
        };
        size_t banned_edges = 0;
        for (size_t i = 0; i < links.size(); ++i) {
            std::vector<size_t>& outgoing = links[i];
            outgoing.erase(
                std::remove_if(
                    outgoing.begin(),
                    outgoing.end(),
                    [&](size_t j) {
                        for (const ZiplineHopBan& ban : param.banned_zipline_hops) {
                            if ((near_tower(i, ban.from_x, ban.from_y) && near_tower(j, ban.to_x, ban.to_y))
                                || (near_tower(i, ban.to_x, ban.to_y) && near_tower(j, ban.from_x, ban.from_y))) {
                                ++banned_edges;
                                return true;
                            }
                        }
                        return false;
                    }),
                outgoing.end());
        }
        LogInfo << "ZiplineRoute: dropped the hops the runtime already gave up on." << VAR(param.banned_zipline_hops.size())
                << VAR(banned_edges);
    }

    std::vector<Candidate> candidates;
    std::vector<double> chain_cost;
    std::vector<size_t> chain_prev;
    for (size_t i = 0; i < nodes.size(); ++i) {
        // 这根架子连白送一整段滑行都够不着收益门槛，从它起头的所有链就都不必算了。
        if (!reachable[i] || links[i].empty() || lb_from_start[i] + cost.mount_penalty >= gain_threshold) {
            continue;
        }

        SolveZipChains(nodes, links, cost, i, &chain_cost, &chain_prev);
        for (size_t j = 0; j < nodes.size(); ++j) {
            if (j == i || !reachable[j] || !std::isfinite(chain_cost[j])) {
                continue;
            }
            const double zip_cost = cost.mount_penalty - cost.transfer_penalty + chain_cost[j];
            const double lower_bound = lb_from_start[i] + zip_cost + lb_to_goal[j];
            if (lower_bound >= gain_threshold) {
                continue;
            }
            candidates.push_back(Candidate { .mount = i, .dismount = j, .zip_cost = zip_cost, .lower_bound = lower_bound });
        }
    }

    if (candidates.empty()) {
        // 摸得到、挂得上、还顺路的那一根不存在，三件事在这里合成一个结果。
        return no_zipline("no reachable pair of ziplines leads anywhere useful", &g_zipline_not_chosen);
    }
    std::sort(candidates.begin(), candidates.end(), [](const Candidate& a, const Candidate& b) { return a.lower_bound < b.lower_bound; });

    size_t plan_budget = kMaxExtraPlans;

    // 上索点的高度是导入数据里带来的逐点真值，直接钉住终点所在的那一层。
    LegCache approach_cache(
        [&](size_t index) -> std::optional<PlannedLeg> {
            NavmeshRouteDiagnostic diagnostic;
            auto route = PlanNavmeshRoute(
                param,
                locator_zone,
                start,
                ToWorld(nodes[index]),
                nodes[index].height,
                std::nullopt,
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
                ToWorld(nodes[index]),
                goal,
                goal_deck_y,
                nodes[index].height,
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

    double best_cost = gain_threshold;
    std::optional<ZiplineRoute> best;
    std::optional<Candidate> best_candidate;
    bool truncated = false;
    bool interrupted = false;

    for (const auto& candidate : candidates) {
        if (should_stop && should_stop()) {
            interrupted = true;
            break;
        }
        // 候选按下界升序，当前下界都追不上最好成绩时，后面的更追不上。
        if (candidate.lower_bound >= best_cost) {
            break;
        }

        const std::optional<PlannedLeg>* approach = approach_cache.get(candidate.mount);
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

        const std::optional<PlannedLeg>* departure = departure_cache.get(candidate.dismount);
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
        best = ZiplineRoute {
            .approach = (*approach)->path,
            .departure = (*departure)->path,
            .cost = total,
        };
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
    SolveZipChains(nodes, links, cost, best_candidate->mount, &chain_cost, &chain_prev);
    for (size_t at = best_candidate->dismount; at != kNoTower; at = chain_prev[at]) {
        best->towers.push_back(nodes[at]);
    }
    std::reverse(best->towers.begin(), best->towers.end());
    if (capture_diagnostics) {
        const std::optional<PlannedLeg>* approach = approach_cache.get(best_candidate->mount);
        const std::optional<PlannedLeg>* departure = departure_cache.get(best_candidate->dismount);
        if (approach != nullptr && approach->has_value()) {
            best->diagnostics.push_back((*approach)->diagnostic);
        }
        if (departure != nullptr && departure->has_value()) {
            best->diagnostics.push_back((*departure)->diagnostic);
        }
    }
    // 只有链首那一根要按提示上索, 中途都是从索上落到下一根架子上的
    best->mount_restand = MountStandPoint(param, locator_zone, best->towers.front(), supply_points);

    LogInfo << "ZiplineRoute: picked" << VAR(walking_baseline_available) << VAR(baseline_length) << VAR(best->cost)
            << VAR(best->towers.size()) << VAR(best->towers.front().x) << VAR(best->towers.front().y) << VAR(best->towers.back().x)
            << VAR(best->towers.back().y);
    g_zipline_used = true;
    return best;
}

} // namespace mapnavigator
