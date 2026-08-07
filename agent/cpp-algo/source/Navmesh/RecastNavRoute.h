#pragma once

#include <cstdint>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>
#include <vector>

#include "BaseNavPack.h"
#include "BaseNavPlanner.h"
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
    std::vector<WorldPoint> wall_cross; // 不可避穿墙步的格心
    double snap_start = 0.0;            // 起/终点到可走格锚点距离 px
    double snap_goal = 0.0;
};

// 逐档扩窗的预算。窗口面积随边距增长,不可达时每一档都要把整个窗口栅格化再搜一遍,
// 跑满四档是分钟级。按上一档实测速度外推下一档,装不下就停在已有结论上。
struct RecastPlanBudget
{
    int64_t wall_ms = 6000;            // 整次 plan 的墙钟上限
    int64_t dead_end_ms = 6000;        // 上一档没要求扩窗时的上限:扩窗改不了结论,别一直把窗口翻番
    std::function<bool()> should_stop; // 外部取消,逐档之间查
};

class RecastNavEngine
{
public:
    RecastNavEngine(const BaseNavPack& pack, const BaseNavPlanner& planner);

    // start/goal 各带楼层高度(<= kBaseNavFloorYValidMin ⇒ floor 盲吸附);
    // goal_deck_y = 终点所在重叠面的高度,选层用,与吸附用的 floor_y 是两件事;
    // blocked = pack 全局三角形号封堵集,命中格从可走层盖掉;
    // blocked_points = 世界坐标封堵点,kBlockedPointRadius 半径内的格盖掉
    RecastPlanResult plan(
        const std::string& zone_name,
        const WorldPoint& start,
        const WorldPoint& goal,
        float start_floor_y = kBaseNavFloorYNone,
        float goal_floor_y = kBaseNavFloorYNone,
        float goal_deck_y = kBaseNavFloorYNone,
        const std::vector<uint32_t>& blocked = {},
        const std::vector<WorldPoint>& blocked_points = {},
        const RecastPlanBudget& budget = {});

private:
    struct ZoneEntry
    {
        std::unique_ptr<ZoneClean> zc;
        std::unique_ptr<WallOracle> wo;
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
        const RecastPlanBudget& budget);

    const BaseNavPack& pack_;
    const BaseNavPlanner& planner_;
    std::mutex mutex_;
    std::unordered_map<std::string, ZoneEntry> zones_;
};

}
