#include "RecastNavBake.h"

#include <utility>

namespace navmesh::recast
{

BakedWalls BakeWalls(WallOracle& wo, double x0, double y0, int64_t nx, int64_t ny)
{
    BakedWalls out;
    const auto widx = wo.wallsInBbox(x0 - 4, y0 - 4, x0 + static_cast<double>(nx) * kCS + 4, y0 + static_cast<double>(ny) * kCS + 4);
    out.p0.reserve(widx.size());
    out.p1.reserve(widx.size());
    out.h0.reserve(widx.size());
    out.h1.reserve(widx.size());
    out.hh.reserve(widx.size());
    for (const int64_t i : widx) {
        out.p0.push_back(wo.P0[static_cast<size_t>(i)]);
        out.p1.push_back(wo.P1[static_cast<size_t>(i)]);
        out.h0.push_back(wo.H0[static_cast<size_t>(i)]);
        out.h1.push_back(wo.H1[static_cast<size_t>(i)]);
        out.hh.push_back(wo.HH[static_cast<size_t>(i)]);
    }
    return out;
}

BakedCells BakeCells(const ZoneClean& zc, WallOracle& wo, double x0, double y0, int64_t nx, int64_t ny)
{
    BakedCells out;
    out.x0 = x0;
    out.y0 = y0;
    out.nx = nx;
    out.ny = ny;
    out.rcs = Rasterize(zc.mesh.V, zc.mesh.H, zc.mesh.T, x0, y0, nx, ny);
    AppendSeamBridge(out.rcs, nx, ny);
    out.st = BuildSpans(out.rcs.cell, out.rcs.h);

    BakedWalls walls = BakeWalls(wo, x0, y0, nx, ny);
    out.wallP0 = std::move(walls.p0);
    out.wallP1 = std::move(walls.p1);
    out.wallH0 = std::move(walls.h0);
    out.wallH1 = std::move(walls.h1);
    out.wallHH = std::move(walls.hh);
    out.dead = StampWalls(out.wallP0, out.wallP1, out.wallHH, x0, y0, nx, ny, out.st);
    return out;
}

}
