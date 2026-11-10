#include "RecastNavZone.h"

#include <algorithm>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <numeric>
#include <utility>

#include "BaseNavGeometry.h"
#include "NavParallel.h"

namespace navmesh::recast
{

namespace
{

constexpr double kTriGridCell = 24.0; // 三角分桶的方格边长 px

double triHeight(const PolyMesh& mesh, int32_t t)
{
    const auto& tri = mesh.T[static_cast<size_t>(t)];
    return (MeshHeight(mesh, tri[0]) + MeshHeight(mesh, tri[1]) + MeshHeight(mesh, tri[2])) / 3.0;
}

std::pair<WorldPoint, double> closestOnTri(const WorldPoint& p, const std::array<WorldPoint, 3>& tri)
{
    if (detail::PointInTriangle(p, tri)) {
        return { p, 0.0 };
    }
    const WorldPoint q = detail::ClosestPointOnTriangle(p, tri);
    return { q, std::hypot(q.x - p.x, q.y - p.y) };
}

struct TriBox
{
    int64_t x0, y0, x1, y1;
};

TriBox tri_box(const PolyMesh& mesh, size_t i)
{
    const WorldPoint a = MeshPoint(mesh, mesh.T[i][0]);
    const WorldPoint b = MeshPoint(mesh, mesh.T[i][1]);
    const WorldPoint c = MeshPoint(mesh, mesh.T[i][2]);
    return TriBox { static_cast<int64_t>(std::floor(std::min({ a.x, b.x, c.x }) / kTriGridCell)),
                    static_cast<int64_t>(std::floor(std::min({ a.y, b.y, c.y }) / kTriGridCell)),
                    static_cast<int64_t>(std::floor(std::max({ a.x, b.x, c.x }) / kTriGridCell)),
                    static_cast<int64_t>(std::floor(std::max({ a.y, b.y, c.y }) / kTriGridCell)) };
}

// 槽 s 即三角 s/3 的第 s%3 条有向边, 返回它的终点
int32_t slot_dst(const std::vector<std::array<int32_t, 3>>& T, int32_t s)
{
    return T[static_cast<size_t>(s / 3)][(s % 3 + 1) % 3];
}

void build_neighbors(PolyMesh* mesh, std::vector<uint8_t>* dup)
{
    const std::vector<std::array<int32_t, 3>>& T = mesh->T;
    const int64_t m = static_cast<int64_t>(T.size());
    mesh->NB.assign(T.size(), { -1, -1, -1 });
    if (dup != nullptr) {
        dup->assign(static_cast<size_t>(3 * m), 0);
    }
    if (m == 0) {
        return;
    }
    // 桶内按槽号递增填, 重边时首个命中即稳定序首槽
    const int64_t n = mesh->nv;
    std::vector<int32_t> at(static_cast<size_t>(n) + 1, 0);
    for (const auto& tri : T) {
        for (const int32_t a : tri) {
            ++at[static_cast<size_t>(a) + 1];
        }
    }
    for (int64_t i = 0; i < n; ++i) {
        at[static_cast<size_t>(i) + 1] += at[static_cast<size_t>(i)];
    }
    std::vector<int32_t> slot(static_cast<size_t>(3 * m));
    {
        std::vector<int32_t> wr = at;
        for (int64_t i = 0; i < m; ++i) {
            for (int64_t k = 0; k < 3; ++k) {
                const int32_t a = T[static_cast<size_t>(i)][k];
                slot[static_cast<size_t>(wr[static_cast<size_t>(a)]++)] = static_cast<int32_t>(i * 3 + k);
            }
        }
    }
    ParallelChunks(m, NavWorkerCount(m), [&](size_t, int64_t lo_i, int64_t hi_i) {
        for (int64_t i = lo_i; i < hi_i; ++i) {
            for (int64_t k = 0; k < 3; ++k) {
                const int32_t a = T[static_cast<size_t>(i)][k];
                const int32_t b = T[static_cast<size_t>(i)][(k + 1) % 3];
                for (int32_t p = at[static_cast<size_t>(b)]; p < at[static_cast<size_t>(b) + 1]; ++p) {
                    if (slot_dst(T, slot[static_cast<size_t>(p)]) == a) {
                        mesh->NB[static_cast<size_t>(i)][k] = slot[static_cast<size_t>(p)] / 3;
                        break;
                    }
                }
                if (dup == nullptr) {
                    continue;
                }
                // 每个线程只写自己的槽
                const auto self = static_cast<int32_t>(i * 3 + k);
                for (int32_t p = at[static_cast<size_t>(a)]; p < at[static_cast<size_t>(a) + 1]; ++p) {
                    if (slot[static_cast<size_t>(p)] != self && slot_dst(T, slot[static_cast<size_t>(p)]) == b) {
                        (*dup)[static_cast<size_t>(self)] = 1;
                        break;
                    }
                }
            }
        }
    });
}

void build_tri_grid(PolyMesh* mesh)
{
    const std::vector<std::array<int32_t, 3>>& T = mesh->T;
    mesh->goff.assign(1, 0);
    mesh->gtris.clear();
    mesh->gnx = 0;
    mesh->gny = 0;
    if (T.empty()) {
        return;
    }
    mesh->gox = std::numeric_limits<int64_t>::max();
    mesh->goy = std::numeric_limits<int64_t>::max();
    int64_t hx = std::numeric_limits<int64_t>::min();
    int64_t hy = std::numeric_limits<int64_t>::min();
    for (size_t i = 0; i < T.size(); ++i) {
        const TriBox b = tri_box(*mesh, i);
        mesh->gox = std::min(mesh->gox, b.x0);
        mesh->goy = std::min(mesh->goy, b.y0);
        hx = std::max(hx, b.x1);
        hy = std::max(hy, b.y1);
    }
    mesh->gnx = hx - mesh->gox + 1;
    mesh->gny = hy - mesh->goy + 1;
    mesh->goff.assign(static_cast<size_t>(mesh->gnx * mesh->gny) + 1, 0);
    for (size_t i = 0; i < T.size(); ++i) {
        const TriBox b = tri_box(*mesh, i);
        for (int64_t gx = b.x0; gx <= b.x1; ++gx) {
            for (int64_t gy = b.y0; gy <= b.y1; ++gy) {
                ++mesh->goff[static_cast<size_t>((gx - mesh->gox) * mesh->gny + (gy - mesh->goy)) + 1];
            }
        }
    }
    for (size_t c = 0; c + 1 < mesh->goff.size(); ++c) {
        mesh->goff[c + 1] += mesh->goff[c];
    }
    std::vector<int32_t> wr = mesh->goff;
    mesh->gtris.resize(static_cast<size_t>(mesh->goff.back()));
    for (size_t i = 0; i < T.size(); ++i) {
        const TriBox b = tri_box(*mesh, i);
        for (int64_t gx = b.x0; gx <= b.x1; ++gx) {
            for (int64_t gy = b.y0; gy <= b.y1; ++gy) {
                mesh->gtris[static_cast<size_t>(wr[static_cast<size_t>((gx - mesh->gox) * mesh->gny + (gy - mesh->goy))]++)] =
                    static_cast<int32_t>(i);
            }
        }
    }
}

// dup 非空时标出重复出现的有向边, 长 3*三角数, 按 i*3+k 索引
PolyMesh make_poly_mesh(std::vector<uint8_t>* dup, const BaseNavVertex* vb, int64_t nv, std::vector<std::array<int32_t, 3>> t)
{
    PolyMesh mesh;
    mesh.vb = vb;
    mesh.nv = nv;
    mesh.T = std::move(t);
    const int64_t nt = static_cast<int64_t>(mesh.T.size());
    ParallelChunks(nt, NavWorkerCount(nt), [&](size_t, int64_t b, int64_t e) {
        for (int64_t i = b; i < e; ++i) {
            auto& tri = mesh.T[static_cast<size_t>(i)];
            const WorldPoint a = MeshPoint(mesh, tri[0]);
            const WorldPoint bb = MeshPoint(mesh, tri[1]);
            const WorldPoint c = MeshPoint(mesh, tri[2]);
            const double abx = bb.x - a.x;
            const double aby = bb.y - a.y;
            const double acx = c.x - a.x;
            const double acy = c.y - a.y;
            if (abx * acy - aby * acx < 0.0) {
                std::swap(tri[1], tri[2]);
            }
        }
    });
    build_neighbors(&mesh, dup);
    build_tri_grid(&mesh);
    return mesh;
}

PolyMesh make_owned_poly_mesh(std::vector<uint8_t>* dup, std::vector<BaseNavVertex> own, std::vector<std::array<int32_t, 3>> t)
{
    PolyMesh mesh = make_poly_mesh(dup, own.data(), static_cast<int64_t>(own.size()), std::move(t));
    mesh.vown = std::move(own); // 堆块随 move 原样搬走
    return mesh;
}

// NB → bnd, 然后释放 NB
void fold_neighbors(PolyMesh* mesh)
{
    mesh->bnd.assign(mesh->T.size(), 0);
    for (size_t i = 0; i < mesh->NB.size(); ++i) {
        for (int k = 0; k < 3; ++k) {
            if (mesh->NB[i][k] < 0) {
                mesh->bnd[i] |= static_cast<uint8_t>(1U << k);
            }
        }
    }
    std::vector<std::array<int32_t, 3>>().swap(mesh->NB);
}

double cv_height(const std::vector<BaseNavVertex>& cv, int32_t i)
{
    return static_cast<double>(cv[static_cast<size_t>(i)].height);
}

int32_t find_root(std::vector<int32_t>* par, int32_t x)
{
    while ((*par)[x] != x) {
        (*par)[x] = (*par)[(*par)[x]];
        x = (*par)[x];
    }
    return x;
}

// 先按 radius 找, 一个都没有再放宽到兜底半径
std::vector<double> snap_radii(double radius)
{
    const double r = std::max(0.0, radius);
    if (r >= kSnapFallbackRadius) {
        return { r };
    }
    return { r, kSnapFallbackRadius };
}

// 三角 t 可走且离 p 不超过 rr 时给出吸附点
std::optional<ZoneSnapHit> snap_hit(const ZoneClean& zc, const WorldPoint& p, double rr, int32_t t)
{
    if (zc.walkable[static_cast<size_t>(t)] == 0) {
        return std::nullopt;
    }
    const auto& tri = zc.mesh.T[static_cast<size_t>(t)];
    const auto [sp, dist] = closestOnTri(p, { MeshPoint(zc.mesh, tri[0]), MeshPoint(zc.mesh, tri[1]), MeshPoint(zc.mesh, tri[2]) });
    if (dist > rr) {
        return std::nullopt;
    }
    return ZoneSnapHit { t, sp, dist };
}

std::array<double, 4> snap_key(const ZoneClean& zc, int32_t t, double dist, std::optional<double> floor_y)
{
    const double isl = zc.island[static_cast<size_t>(t)] != 0 ? 1.0 : 0.0;
    // 未给楼层时多层同距取最高层, 高层能往下掉, 低层爬不上去
    if (!floor_y.has_value()) {
        return { dist, -triHeight(zc.mesh, t), isl, static_cast<double>(t) };
    }
    const double delta = std::fabs(triHeight(zc.mesh, t) - *floor_y);
    return { delta <= static_cast<double>(kBaseNavFloorBand) ? 0.0 : 1.0, dist, delta, isl };
}

}

std::vector<int32_t> TrisNear(const PolyMesh& mesh, const WorldPoint& p, double r)
{
    return TrisInBox(mesh, p.x - r, p.y - r, p.x + r, p.y + r);
}

std::vector<int32_t> TrisInBox(const PolyMesh& mesh, double x0, double y0, double x1, double y1)
{
    std::vector<int32_t> out;
    const int64_t qx0 = std::max(mesh.gox, static_cast<int64_t>(std::floor(x0 / kTriGridCell)));
    const int64_t qx1 = std::min(mesh.gox + mesh.gnx - 1, static_cast<int64_t>(std::floor(x1 / kTriGridCell)));
    const int64_t qy0 = std::max(mesh.goy, static_cast<int64_t>(std::floor(y0 / kTriGridCell)));
    const int64_t qy1 = std::min(mesh.goy + mesh.gny - 1, static_cast<int64_t>(std::floor(y1 / kTriGridCell)));
    for (int64_t gx = qx0; gx <= qx1; ++gx) {
        for (int64_t gy = qy0; gy <= qy1; ++gy) {
            const auto c = static_cast<size_t>((gx - mesh.gox) * mesh.gny + (gy - mesh.goy));
            for (int32_t s = mesh.goff[c]; s < mesh.goff[c + 1]; ++s) {
                out.push_back(mesh.gtris[static_cast<size_t>(s)]);
            }
        }
    }
    std::sort(out.begin(), out.end());
    out.erase(std::unique(out.begin(), out.end()), out.end());
    return out;
}

ZoneClean BuildZoneClean(const BaseNavPack& pack, const BaseNavPlanner& planner, const std::string& zone_name, uint32_t walkable_flags)
{
    ZoneClean zc;
    zc.name = zone_name;
    zc.walkable_flags = walkable_flags;
    const BaseNavZone* zone = FindBaseNavZoneByName(pack, zone_name);
    if (zone == nullptr || zone->triangle_count == 0) {
        zc.error = zone_name + ": zone not found or empty";
        return zc;
    }
    zc.zone_id = zone->zone_id;
    zc.lo = zone->first_triangle;
    zc.hi = zc.lo + zone->triangle_count;
    const auto& ptris = pack.triangles;
    const auto& pverts = pack.vertices;
    // 带 BGEO 段的包顶点已精确焊接, 再取整焊接只会挪动坐标
    const bool source_exact =
        FindBaseNavSection(pack, "BGEO") != nullptr || (!pack.surfaces.empty() && pack.surfaces.size() == ptris.size());
    const bool has_source_surfaces = !pack.surfaces.empty() && pack.surfaces.size() == ptris.size();

    const int64_t m_all = zc.hi - zc.lo;
    zc.walkable.assign(static_cast<size_t>(m_all), 1);
    int64_t n_masked = 0;
    if (has_source_surfaces) {
        const auto& psurf = pack.surfaces;
        for (int64_t i = 0; i < m_all; ++i) {
            if ((psurf[static_cast<size_t>(zc.lo + i)].flags & zc.walkable_flags) == 0U) {
                zc.walkable[static_cast<size_t>(i)] = 0;
                ++n_masked;
            }
        }
    }
    if (n_masked == m_all) {
        zc.error = zone_name + ": walkable mask " + std::to_string(zc.walkable_flags) + " left no walkable tris";
        return zc;
    }

    uint32_t vmin = UINT32_MAX;
    uint32_t vmax = 0;
    for (int64_t i = zc.lo; i < zc.hi; ++i) {
        for (const uint32_t vi : ptris[static_cast<size_t>(i)].vertices) {
            vmin = std::min(vmin, vi);
            vmax = std::max(vmax, vi);
        }
    }
    const int64_t nv = static_cast<int64_t>(vmax) - vmin + 1;
    std::vector<BaseNavVertex> CV;
    std::vector<int32_t> MAP;
    int64_t n_weld = 0;
    if (!source_exact) {
        CV.resize(static_cast<size_t>(nv));
        for (int64_t i = 0; i < nv; ++i) {
            const auto& vt = pverts[vmin + static_cast<size_t>(i)];
            CV[static_cast<size_t>(i)].u = static_cast<float>(std::nearbyint(static_cast<double>(vt.u) * 20.0) / 20.0);
            CV[static_cast<size_t>(i)].v = static_cast<float>(std::nearbyint(static_cast<double>(vt.v) * 20.0) / 20.0);
            CV[static_cast<size_t>(i)].height = vt.height;
        }
        MAP.resize(static_cast<size_t>(nv));
        std::iota(MAP.begin(), MAP.end(), 0);
        std::vector<int64_t> kk(static_cast<size_t>(nv));
        for (int64_t i = 0; i < nv; ++i) {
            const int64_t kx = static_cast<int64_t>(std::nearbyint(static_cast<double>(CV[static_cast<size_t>(i)].u) * 1e4));
            const int64_t ky = static_cast<int64_t>(std::nearbyint(static_cast<double>(CV[static_cast<size_t>(i)].v) * 1e4));
            kk[static_cast<size_t>(i)] = kx * (int64_t(1) << 40) + ky;
        }
        std::vector<int32_t> order(static_cast<size_t>(nv));
        std::iota(order.begin(), order.end(), 0);
        std::stable_sort(order.begin(), order.end(), [&](int32_t a, int32_t b) { return kk[a] < kk[b]; });
        for (size_t s0 = 0; s0 < order.size();) {
            size_t e0 = s0 + 1;
            while (e0 < order.size() && kk[order[e0]] == kk[order[s0]]) {
                ++e0;
            }
            if (e0 - s0 >= 2) {
                std::vector<int32_t> ids(order.begin() + s0, order.begin() + e0);
                std::stable_sort(ids.begin(), ids.end(), [&CV](int32_t a, int32_t b) { return cv_height(CV, a) < cv_height(CV, b); });
                int32_t rep = ids[0];
                for (size_t t = 1; t < ids.size(); ++t) {
                    if (cv_height(CV, ids[t]) - cv_height(CV, ids[t - 1]) <= kWeldDh) {
                        MAP[ids[t]] = rep;
                        ++n_weld;
                    }
                    else {
                        rep = ids[t];
                    }
                }
            }
            s0 = e0;
        }
    }
    std::vector<std::array<int32_t, 3>> CT2(static_cast<size_t>(zc.hi - zc.lo));
    int64_t degen = 0;
    for (int64_t i = 0; i < zc.hi - zc.lo; ++i) {
        auto& row = CT2[static_cast<size_t>(i)];
        for (int k = 0; k < 3; ++k) {
            const auto local = static_cast<int32_t>(ptris[static_cast<size_t>(zc.lo + i)].vertices[k] - vmin);
            row[k] = MAP.empty() ? local : MAP[static_cast<size_t>(local)];
        }
        if (row[0] == row[1] || row[1] == row[2] || row[2] == row[0]) {
            ++degen;
        }
    }
    if (degen != 0) {
        zc.error = zone_name + ": weld produced " + std::to_string(degen) + " degenerate tris";
        return zc;
    }
    std::vector<int32_t>().swap(MAP);

    std::vector<uint8_t> dup;
    zc.mesh = source_exact ? make_poly_mesh(&dup, pverts.data() + vmin, nv, std::move(CT2))
                           : make_owned_poly_mesh(&dup, std::move(CV), std::move(CT2));
    const auto& T = zc.mesh.T;
    auto& NB = zc.mesh.NB;
    const int64_t m = static_cast<int64_t>(T.size());
    int64_t n_dup = 0;
    for (int64_t slot = 0; slot < 3 * m; ++slot) {
        if (dup[static_cast<size_t>(slot)] == 0) {
            continue;
        }
        const int64_t i = slot / 3;
        const int64_t k = slot % 3;
        const int32_t j = NB[static_cast<size_t>(i)][k];
        NB[static_cast<size_t>(i)][k] = -1;
        if (j >= 0) {
            for (int k2 = 0; k2 < 3; ++k2) {
                if (NB[static_cast<size_t>(j)][k2] == i) {
                    NB[static_cast<size_t>(j)][k2] = -1;
                }
            }
        }
        ++n_dup;
    }
    std::vector<int64_t> kills;
    {
        const size_t nw = NavWorkerCount(3 * m);
        std::vector<std::vector<int64_t>> bins(nw);
        ParallelChunks(3 * m, nw, [&](size_t w, int64_t lo_s, int64_t hi_s) {
            for (int64_t slot = lo_s; slot < hi_s; ++slot) {
                const int32_t j = NB[static_cast<size_t>(slot / 3)][slot % 3];
                if (j < 0) {
                    continue;
                }
                const auto& back = NB[static_cast<size_t>(j)];
                if (back[0] != slot / 3 && back[1] != slot / 3 && back[2] != slot / 3) {
                    bins[w].push_back(slot);
                }
            }
        });
        ConcatBins(bins, kills);
    }
    for (const int64_t slot : kills) {
        NB[static_cast<size_t>(slot / 3)][slot % 3] = -1;
    }

    std::vector<int64_t> cand;
    int64_t n_mask_cut = 0;
    {
        const size_t nw = NavWorkerCount(3 * m);
        std::vector<std::vector<int64_t>> bins(nw);
        std::vector<int64_t> masked(nw, 0);
        ParallelChunks(3 * m, nw, [&](size_t w, int64_t lo_s, int64_t hi_s) {
            for (int64_t slot = lo_s; slot < hi_s; ++slot) {
                const int64_t i = slot / 3;
                const int32_t j = NB[static_cast<size_t>(i)][slot % 3];
                if (j < 0) {
                    continue;
                }
                if (zc.walkable[static_cast<size_t>(i)] == 0 || zc.walkable[static_cast<size_t>(j)] == 0) {
                    bins[w].push_back(slot);
                    ++masked[w];
                    continue;
                }
                const auto a = static_cast<int32_t>(std::min<int64_t>(i, j));
                const auto b = static_cast<int32_t>(std::max<int64_t>(i, j));
                if (!HasLink(planner, pack, static_cast<uint32_t>(zc.lo + a), static_cast<uint32_t>(zc.lo + b))) {
                    bins[w].push_back(slot);
                }
            }
        });
        ConcatBins(bins, cand);
        for (const int64_t c : masked) {
            n_mask_cut += c;
        }
    }
    int64_t n_cut = 0;
    for (const int64_t slot : cand) {
        const int64_t i = slot / 3;
        const int32_t j = NB[static_cast<size_t>(i)][slot % 3];
        if (j < 0) {
            continue;
        }
        NB[static_cast<size_t>(i)][slot % 3] = -1;
        for (int k2 = 0; k2 < 3; ++k2) {
            if (NB[static_cast<size_t>(j)][k2] == i) {
                NB[static_cast<size_t>(j)][k2] = -1;
            }
        }
        ++n_cut;
    }

    std::vector<int32_t> par(static_cast<size_t>(m));
    std::iota(par.begin(), par.end(), 0);
    for (int64_t t = 0; t < m; ++t) {
        for (int k = 0; k < 3; ++k) {
            const int32_t nb = NB[static_cast<size_t>(t)][k];
            if (nb >= 0) {
                const int32_t ra = find_root(&par, static_cast<int32_t>(t));
                const int32_t rb = find_root(&par, nb);
                if (ra != rb) {
                    par[std::max(ra, rb)] = std::min(ra, rb);
                }
            }
        }
    }
    fold_neighbors(&zc.mesh);
    for (int64_t i = 0; i < m; ++i) {
        par[static_cast<size_t>(i)] = find_root(&par, static_cast<int32_t>(i));
    }

    std::vector<int32_t> n_tot(static_cast<size_t>(m), 0);
    std::vector<int32_t> n_isl(static_cast<size_t>(m), 0);
    for (int64_t i = 0; i < m; ++i) {
        ++n_tot[par[static_cast<size_t>(i)]];
        if (IsSmallIslandTriangle(planner, static_cast<uint32_t>(zc.lo + i))) {
            ++n_isl[par[static_cast<size_t>(i)]];
        }
    }
    zc.island.resize(static_cast<size_t>(m));
    for (int64_t i = 0; i < m; ++i) {
        const auto c = static_cast<size_t>(par[static_cast<size_t>(i)]);
        zc.island[static_cast<size_t>(i)] = (n_tot[c] == 0 || n_isl[c] * 2 > n_tot[c]) ? 1 : 0;
    }

    int64_t ncomps = 0;
    for (int64_t i = 0; i < m; ++i) {
        if (par[static_cast<size_t>(i)] == i) {
            ++ncomps;
        }
    }
    zc.stats = source_exact ? "source-exact " : "weld " + std::to_string(n_weld) + "v ";
    zc.stats +=
        "mask " + std::to_string(zc.walkable_flags) + " masked " + std::to_string(n_masked) + " cut " + std::to_string(n_mask_cut) + ", ";
    zc.stats += "dup-sever " + std::to_string(n_dup) + ", link-mask cut " + std::to_string(n_cut) + ", comps " + std::to_string(ncomps);
    return zc;
}

bool ZoneCleanValid(const ZoneClean& zc)
{
    return zc.error.empty();
}

std::optional<ZoneSnapHit> ZoneSnap(const ZoneClean& zc, const WorldPoint& p, double radius, std::optional<double> floor_y)
{
    for (const double rr : snap_radii(radius)) {
        bool have = false;
        std::array<double, 4> bk {};
        ZoneSnapHit best;
        for (const int32_t t : TrisNear(zc.mesh, p, rr)) {
            const std::optional<ZoneSnapHit> hit = snap_hit(zc, p, rr, t);
            if (!hit.has_value()) {
                continue;
            }
            const std::array<double, 4> k = snap_key(zc, t, hit->dist, floor_y);
            if (!have || k < bk) {
                have = true;
                bk = k;
                best = *hit;
            }
        }
        if (have) {
            return best;
        }
    }
    return std::nullopt;
}

std::vector<ZoneSnapHit> ZoneSnapCandidates(const ZoneClean& zc, const WorldPoint& p, double radius, std::optional<double> floor_y)
{
    std::vector<ZoneSnapHit> out;
    for (const double rr : snap_radii(radius)) {
        std::vector<std::pair<std::array<double, 4>, ZoneSnapHit>> ring;
        for (const int32_t t : TrisNear(zc.mesh, p, rr)) {
            const std::optional<ZoneSnapHit> hit = snap_hit(zc, p, rr, t);
            if (hit.has_value()) {
                ring.emplace_back(snap_key(zc, t, hit->dist, floor_y), *hit);
            }
        }
        // 同键保持三角号升序, 与 ZoneSnap 同键取先到者一致
        std::stable_sort(ring.begin(), ring.end(), [](const auto& a, const auto& b) { return a.first < b.first; });
        for (const auto& [key, hit] : ring) {
            out.push_back(hit);
        }
    }
    return out;
}

std::optional<ZoneSnapHit> ZoneSnapOnDeck(const ZoneClean& zc, const WorldPoint& p, double radius, double deck_y)
{
    for (const double rr : snap_radii(radius)) {
        bool have = false;
        std::array<double, 4> bk {};
        ZoneSnapHit best;
        for (const int32_t t : TrisNear(zc.mesh, p, rr)) {
            const std::optional<ZoneSnapHit> hit = snap_hit(zc, p, rr, t);
            if (!hit.has_value()) {
                continue;
            }
            const double delta = std::fabs(triHeight(zc.mesh, t) - deck_y);
            const std::array<double, 4> k = { delta <= kDeckBand ? 0.0 : 1.0, hit->dist, delta, static_cast<double>(t) };
            if (!have || k < bk) {
                have = true;
                bk = k;
                best = *hit;
            }
        }
        if (have) {
            return best;
        }
    }
    return std::nullopt;
}

}
