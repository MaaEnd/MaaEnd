#include "RecastNavBake.h"

#include <algorithm>
#include <utility>

namespace navmesh::recast
{

// 收齐窗口内的边界边。这份数据只说哪里能走, 不带墙面/断崖之类的信息,
// 再去分辨某条边是墙还是接缝等于凭空造数据, 所以这里只收边、不分类。
// 三角的包围盒罩得住它自己的每条边, 于是分桶查出来的三角必是超集; 按三角号升序、
// 边内序 k 升序枚举, 得到的就是全网格顺序扫描的同一子序列。
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
            // 掩码外的三角不出边。它与可走面之间那条缝已在 ZoneClean 里割断,
            // 所以那条边会由可走面这一侧收成边界边 —— 水岸因此成墙,而不是消失。
            continue;
        }
        for (int k = 0; k < 3; ++k) {
            if ((mesh.bnd[i] >> k & 1U) == 0U) {
                continue; // 有邻居的边不是墙
            }
            const int32_t a = mesh.T[i][k];
            const int32_t b = mesh.T[i][(k + 1) % 3];
            const WorldPoint p0 = mesh.v(a);
            const WorldPoint p1 = mesh.v(b);
            if (std::max(p0.x, p1.x) >= bx0 && std::min(p0.x, p1.x) <= bx1 && std::max(p0.y, p1.y) >= by0
                && std::min(p0.y, p1.y) <= by1) {
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
    // 掩码随网格一起送进体素化 —— 烘焙期与运行期共用这一处判据,两边的可走面必然一致
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
