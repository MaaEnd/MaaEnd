#pragma once

#include <array>
#include <cstdint>
#include <optional>
#include <string>
#include <unordered_map>
#include <vector>

#include "BaseNavPack.h"
#include "BaseNavPlanner.h"
#include "RecastNavGrid.h"

namespace navmesh::recast
{

inline constexpr double kWeldDh = 3.0;              // 顶点焊接同柱高差容差 px
inline constexpr double kSnapFallbackRadius = 16.0; // 吸附兜底半径 px

// 源 surface 表(BSRF 段)那 32 位 flags 的收取掩码:第 n 位为 1 表示 area n 算可走面。
// 导出端把 flags 写成 1<<area,所以这个掩码与一张 area 白名单等价。
// 默认值取自游戏自己的 NavMeshProjectSettings.areas[32](Data/globalgamemanagers):
// 判据是该 area 的 cost < 9999,即 {0 Walkable, 2 Jump, 3 Erosion, 4 Rock, 6 Trap,
// 7 Tree, 10 Shallow Water, 11 Mid Water, 17 Walkable Override} = 0x00020CDD。
// 排除的两类正是游戏自己标成 9999 的 12 Deep Water 与 18 Forbidden。
// 它们照旧留在网格里 —— 几何与语义一个字节不丢,只是不参与邻接、分量、hop、吸附、
// 墙判据与体素化,于是不再被当成普通地面走。
// 包里没有 surface 表时全部可走,与历史行为逐字节相同。
inline constexpr uint32_t kWalkableFlagsDefault = 0x00020CDDU;

struct PolyMesh
{
    std::vector<WorldPoint> V;
    std::vector<double> H;
    std::vector<std::array<int32_t, 3>> T;
    std::vector<std::array<int32_t, 3>> NB;

    PolyMesh() = default;
    PolyMesh(std::vector<WorldPoint> v, std::vector<std::array<int32_t, 3>> t, std::vector<double> h);

    void buildNb();
    std::vector<int32_t> trisNear(const WorldPoint& p, double r) const;                // 升序去重
    std::vector<int32_t> trisInBox(double x0, double y0, double x1, double y1) const;  // 升序去重

    // 三角按 24px 方格分桶。桶号在包围盒内连续, 所以只存一张偏移表, 查询按下标直接落桶。
    static constexpr double kGridCell = 24.0;
    int64_t gox = 0;
    int64_t goy = 0;
    int64_t gnx = 0;
    int64_t gny = 0;
    std::vector<int32_t> goff;
    std::vector<int32_t> gtris;

private:
    void buildGrid();
};

class ZoneClean
{
public:
    ZoneClean(const BaseNavPack& pack, const BaseNavPlanner& planner, const std::string& zone_name,
        uint32_t walkable_flags = kWalkableFlagsDefault);

    bool valid() const { return error_.empty(); }

    const std::string& error() const { return error_; }

    struct SnapHit
    {
        int32_t tri = -1;
        WorldPoint point;
        double dist = 0.0;
    };

    std::optional<SnapHit> snap(const WorldPoint& p, double radius, std::optional<double> floor_y) const;

    // 交还几何与逐三角的表。调用方保证之后不再读它们, 区号一类的标量照旧可用。
    void release();

    std::string name;
    uint16_t zone_id = 0;
    int64_t lo = 0;
    int64_t hi = 0;
    PolyMesh mesh;
    std::vector<int32_t> comp;        // 三角 → 分量代表(区内最小三角号)
    std::vector<uint8_t> comp_island; // 按分量代表值索引
    // 逐三角可走标记,与 mesh.T 同长同序。掩码外的三角照样占着自己那一行 ——
    // RecastNavRoute 拿 (全局三角号 - lo) 直接索引 mesh,下标身份是硬约束,只能就地打标,
    // 绝不能压缩重排。
    std::vector<uint8_t> walkable;
    uint32_t walkable_flags = kWalkableFlagsDefault;
    std::string stats;

private:
    std::string error_;
};

}
