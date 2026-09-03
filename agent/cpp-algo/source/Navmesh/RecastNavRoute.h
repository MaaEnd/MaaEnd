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

class RecastNavEngine
{
public:
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

    // 把该区的清洗网格与墙 oracle 提前建好,让首条路线不必冷吃这份开销。
    void warm(const std::string& zone_name);

    // 改可走面掩码(源 surface flags 的收取位)。运行端必须与烘格图时用的是同一个值,
    // 否则格图铺出来的可走面与规划器认的可走面对不上。改值会丢掉已建好的区缓存。
    void setWalkableFlags(uint32_t flags);

    // 逐点给出附近的全区类号。一次规划只在单一类号内找路,所以两点的类号集合不相交
    // 时这条腿必败,不必真去规划。半径取 kSnapRadius:选类只发生在点所在格或吸附后
    // 那一格,都不出这个圈。空集表示无从判断(无格图、未知区、点周围无体素)。
    std::vector<std::vector<uint32_t>> regionsNear(const std::string& zone_name, const std::vector<WorldPoint>& points);

private:
    struct ZoneEntry
    {
        std::unique_ptr<ZoneClean> zc;
    };

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
    GridPack grid_; // 包里的预烘格图,没有它就没法规划
    std::string grid_error_;
    uint32_t walkable_flags_ = kWalkableFlagsDefault;
};

}
