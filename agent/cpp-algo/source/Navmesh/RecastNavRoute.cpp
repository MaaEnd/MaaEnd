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
    std::vector<WorldPoint> sourceWallP0;
    std::vector<WorldPoint> sourceWallP1;
    std::vector<double> sourceWallH0;
    std::vector<double> sourceWallH1;
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
    bool crossed_barrier = false;
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
    bool layered_core,
    std::optional<double> layer_hint,
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
    std::vector<double> wh0;
    std::vector<double> wh1;
    std::vector<double> hh;
    for (const int64_t i : widx) {
        p0.push_back(wo.P0[static_cast<size_t>(i)]);
        p1.push_back(wo.P1[static_cast<size_t>(i)]);
        wh0.push_back(wo.H0[static_cast<size_t>(i)]);
        wh1.push_back(wo.H1[static_cast<size_t>(i)]);
        hh.push_back(wo.HH[static_cast<size_t>(i)]);
    }
    const std::vector<uint8_t> dead = layered_core ? std::vector<uint8_t>() : StampWalls(p0, p1, hh, x0, y0, nx, ny, st);

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
    // 声明面只参与墙的局部选层；旧 core 仍用原来的层图，避免成功线路被恢复逻辑改写。
    Grid<float> wall_lh = info.lh;
    if (layer_hint.has_value()) {
        std::fill(wall_lh.v.begin(), wall_lh.v.end(), std::numeric_limits<float>::quiet_NaN());
        for (size_t row = 0; row < st.occ.size(); ++row) {
            int64_t best_span = -1;
            double best_delta = 0.0;
            for (int64_t k = 0; k < st.K; ++k) {
                const int64_t sid = st.IK[row * static_cast<size_t>(st.K) + static_cast<size_t>(k)];
                if (sid < 0 || vis[static_cast<size_t>(sid)] == 0) {
                    continue;
                }
                const double delta = std::abs(static_cast<double>(st.sp_h[static_cast<size_t>(sid)]) - *layer_hint);
                if (best_span < 0 || delta < best_delta) {
                    best_span = sid;
                    best_delta = delta;
                }
            }
            if (best_span >= 0) {
                wall_lh.v[static_cast<size_t>(st.occ[row])] = st.sp_h[static_cast<size_t>(best_span)];
            }
        }
    }
    if (layered_core) {
        info.lh = wall_lh;
    }
    info.sourceWallP0 = p0;
    info.sourceWallP1 = p1;
    info.sourceWallH0 = wh0;
    info.sourceWallH1 = wh1;
    if (layer_hint.has_value()) {
        WallSegments walls = ClipWallsAtLayerInterpolated(p0, p1, wh0, wh1, wall_lh, x0, y0);
        info.wP0 = std::move(walls.p0);
        info.wP1 = std::move(walls.p1);
    }
    else {
        const std::vector<uint8_t> keep = WallsAtLayer(p0, p1, hh, info.lh, x0, y0);
        for (size_t i = 0; i < keep.size(); ++i) {
            if (keep[i] != 0) {
                info.wP0.push_back(p0[i]);
                info.wP1.push_back(p1[i]);
            }
        }
    }
    info.wcsr = BuildWallIndex(info.wP0, info.wP1, x0, y0, nx, ny);
    Mask wallcell(nx, ny, 0);
    if (layered_core) {
        for (size_t cell = 0; cell < wallcell.v.size(); ++cell) {
            wallcell.v[cell] = static_cast<uint8_t>(info.wcsr.start[cell + 1] > info.wcsr.start[cell]);
        }
    }
    else {
        for (size_t si = 0; si < dead.size(); ++si) {
            if (dead[si] != 0) {
                wallcell.v[static_cast<size_t>(st.sp_cell[si])] = 1;
            }
        }
    }
    info.lay = FillHoles(info.lay, kHoleMaxCells, &wallcell);
    Mask corein(nx, ny, 0);
    std::vector<int64_t> span_row;
    if (layered_core) {
        span_row.assign(static_cast<size_t>(nx * ny), -1);
        for (size_t row = 0; row < st.occ.size(); ++row) {
            span_row[static_cast<size_t>(st.occ[row])] = static_cast<int64_t>(row);
        }
    }
    for (size_t ci = 0; ci < rcs.cell.size(); ++ci) {
        if (rcs.ins[ci] == 0) {
            continue;
        }
        bool matches = false;
        if (layered_core) {
            const int64_t row_index = span_row[static_cast<size_t>(rcs.cell[ci])];
            if (row_index >= 0) {
                const size_t row = static_cast<size_t>(row_index);
                for (int64_t k = 0; k < st.K && !matches; ++k) {
                    const int64_t sid = st.IK[row * static_cast<size_t>(st.K) + static_cast<size_t>(k)];
                    matches = sid >= 0 && vis[static_cast<size_t>(sid)] != 0
                              && std::fabs(rcs.h[ci] - st.sp_h[static_cast<size_t>(sid)]) <= static_cast<float>(kQH);
                }
            }
        }
        else {
            const float lf = info.lh.v[static_cast<size_t>(rcs.cell[ci])];
            matches = !std::isnan(lf) && std::fabs(rcs.h[ci] - lf) <= static_cast<float>(kQH);
        }
        if (matches) {
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

    info.segA = info.wP0;
    info.segA.insert(info.segA.end(), info.sev.p0.begin(), info.sev.p0.end());
    info.segB = info.wP1;
    info.segB.insert(info.segB.end(), info.sev.p1.begin(), info.sev.p1.end());
    info.dist = Clearance(info.core);
    return info;
}

bool layerWallClear(
    const WindowInfo& info,
    const LayerOracle& layer_oracle,
    const std::vector<WorldPoint>& line,
    float start_height,
    std::optional<double> goal_deck)
{
    struct Hit
    {
        double route_t = 0.0;
        double wall_height = 0.0;
    };

    constexpr double kIntersectionEpsilon = 1e-7;
    // 拉直后的终线再对原始墙边逐交点验层。候选在交点若只剩与墙同高的
    // 连续层就判死；上层墙与下层路线平面重叠时则不会互相误伤。
    std::vector<float> heights { start_height };
    for (size_t segment = 1; segment < line.size(); ++segment) {
        const WorldPoint& p = line[segment - 1];
        const WorldPoint& q = line[segment];
        const double route_x = q.x - p.x;
        const double route_y = q.y - p.y;
        const double route_min_x = std::min(p.x, q.x);
        const double route_max_x = std::max(p.x, q.x);
        const double route_min_y = std::min(p.y, q.y);
        const double route_max_y = std::max(p.y, q.y);
        std::vector<Hit> hits;
        for (size_t wall = 0; wall < info.sourceWallP0.size(); ++wall) {
            const WorldPoint& a = info.sourceWallP0[wall];
            const WorldPoint& b = info.sourceWallP1[wall];
            if (std::max(a.x, b.x) < route_min_x || std::min(a.x, b.x) > route_max_x || std::max(a.y, b.y) < route_min_y
                || std::min(a.y, b.y) > route_max_y) {
                continue;
            }
            const double wall_x = b.x - a.x;
            const double wall_y = b.y - a.y;
            const double den = route_x * wall_y - route_y * wall_x;
            if (std::abs(den) <= 1e-12) {
                continue;
            }
            const double offset_x = a.x - p.x;
            const double offset_y = a.y - p.y;
            const double route_t = (offset_x * wall_y - offset_y * wall_x) / den;
            const double wall_t = (offset_x * route_y - offset_y * route_x) / den;
            if (route_t <= kIntersectionEpsilon || route_t >= 1.0 - kIntersectionEpsilon || wall_t <= kIntersectionEpsilon
                || wall_t >= 1.0 - kIntersectionEpsilon) {
                continue;
            }
            hits.push_back({ route_t, info.sourceWallH0[wall] + (info.sourceWallH1[wall] - info.sourceWallH0[wall]) * wall_t });
        }
        std::stable_sort(hits.begin(), hits.end(), [](const Hit& a, const Hit& b) { return a.route_t < b.route_t; });

        WorldPoint cursor = p;
        for (size_t first = 0; first < hits.size();) {
            size_t last = first + 1;
            while (last < hits.size() && std::abs(hits[last].route_t - hits[first].route_t) <= kIntersectionEpsilon) {
                ++last;
            }
            const WorldPoint crossing {
                p.x + route_x * hits[first].route_t,
                p.y + route_y * hits[first].route_t,
            };
            auto at_crossing = layer_oracle.walk({ cursor, crossing }, heights);
            if (!at_crossing.has_value()) {
                return false;
            }
            std::erase_if(*at_crossing, [&](float height) {
                for (size_t hit = first; hit < last; ++hit) {
                    if (std::abs(static_cast<double>(height) - hits[hit].wall_height) <= kQH) {
                        return true;
                    }
                }
                return false;
            });
            if (at_crossing->empty()) {
                return false;
            }
            heights = std::move(*at_crossing);
            cursor = crossing;
            first = last;
        }
        auto at_end = layer_oracle.walk({ cursor, q }, heights);
        if (!at_end.has_value()) {
            return false;
        }
        heights = std::move(*at_end);
    }
    return !goal_deck.has_value() || std::any_of(heights.begin(), heights.end(), [&](float height) {
        return std::abs(static_cast<double>(height) - *goal_deck) <= kDeckBand;
    });
}

// goal_deck: 终点所在面的高度。不声明时终点集是该格全部 span,先够到哪张停哪张
std::optional<std::vector<WorldPoint>> routeWindow(
    const WindowInfo& info,
    const WorldPoint& s,
    const WorldPoint& g,
    bool climb_faces,
    RouteDiag& dg,
    std::optional<double> goal_deck = std::nullopt,
    bool hard_walls = false,
    bool validate_layer_walls = false)
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
        const float cdef = std::min(std::max((static_cast<float>(kR) - dist.v[i]) / static_cast<float>(kR), 0.0F), 1.0F);
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
    // 高度最近的一张; 超出 kDeckBand 视为该面不在此格
    const auto atDeck = [&](const std::vector<int64_t>& vs, double deck) {
        int64_t best = -1;
        double bd = 0.0;
        for (const int64_t v : vs) {
            const double d = std::fabs(static_cast<double>(st3.sp_h[static_cast<size_t>(v)]) - deck);
            if (best < 0 || d < bd) {
                best = v;
                bd = d;
            }
        }
        return best >= 0 && bd <= kDeckBand ? best : -1;
    };
    // 起点的面只由起点自己的高度决定, 终点的声明不参与。h0 来自二维吸附,
    // 重叠处可能落在屋顶, 所以终点声明了面时起点层不再当已知, 按 h0 由近到远逐张试
    const auto seedsOf = [&](const std::vector<int64_t>& vs) -> std::vector<int64_t> {
        if (!goal_deck.has_value()) {
            return std::vector<int64_t> { atSeedLayer(vs) };
        }
        const auto h0 = static_cast<float>(info.h0);
        std::vector<int64_t> out = vs;
        std::stable_sort(out.begin(), out.end(), [&](const int64_t a, const int64_t b) {
            return std::fabs(st3.sp_h[static_cast<size_t>(a)] - h0) < std::fabs(st3.sp_h[static_cast<size_t>(b)] - h0);
        });
        return out;
    };
    // 终点声明是硬的: 收敛到单张 span, 匹配不上交空集让本级失败
    const auto goalsOf = [&](const std::vector<int64_t>& vs) {
        if (!goal_deck.has_value()) {
            return vs;
        }
        const int64_t v = atDeck(vs, *goal_deck);
        return v >= 0 ? std::vector<int64_t> { v } : std::vector<int64_t> {};
    };

    auto [as_, dsa] = near(cw3, sc);
    auto [ag_, dga] = near(cw3, gc);
    if (!as_.has_value()) {
        dg.err = "walk 掩膜为空";
        return std::nullopt;
    }

    const double BIGP = static_cast<double>(nx * ny) * kCS * (1.0 + kLam);
    const std::unordered_set<int64_t>* faces = hard_walls ? &blocked_steps : (climb_faces ? nullptr : &info.sev.steps);
    const std::unordered_set<int64_t>* soft = hard_walls ? nullptr : (climb_faces ? &blocked_steps : &bn);
    const double* soft_penalty = hard_walls ? nullptr : &BIGP;
    Mask on3 = cw3;
    // 起点层不确定时按 seedsOf 的次序逐张试, 第一张走通的就是它所在的面
    const auto search = [&](const std::vector<uint8_t>& use,
                            const Mask& c3,
                            const std::vector<int64_t>& svs,
                            const std::vector<int64_t>& gs) -> std::optional<std::vector<int64_t>> {
        for (const int64_t sd : seedsOf(svs)) {
            auto r = SpanAstar(st3, use, info.cidx, c3, sd, gs, mult, soft, soft_penalty, faces);
            if (r.has_value()) {
                return r;
            }
        }
        return std::nullopt;
    };
    std::optional<std::vector<int64_t>> qs;
    if (as_->x == ag_->x && as_->y == ag_->y) {
        const std::vector<int64_t> vs = pick(*as_, useW);
        const std::vector<int64_t> gs = goalsOf(vs);
        if (!goal_deck.has_value()) {
            qs = std::vector<int64_t> { seedsOf(vs).front() };
        }
        else if (!gs.empty()) {
            qs = std::vector<int64_t> { gs.front() };
        }
    }
    else {
        const std::vector<int64_t> gs = goalsOf(pick(*ag_, useW));
        if (!goal_deck.has_value() || !gs.empty()) {
            qs = search(useW, cw3, pick(*as_, useW), gs);
        }
    }
    if (!qs.has_value()) {
        const auto [ac_, dc_] = near(cc3, sc);
        const auto [ag2, dg2] = near(cc3, gc);
        if (ac_.has_value() && ag2.has_value()) {
            if (ac_->x == ag2->x && ac_->y == ag2->y) {
                const std::vector<int64_t> vs = pick(*ac_, useC);
                const std::vector<int64_t> gs = goalsOf(vs);
                if (!goal_deck.has_value()) {
                    qs = std::vector<int64_t> { seedsOf(vs).front() };
                }
                else if (!gs.empty()) {
                    qs = std::vector<int64_t> { gs.front() };
                }
            }
            else {
                const std::vector<int64_t> gs = goalsOf(pick(*ag2, useC));
                if (!goal_deck.has_value() || !gs.empty()) {
                    qs = search(useC, cc3, pick(*ac_, useC), gs);
                }
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
        // 格级搜索连 span 都不看, 退到这一级等于把选层交回给楼层盲的那一级
        if (goal_deck.has_value()) {
            const std::vector<int64_t> gv = pick(*ag_, useW);
            std::vector<float> hs;
            for (const int64_t v : gv) {
                hs.push_back(st3.sp_h[static_cast<size_t>(v)]);
            }
            std::sort(hs.begin(), hs.end());
            hs.erase(std::unique(hs.begin(), hs.end()), hs.end());
            std::string list;
            char buf[32];
            for (const float h : hs) {
                std::snprintf(buf, sizeof(buf), "%.2f", static_cast<double>(h));
                list += (list.empty() ? "" : ", ") + std::string(buf);
            }
            std::snprintf(buf, sizeof(buf), "%.2f", *goal_deck);
            // 声明的面在表里就是这一跳连不上, 不在表里是这个坐标底下没有那张面
            const std::string tail = atDeck(gv, *goal_deck) >= 0 ? "这一跳连不上, 拆成多段" : "该坐标下没有这张面";
            dg.err = "目标面不可达 (声明 " + std::string(buf) + ", 终点格里的面 "
                + (list.empty() ? std::string("无") : "[" + list + "]") + ") — " + tail;
            return std::nullopt;
        }
        std::tie(as_, dsa) = near(walk, sc);
        std::tie(ag_, dga) = near(walk, gc);
        on3 = walk;
        if (as_->x == ag_->x && as_->y == ag_->y) {
            q = std::vector<CellPt> { *as_ };
        }
        else {
            q = CostAstar(walk, *as_, *ag_, mult, soft, soft_penalty, faces);
        }
        if (!q.has_value()) {
            on3 = info.core;
            q = CostAstar(info.core, *as_, *ag_, mult, soft, soft_penalty, faces);
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
    dg.crossed_barrier = !bad.empty();

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
    if (validate_layer_walls
        && (!qs.has_value() || !layerWallClear(info, lyo, out, st3.sp_h[static_cast<size_t>(qs->front())], goal_deck))) {
        dg.err = "终线穿过当前层墙体";
        return std::nullopt;
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
    float goal_deck_y,
    const std::vector<uint32_t>& blocked,
    const std::vector<WorldPoint>& blocked_points,
    const RecastPlanBudget& budget)
{
    const std::lock_guard<std::mutex> lock(mutex_);
    return planLocked(
        zone_name,
        start,
        goal,
        start_floor_y,
        goal_floor_y,
        goal_deck_y,
        blocked,
        blocked_points,
        budget);
}

RecastPlanResult RecastNavEngine::planLocked(
    const std::string& zone_name,
    const WorldPoint& start,
    const WorldPoint& goal,
    float start_floor_y,
    float goal_floor_y,
    float goal_deck_y,
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
    const std::optional<double> gdk =
        goal_deck_y > kBaseNavFloorYValidMin ? std::optional<double>(static_cast<double>(goal_deck_y)) : std::nullopt;
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
        auto info = buildWindow(zc, wo, start, ss->point, h0, x0, y0, x1, y1, blocked_local, blocked_points, false, std::nullopt, err);
        if (info.has_value()) {
            RouteDiag dg;
            auto line = routeWindow(*info, start, goal, climb_faces, dg, gdk);
            const bool same_deck_recovery = gdk.has_value() && std::abs(h0 - *gdk) <= kDeckBand;
            if (same_deck_recovery && !line.has_value()) {
                // 成功的旧线路原样保留：f4768a05 明确保留不可避免的软约束步，
                // 恢复寻路不能借声明面反向改写它。仅在旧拓扑确实失败时换稳定相位重建。
                std::string recovery_err;
                const double recovery_x0 = std::floor((x0 - kCS / 2.0) / kCS) * kCS + kCS / 2.0;
                const double recovery_y0 = std::floor((y0 - kCS / 2.0) / kCS) * kCS + kCS / 2.0;
                const double recovery_x1 = recovery_x0 + std::ceil((x1 - recovery_x0) / kCS) * kCS;
                const double recovery_y1 = recovery_y0 + std::ceil((y1 - recovery_y0) / kCS) * kCS;
                const int64_t recovery_nx = static_cast<int64_t>(std::ceil((recovery_x1 - recovery_x0) / kCS));
                const int64_t recovery_ny = static_cast<int64_t>(std::ceil((recovery_y1 - recovery_y0) / kCS));
                std::optional<WindowInfo> recovery_info;
                if (recovery_nx * recovery_ny > kMaxCells) {
                    recovery_err = "恢复窗口过大 (" + std::to_string(recovery_nx) + "×" + std::to_string(recovery_ny) + " 格)";
                }
                else {
                    recovery_info = buildWindow(
                        zc,
                        wo,
                        start,
                        ss->point,
                        h0,
                        recovery_x0,
                        recovery_y0,
                        recovery_x1,
                        recovery_y1,
                        blocked_local,
                        blocked_points,
                        false,
                        gdk,
                        recovery_err);
                }
                if (recovery_info.has_value()) {
                    RouteDiag recovery_dg;
                    auto recovery_line = routeWindow(*recovery_info, start, goal, false, recovery_dg, gdk, false, true);
                    if (recovery_line.has_value() && !recovery_dg.crossed_barrier) {
                        info = std::move(recovery_info);
                        line = std::move(recovery_line);
                        dg = std::move(recovery_dg);
                    }
                    else {
                        dg = std::move(recovery_dg);
                    }
                }
                else {
                    dg.err = std::move(recovery_err);
                }
                if (!line.has_value()) {
                    // 稳定相位仍无安全终线时，退回完整分层且墙体硬约束的保守路径。
                    const double hard_x0 = std::floor(x0 / kCS) * kCS;
                    const double hard_y0 = std::floor(y0 / kCS) * kCS;
                    const double hard_x1 = std::ceil(x1 / kCS) * kCS;
                    const double hard_y1 = std::ceil(y1 / kCS) * kCS;
                    const int64_t hard_nx = static_cast<int64_t>(std::ceil((hard_x1 - hard_x0) / kCS));
                    const int64_t hard_ny = static_cast<int64_t>(std::ceil((hard_y1 - hard_y0) / kCS));
                    std::string hard_err;
                    std::optional<WindowInfo> hard_info;
                    if (hard_nx * hard_ny > kMaxCells) {
                        hard_err = "恢复窗口过大 (" + std::to_string(hard_nx) + "×" + std::to_string(hard_ny) + " 格)";
                    }
                    else {
                        hard_info = buildWindow(
                            zc,
                            wo,
                            start,
                            ss->point,
                            h0,
                            hard_x0,
                            hard_y0,
                            hard_x1,
                            hard_y1,
                            blocked_local,
                            blocked_points,
                            true,
                            gdk,
                            hard_err);
                    }
                    if (hard_info.has_value()) {
                        RouteDiag hard_dg;
                        auto hard_line = routeWindow(*hard_info, start, goal, false, hard_dg, gdk, true, true);
                        if (hard_line.has_value() && !hard_dg.crossed_barrier) {
                            info = std::move(hard_info);
                            line = std::move(hard_line);
                            dg = std::move(hard_dg);
                        }
                        else {
                            dg = std::move(hard_dg);
                        }
                    }
                    else {
                        dg.err = std::move(hard_err);
                    }
                }
            }
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
