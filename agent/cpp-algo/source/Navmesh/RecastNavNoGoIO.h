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

struct NoGoPoly
{
    std::string id;
    std::string tier;             // 空串表示画在底图上, 环下各层全盖
    std::vector<WorldPoint> ring; // 世界坐标, 首尾不重复
    // 为真时只盖层高在 [y_lo, y_hi] 内的格, 即 tier 地面高度 ±kBaseNavFloorBand
    bool banded = false;
    double y_lo = 0.0;
    double y_hi = 0.0;
    double bx0 = 0.0; // 环的包围盒
    double by0 = 0.0;
    double bx1 = 0.0;
    double by1 = 0.0;
};

bool NoGoPolyContains(const NoGoPoly& poly, const WorldPoint& p);

struct NoGoTable
{
    std::unordered_map<std::string, std::vector<NoGoPoly>> zones;
};

// 文件不存在视为没有禁区, 存在但解析失败或引用包里没有的 tier 时返回 false
bool LoadNoGoTable(NoGoTable* table, std::string* err, const std::filesystem::path& path, const BaseNavPack& pack);

// 区名不在表里时返回 nullptr
const std::vector<NoGoPoly>* FindNoGoZone(const NoGoTable& table, const std::string& name);

// 格心落在环内即盖, lh 是逐格层高, NaN 表示该格没有面
void StampNoGo(
    Mask* core,
    Mask* lay,
    Grid<float>* dist,
    const std::vector<NoGoPoly>& polys,
    double x0,
    double y0,
    int64_t nx,
    int64_t ny,
    const Grid<float>& lh);

// h 是点所在面的高度, 命中时回填禁区 id
bool NoGoContains(std::string* hit_id, const std::vector<NoGoPoly>& polys, const WorldPoint& p, double h);

}
