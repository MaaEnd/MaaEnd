#include "RecastNavZone.h"

#include <algorithm>
#include <cmath>
#include <deque>
#include <numeric>
#include <utility>

#include "BaseNavGeometry.h"

namespace navmesh::recast
{

namespace
{

constexpr double kPortalSamples[5] = { 0.5, 0.25, 0.75, 0.1, 0.9 };  // 共边 hop 的门户采样位

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
    const int64_t n = static_cast<int64_t>(V.size());
    std::vector<std::pair<int64_t, int32_t>> se(static_cast<size_t>(3 * m));
    for (int64_t i = 0; i < m; ++i) {
        for (int64_t k = 0; k < 3; ++k) {
            const int64_t a = T[static_cast<size_t>(i)][k];
            const int64_t b = T[static_cast<size_t>(i)][(k + 1) % 3];
            se[static_cast<size_t>(i * 3 + k)] = { a * n + b, static_cast<int32_t>(i * 3 + k) };
        }
    }
    std::stable_sort(se.begin(), se.end(), [](const auto& l, const auto& r) { return l.first < r.first; });
    std::vector<int64_t> skeys(se.size());
    for (size_t s = 0; s < se.size(); ++s) {
        skeys[s] = se[s].first;
    }
    for (int64_t i = 0; i < m; ++i) {
        for (int64_t k = 0; k < 3; ++k) {
            const int64_t a = T[static_cast<size_t>(i)][k];
            const int64_t b = T[static_cast<size_t>(i)][(k + 1) % 3];
            const int64_t kr = b * n + a;
            const size_t pos = std::min(
                static_cast<size_t>(std::lower_bound(skeys.begin(), skeys.end(), kr) - skeys.begin()),
                skeys.size() - 1);
            if (skeys[pos] == kr) {
                NB[static_cast<size_t>(i)][k] = se[pos].second / 3;
            }
        }
    }
}

void PolyMesh::buildGrid()
{
    std::vector<std::pair<int64_t, int32_t>> kt;
    for (size_t i = 0; i < T.size(); ++i) {
        const WorldPoint& a = V[T[i][0]];
        const WorldPoint& b = V[T[i][1]];
        const WorldPoint& c = V[T[i][2]];
        const int64_t g0x = static_cast<int64_t>(std::floor(std::min({ a.x, b.x, c.x }) / kGridCell));
        const int64_t g0y = static_cast<int64_t>(std::floor(std::min({ a.y, b.y, c.y }) / kGridCell));
        const int64_t g1x = static_cast<int64_t>(std::floor(std::max({ a.x, b.x, c.x }) / kGridCell));
        const int64_t g1y = static_cast<int64_t>(std::floor(std::max({ a.y, b.y, c.y }) / kGridCell));
        for (int64_t gy = g0y; gy <= g1y; ++gy) {
            for (int64_t gx = g0x; gx <= g1x; ++gx) {
                kt.emplace_back(gx * kGridStride + gy, static_cast<int32_t>(i));
            }
        }
    }
    std::stable_sort(kt.begin(), kt.end(), [](const auto& l, const auto& r) { return l.first < r.first; });
    gkeys.resize(kt.size());
    gtris.resize(kt.size());
    for (size_t s = 0; s < kt.size(); ++s) {
        gkeys[s] = kt[s].first;
        gtris[s] = kt[s].second;
    }
}

std::vector<int32_t> PolyMesh::trisNear(const WorldPoint& p, double r) const
{
    std::vector<int32_t> out;
    const int64_t gx0 = static_cast<int64_t>(std::floor((p.x - r) / kGridCell));
    const int64_t gx1 = static_cast<int64_t>(std::floor((p.x + r) / kGridCell));
    const int64_t gy0 = static_cast<int64_t>(std::floor((p.y - r) / kGridCell));
    const int64_t gy1 = static_cast<int64_t>(std::floor((p.y + r) / kGridCell));
    for (int64_t gx = gx0; gx <= gx1; ++gx) {
        for (int64_t gy = gy0; gy <= gy1; ++gy) {
            const auto [lo, hi] = std::equal_range(gkeys.begin(), gkeys.end(), gx * kGridStride + gy);
            for (auto it = lo; it != hi; ++it) {
                out.push_back(gtris[static_cast<size_t>(it - gkeys.begin())]);
            }
        }
    }
    std::sort(out.begin(), out.end());
    out.erase(std::unique(out.begin(), out.end()), out.end());
    return out;
}

ZoneClean::ZoneClean(const BaseNavPack& pack, const BaseNavPlanner& planner, const std::string& zone_name)
{
    name = zone_name;
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

    // 区网格:顶点段连续,f32 坐标回吸 0.05 格点得精确 f64
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
        CV[static_cast<size_t>(i)] = { std::nearbyint(static_cast<double>(vt.u) * 20.0) / 20.0,
                                       std::nearbyint(static_cast<double>(vt.v) * 20.0) / 20.0 };
        CH[static_cast<size_t>(i)] = static_cast<double>(vt.height);
    }

    std::vector<int64_t> kk(static_cast<size_t>(nv));
    for (int64_t i = 0; i < nv; ++i) {
        const int64_t kx = static_cast<int64_t>(std::nearbyint(CV[static_cast<size_t>(i)].x * 1e4));
        const int64_t ky = static_cast<int64_t>(std::nearbyint(CV[static_cast<size_t>(i)].y * 1e4));
        kk[static_cast<size_t>(i)] = kx * (int64_t(1) << 40) + ky;
    }
    std::vector<int32_t> order(static_cast<size_t>(nv));
    std::iota(order.begin(), order.end(), 0);
    std::stable_sort(order.begin(), order.end(), [&](int32_t a, int32_t b) { return kk[a] < kk[b]; });
    std::vector<int32_t> MAP(static_cast<size_t>(nv));
    std::iota(MAP.begin(), MAP.end(), 0);
    int64_t n_weld = 0;
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

    const auto slotKey = [&](int64_t slot) {
        const int64_t i = slot / 3;
        const int64_t k = slot % 3;
        return static_cast<int64_t>(T[static_cast<size_t>(i)][k]) * nvv + T[static_cast<size_t>(i)][(k + 1) % 3];
    };
    std::unordered_map<int64_t, int32_t> kcnt;
    kcnt.reserve(static_cast<size_t>(3 * m));
    for (int64_t slot = 0; slot < 3 * m; ++slot) {
        ++kcnt[slotKey(slot)];
    }
    int64_t n_dup = 0;
    for (int64_t slot = 0; slot < 3 * m; ++slot) {
        if (kcnt.find(slotKey(slot))->second < 2) {
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
    std::unordered_set<int64_t> lkey;
    for (int64_t src = lo; src < hi; ++src) {
        for (uint32_t li = offs[static_cast<size_t>(src)]; li < offs[static_cast<size_t>(src) + 1]; ++li) {
            const int64_t tgt = lnks[li];
            if (tgt >= lo && tgt < hi && src < tgt) {
                lab.emplace_back(static_cast<int32_t>(src - lo), static_cast<int32_t>(tgt - lo));
                lkey.insert((src - lo) * m + (tgt - lo));
            }
        }
    }
    std::vector<int64_t> cand;
    for (int64_t slot = 0; slot < 3 * m; ++slot) {
        const int64_t i = slot / 3;
        const int32_t j = NB[static_cast<size_t>(i)][slot % 3];
        if (j < 0) {
            continue;
        }
        const int64_t pk = std::min<int64_t>(i, j) * m + std::max<int64_t>(i, j);
        if (!lkey.contains(pk)) {
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
        comp_island[static_cast<size_t>(c)] = (n_tot[static_cast<size_t>(c)] == 0
                                               || n_isl[static_cast<size_t>(c)] * 2 > n_tot[static_cast<size_t>(c)])
            ? 1
            : 0;
    }

    // link 层 hop:NB 已邻接跳过;跨分量共焊边 → 门户采样,否则 touch/bridge;
    // 同分量共焊边且非近连通 → srcadj 窄通道
    std::vector<int32_t> ia;
    std::vector<int32_t> ib;
    for (const auto& [la, lb] : lab) {
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
            if (v == T[static_cast<size_t>(tj)][0] || v == T[static_cast<size_t>(tj)][1]
                || v == T[static_cast<size_t>(tj)][2]) {
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
            const auto br =
                planner.closestEdgeBridgePoints(static_cast<uint32_t>(lo + i), static_cast<uint32_t>(lo + j));
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
                if (nb2 == tb) {  // 目标判定先于出框判定
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
            const auto br =
                planner.closestEdgeBridgePoints(static_cast<uint32_t>(lo + ta), static_cast<uint32_t>(lo + tb));
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
    stats = "weld " + std::to_string(n_weld) + "v dup-sever " + std::to_string(n_dup) + ", link-mask cut "
        + std::to_string(n_cut) + ", comps " + std::to_string(ncomps) + ", hops edge " + std::to_string(n_edge)
        + " touch " + std::to_string(n_touch) + " bridge " + std::to_string(n_bridge) + " srcadj "
        + std::to_string(n_srcadj);
}

std::vector<int32_t> ZoneClean::batchLocate(const std::vector<WorldPoint>& pts, const std::vector<double>& hints) const
{
    std::vector<int32_t> out(pts.size(), -1);
    for (size_t p = 0; p < pts.size(); ++p) {
        const int64_t gx = static_cast<int64_t>(std::floor(pts[p].x / PolyMesh::kGridCell));
        const int64_t gy = static_cast<int64_t>(std::floor(pts[p].y / PolyMesh::kGridCell));
        const auto [glo, ghi] =
            std::equal_range(mesh.gkeys.begin(), mesh.gkeys.end(), gx * PolyMesh::kGridStride + gy);
        double best = 0.0;
        int32_t bt = -1;
        for (auto it = glo; it != ghi; ++it) {
            const int32_t t = mesh.gtris[static_cast<size_t>(it - mesh.gkeys.begin())];
            const auto& tri = mesh.T[static_cast<size_t>(t)];
            const WorldPoint& A = mesh.V[tri[0]];
            const WorldPoint& B = mesh.V[tri[1]];
            const WorldPoint& C = mesh.V[tri[2]];
            const double den = (B.y - C.y) * (A.x - C.x) + (C.x - B.x) * (A.y - C.y);
            if (std::fabs(den) < 1e-12) {
                continue;
            }
            const double wa = ((B.y - C.y) * (pts[p].x - C.x) + (C.x - B.x) * (pts[p].y - C.y)) / den;
            const double wb = ((C.y - A.y) * (pts[p].x - C.x) + (A.x - C.x) * (pts[p].y - C.y)) / den;
            const double wc = 1.0 - wa - wb;
            if (!(wa >= -1e-9 && wb >= -1e-9 && wc >= -1e-9)) {
                continue;
            }
            const double h = wa * mesh.H[tri[0]] + wb * mesh.H[tri[1]] + wc * mesh.H[tri[2]];
            const double score = std::fabs(h - hints[p]);
            if (bt < 0 || score < best) {
                best = score;
                bt = t;
            }
        }
        out[p] = bt;
    }
    return out;
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
            const auto& tri = mesh.T[static_cast<size_t>(t)];
            const auto [sp, dist] = closestOnTri(p, { mesh.V[tri[0]], mesh.V[tri[1]], mesh.V[tri[2]] });
            if (dist > rr) {
                continue;
            }
            const double isl = comp_island[comp[static_cast<size_t>(t)]] != 0 ? 1.0 : 0.0;
            // floor 盲键 (isl, dist, t) 全序;floor 感知键 (带外, isl, dist, delta)
            std::array<double, 4> k;
            if (!floor_y.has_value()) {
                k = { isl, dist, static_cast<double>(t), 0.0 };
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

WallOracle::WallOracle(const ZoneClean& zc)
    : zc_(zc)
{
    const auto& mesh = zc.mesh;
    for (size_t i = 0; i < mesh.T.size(); ++i) {
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
            M_.push_back({ (p0.x + p1.x) / 2.0, (p0.y + p1.y) / 2.0 });
            const double dx = p1.x - p0.x;
            const double dy = p1.y - p0.y;
            const double ln = std::max(std::hypot(dx, dy), 1e-12);
            NOBS_.push_back({ dy / ln, -dx / ln });
            HH.push_back((mesh.H[a] + mesh.H[b]) / 2.0);
            lo_.push_back({ std::min(p0.x, p1.x), std::min(p0.y, p1.y) });
            hi_.push_back({ std::max(p0.x, p1.x), std::max(p0.y, p1.y) });
        }
    }
    cls_.assign(P0.size(), -1);
    const auto cellKey = [](int64_t cx, int64_t cy) {
        return (cx + (int64_t(1) << 30)) * (int64_t(1) << 31) + (cy + (int64_t(1) << 30));
    };
    for (const HopPt& hp : zc.hops) {
        const double hz = triHeight(mesh, hp.to_tri);
        for (const WorldPoint* pt : { &hp.exit_pt, &hp.entry_pt }) {
            const int64_t cx = static_cast<int64_t>(std::floor(pt->x / kHopCell));
            const int64_t cy = static_cast<int64_t>(std::floor(pt->y / kHopCell));
            hop_grid_[cellKey(cx, cy)].push_back({ pt->x, pt->y, hz });
        }
    }
}

bool WallOracle::hopNear(int64_t ei, double tol) const
{
    const WorldPoint p0 = P0[static_cast<size_t>(ei)];
    const WorldPoint p1 = P1[static_cast<size_t>(ei)];
    const double hh = HH[static_cast<size_t>(ei)];
    const double dx = p1.x - p0.x;
    const double dy = p1.y - p0.y;
    const double l2 = std::max(dx * dx + dy * dy, 1e-18);
    const int64_t cx0 = static_cast<int64_t>(std::floor((std::min(p0.x, p1.x) - tol) / kHopCell));
    const int64_t cx1 = static_cast<int64_t>(std::floor((std::max(p0.x, p1.x) + tol) / kHopCell));
    const int64_t cy0 = static_cast<int64_t>(std::floor((std::min(p0.y, p1.y) - tol) / kHopCell));
    const int64_t cy1 = static_cast<int64_t>(std::floor((std::max(p0.y, p1.y) + tol) / kHopCell));
    for (int64_t cx = cx0; cx <= cx1; ++cx) {
        for (int64_t cy = cy0; cy <= cy1; ++cy) {
            const auto it =
                hop_grid_.find((cx + (int64_t(1) << 30)) * (int64_t(1) << 31) + (cy + (int64_t(1) << 30)));
            if (it == hop_grid_.end()) {
                continue;
            }
            for (const auto& h : it->second) {
                if (std::fabs(h[2] - hh) > kMcHBand) {
                    continue;
                }
                const double t = std::min(1.0, std::max(0.0, ((h[0] - p0.x) * dx + (h[1] - p0.y) * dy) / l2));
                if (std::hypot(p0.x + dx * t - h[0], p0.y + dy * t - h[1]) <= tol) {
                    return true;
                }
            }
        }
    }
    return false;
}

void WallOracle::classify(const std::vector<int64_t>& idx)
{
    std::vector<int64_t> todo;
    for (const int64_t i : idx) {
        if (cls_[static_cast<size_t>(i)] < 0) {
            todo.push_back(i);
        }
    }
    if (todo.empty()) {
        return;
    }
    std::vector<WorldPoint> P;
    std::vector<double> hh;
    for (const int64_t i : todo) {
        P.push_back({ M_[static_cast<size_t>(i)].x + NOBS_[static_cast<size_t>(i)].x * kEpsProbe,
                      M_[static_cast<size_t>(i)].y + NOBS_[static_cast<size_t>(i)].y * kEpsProbe });
        hh.push_back(HH[static_cast<size_t>(i)]);
    }
    const auto tri = zc_.batchLocate(P, hh);
    for (size_t j = 0; j < todo.size(); ++j) {
        const bool free =
            tri[j] >= 0 && std::fabs(triHeight(zc_.mesh, tri[j]) - hh[j]) <= kHBand;
        bool wall = !free;
        if (wall && hopNear(todo[j])) {
            wall = false;
        }
        cls_[static_cast<size_t>(todo[j])] = wall ? 1 : 0;
    }
}

std::vector<int64_t> WallOracle::wallsInBbox(double x0, double y0, double x1, double y1)
{
    std::vector<int64_t> idx;
    for (int64_t i = 0; i < static_cast<int64_t>(P0.size()); ++i) {
        if (hi_[static_cast<size_t>(i)].x >= x0 && lo_[static_cast<size_t>(i)].x <= x1
            && hi_[static_cast<size_t>(i)].y >= y0 && lo_[static_cast<size_t>(i)].y <= y1) {
            idx.push_back(i);
        }
    }
    classify(idx);
    std::vector<int64_t> out;
    for (const int64_t i : idx) {
        if (cls_[static_cast<size_t>(i)] == 1) {
            out.push_back(i);
        }
    }
    return out;
}

}
