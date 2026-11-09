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

// 第 n 位为 1 表示 area n 算可走面, 包里没有 surface 表时全部可走
inline constexpr uint32_t kWalkableFlagsDefault = 0x00020CDDU;

struct PolyMesh
{
    // vb 直接指向包里的顶点数组, 需要焊接时才自留一份到 vown
    const BaseNavVertex* vb = nullptr;
    std::vector<BaseNavVertex> vown;
    int64_t nv = 0;
    std::vector<std::array<int32_t, 3>> T;
    // NB 只在建网格期间存在, 之后压成 bnd, 第 k 位为 1 表示第 k 条边无邻居
    std::vector<std::array<int32_t, 3>> NB;
    std::vector<uint8_t> bnd;
    // 三角分桶的方格表
    int64_t gox = 0;
    int64_t goy = 0;
    int64_t gnx = 0;
    int64_t gny = 0;
    std::vector<int32_t> goff;
    std::vector<int32_t> gtris;
};

inline const BaseNavVertex* MeshVertices(const PolyMesh& mesh)
{
    return mesh.vown.empty() ? mesh.vb : mesh.vown.data();
}

inline WorldPoint MeshPoint(const PolyMesh& mesh, int32_t i)
{
    const BaseNavVertex& p = MeshVertices(mesh)[i];
    return { static_cast<double>(p.u), static_cast<double>(p.v) };
}

inline double MeshHeight(const PolyMesh& mesh, int32_t i)
{
    return static_cast<double>(MeshVertices(mesh)[i].height);
}

std::vector<int32_t> TrisNear(const PolyMesh& mesh, const WorldPoint& p, double r);               // 升序去重
std::vector<int32_t> TrisInBox(const PolyMesh& mesh, double x0, double y0, double x1, double y1); // 升序去重

struct ZoneSnapHit
{
    int32_t tri = -1;
    WorldPoint point;
    double dist = 0.0;
};

struct ZoneClean
{
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
    // 非空表示没建成, 此时其余字段不可用
    std::string error;
};

ZoneClean BuildZoneClean(
    const BaseNavPack& pack,
    const BaseNavPlanner& planner,
    const std::string& zone_name,
    uint32_t walkable_flags = kWalkableFlagsDefault);

bool ZoneCleanValid(const ZoneClean& zc);

std::optional<ZoneSnapHit> ZoneSnap(const ZoneClean& zc, const WorldPoint& p, double radius, std::optional<double> floor_y);

// 按 ZoneSnap 的尝试次序列出全部候选: 先是 radius 一圈, 再是兜底半径一圈, 圈内按吸附优先级排
std::vector<ZoneSnapHit> ZoneSnapCandidates(const ZoneClean& zc, const WorldPoint& p, double radius, std::optional<double> floor_y);

// 优先取与 deck_y 相差 kDeckBand 内的面, 再按距离
std::optional<ZoneSnapHit> ZoneSnapOnDeck(const ZoneClean& zc, const WorldPoint& p, double radius, double deck_y);

}
