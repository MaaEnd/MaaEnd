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
    std::vector<WorldPoint> wall_cross; // 不可避穿墙步的格心
    double snap_start = 0.0;            // 起/终点到可走格锚点距离 px
    double snap_goal = 0.0;
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
        const std::function<bool()>& should_stop);

    const BaseNavPack& pack_;
    const BaseNavPlanner& planner_;
    std::mutex mutex_;
    std::unordered_map<std::string, ZoneEntry> zones_;
    GridPack grid_;         // 包里的预烘格图,没有它就没法规划
    std::string grid_error_;
};

}
