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

// GRID 段一块瓦的逐 span 记录。一格可以挂多条:同列的几层地面各一条,
// 补出来的格没有实体 span 时挂一条 ghost 或 fill。
struct GridSpanRec
{
    int32_t cell = 0;  // 瓦内格号 y * nx + x
    uint32_t rid = 0;  // 全区类号
    float h = 0.0F;
    uint16_t clr = 0;  // 净空的平方格距,还原用 GridClearance
    uint8_t flags = 0; // kGridFlag*
    uint8_t steps = 0; // 4 向各 2 位:禁 c->b、禁 b->c
};

inline constexpr uint8_t kGridFlagDead = 0x01;  // 墙压过的 span
inline constexpr uint8_t kGridFlagWalk = 0x02;  // 既在层里又在核心里
inline constexpr uint8_t kGridFlagCore = 0x04;  // 补洞封缝后的核心掩码
inline constexpr uint8_t kGridFlagGhost = 0x08; // 补出来的格,高度由邻格传播
inline constexpr uint8_t kGridFlagFill = 0x10;  // 补出来的格且无高度可传播

// v3 瓦载荷:五个字段各一条残差流,前面三条是格号、层数、类号字典。
inline constexpr int kGridFieldCount = 5;
inline constexpr int kGridStreamCount = 3 + kGridFieldCount;
// 残差的预测子。编码端逐字段逐层挑一个写进选择子,解码端只照做。
inline constexpr uint8_t kGridPredUp = 0;   // 面内上邻:同列上一个存在的格
inline constexpr uint8_t kGridPredDown = 1; // 同格下一层

// steps 的位序对应的 4 个规范方向,与 StepBreaks 扫的顺序一致。
inline constexpr int64_t kGridStepDx[4] = { 1, 0, 1, 1 };
inline constexpr int64_t kGridStepDy[4] = { 0, 1, 1, -1 };

// 净空存的是整数平方格距,还原表达式与 Clearance 的返回值逐位相同。
inline float GridClearance(uint16_t q)
{
    return std::min(std::sqrt(static_cast<float>(q)) * 0.25F, 12.0F);
}

struct GridTile
{
    uint32_t regions = 0; // 本瓦出现的类号数
    std::vector<GridSpanRec> rec;
};

// 解一块 v2 瓦的载荷(已解压)。字节走完且自洽才返回 true。
bool DecodeGridTile(const uint8_t* data, size_t len, GridTile& out);

// 解一块 v3 瓦。段里的原始字节直接进,内部逐流 inflate;差分要瓦宽 nx。
bool DecodeGridTileV3(const uint8_t* data, size_t len, int32_t nx, GridTile& out);

// 只取一块 v3 瓦的类号字典。前两条流带长度前缀,跳过就行,所以不必解开整块瓦。
bool DecodeGridTileRegionsV3(const uint8_t* data, size_t len, std::vector<uint32_t>& out);

// 段目录里的一块瓦。格号全是全局的(gx0 = llround(ox / kCS)),自有矩形
// [px0,px1]×[py0,py1] 是瓦内格号闭区间,各瓦互不重叠且拼起来覆盖全区。
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

// 包里 GRID 段的目录。段字节归 BaseNavPack 持有,这里只记指针,
// 所以 GridPack 的寿命不能长过它解析的那个包。
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
    // 解一块瓦:按需 inflate 再解码,不缓存。空瓦返回空 GridTile。
    bool decodeTile(const GridTileRef& t, GridTile& out) const;
    // 一块瓦里出现过的类号。只解类号字典那一条流,比整块解开便宜得多,
    // 用来在解瓦之前就把不含目标类的瓦筛掉。
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

// 自有矩形与全局格矩形 [gx0,gx1]×[gy0,gy1] 相交的瓦。
std::vector<const GridTileRef*> GridTilesInRect(const GridZoneDir& zone, int64_t gx0, int64_t gy0, int64_t gx1, int64_t gy1);

}
