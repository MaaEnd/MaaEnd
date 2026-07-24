#pragma once

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
    // blocked = pack 全局三角形号封堵集,命中格从可走层盖掉;
    // blocked_points = 世界坐标封堵点,kBlockedPointRadius 半径内的格盖掉
    RecastPlanResult plan(
        const std::string& zone_name,
        const WorldPoint& start,
        const WorldPoint& goal,
        float start_floor_y = kBaseNavFloorYNone,
        float goal_floor_y = kBaseNavFloorYNone,
        const std::vector<uint32_t>& blocked = {},
        const std::vector<WorldPoint>& blocked_points = {});

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
        const std::vector<uint32_t>& blocked,
        const std::vector<WorldPoint>& blocked_points);

    const BaseNavPack& pack_;
    const BaseNavPlanner& planner_;
    std::mutex mutex_;
    std::unordered_map<std::string, ZoneEntry> zones_;
};

}
