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
#include "RecastNavNoGoIO.h"
#include "RecastNavZone.h"

namespace navmesh::recast
{

struct RecastPlanResult
{
    bool ok = false;
    std::string error;
    // 端点落在虚拟禁区内, 调用方不该再兜底凑线
    bool no_go = false;
    std::vector<WorldPoint> points;
    std::vector<double> clearance; // 逐点通道半宽 px
    std::vector<double> heights;   // 逐点所在面的高度, 没有层信息时为空
    double length = 0.0;
    std::vector<std::string> warnings;
    double snap_start = 0.0; // 起/终点到可走格锚点距离 px
    double snap_goal = 0.0;
    // points 的下标, 不含起点, 末位恒为 points.size()-1, 为空时由调用方拉直
    std::vector<size_t> waypoints;
    std::vector<DropLanding> drops;
    std::vector<RopeRide> ropes;

    struct Debug
    {
        struct Timing
        {
            double window_ms = 0.0;
            double topology_ms = 0.0;
            double geometry_ms = 0.0;
            double pull_ms = 0.0;
            double assemble_ms = 0.0;
            double lift_ms = 0.0;
            double total_ms = 0.0;
        } timing;

        double x0 = 0.0;
        double y0 = 0.0;
        int64_t nx = 0;
        int64_t ny = 0;
        double cell_size = 0.0;
        // 采用的窗口档位, 0 是端点包围盒外扩的最小一档
        int tier = 0;
        std::vector<std::string> tier_notes;
        std::vector<WorldPoint> topology_cells;
        std::vector<double> topology_heights;
        std::vector<WorldPoint> taut_points;
        std::vector<WorldPoint> pulled_points;
        std::vector<WorldPoint> assembled_points;
        std::vector<WorldPoint> planned_points;
        std::optional<WorldPoint> gap_start;
        std::optional<WorldPoint> gap_goal;
        std::optional<double> gap_distance;
        std::vector<std::string> warnings;
    } debug;
};

// 一个类占的格范围, 单位是全局格号, 闭区间
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
    // 场数据包按名从主包同目录读取 (base.nav.gz → base.fields.nav.gz), 读不到就无法规划
    RecastNavEngine(const BaseNavPack& pack, const BaseNavPlanner& planner, const std::filesystem::path& nogo_table);

    // floor_y <= kBaseNavFloorYValidMin 时不分楼层吸附, deck_y 是选层用的重叠面高度, start_deck_y 盖过 start_floor_y
    // no_go_discs 盖格方式同作者禁区, 但端点落在里面照常规划
    // rope_off 是本次不可用的升降索底端 uid, 空指针表示不用升降索
    RecastPlanResult plan(
        const std::string& zone_name,
        const WorldPoint& start,
        const WorldPoint& goal,
        float start_floor_y = kBaseNavFloorYNone,
        float goal_floor_y = kBaseNavFloorYNone,
        float goal_deck_y = kBaseNavFloorYNone,
        float start_deck_y = kBaseNavFloorYNone,
        const std::vector<BaseNavNoGoDisc>& no_go_discs = {},
        const std::function<bool()>& should_stop = {},
        const std::vector<uint32_t>* rope_off = nullptr);

    void warm(const std::string& zone_name);

    // 两点类号集合不相交则这条腿必败, 空集表示无法判断
    std::vector<std::vector<uint32_t>> regionsNear(const std::string& zone_name, const std::vector<WorldPoint>& points);

private:
    struct ZoneEntry
    {
        std::unique_ptr<ZoneClean> zc;
        std::unique_ptr<FieldsZone> fz;
        std::string fields_error;
        uint64_t used_at = 0; // zoneEntry 的调用序号
        std::unordered_map<uint32_t, ZoneBoundsPx> bounds;
    };

    // 每个 ZoneClean 常驻几十到两百 MB
    static constexpr size_t kZoneCacheMax = 2;

    ZoneEntry& zoneEntry(const std::string& name);
    RecastPlanResult planLocked(
        const std::string& zone_name,
        const WorldPoint& start,
        const WorldPoint& goal,
        float start_floor_y,
        float goal_floor_y,
        float goal_deck_y,
        float start_deck_y,
        const std::vector<BaseNavNoGoDisc>& no_go_discs,
        const std::function<bool()>& should_stop);

    const BaseNavPack& pack_;
    const BaseNavPlanner& planner_;
    std::mutex mutex_;
    std::unordered_map<std::string, ZoneEntry> zones_;
    uint64_t zone_clock_ = 0;
    GridPack grid_;
    std::shared_ptr<const FieldsPack> fields_ = std::make_shared<const FieldsPack>();
    NoGoTable nogo_;
    std::string grid_error_;
    bool hopping_ = false; // 正在规划跨类下落的两段, 它们自己不再往下接
    bool roping_ = false;  // 正在规划升降索的两段, 它们自己不再接升降索
    const std::vector<uint32_t>* rope_off_ = nullptr;
    // 每根升降索从绳底走到绳顶的路程 px, 键是 区号 << 32 | 底端 uid, 走不到记无穷
    std::unordered_map<uint64_t, double> rope_walk_;

    // 只在总代价低于 best_cost 时返回
    std::optional<RecastPlanResult> tryRopes(
        const std::string& zone_name,
        uint16_t zone_id,
        uint32_t start_region,
        uint32_t goal_region,
        const WorldPoint& start,
        const WorldPoint& goal,
        float start_floor_y,
        float goal_floor_y,
        float goal_deck_y,
        float start_deck_y,
        const std::vector<BaseNavNoGoDisc>& no_go_discs,
        const std::function<bool()>& should_stop,
        double best_cost,
        size_t max_tries,
        const std::vector<uint32_t>& out_of_reach);
};

}
