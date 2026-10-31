#pragma once

#include <array>
#include <cstdint>
#include <functional>
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

// 第 n 位为 1 表示 area n 算可走面, 包里没有 surface 表时全部可走
inline constexpr uint32_t kWalkableFlagsDefault = 0x00020CDDU;

struct PolyMesh
{
    // vb 直接指向包里的顶点数组, 需要焊接时才自留一份到 vown
    const BaseNavVertex* vb = nullptr;
    std::vector<BaseNavVertex> vown;
    int64_t nv = 0;
    std::vector<std::array<int32_t, 3>> T;
    // NB 只在构造期存在, 之后压成 bnd, 第 k 位为 1 表示第 k 条边无邻居
    std::vector<std::array<int32_t, 3>> NB;
    std::vector<uint8_t> bnd;

    PolyMesh() = default;
    // dup 非空时标出重复出现的有向边, 长 3*三角数, 按 i*3+k 索引
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
    void foldNb();                                                                    // NB → bnd, 然后释放 NB
    std::vector<int32_t> trisNear(const WorldPoint& p, double r) const;               // 升序去重
    std::vector<int32_t> trisInBox(double x0, double y0, double x1, double y1) const; // 升序去重

    // 三角分桶的方格边长 px
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
    ZoneClean(
        const BaseNavPack& pack,
        const BaseNavPlanner& planner,
        const std::string& zone_name,
        uint32_t walkable_flags = kWalkableFlagsDefault);

    bool valid() const { return error_.empty(); }

    const std::string& error() const { return error_; }

    struct SnapHit
    {
        int32_t tri = -1;
        WorldPoint point;
        double dist = 0.0;
    };

    // accept 非空时只在它放行的面里挑, 入参是三角号与三角上离 p 最近的点
    std::optional<SnapHit> snap(
        const WorldPoint& p,
        double radius,
        std::optional<double> floor_y,
        const std::function<bool(int32_t, const WorldPoint&)>& accept = {}) const;
    // 优先取与 deck_y 相差 kDeckBand 内的面, 再按距离
    std::optional<SnapHit> snapOnDeck(const WorldPoint& p, double radius, double deck_y) const;

    // 之后不得再读几何与逐三角表, 区号等标量仍可用
    void release();

    std::string name;
    uint16_t zone_id = 0;
    int64_t lo = 0;
    int64_t hi = 0;
    PolyMesh mesh;
    std::vector<uint8_t> island; // 逐三角, 标记所在分量是否以小岛三角为主
    // 与 mesh.T 同长同序, 下游按 (全局三角号 - lo) 索引, 不得压缩重排
    std::vector<uint8_t> walkable;
    uint32_t walkable_flags = kWalkableFlagsDefault;
    std::string stats;

private:
    std::string error_;
};

}
