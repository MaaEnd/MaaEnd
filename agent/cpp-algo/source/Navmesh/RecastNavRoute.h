#pragma once

#include <cstdint>
#include <functional>
#include <memory>
#include <mutex>
#include <optional>
#include <string>
#include <unordered_map>
#include <vector>

#include "BaseNavPack.h"
#include "BaseNavPlanner.h"
#include "RecastNavFieldsIO.h"
#include "RecastNavGridIO.h"
#include "RecastNavZone.h"

namespace navmesh::recast
{

struct RecastPlanResult
{
    bool ok = false;
    std::string error;
    std::vector<WorldPoint> points;
    std::vector<double> clearance; // 逐点通道半宽 px
    double length = 0.0;
    std::vector<std::string> warnings;
    double snap_start = 0.0; // 起/终点到可走格锚点距离 px
    double snap_goal = 0.0;
    // 贪心拉直后的驱动航点下标(points 的下标,不含起点,末位恒为 points.size()-1)。
    // 空 = 该腿没有层预言机,拉直交给调用方。
    std::vector<size_t> waypoints;

    // 规划各阶段的中间产物。每个数组恰好对应一段算法的出口, 看哪一段把线拐坏了就开哪一层。
    struct Debug
    {
        struct Timing
        {
            double window_ms = 0.0; // 建窗
            double topology_ms = 0.0; // 硬约束基线 + 通道拓扑
            double geometry_ms = 0.0; // 走廊内带视线重解
            double pull_ms = 0.0; // 取直
            double assemble_ms = 0.0; // 端点拼接、去重、共线去冗
            double lift_ms = 0.0; // 拐角抬升
            double total_ms = 0.0;
        } timing;

        double x0 = 0.0;
        double y0 = 0.0;
        int64_t nx = 0;
        int64_t ny = 0;
        double cell_size = 0.0;
        // 采信的是第几档窗口 (0 = 端点包围盒外扩的最小一档), 以及前面各档被否掉的原因。
        int tier = 0;
        std::vector<std::string> tier_notes;
        std::vector<WorldPoint> topology_cells; // 拓扑那一层的逐格路径
        std::vector<double> topology_heights; // 与 topology_cells 同长的所在面高度
        std::vector<WorldPoint> taut_points; // 几何搜索交出的父链折线, 取直之前
        std::vector<WorldPoint> pulled_points; // 取直之后
        std::vector<WorldPoint> assembled_points; // 拼上端点并去冗之后, 抬升之前
        std::vector<WorldPoint> planned_points; // 终线
        std::optional<WorldPoint> gap_start;
        std::optional<WorldPoint> gap_goal;
        std::optional<double> gap_distance;
        std::vector<std::string> warnings;
    } debug;
};

// 一个类占的格范围, 单位是全局格号, 闭区间。
struct ZoneBoundsPx
{
    int64_t x0 = 0;
    int64_t y0 = 0;
    int64_t x1 = -1;
    int64_t y1 = -1;

    bool empty() const { return x1 < x0 || y1 < y0; }
};

class RecastNavEngine
{
public:
    // 预烘场旁包从主包同目录按名找 (base.nav.gz → base.fields.nav.gz), 读不到或对不上主包
    // 就没法规划 —— 运行期不再重建那些场。
    RecastNavEngine(const BaseNavPack& pack, const BaseNavPlanner& planner);

    // start/goal 各带楼层高度(<= kBaseNavFloorYValidMin ⇒ floor 盲吸附);
    // goal_deck_y = 终点所在重叠面的高度,选层用,与吸附用的 floor_y 是两件事;
    // blocked = pack 全局三角形号封堵集,命中格从可走层盖掉;
    // blocked_points = 世界坐标封堵点,kBlockedPointRadius 半径内的格盖掉;
    // should_stop = 外部取消,两档窗口之间查一次
    RecastPlanResult plan(
        const std::string& zone_name,
        const WorldPoint& start,
        const WorldPoint& goal,
        float start_floor_y = kBaseNavFloorYNone,
        float goal_floor_y = kBaseNavFloorYNone,
        float goal_deck_y = kBaseNavFloorYNone,
        const std::vector<uint32_t>& blocked = {},
        const std::vector<WorldPoint>& blocked_points = {},
        const std::function<bool()>& should_stop = {});

    // 把该区的清洗网格与预烘场提前建好,让首条路线不必冷吃这份开销。
    void warm(const std::string& zone_name);

    // 逐点给出附近的全区类号。一次规划只在单一类号内找路,所以两点的类号集合不相交
    // 时这条腿必败,不必真去规划。半径取 kSnapRadius:选类只发生在点所在格或吸附后
    // 那一格,都不出这个圈。空集表示无从判断(无格图、未知区、点周围无体素)。
    std::vector<std::vector<uint32_t>> regionsNear(const std::string& zone_name, const std::vector<WorldPoint>& points);

private:
    struct ZoneEntry
    {
        std::unique_ptr<ZoneClean> zc;
        std::unique_ptr<FieldsZone> fz; // 该区的分量图与留墙表, 与 zc 同进同出
        std::string fields_error; // fz 为空时的原因
        uint64_t used_at = 0; // zoneEntry 的调用序号, 淘汰最久没用的
        std::unordered_map<uint32_t, ZoneBoundsPx> bounds; // 每类的格范围, 整类量, 算一次
    };
    // 每个 ZoneClean 常驻几十到两百 MB, 只留最近用过的两个(通常是一层 base 加一层 tier)
    static constexpr size_t kZoneCacheMax = 2;

    ZoneEntry& zoneEntry(const std::string& name);
    RecastPlanResult planLocked(
        const std::string& zone_name,
        const WorldPoint& start,
        const WorldPoint& goal,
        float start_floor_y,
        float goal_floor_y,
        float goal_deck_y,
        const std::vector<uint32_t>& blocked,
        const std::vector<WorldPoint>& blocked_points,
        const std::function<bool()>& should_stop);

    const BaseNavPack& pack_;
    const BaseNavPlanner& planner_;
    std::mutex mutex_;
    std::unordered_map<std::string, ZoneEntry> zones_;
    uint64_t zone_clock_ = 0;
    GridPack grid_; // 包里的预烘格图,没有它就没法规划
    FieldsPack fields_; // 旁包里的预烘场, 同样缺不得
    std::string grid_error_;
};

}
