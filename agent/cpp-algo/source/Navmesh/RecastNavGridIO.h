#pragma once

#include <algorithm>
#include <cmath>
#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace navmesh::recast
{

inline constexpr char kGridSectionTag[5] = "BGRD";

// 一格可挂多条, 同列每层地面一条, 无实体 span 的补格挂一条 ghost 或 fill
struct GridSpanRec
{
    int32_t cell = 0;  // 瓦内格号 y * nx + x
    uint32_t rid = 0;  // 全区类号
    float h = 0.0F;
    uint16_t clr = 0;  // 净空的平方格距, 用 GridClearance 还原
    uint8_t flags = 0; // kGridFlag*
    uint8_t steps = 0; // 4 向各 2 位: 禁 c->b、禁 b->c
};

inline constexpr uint8_t kGridFlagDead = 0x01;  // 墙压过的 span
inline constexpr uint8_t kGridFlagWalk = 0x02;  // 既在层里又在核心里
inline constexpr uint8_t kGridFlagCore = 0x04;  // 补洞封缝后的核心掩码
inline constexpr uint8_t kGridFlagGhost = 0x08; // 补出来的格, 高度由邻格传播
inline constexpr uint8_t kGridFlagFill = 0x10;  // 补出来的格且无高度可传播

// v3 瓦载荷: 五个字段各一条残差流, 前面三条是格号、层数、类号字典
inline constexpr int kGridFieldCount = 5;
inline constexpr int kGridStreamCount = 3 + kGridFieldCount;
// 残差预测子, 逐字段逐层记在选择子里
inline constexpr uint8_t kGridPredUp = 0;   // 面内上邻, 同列上一个存在的格
inline constexpr uint8_t kGridPredDown = 1; // 同格下一层

// steps 位序对应的 4 个规范方向
inline constexpr int64_t kGridStepDx[4] = { 1, 0, 1, 1 };
inline constexpr int64_t kGridStepDy[4] = { 0, 1, 1, -1 };

// 还原式须与 Clearance 的返回值逐位相同
inline float GridClearance(uint16_t q)
{
    return std::min(std::sqrt(static_cast<float>(q)) * 0.25F, 12.0F);
}

struct GridTile
{
    uint32_t regions = 0; // 本瓦出现的类号数
    std::vector<GridSpanRec> rec;
};

// v2 瓦载荷, 输入须已解压
bool DecodeGridTile(const uint8_t* data, size_t len, GridTile& out);

// 输入段里的原始字节, 内部逐流 inflate
bool DecodeGridTileV3(const uint8_t* data, size_t len, int32_t nx, GridTile& out);

bool DecodeGridTileRegionsV3(const uint8_t* data, size_t len, std::vector<uint32_t>& out);

// gx0/gy0 是全局格号, [px0,px1]×[py0,py1] 是瓦内闭区间, 各瓦互不重叠且覆盖全区
struct GridTileRef
{
    int32_t gx0 = 0;
    int32_t gy0 = 0;
    int32_t nx = 0;
    int32_t ny = 0;
    int32_t px0 = 0;
    int32_t px1 = -1;
    int32_t py0 = 0;
    int32_t py1 = -1;
    uint64_t offset = 0; // 相对段首
    uint32_t len = 0;
    uint32_t records = 0;
};

struct GridZoneDir
{
    std::string name;
    double x0 = 0.0;
    double y0 = 0.0;
    uint32_t global_regions = 0;
    std::vector<GridTileRef> tiles;
};

// 只记段字节的指针, 寿命不能长过它解析的那个包
class GridPack
{
public:
    bool parse(const uint8_t* data, size_t len, std::string& err);

    bool valid() const { return base_ != nullptr; }

    double cellSize() const { return cell_size_; }

    double tilePx() const { return tile_px_; }

    double apronPx() const { return apron_px_; }

    const std::vector<GridZoneDir>& zones() const { return zones_; }

    const GridZoneDir* findZone(const std::string& name) const;
    bool decodeTile(const GridTileRef& t, GridTile& out) const;
    bool tileRegions(const GridTileRef& t, std::vector<uint32_t>& out) const;

private:
    const uint8_t* base_ = nullptr;
    size_t len_ = 0;
    uint32_t version_ = 0;
    double cell_size_ = 0.0;
    double tile_px_ = 0.0;
    double apron_px_ = 0.0;
    std::vector<GridZoneDir> zones_;
};

// 自有矩形与全局格闭区间 [gx0,gx1]×[gy0,gy1] 相交的瓦
std::vector<const GridTileRef*> GridTilesInRect(const GridZoneDir& zone, int64_t gx0, int64_t gy0, int64_t gx1, int64_t gy1);

// 解不带原长的 zlib 流, 覆盖写入 out
bool InflateBytes(const uint8_t* data, size_t len, std::vector<uint8_t>& out);

}
