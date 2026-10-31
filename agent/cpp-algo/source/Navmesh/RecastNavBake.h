#pragma once

#include <cstdint>
#include <vector>

#include "NavmeshTypes.h"
#include "RecastNavGrid.h"
#include "RecastNavZone.h"

namespace navmesh::recast
{

// 取窗口外扩 4px 内的边, tri/k 是区内三角号与边内序
struct BakedWalls
{
    std::vector<WorldPoint> p0;
    std::vector<WorldPoint> p1;
    std::vector<double> h0;
    std::vector<double> h1;
    std::vector<double> hh;
    std::vector<int32_t> tri;
    std::vector<uint8_t> k;
};

BakedWalls BakeWalls(const ZoneClean& zc, double x0, double y0, int64_t nx, int64_t ny);

// 只由区几何与墙决定, 与起终点和封堵无关
struct BakedCells
{
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
    RasterCells rcs;
    SpanTable st;
    std::vector<uint8_t> dead; // 逐 span, 标记被立面盖掉
    std::vector<WorldPoint> wallP0;
    std::vector<WorldPoint> wallP1;
    std::vector<double> wallH0;
    std::vector<double> wallH1;
    std::vector<double> wallHH;
};

// (x0,y0) 是矩形左下角, 共 nx×ny 格
BakedCells BakeCells(const ZoneClean& zc, double x0, double y0, int64_t nx, int64_t ny);

}
