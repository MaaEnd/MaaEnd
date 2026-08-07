#include "RecastNavRoute.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdio>
#include <limits>
#include <optional>

namespace navmesh::recast
{

namespace
{

double triHeightOf(const PolyMesh& mesh, int32_t t)
{
    const auto& tri = mesh.T[static_cast<size_t>(t)];
    return (mesh.H[tri[0]] + mesh.H[tri[1]] + mesh.H[tri[2]]) / 3.0;
}

struct WindowInfo
{
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
    Mask lay;
    Grid<float> lh;
    Mask core;
    Grid<float> dist;
    std::vector<WorldPoint> wP0;
    std::vector<WorldPoint> wP1;
    WallCsr wcsr;
    StepBarrier sev;
    std::vector<WorldPoint> segA;
    std::vector<WorldPoint> segB;
    double h0 = 0.0;
    SpanTable st3;
    std::vector<uint8_t> vis3;
    std::vector<int64_t> cidx;
    std::vector<uint8_t> reach3;
};

struct RouteDiag
{
    std::string err;
    std::vector<std::string> warn;
    std::vector<WorldPoint> xwall;
    std::vector<double> clearance;
    double snap_start = 0.0;
    double snap_goal = 0.0;
};

std::optional<WindowInfo> buildWindow(
    const ZoneClean& zc,
    WallOracle& wo,
    const WorldPoint& s,
    const WorldPoint& s_snap,
    double h0,
    double x0,
    double y0,
    double x1,
    double y1,
    const std::vector<int32_t>& blocked_local,
    const std::vector<WorldPoint>& blocked_points,
    std::string& err)
{
    const int64_t nx = static_cast<int64_t>(std::ceil((x1 - x0) / kCS));
    const int64_t ny = static_cast<int64_t>(std::ceil((y1 - y0) / kCS));
    RasterCells rcs = Rasterize(zc.mesh.V, zc.mesh.H, zc.mesh.T, x0, y0, nx, ny);
    AppendSeamBridge(rcs, nx, ny);
    const SpanTable st = BuildSpans(rcs.cell, rcs.h);

    const auto widx = wo.wallsInBbox(x0 - 4, y0 - 4, x0 + static_cast<double>(nx) * kCS + 4, y0 + static_cast<double>(ny) * kCS + 4);
    std::vector<WorldPoint> p0;
    std::vector<WorldPoint> p1;
    std::vector<double> hh;
    for (const int64_t i : widx) {
        p0.push_back(wo.P0[static_cast<size_t>(i)]);
        p1.push_back(wo.P1[static_cast<size_t>(i)]);
        hh.push_back(wo.HH[static_cast<size_t>(i)]);
    }
    const std::vector<uint8_t> dead = StampWalls(p0, p1, hh, x0, y0, nx, ny, st);

    int64_t gx = static_cast<int64_t>((s.x - x0) / kCS);
    int64_t gy = static_cast<int64_t>((s.y - y0) / kCS);
    int64_t cell0 = gy * nx + gx;
    auto occ_it = std::lower_bound(st.occ.begin(), st.occ.end(), cell0);
    if (occ_it == st.occ.end() || *occ_it != cell0) {
        // 起点离网时其所在格无体素,退用按楼层吸附过的起点定种子
        gx = static_cast<int64_t>((s_snap.x - x0) / kCS);
        gy = static_cast<int64_t>((s_snap.y - y0) / kCS);
        cell0 = gy * nx + gx;
        occ_it = std::lower_bound(st.occ.begin(), st.occ.end(), cell0);
    }
    if (occ_it == st.occ.end() || *occ_it != cell0) {
        err = "起点格无体素 (gx=" + std::to_string(gx) + ",gy=" + std::to_string(gy) + ")";
        return std::nullopt;
    }
    const int64_t j = occ_it - st.occ.begin();
    int64_t seed = -1;
    float best = 0.0F;
    for (int64_t k = 0; k < st.K; ++k) {
        const int64_t sid = st.IK[static_cast<size_t>(j * st.K + k)];
        if (sid < 0) {
            continue;
        }
        const float d = std::fabs(st.sp_h[static_cast<size_t>(sid)] - static_cast<float>(h0));
        if (seed < 0 || d < best) {
            seed = sid;
            best = d;
        }
    }
    const std::vector<uint8_t> vis = Flood(seed, st, nx);

    WindowInfo info;
    info.x0 = x0;
    info.y0 = y0;
    info.nx = nx;
    info.ny = ny;
    info.lay = Mask(nx, ny, 0);
    info.lh = Grid<float>(nx, ny, std::numeric_limits<float>::quiet_NaN());
    for (size_t si = 0; si < vis.size(); ++si) {
        if (vis[si] != 0) {
            info.lay.v[static_cast<size_t>(st.sp_cell[si])] = 1;
            info.lh.v[static_cast<size_t>(st.sp_cell[si])] = st.sp_h[si];
        }
    }
    Mask wallcell(nx, ny, 0);
    for (size_t si = 0; si < dead.size(); ++si) {
        if (dead[si] != 0) {
            wallcell.v[static_cast<size_t>(st.sp_cell[si])] = 1;
        }
    }
    info.lay = FillHoles(info.lay, kHoleMaxCells, &wallcell);
    Mask corein(nx, ny, 0);
    for (size_t ci = 0; ci < rcs.cell.size(); ++ci) {
        if (rcs.ins[ci] == 0) {
            continue;
        }
        const float lf = info.lh.v[static_cast<size_t>(rcs.cell[ci])];
        if (!std::isnan(lf) && std::fabs(rcs.h[ci] - lf) <= static_cast<float>(kQH)) {
            corein.v[static_cast<size_t>(rcs.cell[ci])] = 1;
        }
    }
    for (size_t i = 0; i < corein.v.size(); ++i) {
        corein.v[i] = static_cast<uint8_t>(corein.v[i] != 0 && info.lay.v[i] != 0);
    }
    info.core = FillHoles(corein, kHoleMaxCells, &wallcell);
    info.core = CloseCracks(info.core, info.lay, &wallcell);

    if (!blocked_local.empty()) {
        std::vector<std::array<int32_t, 3>> bt;
        bt.reserve(blocked_local.size());
        for (const int32_t t : blocked_local) {
            bt.push_back(zc.mesh.T[static_cast<size_t>(t)]);
        }
        const RasterCells brc = Rasterize(zc.mesh.V, zc.mesh.H, bt, x0, y0, nx, ny);
        for (size_t ci = 0; ci < brc.cell.size(); ++ci) {
            const auto cell = static_cast<size_t>(brc.cell[ci]);
            const float lf = info.lh.v[cell];
            // 层高带内才盖掉,免得误伤其他楼层的格
            if (!std::isnan(lf) && std::fabs(brc.h[ci] - lf) <= static_cast<float>(kClimb)) {
                info.core.v[cell] = 0;
                info.lay.v[cell] = 0;
            }
        }
    }

    // 封堵点无自带高度;窗口层已按起点层高筛过,直接按平面距离盖格即可
    if (!blocked_points.empty()) {
        const int64_t pr = static_cast<int64_t>(std::ceil(kBlockedPointRadius / kCS));
        for (const WorldPoint& bp : blocked_points) {
            const int64_t cgx = static_cast<int64_t>(std::floor((bp.x - x0) / kCS));
            const int64_t cgy = static_cast<int64_t>(std::floor((bp.y - y0) / kCS));
            for (int64_t by = std::max<int64_t>(cgy - pr, 0); by <= std::min<int64_t>(cgy + pr, ny - 1); ++by) {
                for (int64_t bx = std::max<int64_t>(cgx - pr, 0); bx <= std::min<int64_t>(cgx + pr, nx - 1); ++bx) {
                    const double px = x0 + (static_cast<double>(bx) + 0.5) * kCS;
                    const double py = y0 + (static_cast<double>(by) + 0.5) * kCS;
                    if (std::hypot(px - bp.x, py - bp.y) > kBlockedPointRadius) {
                        continue;
                    }
                    const size_t cell = static_cast<size_t>(by * nx + bx);
                    info.core.v[cell] = 0;
                    info.lay.v[cell] = 0;
                }
            }
        }
    }

    info.sev = StepBreaks(st, vis, info.lay, x0, y0);

    info.h0 = h0;
    std::vector<uint8_t> have(static_cast<size_t>(nx * ny), 0);
    for (size_t si = 0; si < vis.size(); ++si) {
        if (vis[si] != 0) {
            have[static_cast<size_t>(st.sp_cell[si])] = 1;
        }
    }
    std::vector<int64_t> ghost;
    for (int64_t c = 0; c < nx * ny && !info.sev.t0.empty(); ++c) {
        if (info.lay.v[static_cast<size_t>(c)] != 0 && have[static_cast<size_t>(c)] == 0
            && std::isfinite(info.sev.t0[static_cast<size_t>(c)])) {
            ghost.push_back(c);
        }
    }
    info.vis3 = vis;
    if (ghost.empty()) {
        info.st3 = st;
    }
    else {
        std::vector<int64_t> gc3 = st.sp_cell;
        std::vector<float> gh3 = st.sp_h;
        for (const int64_t c : ghost) {
            gc3.push_back(c);
            gh3.push_back(info.sev.t0[static_cast<size_t>(c)]);
            info.vis3.push_back(1);
        }
        info.st3 = PackSpans(std::move(gc3), std::move(gh3), &info.vis3);
    }
    info.cidx.assign(static_cast<size_t>(nx * ny), -1);
    for (size_t ci = 0; ci < info.st3.occ.size(); ++ci) {
        info.cidx[static_cast<size_t>(info.st3.occ[ci])] = static_cast<int64_t>(ci);
    }
    const int64_t sj = info.cidx[static_cast<size_t>(cell0)];
    int64_t seed3 = -1;
    float best3 = 0.0F;
    for (int64_t k = 0; k < info.st3.K; ++k) {
        const int64_t sid = info.st3.IK[static_cast<size_t>(sj * info.st3.K + k)];
        if (sid < 0) {
            continue;
        }
        const float d = std::fabs(info.st3.sp_h[static_cast<size_t>(sid)] - static_cast<float>(h0));
        if (seed3 < 0 || d < best3) {
            seed3 = sid;
            best3 = d;
        }
    }
    info.reach3 = SpanReach(seed3, info.st3, info.vis3, nx, ny);

    const std::vector<uint8_t> keep = WallsAtLayer(p0, p1, hh, info.lh, x0, y0);
    for (size_t i = 0; i < keep.size(); ++i) {
        if (keep[i] != 0) {
            info.wP0.push_back(p0[i]);
            info.wP1.push_back(p1[i]);
        }
    }
    info.wcsr = BuildWallIndex(info.wP0, info.wP1, x0, y0, nx, ny);
    info.segA = info.wP0;
    info.segA.insert(info.segA.end(), info.sev.p0.begin(), info.sev.p0.end());
    info.segB = info.wP1;
    info.segB.insert(info.segB.end(), info.sev.p1.begin(), info.sev.p1.end());
    info.dist = Clearance(info.core);
    return info;
}

std::optional<std::vector<WorldPoint>>
    routeWindow(const WindowInfo& info, const WorldPoint& s, const WorldPoint& g, bool climb_faces, RouteDiag& dg)
{
    const int64_t nx = info.nx;
    const int64_t ny = info.ny;
    const double x0 = info.x0;
    const double y0 = info.y0;
    Mask walk(nx, ny, 0);
    for (size_t i = 0; i < walk.v.size(); ++i) {
        walk.v[i] = static_cast<uint8_t>(info.core.v[i] != 0 && info.lay.v[i] != 0);
    }
    const auto bn = BannedSteps(info.lay, info.wcsr, info.wP0, info.wP1, x0, y0);
    std::unordered_set<int64_t> blocked_steps = bn;
    blocked_steps.insert(info.sev.steps.begin(), info.sev.steps.end());
    // 掩膜距离场对跨越约束的墙无感, 取真墙距离的下确界补上
    Mask wfree(nx, ny, 0);
    for (size_t i = 0; i < wfree.v.size(); ++i) {
        wfree.v[i] = info.wcsr.start[i + 1] > info.wcsr.start[i] ? 0 : 1;
    }
    const Grid<float> wdist = Clearance(wfree);
    Grid<float> dist(nx, ny, 0.0F);
    for (size_t i = 0; i < dist.v.size(); ++i) {
        dist.v[i] = std::min(info.dist.v[i], wdist.v[i]);
    }
    // 亏欠越多单价越高;脊线保底只进几何口径 prefg,禁入 mult
    // 拓扑口径的余量目标随局部通道半宽下降, 窄通道的余量单价才不会把能走的路挤出选路
    const Grid<float> pref = PrefField(dist, false);
    const Grid<float> prefg = PrefField(dist, true);
    Grid<float> mult(nx, ny, 0.0F);
    for (size_t i = 0; i < mult.v.size(); ++i) {
        const float c = std::min(std::max((pref.v[i] - dist.v[i]) / pref.v[i], 0.0F), 1.0F);
        mult.v[i] = 1.0F + static_cast<float>(kLam) * c;
    }
    // 几何口径的余量目标: 通道半宽封顶 kGeoR, 供绿段重寻与拉直判定
    // 通道目标之外再按固定余量目标 kR 追加平方亏欠, 使窄通道内部仍向中间收敛
    const Grid<float> tgt = TargetField(dist);
    Grid<float> multg(nx, ny, 0.0F);
    Grid<float> cf(nx, ny, 0.0F);
    for (size_t i = 0; i < multg.v.size(); ++i) {
        const float c = std::min(std::max((tgt.v[i] - dist.v[i]) / tgt.v[i], 0.0F), 1.0F);
        const float cdef =
            std::min(std::max((static_cast<float>(kR) - dist.v[i]) / static_cast<float>(kR), 0.0F), 1.0F);
        multg.v[i] = 1.0F + static_cast<float>(kLam) * c + static_cast<float>(kLamR) * cdef * cdef;
        cf.v[i] = std::min(dist.v[i], tgt.v[i]);
    }
    const ClearanceFloor cfl(&cf, &multg, x0, y0, kCS);

    const CellPt sc { static_cast<int64_t>((s.x - x0) / kCS), static_cast<int64_t>((s.y - y0) / kCS) };
    const CellPt gc { static_cast<int64_t>((g.x - x0) / kCS), static_cast<int64_t>((g.y - y0) / kCS) };

    const auto near = [&](const Mask& mask, const CellPt& p) -> std::pair<std::optional<CellPt>, double> {
        bool have = false;
        int64_t bd = 0;
        CellPt bc;
        for (int64_t y = 0; y < ny; ++y) {
            for (int64_t x = 0; x < nx; ++x) {
                if (mask.at(y, x) == 0) {
                    continue;
                }
                const int64_t d = (x - p.x) * (x - p.x) + (y - p.y) * (y - p.y);
                if (!have || d < bd) {
                    have = true;
                    bd = d;
                    bc = { x, y };
                }
            }
        }
        if (!have) {
            return { std::nullopt, 0.0 };
        }
        return { bc, std::sqrt(static_cast<double>(bd)) * kCS };
    };
    const SpanTable& st3 = info.st3;
    const LayerOracle lyo(&st3, &info.cidx, nx, ny, x0, y0);
    const auto mk = [&](const Mask& m2, std::vector<uint8_t>& use, Mask& c3) {
        use.assign(st3.sp_h.size(), 0);
        c3 = Mask(nx, ny, 0);
        for (size_t i = 0; i < use.size(); ++i) {
            const auto cell = static_cast<size_t>(st3.sp_cell[i]);
            if (info.reach3[i] != 0 && m2.v[cell] != 0) {
                use[i] = 1;
                c3.v[cell] = 1;
            }
        }
    };
    std::vector<uint8_t> useW;
    std::vector<uint8_t> useC;
    Mask cw3;
    Mask cc3;
    mk(walk, useW, cw3);
    mk(info.core, useC, cc3);
    const auto pick = [&](const CellPt& c, const std::vector<uint8_t>& use) {
        std::vector<int64_t> out;
        const int64_t j = info.cidx[static_cast<size_t>(c.y * nx + c.x)];
        if (j < 0) {
            return out;
        }
        for (int64_t k = 0; k < st3.K; ++k) {
            const int64_t v = st3.IK[static_cast<size_t>(j * st3.K + k)];
            if (v >= 0 && use[static_cast<size_t>(v)] != 0) {
                out.push_back(v);
            }
        }
        return out;
    };
    const auto atSeedLayer = [&](const std::vector<int64_t>& vs) {
        int64_t best = -1;
        float bd = 0.0F;
        for (const int64_t v : vs) {
            const float d = std::fabs(st3.sp_h[static_cast<size_t>(v)] - static_cast<float>(info.h0));
            if (best < 0 || d < bd) {
                best = v;
                bd = d;
            }
        }
        return best;
    };

    auto [as_, dsa] = near(cw3, sc);
    auto [ag_, dga] = near(cw3, gc);
    if (!as_.has_value()) {
        dg.err = "walk 掩膜为空";
        return std::nullopt;
    }

    const double BIGP = static_cast<double>(nx * ny) * kCS * (1.0 + kLam);
    const std::unordered_set<int64_t>* faces = climb_faces ? nullptr : &info.sev.steps;
    const std::unordered_set<int64_t>& soft = climb_faces ? blocked_steps : bn;
    Mask on3 = cw3;
    std::optional<std::vector<int64_t>> qs;
    if (as_->x == ag_->x && as_->y == ag_->y) {
        qs = std::vector<int64_t> { atSeedLayer(pick(*as_, useW)) };
    }
    else {
        qs = SpanAstar(st3, useW, info.cidx, cw3, atSeedLayer(pick(*as_, useW)), pick(*ag_, useW), mult, &soft, &BIGP, faces);
    }
    if (!qs.has_value()) {
        const auto [ac_, dc_] = near(cc3, sc);
        const auto [ag2, dg2] = near(cc3, gc);
        if (ac_.has_value() && ag2.has_value()) {
            if (ac_->x == ag2->x && ac_->y == ag2->y) {
                qs = std::vector<int64_t> { atSeedLayer(pick(*ac_, useC)) };
            }
            else {
                qs = SpanAstar(st3, useC, info.cidx, cc3, atSeedLayer(pick(*ac_, useC)), pick(*ag2, useC), mult, &soft, &BIGP, faces);
            }
            if (qs.has_value()) {
                on3 = cc3;
                as_ = ac_;
                dsa = dc_;
                ag_ = ag2;
                dga = dg2;
                dg.warn.push_back("walk 断开→退回 core");
            }
        }
    }
    std::optional<std::vector<CellPt>> q;
    if (qs.has_value()) {
        std::vector<CellPt> cellq;
        cellq.reserve(qs->size());
        for (const int64_t v : *qs) {
            const int64_t c = st3.sp_cell[static_cast<size_t>(v)];
            cellq.push_back({ c % nx, c / nx });
        }
        q = std::move(cellq);
    }
    else {
        std::tie(as_, dsa) = near(walk, sc);
        std::tie(ag_, dga) = near(walk, gc);
        on3 = walk;
        if (as_->x == ag_->x && as_->y == ag_->y) {
            q = std::vector<CellPt> { *as_ };
        }
        else {
            q = CostAstar(walk, *as_, *ag_, mult, &soft, &BIGP, faces);
        }
        if (!q.has_value()) {
            on3 = info.core;
            q = CostAstar(info.core, *as_, *ag_, mult, &soft, &BIGP, faces);
            if (q.has_value()) {
                dg.warn.push_back("walk 断开→退回 core");
            }
        }
        if (!q.has_value()) {
            dg.err = "不连通";
            return std::nullopt;
        }
        dg.warn.push_back("层不连通→退回格级");
    }
    dg.snap_start = dsa;
    dg.snap_goal = dga;
    const int64_t NC = nx * ny;
    std::vector<size_t> bad;
    for (size_t k = 1; k < q->size(); ++k) {
        const int64_t ca = (*q)[k - 1].y * nx + (*q)[k - 1].x;
        const int64_t cb = (*q)[k].y * nx + (*q)[k].x;
        if (!blocked_steps.contains(ca * NC + cb)) {
            continue;
        }
        bad.push_back(k);
        if (bn.contains(ca * NC + cb)) {
            dg.xwall.push_back({ x0 + (static_cast<double>((*q)[k].x) + 0.5) * kCS, y0 + (static_cast<double>((*q)[k].y) + 0.5) * kCS });
        }
    }
    if (!dg.xwall.empty()) {
        dg.warn.push_back("不可避穿墙 " + std::to_string(dg.xwall.size()) + " 步");
    }
    if (bad.size() > dg.xwall.size()) {
        dg.warn.push_back("不可避立面 " + std::to_string(bad.size() - dg.xwall.size()) + " 步");
    }

    const auto cen = [&](const std::vector<CellPt>& P) {
        std::vector<WorldPoint> out;
        out.reserve(P.size());
        for (const auto& c : P) {
            out.push_back({ x0 + (static_cast<double>(c.x) + 0.5) * kCS, y0 + (static_cast<double>(c.y) + 0.5) * kCS });
        }
        return out;
    };
    const auto toWorld = [&](const std::vector<std::vector<WorldPoint>>& loops) {
        std::vector<std::vector<WorldPoint>> out;
        out.reserve(loops.size());
        for (const auto& L : loops) {
            std::vector<WorldPoint> w;
            w.reserve(L.size());
            for (const auto& p : L) {
                w.push_back({ x0 + p.x * kCS, y0 + p.y * kCS });
            }
            out.push_back(std::move(w));
        }
        return out;
    };

    const auto loops_core = toWorld(TraceContours(info.core));
    const Blockers::OnMask onm { &on3, x0, y0, kCS };
    const Blockers blk_gray(loops_core, &info.segA, &info.segB, onm);

    std::vector<uint8_t> grn(q->size());
    for (size_t i = 0; i < q->size(); ++i) {
        grn[i] = static_cast<uint8_t>(dist.at((*q)[i].y, (*q)[i].x) >= prefg.at((*q)[i].y, (*q)[i].x) - 1e-9F);
    }

    struct Run
    {
        bool green;
        int64_t i0;
        int64_t i1;
    };

    std::vector<Run> runs;
    for (size_t i = 0; i < q->size();) {
        size_t j2 = i;
        while (j2 + 1 < q->size() && grn[j2 + 1] == grn[i]) {
            ++j2;
        }
        runs.push_back({ grn[i] != 0, static_cast<int64_t>(i), static_cast<int64_t>(j2) });
        i = j2 + 1;
    }
    const auto merge = [](const std::vector<Run>& rs) {
        std::vector<Run> out;
        for (const auto& r : rs) {
            if (!out.empty() && out.back().green == r.green) {
                out.back().i1 = r.i1;
            }
            else {
                out.push_back(r);
            }
        }
        return out;
    };
    for (size_t k = 0; k < runs.size(); ++k) {
        if (!runs[k].green && static_cast<double>(runs[k].i1 - runs[k].i0) * kCS < 2.0 && k > 0 && k < runs.size() - 1) {
            runs[k].green = true;
        }
    }
    runs = merge(runs);
    for (auto& r : runs) {
        if (r.green && static_cast<double>(r.i1 - r.i0) * kCS < 1.5) {
            r.green = false;
        }
    }
    const std::vector<Run> mg = merge(runs);

    std::vector<WorldPoint> taut;
    for (const auto& run : mg) {
        const int64_t iend = std::min(run.i1 + 1, static_cast<int64_t>(q->size()) - 1);
        const std::vector<CellPt> cells(q->begin() + run.i0, q->begin() + iend + 1);
        std::vector<int64_t> sub;
        std::vector<float> hs;
        if (qs.has_value()) {
            sub.assign(qs->begin() + run.i0, qs->begin() + iend + 1);
            hs.reserve(sub.size());
            for (const int64_t v : sub) {
                hs.push_back(st3.sp_h[static_cast<size_t>(v)]);
            }
        }
        std::vector<WorldPoint> pp = cen(cells);
        if (cells.size() >= 2) {
            std::optional<Blockers> blk_green;
            if (run.green) {
                // 绿段:er = 腐蚀掩膜(脊线保底限路径走廊±kR),重寻守卫 l2≤l1×1.2+2px
                Mask pm(nx, ny, 0);
                for (const auto& c : cells) {
                    pm.at(c.y, c.x) = 1;
                }
                Mask pmd = pm;
                const int64_t kd = static_cast<int64_t>(std::ceil(kR / kCS));
                const std::pair<int64_t, int64_t> axes[2] = { { 0, 1 }, { 1, 0 } };
                for (const auto& [ddy, ddx] : axes) {
                    Mask acc = pmd;
                    for (int64_t sh = 1; sh <= kd; ++sh) {
                        for (const int64_t sgn : { int64_t(1), int64_t(-1) }) {
                            const int64_t dy = sgn * sh * ddy;
                            const int64_t dx = sgn * sh * ddx;
                            for (int64_t y = std::max<int64_t>(0, dy); y < ny + std::min<int64_t>(0, dy); ++y) {
                                for (int64_t x = std::max<int64_t>(0, dx); x < nx + std::min<int64_t>(0, dx); ++x) {
                                    if (pmd.at(y - dy, x - dx) != 0) {
                                        acc.at(y, x) = 1;
                                    }
                                }
                            }
                        }
                    }
                    pmd = acc;
                }
                Mask er(nx, ny, 0);
                for (int64_t y = 0; y < ny; ++y) {
                    for (int64_t x = 0; x < nx; ++x) {
                        er.at(y, x) = static_cast<uint8_t>(
                            dist.at(y, x) >= pref.at(y, x) || (dist.at(y, x) >= prefg.at(y, x) && pmd.at(y, x) != 0) || pm.at(y, x) != 0);
                    }
                }
                // 切角规则要求对角步两个正交伴格都在掩膜里, 原掩膜搜不出时才放行伴格;
                // 挡线集恒用 er, 伴格进挡线集会把角内侧开口
                Mask ers = er;
                for (size_t k2 = 1; k2 < cells.size(); ++k2) {
                    const CellPt& a3 = cells[k2 - 1];
                    const CellPt& b3 = cells[k2];
                    if (a3.x != b3.x && a3.y != b3.y) {
                        ers.at(a3.y, b3.x) = 1;
                        ers.at(b3.y, a3.x) = 1;
                    }
                }
                // 重寻硬禁穿墙步,不可避穿墙处切开逐子段重寻,原步原样保留
                std::vector<size_t> cuts;
                for (const size_t k : bad) {
                    if (k > static_cast<size_t>(run.i0) && k <= static_cast<size_t>(run.i0) + cells.size() - 1) {
                        cuts.push_back(k - static_cast<size_t>(run.i0));
                    }
                }
                cuts.push_back(cells.size());
                const auto research = [&](const Mask& m3, std::vector<float>& oh) -> std::optional<std::vector<CellPt>> {
                    std::vector<uint8_t> ue;
                    Mask ce;
                    if (qs.has_value()) {
                        mk(m3, ue, ce);
                    }
                    std::vector<CellPt> o2;
                    oh.clear();
                    size_t a2 = 0;
                    for (const size_t c2 : cuts) {
                        const size_t b2 = c2 - 1;
                        if (a2 == b2) {
                            o2.push_back(cells[a2]);
                            if (qs.has_value()) {
                                oh.push_back(hs[a2]);
                            }
                        }
                        else if (!qs.has_value()) {
                            const auto r2 = CostAstar(m3, cells[a2], cells[b2], multg, &blocked_steps, nullptr);
                            if (!r2.has_value()) {
                                return std::nullopt;
                            }
                            o2.insert(o2.end(), r2->begin(), r2->end());
                        }
                        else {
                            const auto r2 = SpanAstar(st3, ue, info.cidx, ce, sub[a2], { sub[b2] }, multg, &blocked_steps, nullptr);
                            if (!r2.has_value()) {
                                return std::nullopt;
                            }
                            for (const int64_t v : *r2) {
                                const int64_t c = st3.sp_cell[static_cast<size_t>(v)];
                                o2.push_back({ c % nx, c / nx });
                                oh.push_back(st3.sp_h[static_cast<size_t>(v)]);
                            }
                        }
                        a2 = c2;
                    }
                    return o2;
                };
                std::vector<float> h2;
                auto q2 = research(er, h2);
                if (!q2.has_value()) {
                    q2 = research(ers, h2);
                }
                if (q2.has_value()) {
                    double l1 = 0.0;
                    double l2 = 0.0;
                    for (size_t k = 1; k < cells.size(); ++k) {
                        l1 +=
                            std::hypot(static_cast<double>(cells[k].x - cells[k - 1].x), static_cast<double>(cells[k].y - cells[k - 1].y));
                    }
                    for (size_t k = 1; k < q2->size(); ++k) {
                        l2 +=
                            std::hypot(static_cast<double>((*q2)[k].x - (*q2)[k - 1].x), static_cast<double>((*q2)[k].y - (*q2)[k - 1].y));
                    }
                    if (l2 <= l1 * 1.2 + 2.0 / kCS) {
                        pp = cen(*q2);
                        if (qs.has_value()) {
                            hs = h2;
                        }
                    }
                }
                auto loops_er = TraceContours(er);
                std::vector<std::vector<WorldPoint>> lp;
                lp.reserve(loops_er.size() + loops_core.size());
                for (const auto& L : loops_er) {
                    lp.push_back(SimplifyLoop(L, kMaxErr / kCS));
                }
                auto lw = toWorld(lp);
                lw.insert(lw.end(), loops_core.begin(), loops_core.end());
                blk_green.emplace(lw, &info.segA, &info.segB, onm);
            }
            pp = StringPull(
                pp,
                blk_green.has_value() ? *blk_green : blk_gray,
                &cfl,
                hs.empty() ? nullptr : &lyo,
                hs.empty() ? nullptr : &hs);
        }
        if (!taut.empty() && !pp.empty() && std::hypot(pp.front().x - taut.back().x, pp.front().y - taut.back().y) < 1e-9) {
            pp.erase(pp.begin());
        }
        taut.insert(taut.end(), pp.begin(), pp.end());
    }

    std::vector<WorldPoint> line;
    line.push_back(s);
    line.insert(line.end(), taut.begin(), taut.end());
    line.push_back(g);
    std::vector<WorldPoint> stripped;
    for (size_t i = 0; i < line.size(); ++i) {
        if (i == 0 || i == line.size() - 1
            || (std::hypot(line[i].x - s.x, line[i].y - s.y) > 0.4 && std::hypot(line[i].x - g.x, line[i].y - g.y) > 0.4)) {
            stripped.push_back(line[i]);
        }
    }
    std::vector<WorldPoint> ded { stripped.front() };
    for (size_t i = 1; i < stripped.size(); ++i) {
        if (std::hypot(stripped[i].x - ded.back().x, stripped[i].y - ded.back().y) > 1e-9) {
            ded.push_back(stripped[i]);
        }
    }
    std::vector<WorldPoint> out = DropLoops(ded);
    const LayerOracle* lyo_p = qs.has_value() ? &lyo : nullptr;
    const float lyo_h = qs.has_value() ? st3.sp_h[static_cast<size_t>(qs->front())] : 0.0F;
    if (kSlimEps > 0 && out.size() > 2) {
        out = Slim(out, blk_gray, kSlimEps, &cfl, lyo_p, lyo_h);
    }
    if (out.size() > 2) {
        out = WidenCorners(out, blk_gray, dist, info.x0, info.y0, kCS, &cfl, lyo_p, lyo_h);
    }
    dg.clearance.reserve(out.size());
    for (const auto& p : out) {
        const int64_t cx = std::min(std::max(static_cast<int64_t>(std::floor((p.x - info.x0) / kCS)), int64_t { 0 }), nx - 1);
        const int64_t cy = std::min(std::max(static_cast<int64_t>(std::floor((p.y - info.y0) / kCS)), int64_t { 0 }), ny - 1);
        dg.clearance.push_back(static_cast<double>(dist.at(cy, cx)));
    }
    return out;
}

}

RecastNavEngine::RecastNavEngine(const BaseNavPack& pack, const BaseNavPlanner& planner)
    : pack_(pack)
    , planner_(planner)
{
}

RecastNavEngine::ZoneEntry& RecastNavEngine::zoneEntry(const std::string& name)
{
    auto it = zones_.find(name);
    if (it == zones_.end()) {
        ZoneEntry e;
        e.zc = std::make_unique<ZoneClean>(pack_, planner_, name);
        if (e.zc->valid()) {
            e.wo = std::make_unique<WallOracle>(*e.zc);
        }
        it = zones_.emplace(name, std::move(e)).first;
    }
    return it->second;
}

RecastPlanResult RecastNavEngine::plan(
    const std::string& zone_name,
    const WorldPoint& start,
    const WorldPoint& goal,
    float start_floor_y,
    float goal_floor_y,
    const std::vector<uint32_t>& blocked,
    const std::vector<WorldPoint>& blocked_points,
    const RecastPlanBudget& budget)
{
    const std::lock_guard<std::mutex> lock(mutex_);
    return planLocked(zone_name, start, goal, start_floor_y, goal_floor_y, blocked, blocked_points, budget);
}

RecastPlanResult RecastNavEngine::planLocked(
    const std::string& zone_name,
    const WorldPoint& start,
    const WorldPoint& goal,
    float start_floor_y,
    float goal_floor_y,
    const std::vector<uint32_t>& blocked,
    const std::vector<WorldPoint>& blocked_points,
    const RecastPlanBudget& budget)
{
    RecastPlanResult res;
    ZoneEntry& ze = zoneEntry(zone_name);
    if (!ze.zc->valid()) {
        res.error = ze.zc->error();
        return res;
    }
    const ZoneClean& zc = *ze.zc;
    WallOracle& wo = *ze.wo;
    std::vector<int32_t> blocked_local;
    for (const uint32_t t : blocked) {
        const int64_t local = static_cast<int64_t>(t) - zc.lo;
        if (local >= 0 && local < static_cast<int64_t>(zc.mesh.T.size())) {
            blocked_local.push_back(static_cast<int32_t>(local));
        }
    }
    const std::optional<double> sfl =
        start_floor_y > kBaseNavFloorYValidMin ? std::optional<double>(static_cast<double>(start_floor_y)) : std::nullopt;
    const std::optional<double> gfl =
        goal_floor_y > kBaseNavFloorYValidMin ? std::optional<double>(static_cast<double>(goal_floor_y)) : std::nullopt;
    const auto ss = zc.snap(start, kSnapRadius, sfl);
    if (!ss.has_value()) {
        res.error = "起点不在网格附近";
        return res;
    }
    if (!zc.snap(goal, kSnapRadius, gfl).has_value()) {
        res.error = "终点不在网格附近";
        return res;
    }
    const double h0 = triHeightOf(zc.mesh, ss->tri);

    const double margins[4] = { kMargin, kMargin * 2, kMargin * 4, kMargin * 8 };
    const int pass_count = (blocked.empty() && blocked_points.empty()) ? 8 : 1;
    const auto plan_started_at = std::chrono::steady_clock::now();
    const auto elapsed_ms = [&plan_started_at] {
        return std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - plan_started_at).count();
    };
    int64_t prev_cells = 0;
    int64_t prev_ms = 0;
    std::string last_err;
    bool widen_requested = false;
    for (int pass = 0; pass < pass_count; ++pass) {
        const int mi = pass % 4;
        const bool climb_faces = pass >= 4;
        const double x0 = std::min(start.x, goal.x) - margins[mi];
        const double y0 = std::min(start.y, goal.y) - margins[mi];
        const double x1 = std::max(start.x, goal.x) + margins[mi];
        const double y1 = std::max(start.y, goal.y) + margins[mi];
        const int64_t nx = static_cast<int64_t>(std::ceil((x1 - x0) / kCS));
        const int64_t ny = static_cast<int64_t>(std::ceil((y1 - y0) / kCS));
        if (nx * ny > kMaxCells) {
            res.error = "窗口过大 (" + std::to_string(nx) + "×" + std::to_string(ny) + " 格)";
            return res;
        }
        // 本档窗口按上一档实测速度外推,预算装不下就停在已有结论上。终点不连通时每档都是
        // 同一个失败,而窗口面积逐档翻番,跑满四档能到分钟级;短途窗口小,外推值远在预算内。
        if (pass > 0) {
            if (budget.should_stop && budget.should_stop()) {
                res.error = "规划已取消";
                return res;
            }
            const int64_t projected_ms = prev_cells > 0 ? prev_ms * (nx * ny) / prev_cells : 0;
            // 上一档主动要求扩窗(锚点出窗/终线触界)是有结论的重试,下一档通常就成,按整次预算走。
            // 没要求就是同一个失败重来一遍,只把窗口翻番,用更紧的上限收住。
            const int64_t limit = widen_requested ? budget.wall_ms : std::min(budget.wall_ms, budget.dead_end_ms);
            if (elapsed_ms() + projected_ms > limit) {
                char buf[192];
                std::snprintf(
                    buf,
                    sizeof(buf),
                    "%s (扩窗预算耗尽: 已用 %lldms, 下档 %lld×%lld 格约需 %lldms)",
                    last_err.empty() ? "路线失败" : last_err.c_str(),
                    static_cast<long long>(elapsed_ms()),
                    static_cast<long long>(nx),
                    static_cast<long long>(ny),
                    static_cast<long long>(projected_ms));
                res.error = buf;
                return res;
            }
        }
        const int64_t pass_started_ms = elapsed_ms();
        widen_requested = false;
        std::string err;
        const auto info = buildWindow(zc, wo, start, ss->point, h0, x0, y0, x1, y1, blocked_local, blocked_points, err);
        if (info.has_value()) {
            RouteDiag dg;
            const auto line = routeWindow(*info, start, goal, climb_faces, dg);
            if (line.has_value()) {
                // 锚点远 = 走廊出窗,同触界扩窗,否则末段盲跳穿墙
                if (std::max(dg.snap_start, dg.snap_goal) > kSnapRadius) {
                    if (mi == 3) {
                        char buf[128];
                        std::snprintf(
                            buf,
                            sizeof(buf),
                            "端点接不上可走层 (起 %.1fpx / 终 %.1fpx, 疑似不连通)",
                            dg.snap_start,
                            dg.snap_goal);
                        res.error = buf;
                        return res;
                    }
                    err = "端点锚点过远,扩窗重跑";
                    widen_requested = true;
                }
                else {
                    double mnx = line->front().x;
                    double mxx = mnx;
                    double mny = line->front().y;
                    double mxy = mny;
                    for (const auto& p : *line) {
                        mnx = std::min(mnx, p.x);
                        mxx = std::max(mxx, p.x);
                        mny = std::min(mny, p.y);
                        mxy = std::max(mxy, p.y);
                    }
                    const double pad = 2.0;
                    if (mi == 3 || (mnx > x0 + pad && mxx < x1 - pad && mny > y0 + pad && mxy < y1 - pad)) {
                        res.ok = true;
                        res.points = *line;
                        for (size_t i = 1; i < line->size(); ++i) {
                            res.length += std::hypot((*line)[i].x - (*line)[i - 1].x, (*line)[i].y - (*line)[i - 1].y);
                        }
                        res.warnings = dg.warn;
                        res.clearance = dg.clearance;
                        res.wall_cross = dg.xwall;
                        res.snap_start = dg.snap_start;
                        res.snap_goal = dg.snap_goal;
                        return res;
                    }
                    err = "终线触界,扩窗重跑";
                    widen_requested = true;
                }
            }
            else {
                err = dg.err.empty() ? "路线失败" : dg.err;
            }
        }
        prev_ms = std::max<int64_t>(elapsed_ms() - pass_started_ms, 1);
        prev_cells = nx * ny;
        last_err = err;
    }
    res.error = last_err.empty() ? "路线失败" : last_err;
    return res;
}

}
