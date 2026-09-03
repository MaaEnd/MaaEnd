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
    // 顶点不复制: 源精确包直接指向包里的 float 数组(vb), 旧包要先取整焊接才自留一份(vown)。
    // 最大的区有 247 万顶点, 按 double 抄一遍要 59 MB, 而下游算的本来就是这些 float 值。
    const BaseNavVertex* vb = nullptr;
    std::vector<BaseNavVertex> vown;
    int64_t nv = 0;
    std::vector<std::array<int32_t, 3>> T;
    // 邻接只在 ZoneClean 构造期存在, 分量算完就压成 bnd(第 k 位 = 第 k 条边无邻居)并释放:
    // 运行期唯一的读者 BakeWalls 只问"这条边是不是边界", 12 字节/三角换 1 字节。
    std::vector<std::array<int32_t, 3>> NB;
    std::vector<uint8_t> bnd;

    PolyMesh() = default;
    // dup 非空时顺便标出"同一条有向边出现两次以上"的槽(3*三角数, 按 i*3+k 索引):
    // 它和邻接用同一套按起点分桶的边表, 单独再建一遍要多占三倍三角数的两张 int32 表。
    PolyMesh(const BaseNavVertex* vb, int64_t nv, std::vector<std::array<int32_t, 3>> t, std::vector<uint8_t>* dup = nullptr);
    PolyMesh(std::vector<BaseNavVertex> own, std::vector<std::array<int32_t, 3>> t, std::vector<uint8_t>* dup = nullptr);

    const BaseNavVertex* verts() const { return vown.empty() ? vb : vown.data(); }

    WorldPoint v(int32_t i) const
    {
        const BaseNavVertex& p = verts()[i];
        return { static_cast<double>(p.u), static_cast<double>(p.v) };
    }

    double h(int32_t i) const { return static_cast<double>(verts()[i].height); }

    void buildNb(std::vector<uint8_t>* dup);
    void foldNb(); // NB → bnd, 然后释放 NB
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
    std::vector<uint8_t> island; // 逐三角: 所在分量以小岛三角为主。分量号本身没人读, 不留
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
