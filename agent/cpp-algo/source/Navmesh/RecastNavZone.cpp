#include "RecastNavZone.h"

#include <algorithm>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <deque>
#include <numeric>
#include <utility>

#include "BaseNavGeometry.h"

namespace navmesh::recast
{

namespace
{

constexpr double kPortalSamples[5] = { 0.5, 0.25, 0.75, 0.1, 0.9 }; // 共边 hop 的门户采样位

double triHeight(const PolyMesh& mesh, int32_t t)
{
    const auto& tri = mesh.T[static_cast<size_t>(t)];
    return (mesh.H[tri[0]] + mesh.H[tri[1]] + mesh.H[tri[2]]) / 3.0;
}

std::pair<WorldPoint, double> closestOnTri(const WorldPoint& p, const std::array<WorldPoint, 3>& tri)
{
    if (detail::PointInTriangle(p, tri)) {
        return { p, 0.0 };
    }
    const WorldPoint q = detail::ClosestPointOnTriangle(p, tri);
    return { q, std::hypot(q.x - p.x, q.y - p.y) };
}

}

PolyMesh::PolyMesh(std::vector<WorldPoint> v, std::vector<std::array<int32_t, 3>> t, std::vector<double> h)
    : V(std::move(v))
    , H(std::move(h))
    , T(std::move(t))
{
    for (auto& tri : T) {
        const WorldPoint& a = V[tri[0]];
        const double abx = V[tri[1]].x - a.x;
        const double aby = V[tri[1]].y - a.y;
        const double acx = V[tri[2]].x - a.x;
        const double acy = V[tri[2]].y - a.y;
        if (abx * acy - aby * acx < 0.0) {
            std::swap(tri[1], tri[2]);
        }
    }
    buildNb();
    buildGrid();
}

// 重 key 取稳定序首槽
void PolyMesh::buildNb()
{
    const int64_t m = static_cast<int64_t>(T.size());
    NB.assign(T.size(), { -1, -1, -1 });
    if (m == 0) {
        return;
    }
    // 有向边按起点分桶: 每个顶点平均只挂六条边, 找反向边扫本桶即可。桶内按槽号递增填,
    // 所以首个命中就是稳定序里的首槽, 与把三倍三角数的边整体排一遍再取首个逐位相同。
    const int64_t n = static_cast<int64_t>(V.size());
    std::vector<int32_t> at(static_cast<size_t>(n) + 1, 0);
    for (const auto& tri : T) {
        for (const int32_t a : tri) {
            ++at[static_cast<size_t>(a) + 1];
        }
    }
    for (int64_t i = 0; i < n; ++i) {
        at[static_cast<size_t>(i) + 1] += at[static_cast<size_t>(i)];
    }
    std::vector<int32_t> wr = at;
    std::vector<int32_t> dst(static_cast<size_t>(3 * m));
    std::vector<int32_t> slot(static_cast<size_t>(3 * m));
    for (int64_t i = 0; i < m; ++i) {
        for (int64_t k = 0; k < 3; ++k) {
            const int32_t a = T[static_cast<size_t>(i)][k];
            const auto w = static_cast<size_t>(wr[static_cast<size_t>(a)]++);
            dst[w] = T[static_cast<size_t>(i)][(k + 1) % 3];
            slot[w] = static_cast<int32_t>(i * 3 + k);
        }
    }
    for (int64_t i = 0; i < m; ++i) {
        for (int64_t k = 0; k < 3; ++k) {
            const int32_t a = T[static_cast<size_t>(i)][k];
            const int32_t b = T[static_cast<size_t>(i)][(k + 1) % 3];
            for (int32_t p = at[static_cast<size_t>(b)]; p < at[static_cast<size_t>(b) + 1]; ++p) {
                if (dst[static_cast<size_t>(p)] == a) {
                    NB[static_cast<size_t>(i)][k] = slot[static_cast<size_t>(p)] / 3;
                    break;
                }
            }
        }
    }
}

void PolyMesh::buildGrid()
{
    struct Box
    {
        int64_t x0, y0, x1, y1;
    };
    const auto box = [&](size_t i) {
        const WorldPoint& a = V[T[i][0]];
        const WorldPoint& b = V[T[i][1]];
        const WorldPoint& c = V[T[i][2]];
        return Box { static_cast<int64_t>(std::floor(std::min({ a.x, b.x, c.x }) / kGridCell)),
                     static_cast<int64_t>(std::floor(std::min({ a.y, b.y, c.y }) / kGridCell)),
                     static_cast<int64_t>(std::floor(std::max({ a.x, b.x, c.x }) / kGridCell)),
                     static_cast<int64_t>(std::floor(std::max({ a.y, b.y, c.y }) / kGridCell)) };
    };
    goff.assign(1, 0);
    gtris.clear();
    gnx = 0;
    gny = 0;
    if (T.empty()) {
        return;
    }
    gox = std::numeric_limits<int64_t>::max();
    goy = std::numeric_limits<int64_t>::max();
    int64_t hx = std::numeric_limits<int64_t>::min();
    int64_t hy = std::numeric_limits<int64_t>::min();
    for (size_t i = 0; i < T.size(); ++i) {
        const Box b = box(i);
        gox = std::min(gox, b.x0);
        goy = std::min(goy, b.y0);
        hx = std::max(hx, b.x1);
        hy = std::max(hy, b.y1);
    }
    gnx = hx - gox + 1;
    gny = hy - goy + 1;
    goff.assign(static_cast<size_t>(gnx * gny) + 1, 0);
    for (size_t i = 0; i < T.size(); ++i) {
        const Box b = box(i);
        for (int64_t gx = b.x0; gx <= b.x1; ++gx) {
            for (int64_t gy = b.y0; gy <= b.y1; ++gy) {
                ++goff[static_cast<size_t>((gx - gox) * gny + (gy - goy)) + 1];
            }
        }
    }
    for (size_t c = 0; c + 1 < goff.size(); ++c) {
        goff[c + 1] += goff[c];
    }
    std::vector<int32_t> wr = goff;
    gtris.resize(static_cast<size_t>(goff.back()));
    for (size_t i = 0; i < T.size(); ++i) {
        const Box b = box(i);
        for (int64_t gx = b.x0; gx <= b.x1; ++gx) {
            for (int64_t gy = b.y0; gy <= b.y1; ++gy) {
                gtris[static_cast<size_t>(wr[static_cast<size_t>((gx - gox) * gny + (gy - goy))]++)] = static_cast<int32_t>(i);
            }
        }
    }
}

std::vector<int32_t> PolyMesh::trisNear(const WorldPoint& p, double r) const
{
    std::vector<int32_t> out;
    const int64_t qx0 = std::max(gox, static_cast<int64_t>(std::floor((p.x - r) / kGridCell)));
    const int64_t qx1 = std::min(gox + gnx - 1, static_cast<int64_t>(std::floor((p.x + r) / kGridCell)));
    const int64_t qy0 = std::max(goy, static_cast<int64_t>(std::floor((p.y - r) / kGridCell)));
    const int64_t qy1 = std::min(goy + gny - 1, static_cast<int64_t>(std::floor((p.y + r) / kGridCell)));
    for (int64_t gx = qx0; gx <= qx1; ++gx) {
        for (int64_t gy = qy0; gy <= qy1; ++gy) {
            const auto c = static_cast<size_t>((gx - gox) * gny + (gy - goy));
            for (int32_t s = goff[c]; s < goff[c + 1]; ++s) {
                out.push_back(gtris[static_cast<size_t>(s)]);
            }
        }
    }
    std::sort(out.begin(), out.end());
    out.erase(std::unique(out.begin(), out.end()), out.end());
    return out;
}

ZoneClean::ZoneClean(
    const BaseNavPack& pack, const BaseNavPlanner& planner, const std::string& zone_name, uint32_t walkable_flags_in)
{
    name = zone_name;
    walkable_flags = walkable_flags_in;
    const BaseNavZone* zone = pack.findZoneByName(zone_name);
    if (zone == nullptr || zone->triangle_count == 0) {
        error_ = zone_name + ": zone not found or empty";
        return;
    }
    zone_id = zone->zone_id;
    lo = zone->first_triangle;
    hi = lo + zone->triangle_count;
    const auto& ptris = pack.triangles();
    const auto& pverts = pack.vertices();
    // 坐标是不是导出侧精确焊过的,认 BGEO 段 —— 那是这一代包才有的几何段,带它的包
    // 顶点身份已经定死,再按 0.05 网格 round 一遍并二次焊接只会挪动坐标。BSRF 只是
    // 可选的取证段,拿它当几何判据的话,一个纯可走包(不带 BSRF)会被误判成老包。
    const bool source_exact = pack.section("BGEO") != nullptr
        || (!pack.surfaces().empty() && pack.surfaces().size() == ptris.size());
    const bool has_source_surfaces = !pack.surfaces().empty() && pack.surfaces().size() == ptris.size();

    // 源语义:BSRF 里那 32 位 flags 说了算,掩码没命中的三角不是可走面。就地打标,不压缩、
    // 不重排 —— 下面六处消费点各自跳过它们(邻接、分量、hop、吸附、墙判据、体素化),
    // 几何本身照旧留在包和网格里。没有 surface 表的包全部可走:纯可走包里「在包里」
    // 本身就是可走的意思,旧包则行为与历史逐字节相同。
    const int64_t m_all = hi - lo;
    walkable.assign(static_cast<size_t>(m_all), 1);
    int64_t n_masked = 0;
    if (has_source_surfaces) {
        const auto& psurf = pack.surfaces();
        for (int64_t i = 0; i < m_all; ++i) {
            if ((psurf[static_cast<size_t>(lo + i)].flags & walkable_flags) == 0U) {
                walkable[static_cast<size_t>(i)] = 0;
                ++n_masked;
            }
        }
    }
    if (n_masked == m_all) {
        error_ = zone_name + ": walkable mask " + std::to_string(walkable_flags) + " left no walkable tris";
        return;
    }

    // 带源 surface 表的新包保留导出坐标与顶点身份；旧包继续走历史焊接规则。
    uint32_t vmin = UINT32_MAX;
    uint32_t vmax = 0;
    for (int64_t i = lo; i < hi; ++i) {
        for (const uint32_t vi : ptris[static_cast<size_t>(i)].vertices) {
            vmin = std::min(vmin, vi);
            vmax = std::max(vmax, vi);
        }
    }
    const int64_t nv = static_cast<int64_t>(vmax) - vmin + 1;
    std::vector<WorldPoint> CV(static_cast<size_t>(nv));
    std::vector<double> CH(static_cast<size_t>(nv));
    for (int64_t i = 0; i < nv; ++i) {
        const auto& vt = pverts[vmin + static_cast<size_t>(i)];
        CV[static_cast<size_t>(i)] = source_exact
            ? WorldPoint { static_cast<double>(vt.u), static_cast<double>(vt.v) }
            : WorldPoint { std::nearbyint(static_cast<double>(vt.u) * 20.0) / 20.0,
                           std::nearbyint(static_cast<double>(vt.v) * 20.0) / 20.0 };
        CH[static_cast<size_t>(i)] = static_cast<double>(vt.height);
    }

    std::vector<int32_t> MAP(static_cast<size_t>(nv));
    std::iota(MAP.begin(), MAP.end(), 0);
    int64_t n_weld = 0;
    if (!source_exact) {
        std::vector<int64_t> kk(static_cast<size_t>(nv));
        for (int64_t i = 0; i < nv; ++i) {
            const int64_t kx = static_cast<int64_t>(std::nearbyint(CV[static_cast<size_t>(i)].x * 1e4));
            const int64_t ky = static_cast<int64_t>(std::nearbyint(CV[static_cast<size_t>(i)].y * 1e4));
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
                std::stable_sort(ids.begin(), ids.end(), [&](int32_t a, int32_t b) { return CH[a] < CH[b]; });
                int32_t rep = ids[0];
                for (size_t t = 1; t < ids.size(); ++t) {
                    if (CH[ids[t]] - CH[ids[t - 1]] <= kWeldDh) {
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
    std::vector<std::array<int32_t, 3>> CT2(static_cast<size_t>(hi - lo));
    int64_t degen = 0;
    for (int64_t i = 0; i < hi - lo; ++i) {
        auto& row = CT2[static_cast<size_t>(i)];
        for (int k = 0; k < 3; ++k) {
            row[k] = MAP[ptris[static_cast<size_t>(lo + i)].vertices[k] - vmin];
        }
        if (row[0] == row[1] || row[1] == row[2] || row[2] == row[0]) {
            ++degen;
        }
    }
    if (degen != 0) {
        error_ = zone_name + ": weld produced " + std::to_string(degen) + " degenerate tris";
        return;
    }

    mesh = PolyMesh(std::move(CV), std::move(CT2), std::move(CH));
    const auto& T = mesh.T;
    auto& NB = mesh.NB;
    const int64_t m = static_cast<int64_t>(T.size());
    const int64_t nvv = static_cast<int64_t>(mesh.V.size());

    // 同一条有向边出现两次以上的槽。按起点分桶后同一条边必落在同一桶里, 桶内平均六项,
    // 就地数一遍即可, 不必给三倍三角数的边各算一次键再查散列。
    std::vector<uint8_t> dup(static_cast<size_t>(3 * m), 0);
    {
        std::vector<int32_t> at(static_cast<size_t>(nvv) + 1, 0);
        for (const auto& tri : T) {
            for (const int32_t a : tri) {
                ++at[static_cast<size_t>(a) + 1];
            }
        }
        for (int64_t i = 0; i < nvv; ++i) {
            at[static_cast<size_t>(i) + 1] += at[static_cast<size_t>(i)];
        }
        std::vector<int32_t> wr = at;
        std::vector<int32_t> dst(static_cast<size_t>(3 * m));
        std::vector<int32_t> slt(static_cast<size_t>(3 * m));
        for (int64_t i = 0; i < m; ++i) {
            for (int64_t k = 0; k < 3; ++k) {
                const auto w = static_cast<size_t>(wr[static_cast<size_t>(T[static_cast<size_t>(i)][k])]++);
                dst[w] = T[static_cast<size_t>(i)][(k + 1) % 3];
                slt[w] = static_cast<int32_t>(i * 3 + k);
            }
        }
        for (int64_t v = 0; v < nvv; ++v) {
            for (int32_t p = at[static_cast<size_t>(v)]; p < at[static_cast<size_t>(v) + 1]; ++p) {
                for (int32_t q = p + 1; q < at[static_cast<size_t>(v) + 1]; ++q) {
                    if (dst[static_cast<size_t>(p)] == dst[static_cast<size_t>(q)]) {
                        dup[static_cast<size_t>(slt[static_cast<size_t>(p)])] = 1;
                        dup[static_cast<size_t>(slt[static_cast<size_t>(q)])] = 1;
                    }
                }
            }
        }
    }
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
    for (int64_t slot = 0; slot < 3 * m; ++slot) {
        const int32_t j = NB[static_cast<size_t>(slot / 3)][slot % 3];
        if (j < 0) {
            continue;
        }
        const auto& back = NB[static_cast<size_t>(j)];
        if (back[0] != slot / 3 && back[1] != slot / 3 && back[2] != slot / 3) {
            kills.push_back(slot);
        }
    }
    for (const int64_t slot : kills) {
        NB[static_cast<size_t>(slot / 3)][slot % 3] = -1;
    }

    // NB 掩码:焊接邻接必须在 pack link 表有背书,无背书的缝一律割掉
    const auto& offs = planner.adjacencyOffsets();
    const auto& lnks = planner.adjacencyLinks();
    std::vector<std::pair<int32_t, int32_t>> lab;
    for (int64_t src = lo; src < hi; ++src) {
        for (uint32_t li = offs[static_cast<size_t>(src)]; li < offs[static_cast<size_t>(src) + 1]; ++li) {
            const int64_t tgt = lnks[li];
            if (tgt >= lo && tgt < hi && src < tgt) {
                lab.emplace_back(static_cast<int32_t>(src - lo), static_cast<int32_t>(tgt - lo));
            }
        }
    }
    // 有背书的三角对按小号一端分桶。一个三角挂不了几条链接, 桶内直查比散列表快,
    // 而且查的是同一个集合, 结果与散列表一致。
    std::vector<int32_t> lat(static_cast<size_t>(m) + 1, 0);
    for (const auto& e : lab) {
        ++lat[static_cast<size_t>(e.first) + 1];
    }
    for (int64_t i = 0; i < m; ++i) {
        lat[static_cast<size_t>(i) + 1] += lat[static_cast<size_t>(i)];
    }
    std::vector<int32_t> lwr = lat;
    std::vector<int32_t> lhi(lab.size());
    for (const auto& e : lab) {
        lhi[static_cast<size_t>(lwr[static_cast<size_t>(e.first)]++)] = e.second;
    }
    std::vector<int64_t> cand;
    int64_t n_mask_cut = 0;
    for (int64_t slot = 0; slot < 3 * m; ++slot) {
        const int64_t i = slot / 3;
        const int32_t j = NB[static_cast<size_t>(i)][slot % 3];
        if (j < 0) {
            continue;
        }
        // 掩码外的三角一条缝都不接:它跟谁都断,自己落成孤立分量。割在并查集之前,
        // 分量因此天然把水体、禁区与可走面分开。
        if (walkable[static_cast<size_t>(i)] == 0 || walkable[static_cast<size_t>(j)] == 0) {
            cand.push_back(slot);
            ++n_mask_cut;
            continue;
        }
        const auto a = static_cast<int32_t>(std::min<int64_t>(i, j));
        const auto b = static_cast<int32_t>(std::max<int64_t>(i, j));
        bool hit = false;
        for (int32_t p = lat[static_cast<size_t>(a)]; p < lat[static_cast<size_t>(a) + 1] && !hit; ++p) {
            hit = lhi[static_cast<size_t>(p)] == b;
        }
        if (!hit) {
            cand.push_back(slot);
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
    const auto find = [&](int32_t x) {
        while (par[x] != x) {
            par[x] = par[par[x]];
            x = par[x];
        }
        return x;
    };
    for (int64_t t = 0; t < m; ++t) {
        for (int k = 0; k < 3; ++k) {
            const int32_t nb = NB[static_cast<size_t>(t)][k];
            if (nb >= 0) {
                const int32_t ra = find(static_cast<int32_t>(t));
                const int32_t rb = find(nb);
                if (ra != rb) {
                    par[std::max(ra, rb)] = std::min(ra, rb);
                }
            }
        }
    }
    comp.resize(static_cast<size_t>(m));
    for (int64_t i = 0; i < m; ++i) {
        comp[static_cast<size_t>(i)] = find(static_cast<int32_t>(i));
    }

    // 岛 = 天然分量(pack n 字段)不超过阈值的三角占多数的 comp
    std::vector<int64_t> n_tot(static_cast<size_t>(m), 0);
    std::vector<int64_t> n_isl(static_cast<size_t>(m), 0);
    for (int64_t i = 0; i < m; ++i) {
        ++n_tot[comp[static_cast<size_t>(i)]];
        if (planner.isSmallIslandTriangle(static_cast<uint32_t>(lo + i))) {
            ++n_isl[comp[static_cast<size_t>(i)]];
        }
    }
    comp_island.resize(static_cast<size_t>(m));
    for (int64_t c = 0; c < m; ++c) {
        comp_island[static_cast<size_t>(c)] =
            (n_tot[static_cast<size_t>(c)] == 0 || n_isl[static_cast<size_t>(c)] * 2 > n_tot[static_cast<size_t>(c)]) ? 1 : 0;
    }

    // link 层 hop:NB 已邻接跳过;跨分量共焊边 → 门户采样,否则 touch/bridge;
    // 同分量共焊边且非近连通 → srcadj 窄通道
    std::vector<int32_t> ia;
    std::vector<int32_t> ib;
    for (const auto& [la, lb] : lab) {
        // 掩码外的三角不接 hop。下面两个消费循环(跨分量门户、同分量 srcadj 窄通道)
        // 都从 ia/ib 取料,滤在这里就是两处一起滤。
        if (walkable[static_cast<size_t>(la)] == 0 || walkable[static_cast<size_t>(lb)] == 0) {
            continue;
        }
        const auto& row = NB[static_cast<size_t>(la)];
        if (row[0] != lb && row[1] != lb && row[2] != lb) {
            ia.push_back(la);
            ib.push_back(lb);
        }
    }
    const auto nshared = [&](int32_t a, int32_t b) {
        int n = 0;
        for (int ka = 0; ka < 3; ++ka) {
            for (int kb = 0; kb < 3; ++kb) {
                if (T[static_cast<size_t>(a)][ka] == T[static_cast<size_t>(b)][kb]) {
                    ++n;
                    break;
                }
            }
        }
        return n;
    };
    const auto pushPortals = [&](int32_t ti, int32_t tj) {
        int32_t sh[2] = { -1, -1 };
        int ns = 0;
        for (int ka = 0; ka < 3 && ns < 2; ++ka) {
            const int32_t v = T[static_cast<size_t>(ti)][ka];
            if (v == T[static_cast<size_t>(tj)][0] || v == T[static_cast<size_t>(tj)][1] || v == T[static_cast<size_t>(tj)][2]) {
                sh[ns++] = v;
            }
        }
        const WorldPoint p0 = mesh.V[sh[0]];
        const WorldPoint p1 = mesh.V[sh[1]];
        for (const double t : kPortalSamples) {
            const WorldPoint pt { p0.x + (p1.x - p0.x) * t, p0.y + (p1.y - p0.y) * t };
            hops.push_back({ pt, pt, tj });
            hops.push_back({ pt, pt, ti });
        }
    };
    int64_t n_edge = 0;
    int64_t n_touch = 0;
    int64_t n_bridge = 0;
    int64_t n_srcadj = 0;
    for (size_t r = 0; r < ia.size(); ++r) {
        const int32_t i = ia[r];
        const int32_t j = ib[r];
        if (comp[static_cast<size_t>(i)] == comp[static_cast<size_t>(j)]) {
            continue;
        }
        if (nshared(i, j) == 2) {
            pushPortals(i, j);
            ++n_edge;
        }
        else {
            const auto br = planner.closestEdgeBridgePoints(static_cast<uint32_t>(lo + i), static_cast<uint32_t>(lo + j));
            if (!br) {
                continue;
            }
            const WorldPoint ex = (*br)[0];
            const WorldPoint en = (*br)[1];
            hops.push_back({ ex, en, j });
            hops.push_back({ en, ex, i });
            (std::hypot(ex.x - en.x, ex.y - en.y) > 1e-7 ? ++n_bridge : ++n_touch);
        }
    }

    std::vector<WorldPoint> cent(static_cast<size_t>(m));
    for (int64_t i = 0; i < m; ++i) {
        const auto& tri = T[static_cast<size_t>(i)];
        cent[static_cast<size_t>(i)] = { (mesh.V[tri[0]].x + mesh.V[tri[1]].x + mesh.V[tri[2]].x) / 3.0,
                                         (mesh.V[tri[0]].y + mesh.V[tri[1]].y + mesh.V[tri[2]].y) / 3.0 };
    }
    for (size_t r = 0; r < ia.size(); ++r) {
        const int32_t ta = ia[r];
        const int32_t tb = ib[r];
        if (comp[static_cast<size_t>(ta)] != comp[static_cast<size_t>(tb)]) {
            continue;
        }
        bool near = false;
        for (int k1 = 0; k1 < 3 && !near; ++k1) {
            const int32_t n1 = NB[static_cast<size_t>(ta)][k1];
            if (n1 < 0) {
                continue;
            }
            const auto& row = NB[static_cast<size_t>(n1)];
            near = row[0] == tb || row[1] == tb || row[2] == tb;
        }
        if (near) {
            continue;
        }
        const WorldPoint mx { (cent[static_cast<size_t>(ta)].x + cent[static_cast<size_t>(tb)].x) * 0.5,
                              (cent[static_cast<size_t>(ta)].y + cent[static_cast<size_t>(tb)].y) * 0.5 };
        std::unordered_set<int32_t> seen { ta };
        std::deque<int32_t> dq { ta };
        bool hit = false;
        while (!dq.empty() && !hit) {
            const int32_t t2 = dq.front();
            dq.pop_front();
            for (int k = 0; k < 3; ++k) {
                const int32_t nb2 = NB[static_cast<size_t>(t2)][k];
                if (nb2 < 0 || seen.contains(nb2)) {
                    continue;
                }
                if (nb2 == tb) { // 目标判定先于出框判定
                    hit = true;
                    break;
                }
                if (std::fabs(cent[static_cast<size_t>(nb2)].x - mx.x) > kSrcadjLocalR
                    || std::fabs(cent[static_cast<size_t>(nb2)].y - mx.y) > kSrcadjLocalR) {
                    continue;
                }
                seen.insert(nb2);
                dq.push_back(nb2);
            }
        }
        if (hit) {
            continue;
        }
        if (nshared(ta, tb) == 2) {
            pushPortals(ta, tb);
        }
        else {
            const auto br = planner.closestEdgeBridgePoints(static_cast<uint32_t>(lo + ta), static_cast<uint32_t>(lo + tb));
            if (!br) {
                continue;
            }
            const WorldPoint ex = (*br)[0];
            const WorldPoint en = (*br)[1];
            if (std::hypot(ex.x - en.x, ex.y - en.y) > kSrcadjMaxGap) {
                continue;
            }
            hops.push_back({ ex, en, tb });
            hops.push_back({ en, ex, ta });
        }
        ++n_srcadj;
    }

    int64_t ncomps = 0;
    for (int64_t i = 0; i < m; ++i) {
        if (comp[static_cast<size_t>(i)] == i) {
            ++ncomps;
        }
    }
    stats = source_exact ? "source-exact " : "weld " + std::to_string(n_weld) + "v ";
    stats += "mask " + std::to_string(walkable_flags) + " masked " + std::to_string(n_masked) + " cut "
             + std::to_string(n_mask_cut) + ", ";
    stats += "dup-sever " + std::to_string(n_dup) + ", link-mask cut " + std::to_string(n_cut) + ", comps "
             + std::to_string(ncomps) + ", hops edge " + std::to_string(n_edge) + " touch " + std::to_string(n_touch) + " bridge "
             + std::to_string(n_bridge) + " srcadj " + std::to_string(n_srcadj);
}

std::optional<ZoneClean::SnapHit> ZoneClean::snap(const WorldPoint& p, double radius, std::optional<double> floor_y) const
{
    const double r = std::max(0.0, radius);
    const int nr = r >= kSnapFallbackRadius ? 1 : 2;
    for (int ri = 0; ri < nr; ++ri) {
        const double rr = ri == 0 ? r : kSnapFallbackRadius;
        bool have = false;
        std::array<double, 4> bk {};
        SnapHit best;
        for (const int32_t t : mesh.trisNear(p, rr)) {
            if (walkable[static_cast<size_t>(t)] == 0) {
                continue; // 掩码外的面不接受吸附;这一关两轮半径都要过
            }
            const auto& tri = mesh.T[static_cast<size_t>(t)];
            const auto [sp, dist] = closestOnTri(p, { mesh.V[tri[0]], mesh.V[tri[1]], mesh.V[tri[2]] });
            if (dist > rr) {
                continue;
            }
            const double isl = comp_island[comp[static_cast<size_t>(t)]] != 0 ? 1.0 : 0.0;
            // floor 盲键 (isl, dist, -高度, t) 全序;floor 感知键 (带外, isl, dist, delta)
            // dist 打平只发生在同一 (u,v) 上摞着好几层地面(都含点 ⇒ 都是 0)。此时取最高那层:
            // 底图像素是俯视图上的一点,那点看得见的就是最上面那层。拿三角号破平是任意的 ——
            // 重烘一次三角序一换,起点就可能吸到水下/桥下那层,与终点分属两个分量,直接报不连通。
            std::array<double, 4> k;
            if (!floor_y.has_value()) {
                k = { isl, dist, -triHeight(mesh, t), static_cast<double>(t) };
            }
            else {
                const double delta = std::fabs(triHeight(mesh, t) - *floor_y);
                k = { delta <= static_cast<double>(kBaseNavFloorBand) ? 0.0 : 1.0, isl, dist, delta };
            }
            if (!have || k < bk) {
                have = true;
                bk = k;
                best = { t, sp, dist };
            }
        }
        if (have) {
            return best;
        }
    }
    return std::nullopt;
}

// 收齐网格的边界边。这份数据只说哪里能走, 不带墙面/断崖之类的信息,
// 再去分辨某条边是墙还是接缝等于凭空造数据, 所以这里只收边、不分类
WallOracle::WallOracle(const ZoneClean& zc)
{
    const auto& mesh = zc.mesh;
    for (size_t i = 0; i < mesh.T.size(); ++i) {
        if (zc.walkable[i] == 0) {
            // 掩码外的三角不出边。它与可走面之间那条缝已在 ZoneClean 里割断,
            // 所以那条边会由可走面这一侧收成边界边 —— 水岸因此成墙,而不是消失。
            continue;
        }
        for (int k = 0; k < 3; ++k) {
            if (mesh.NB[i][k] >= 0) {
                continue;
            }
            const int32_t a = mesh.T[i][k];
            const int32_t b = mesh.T[i][(k + 1) % 3];
            const WorldPoint p0 = mesh.V[a];
            const WorldPoint p1 = mesh.V[b];
            P0.push_back(p0);
            P1.push_back(p1);
            H0.push_back(mesh.H[a]);
            H1.push_back(mesh.H[b]);
            HH.push_back((mesh.H[a] + mesh.H[b]) / 2.0);
            lo_.push_back({ std::min(p0.x, p1.x), std::min(p0.y, p1.y) });
            hi_.push_back({ std::max(p0.x, p1.x), std::max(p0.y, p1.y) });
        }
    }
}

std::vector<int64_t> WallOracle::wallsInBbox(double x0, double y0, double x1, double y1) const
{
    std::vector<int64_t> idx;
    for (int64_t i = 0; i < static_cast<int64_t>(P0.size()); ++i) {
        if (hi_[static_cast<size_t>(i)].x >= x0 && lo_[static_cast<size_t>(i)].x <= x1 && hi_[static_cast<size_t>(i)].y >= y0
            && lo_[static_cast<size_t>(i)].y <= y1) {
            idx.push_back(i);
        }
    }
    return idx;
}

}
