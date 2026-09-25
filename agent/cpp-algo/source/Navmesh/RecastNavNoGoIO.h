#pragma once

#include <cstdint>
#include <filesystem>
#include <string>
#include <unordered_map>
#include <vector>

#include "BaseNavPack.h"
#include "NavmeshTypes.h"
#include "RecastNavGrid.h"

namespace navmesh::recast
{

// 一块虚拟禁区: 作者圈出的环, 与网格几何无关。建窗时把环内的格从可走层盖掉并把净空清零,
// 搜索域、弦判据、取直挡线与拐角抬升四层于是一起把它当墙; 净空与中轴照旧采旁包烘好的值,
// 这条腿因此不掉出预烘快路。
struct NoGoPoly
{
    std::string id;
    std::string tier;             // 画在哪个 tier 上; 空 = 画在底图上, 环下各层全盖
    std::vector<WorldPoint> ring; // 世界坐标, 首尾不重复
    // 画在 tier 上的禁区只盖层高落在该 tier 地面高度 ±kBaseNavFloorBand 内的格, 与吸附挑层用的
    // 同一条带, 于是 tier 帧路点吸到哪一层, 画在那个 tier 上的禁区就盖哪一层。
    bool banded = false;
    double y_lo = 0.0;
    double y_hi = 0.0;
    double bx0 = 0.0; // 环的包围盒, 建窗只扫这一块
    double by0 = 0.0;
    double bx1 = 0.0;
    double by1 = 0.0;

    bool contains(const WorldPoint& p) const;
};

class NoGoTable
{
public:
    // 文件不存在 = 零禁区, 逐位回到没有禁区的行为; 存在却读不通 = 失败, 宁可整条腿报错,
    // 也不能让作者以为封住的地方其实通着。tier 名按 pack 的区表解成地面高度, 表里写了包里没有的
    // tier 同样算读不通。
    bool load(const std::filesystem::path& path, const BaseNavPack& pack, std::string& err);

    // 区名不在表里 = 该区没有禁区。
    const std::vector<NoGoPoly>* zone(const std::string& name) const;

private:
    std::unordered_map<std::string, std::vector<NoGoPoly>> zones_;
};

// 盖格。判据是格心落在环内, 与封堵点那一路同源。lh 是逐格层高(NaN = 该格没有面),
// 只有画在 tier 上的禁区读它。
void StampNoGo(
    const std::vector<NoGoPoly>& polys,
    double x0,
    double y0,
    int64_t nx,
    int64_t ny,
    const Grid<float>& lh,
    Mask& core,
    Mask& lay,
    Grid<float>& dist);

// 点落在禁区内。h 是点所在面的高度, 带高度带的禁区按它筛; 命中时回填那块禁区的 id。
bool NoGoContains(const std::vector<NoGoPoly>& polys, const WorldPoint& p, double h, std::string& hit_id);

}
