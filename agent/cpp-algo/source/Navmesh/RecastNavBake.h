#pragma once

#include <cstdint>
#include <vector>

#include "NavmeshTypes.h"
#include "RecastNavGrid.h"
#include "RecastNavZone.h"

namespace navmesh::recast
{

// 一块矩形范围内的立面。取窗口外扩 4px 内的,与盖章口径一致。
struct BakedWalls
{
    std::vector<WorldPoint> p0;
    std::vector<WorldPoint> p1;
    std::vector<double> h0;
    std::vector<double> h1;
    std::vector<double> hh;
};

BakedWalls BakeWalls(const ZoneClean& zc, double x0, double y0, int64_t nx, int64_t ny);

// 一块矩形范围内、只由区几何与墙决定的体素数据。起点、终点、封堵都不参与,
// 所以同一矩形任何时候烤出来的都一样,可以离线算好存进包里。
struct BakedCells
{
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
    RasterCells rcs;           // 面内采样,corein 判据要按采样高度逐个比
    SpanTable st;              // 补缝并按 kMergeH 合并后的 span 表
    std::vector<uint8_t> dead; // 逐 span: 被立面盖章
    std::vector<WorldPoint> wallP0;
    std::vector<WorldPoint> wallP1;
    std::vector<double> wallH0;
    std::vector<double> wallH1;
    std::vector<double> wallHH;
};

// 矩形左下角 (x0,y0)、nx×ny 格。墙取窗口外扩 4px 内的,与盖章口径一致。
BakedCells BakeCells(const ZoneClean& zc, double x0, double y0, int64_t nx, int64_t ny);

}
