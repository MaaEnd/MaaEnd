#include "RecastNavRoute.h"

#include <algorithm>
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
};

struct RouteDiag
{
    std::string err;
    std::vector<std::string> warn;
    std::vector<WorldPoint> xwall;
    double snap_start = 0.0;
    double snap_goal = 0.0;
};

std::optional<WindowInfo> buildWindow(
    const ZoneClean& zc,
    WallOracle& wo,
    const WorldPoint& s,
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

    const auto widx = wo.wallsInBbox(x0 - 4, y0 - 4, x0 + static_cast<double>(nx) * kCS + 4,
                                     y0 + static_cast<double>(ny) * kCS + 4);
    std::vector<WorldPoint> p0;
    std::vector<WorldPoint> p1;
    std::vector<double> hh;
    for (const int64_t i : widx) {
        p0.push_back(wo.P0[static_cast<size_t>(i)]);
        p1.push_back(wo.P1[static_cast<size_t>(i)]);
        hh.push_back(wo.HH[static_cast<size_t>(i)]);
    }
    const std::vector<uint8_t> dead = StampWalls(p0, p1, hh, x0, y0, nx, ny, st);

    const int64_t gx = static_cast<int64_t>((s.x - x0) / kCS);
    const int64_t gy = static_cast<int64_t>((s.y - y0) / kCS);
    const int64_t cell0 = gy * nx + gx;
    const auto occ_it = std::lower_bound(st.occ.begin(), st.occ.end(), cell0);
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
        if (!std::isnan(lf) && std::fabs(rcs.h[ci] - lf) <= static_cast<float>(kMergeH)) {
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

    const std::vector<uint8_t> keep = WallsAtLayer(p0, p1, hh, info.lh, x0, y0);
    for (size_t i = 0; i < keep.size(); ++i) {
        if (keep[i] != 0) {
            info.wP0.push_back(p0[i]);
            info.wP1.push_back(p1[i]);
        }
    }
    info.wcsr = BuildWallIndex(info.wP0, info.wP1, x0, y0, nx, ny);
    info.dist = Clearance(info.core);
    return info;
}

std::optional<std::vector<WorldPoint>> routeWindow(const WindowInfo& info, const WorldPoint& s, const WorldPoint& g, RouteDiag& dg)
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
    // 亏欠越多单价越高;脊线保底只进几何口径 prefg,禁入 mult
    const Grid<float> pref = PrefField(info.dist, false);
    const Grid<float> prefg = PrefField(info.dist, true);
    Grid<float> mult(nx, ny, 0.0F);
    for (size_t i = 0; i < mult.v.size(); ++i) {
        const float c = std::min(std::max((pref.v[i] - info.dist.v[i]) / pref.v[i], 0.0F), 1.0F);
        mult.v[i] = 1.0F + static_cast<float>(kLam) * c;
    }

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
    const auto [as_, dsa] = near(walk, sc);
    const auto [ag_, dga] = near(walk, gc);
    if (!as_.has_value()) {
        dg.err = "walk 掩膜为空";
        return std::nullopt;
    }
    dg.snap_start = dsa;
    dg.snap_goal = dga;

    const double BIGP = static_cast<double>(nx * ny) * kCS * (1.0 + kLam);
    const Mask* used = &walk;
    std::optional<std::vector<CellPt>> q;
    if (as_->x == ag_->x && as_->y == ag_->y) {
        q = std::vector<CellPt> { *as_ };
    }
    else {
        q = CostAstar(walk, *as_, *ag_, mult, &bn, &BIGP);
    }
    if (!q.has_value()) {
        used = &info.core;
        q = CostAstar(info.core, *as_, *ag_, mult, &bn, &BIGP);
        if (q.has_value()) {
            dg.warn.push_back("walk 断开→退回 core");
        }
    }
    if (!q.has_value()) {
        dg.err = "不连通";
        return std::nullopt;
    }
    const int64_t NC = nx * ny;
    for (size_t k = 1; k < q->size(); ++k) {
        const int64_t ca = (*q)[k - 1].y * nx + (*q)[k - 1].x;
        const int64_t cb = (*q)[k].y * nx + (*q)[k].x;
        if (bn.contains(ca * NC + cb)) {
            dg.xwall.push_back({ x0 + (static_cast<double>((*q)[k].x) + 0.5) * kCS,
                                 y0 + (static_cast<double>((*q)[k].y) + 0.5) * kCS });
        }
    }
    if (!dg.xwall.empty()) {
        dg.warn.push_back("不可避穿墙 " + std::to_string(dg.xwall.size()) + " 步");
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
    const Blockers::OnMask onm { used, x0, y0, kCS };
    const Blockers blk_gray(loops_core, &info.wP0, &info.wP1, onm);

    std::vector<uint8_t> grn(q->size());
    for (size_t i = 0; i < q->size(); ++i) {
        grn[i] = static_cast<uint8_t>(
            info.dist.at((*q)[i].y, (*q)[i].x) >= prefg.at((*q)[i].y, (*q)[i].x) - 1e-9F);
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
        if (!runs[k].green && static_cast<double>(runs[k].i1 - runs[k].i0) * kCS < 2.0 && k > 0
            && k < runs.size() - 1) {
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

    const Grid<float> ones(nx, ny, 1.0F);
    std::vector<WorldPoint> taut;
    for (const auto& run : mg) {
        const int64_t iend = std::min(run.i1 + 1, static_cast<int64_t>(q->size()) - 1);
        const std::vector<CellPt> cells(q->begin() + run.i0, q->begin() + iend + 1);
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
                                for (int64_t x = std::max<int64_t>(0, dx); x < nx + std::min<int64_t>(0, dx);
                                     ++x) {
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
                            info.dist.at(y, x) >= pref.at(y, x)
                            || (info.dist.at(y, x) >= prefg.at(y, x) && pmd.at(y, x) != 0) || pm.at(y, x) != 0);
                    }
                }
                const auto q2 = CostAstar(er, cells.front(), cells.back(), ones, &bn, nullptr);
                if (q2.has_value()) {
                    double l1 = 0.0;
                    double l2 = 0.0;
                    for (size_t k = 1; k < cells.size(); ++k) {
                        l1 += std::hypot(static_cast<double>(cells[k].x - cells[k - 1].x),
                                         static_cast<double>(cells[k].y - cells[k - 1].y));
                    }
                    for (size_t k = 1; k < q2->size(); ++k) {
                        l2 += std::hypot(static_cast<double>((*q2)[k].x - (*q2)[k - 1].x),
                                         static_cast<double>((*q2)[k].y - (*q2)[k - 1].y));
                    }
                    if (l2 <= l1 * 1.2 + 2.0 / kCS) {
                        pp = cen(*q2);
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
                blk_green.emplace(lw, &info.wP0, &info.wP1, onm);
            }
            pp = StringPull(pp, blk_green.has_value() ? *blk_green : blk_gray);
        }
        if (!taut.empty() && !pp.empty()
            && std::hypot(pp.front().x - taut.back().x, pp.front().y - taut.back().y) < 1e-9) {
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
            || (std::hypot(line[i].x - s.x, line[i].y - s.y) > 0.4
                && std::hypot(line[i].x - g.x, line[i].y - g.y) > 0.4)) {
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
    if (kSlimEps > 0 && out.size() > 2) {
        out = Slim(out, blk_gray, kSlimEps);
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
    const std::vector<WorldPoint>& blocked_points)
{
    const std::lock_guard<std::mutex> lock(mutex_);
    return planLocked(zone_name, start, goal, start_floor_y, goal_floor_y, blocked, blocked_points);
}

RecastPlanResult RecastNavEngine::planLocked(
    const std::string& zone_name,
    const WorldPoint& start,
    const WorldPoint& goal,
    float start_floor_y,
    float goal_floor_y,
    const std::vector<uint32_t>& blocked,
    const std::vector<WorldPoint>& blocked_points)
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
    const std::optional<double> sfl = start_floor_y > kBaseNavFloorYValidMin
        ? std::optional<double>(static_cast<double>(start_floor_y))
        : std::nullopt;
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
    std::string last_err;
    for (int mi = 0; mi < 4; ++mi) {
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
        std::string err;
        const auto info = buildWindow(zc, wo, start, h0, x0, y0, x1, y1, blocked_local, blocked_points, err);
        if (info.has_value()) {
            RouteDiag dg;
            const auto line = routeWindow(*info, start, goal, dg);
            if (line.has_value()) {
                // 锚点远 = 走廊出窗,同触界扩窗,否则末段盲跳穿墙
                if (std::max(dg.snap_start, dg.snap_goal) > kSnapRadius) {
                    if (mi == 3) {
                        char buf[128];
                        std::snprintf(buf, sizeof(buf), "端点接不上可走层 (起 %.1fpx / 终 %.1fpx, 疑似不连通)",
                                      dg.snap_start, dg.snap_goal);
                        res.error = buf;
                        return res;
                    }
                    err = "端点锚点过远,扩窗重跑";
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
                        res.wall_cross = dg.xwall;
                        res.snap_start = dg.snap_start;
                        res.snap_goal = dg.snap_goal;
                        return res;
                    }
                    err = "终线触界,扩窗重跑";
                }
            }
            else {
                err = dg.err.empty() ? "路线失败" : dg.err;
            }
        }
        last_err = err;
    }
    res.error = last_err.empty() ? "路线失败" : last_err;
    return res;
}

}
