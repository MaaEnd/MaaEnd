#include "RecastNavBake.h"

#include <algorithm>
#include <utility>

namespace navmesh::recast
{

// 输出顺序与全网格按三角号、边内序扫描所得的子序列一致
BakedWalls BakeWalls(const ZoneClean& zc, double x0, double y0, int64_t nx, int64_t ny)
{
    BakedWalls out;
    const double bx0 = x0 - 4;
    const double by0 = y0 - 4;
    const double bx1 = x0 + static_cast<double>(nx) * kCS + 4;
    const double by1 = y0 + static_cast<double>(ny) * kCS + 4;
    const PolyMesh& mesh = zc.mesh;
    for (const int32_t t : mesh.trisInBox(bx0, by0, bx1, by1)) {
        const auto i = static_cast<size_t>(t);
        if (zc.walkable[i] == 0) {
            // 与可走面的缝已在 ZoneClean 割断, 这条边由可走面一侧收成墙
            continue;
        }
        for (int k = 0; k < 3; ++k) {
            if ((mesh.bnd[i] >> k & 1U) == 0U) {
                continue;
            }
            const int32_t a = mesh.T[i][k];
            const int32_t b = mesh.T[i][(k + 1) % 3];
            const WorldPoint p0 = mesh.v(a);
            const WorldPoint p1 = mesh.v(b);
            if (std::max(p0.x, p1.x) >= bx0 && std::min(p0.x, p1.x) <= bx1 && std::max(p0.y, p1.y) >= by0 && std::min(p0.y, p1.y) <= by1) {
                out.p0.push_back(p0);
                out.p1.push_back(p1);
                out.h0.push_back(mesh.h(a));
                out.h1.push_back(mesh.h(b));
                out.hh.push_back((mesh.h(a) + mesh.h(b)) / 2.0);
                out.tri.push_back(t);
                out.k.push_back(static_cast<uint8_t>(k));
            }
        }
    }
    return out;
}

BakedCells BakeCells(const ZoneClean& zc, double x0, double y0, int64_t nx, int64_t ny)
{
    BakedCells out;
    out.x0 = x0;
    out.y0 = y0;
    out.nx = nx;
    out.ny = ny;
    out.rcs = Rasterize(zc.mesh.verts(), zc.mesh.T, x0, y0, nx, ny, &zc.walkable);
    AppendSeamBridge(out.rcs, nx, ny);
    out.st = BuildSpans(out.rcs.cell, out.rcs.h);

    BakedWalls walls = BakeWalls(zc, x0, y0, nx, ny);
    out.wallP0 = std::move(walls.p0);
    out.wallP1 = std::move(walls.p1);
    out.wallH0 = std::move(walls.h0);
    out.wallH1 = std::move(walls.h1);
    out.wallHH = std::move(walls.hh);
    out.dead = StampWalls(out.wallP0, out.wallP1, out.wallHH, x0, y0, nx, ny, out.st);
    return out;
}

}
