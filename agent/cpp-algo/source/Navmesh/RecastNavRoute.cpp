#include "RecastNavRoute.h"

#include "RecastNavBake.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <set>

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
};

struct RouteDiag
{
    RecastPlanResult::Debug::Timing timing;
    std::string err;
    std::vector<std::string> warn;
    std::vector<double> clearance;
    std::vector<double> height; // 逐点所在面的高度; 层预言机走不通时清空
    std::vector<size_t> waypoints;
    bool crossed_barrier = false;
    double snap_start = 0.0;
    double snap_goal = 0.0;
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
    std::vector<WorldPoint> astar_cells;
    std::vector<double> astar_heights;
    std::vector<WorldPoint> rerouted_points;
    std::vector<WorldPoint> string_pull_points;
    std::vector<WorldPoint> assembled_points;
    std::vector<WorldPoint> loop_fixed_points;
    std::vector<WorldPoint> slim_points;
    std::vector<WorldPoint> widened_points;
    std::vector<WorldPoint> planned_points;
    std::optional<WorldPoint> gap_start;
    std::optional<WorldPoint> gap_goal;
    std::optional<double> gap_distance;
};

double ElapsedMs(const std::chrono::steady_clock::time_point started_at)
{
    return std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - started_at).count();
}

std::vector<uint8_t> topologyReach(
    const SpanTable& st,
    const std::vector<uint8_t>& ok,
    const std::vector<int64_t>& cidx,
    const Mask& ok2,
    const std::vector<int64_t>& seeds,
    const std::unordered_set<int64_t>* forbidden,
    bool reverse)
{
    std::vector<uint8_t> visited(st.sp_h.size(), 0);
    std::vector<int64_t> frontier;
    frontier.reserve(seeds.size());
    for (const int64_t seed : seeds) {
        if (seed >= 0 && ok[static_cast<size_t>(seed)] != 0 && visited[static_cast<size_t>(seed)] == 0) {
            visited[static_cast<size_t>(seed)] = 1;
            frontier.push_back(seed);
        }
    }

    struct Step
    {
        int64_t dx;
        int64_t dy;
        double weight;
    };

    constexpr double kDiagonalWeight = 1.4142135623730951;
    constexpr Step kSteps[] = {
        { -1, -1, kDiagonalWeight }, { 0, -1, 1.0 }, { 1, -1, kDiagonalWeight }, { -1, 0, 1.0 }, { 1, 0, 1.0 },
        { -1, 1, kDiagonalWeight },  { 0, 1, 1.0 },  { 1, 1, kDiagonalWeight },
    };
    const int64_t nx = ok2.nx;
    const int64_t ny = ok2.ny;
    const int64_t cell_count = nx * ny;
    for (size_t cursor = 0; cursor < frontier.size(); ++cursor) {
        const int64_t current = frontier[cursor];
        const int64_t current_cell = st.sp_cell[static_cast<size_t>(current)];
        const int64_t x = current_cell % nx;
        const int64_t y = current_cell / nx;
        const float current_height = st.sp_h[static_cast<size_t>(current)];
        for (const Step& step : kSteps) {
            const int64_t next_x = x + step.dx;
            const int64_t next_y = y + step.dy;
            if (next_x < 0 || next_x >= nx || next_y < 0 || next_y >= ny) {
                continue;
            }
            const int64_t next_cell = next_y * nx + next_x;
            if (ok2.v[static_cast<size_t>(next_cell)] == 0) {
                continue;
            }
            if (step.dx != 0 && step.dy != 0 && !(ok2.at(y, next_x) && ok2.at(next_y, x))) {
                continue;
            }
            const int64_t column = cidx[static_cast<size_t>(next_cell)];
            if (column < 0) {
                continue;
            }
            const int64_t edge = reverse ? next_cell * cell_count + current_cell : current_cell * cell_count + next_cell;
            if (forbidden != nullptr && forbidden->contains(edge)) {
                continue;
            }
            for (int64_t slot = 0; slot < st.K; ++slot) {
                const int64_t candidate = st.IK[static_cast<size_t>(column * st.K + slot)];
                if (candidate < 0 || ok[static_cast<size_t>(candidate)] == 0 || visited[static_cast<size_t>(candidate)] != 0) {
                    continue;
                }
                const float candidate_height = st.sp_h[static_cast<size_t>(candidate)];
                const float dh = reverse ? current_height - candidate_height : candidate_height - current_height;
                if (static_cast<double>(dh) > kUp * step.weight || dh < -static_cast<float>(kClimb)) {
                    continue;
                }
                visited[static_cast<size_t>(candidate)] = 1;
                frontier.push_back(candidate);
            }
        }
    }
    return visited;
}

struct ReachGap
{
    WorldPoint start;
    WorldPoint goal;
    double distance = 0.0;
};

std::optional<ReachGap> closestReachGap(
    const SpanTable& st,
    const std::vector<uint8_t>& start_reach,
    const std::vector<uint8_t>& goal_reach,
    int64_t nx,
    int64_t ny,
    double x0,
    double y0)
{
    std::vector<uint8_t> start_cells(static_cast<size_t>(nx * ny), 0);
    std::vector<uint8_t> goal_cells(static_cast<size_t>(nx * ny), 0);
    for (size_t i = 0; i < st.sp_cell.size(); ++i) {
        const size_t cell = static_cast<size_t>(st.sp_cell[i]);
        start_cells[cell] |= start_reach[i];
        goal_cells[cell] |= goal_reach[i];
    }

    struct TaggedCell
    {
        int64_t x;
        int64_t y;
        bool from_start;
    };

    const auto boundary = [&](const std::vector<uint8_t>& cells, int64_t x, int64_t y) {
        constexpr int64_t kDx[] = { -1, 1, 0, 0 };
        constexpr int64_t kDy[] = { 0, 0, -1, 1 };
        for (size_t i = 0; i < std::size(kDx); ++i) {
            const int64_t ax = x + kDx[i];
            const int64_t ay = y + kDy[i];
            if (ax < 0 || ax >= nx || ay < 0 || ay >= ny || cells[static_cast<size_t>(ay * nx + ax)] == 0) {
                return true;
            }
        }
        return false;
    };

    std::vector<TaggedCell> points;
    std::optional<TaggedCell> first_start;
    std::optional<TaggedCell> first_goal;
    for (int64_t y = 0; y < ny; ++y) {
        for (int64_t x = 0; x < nx; ++x) {
            const size_t cell = static_cast<size_t>(y * nx + x);
            if (start_cells[cell] != 0 && boundary(start_cells, x, y)) {
                const TaggedCell point { .x = x, .y = y, .from_start = true };
                points.push_back(point);
                if (!first_start) {
                    first_start = point;
                }
            }
            if (goal_cells[cell] != 0 && boundary(goal_cells, x, y)) {
                const TaggedCell point { .x = x, .y = y, .from_start = false };
                points.push_back(point);
                if (!first_goal) {
                    first_goal = point;
                }
            }
        }
    }
    if (!first_start || !first_goal) {
        return std::nullopt;
    }

    std::sort(points.begin(), points.end(), [](const TaggedCell& lhs, const TaggedCell& rhs) {
        return lhs.x < rhs.x || (lhs.x == rhs.x && (lhs.y < rhs.y || (lhs.y == rhs.y && lhs.from_start < rhs.from_start)));
    });
    const auto squaredDistance = [](const TaggedCell& lhs, const TaggedCell& rhs) {
        const int64_t dx = lhs.x - rhs.x;
        const int64_t dy = lhs.y - rhs.y;
        return dx * dx + dy * dy;
    };
    int64_t best_squared = squaredDistance(*first_start, *first_goal);
    TaggedCell best_start = *first_start;
    TaggedCell best_goal = *first_goal;
    std::set<std::pair<int64_t, size_t>> active_start;
    std::set<std::pair<int64_t, size_t>> active_goal;
    size_t left = 0;
    for (size_t i = 0; i < points.size(); ++i) {
        const TaggedCell& point = points[i];
        while (left < i) {
            const int64_t dx = point.x - points[left].x;
            if (dx * dx <= best_squared) {
                break;
            }
            auto& active = points[left].from_start ? active_start : active_goal;
            active.erase({ points[left].y, left });
            ++left;
        }
        const auto& opposite = point.from_start ? active_goal : active_start;
        const int64_t radius = static_cast<int64_t>(std::floor(std::sqrt(static_cast<double>(best_squared))));
        auto it = opposite.lower_bound({ point.y - radius, 0 });
        const auto end = opposite.upper_bound({ point.y + radius, std::numeric_limits<size_t>::max() });
        for (; it != end; ++it) {
            const TaggedCell& candidate = points[it->second];
            const int64_t distance = squaredDistance(point, candidate);
            if (distance < best_squared) {
                best_squared = distance;
                if (point.from_start) {
                    best_start = point;
                    best_goal = candidate;
                }
                else {
                    best_start = candidate;
                    best_goal = point;
                }
            }
        }
        auto& active = point.from_start ? active_start : active_goal;
        active.emplace(point.y, i);
    }

    const auto toWorld = [&](const TaggedCell& point) {
        return WorldPoint {
            .x = x0 + (static_cast<double>(point.x) + 0.5) * kCS,
            .y = y0 + (static_cast<double>(point.y) + 0.5) * kCS,
        };
    };
    return ReachGap {
        .start = toWorld(best_start),
        .goal = toWorld(best_goal),
        .distance = std::sqrt(static_cast<double>(best_squared)) * kCS,
    };
}

// 窗口里从预烘图解出来的记录。格号已换成窗口格号,窗外的与不属于本瓦自有矩形的
// 都已剔除;一格只归一块瓦,所以同格的记录必然来自同一块瓦、按 (类号, 高) 排好。
struct GridWindow
{
    std::vector<GridSpanRec> rec;
    std::vector<int64_t> head; // 逐格: 记录链表头,无记录为 -1
    std::vector<int64_t> next; // 逐记录: 同格的下一条
};

bool loadGridWindow(const GridPack& gp, const GridZoneDir& gz, int64_t wgx0, int64_t wgy0, int64_t nx, int64_t ny, GridWindow& out)
{
    GridTile tile;
    for (const GridTileRef* t : GridTilesInRect(gz, wgx0, wgy0, wgx0 + nx - 1, wgy0 + ny - 1)) {
        if (t->records == 0) {
            continue;
        }
        if (!gp.decodeTile(*t, tile)) {
            return false;
        }
        for (GridSpanRec& r : tile.rec) {
            const int64_t ix = r.cell % t->nx;
            const int64_t iy = r.cell / t->nx;
            if (ix < t->px0 || ix > t->px1 || iy < t->py0 || iy > t->py1) {
                continue;
            }
            const int64_t wx = t->gx0 + ix - wgx0;
            const int64_t wy = t->gy0 + iy - wgy0;
            if (wx < 0 || wx >= nx || wy < 0 || wy >= ny) {
                continue;
            }
            r.cell = wy * nx + wx;
            out.rec.push_back(r);
        }
    }
    out.head.assign(static_cast<size_t>(nx * ny), -1);
    out.next.assign(out.rec.size(), -1);
    for (size_t i = out.rec.size(); i-- > 0;) {
        const auto c = static_cast<size_t>(out.rec[i].cell);
        out.next[i] = out.head[c];
        out.head[c] = static_cast<int64_t>(i);
    }
    return true;
}

// 起点格里高度离 h0 最近的那条真 span 定类。类选错整条线就落在另一层上。
int64_t pickStartRec(const GridWindow& gw, int64_t cell, double h0)
{
    int64_t best = -1;
    double bd = 0.0;
    for (int64_t i = gw.head[static_cast<size_t>(cell)]; i >= 0; i = gw.next[static_cast<size_t>(i)]) {
        const GridSpanRec& r = gw.rec[static_cast<size_t>(i)];
        if ((r.flags & (kGridFlagGhost | kGridFlagFill)) != 0) {
            continue;
        }
        const double d = std::fabs(static_cast<double>(r.h) - h0);
        if (best < 0 || d < bd) {
            best = i;
            bd = d;
        }
    }
    return best;
}

// 终点声明了面时改由终点定类:终点格附近带内、能走的那条,先按格距再按高度差挑。
// 起点那侧只在这个类里选面,所以起点二维吸附落在屋顶上也不会把线拉到别层去。
int64_t pickDeckRec(const GridWindow& gw, int64_t nx, int64_t ny, int64_t gcx, int64_t gcy, double deck)
{
    const auto rad = static_cast<int64_t>(std::ceil(kSnapRadius / kCS));
    int64_t best = -1;
    int64_t bcell_d = 0;
    double bh_d = 0.0;
    for (int64_t y = std::max<int64_t>(gcy - rad, 0); y <= std::min<int64_t>(gcy + rad, ny - 1); ++y) {
        for (int64_t x = std::max<int64_t>(gcx - rad, 0); x <= std::min<int64_t>(gcx + rad, nx - 1); ++x) {
            const int64_t cd = (x - gcx) * (x - gcx) + (y - gcy) * (y - gcy);
            if (best >= 0 && cd > bcell_d) {
                continue;
            }
            for (int64_t i = gw.head[static_cast<size_t>(y * nx + x)]; i >= 0; i = gw.next[static_cast<size_t>(i)]) {
                const GridSpanRec& r = gw.rec[static_cast<size_t>(i)];
                if ((r.flags & kGridFlagWalk) == 0 || (r.flags & kGridFlagFill) != 0) {
                    continue;
                }
                const double hd = std::fabs(static_cast<double>(r.h) - deck);
                if (hd > kDeckBand) {
                    continue;
                }
                if (best < 0 || cd < bcell_d || (cd == bcell_d && hd < bh_d)) {
                    best = i;
                    bcell_d = cd;
                    bh_d = hd;
                }
            }
        }
    }
    return best;
}

// 点到最近核心格的格距 × kCS,与窗口里的 near() 同口径,只是在全区图上量。
// 搜索半径取判据的两倍,够不着的点只报这个下界,反正它已经在闸外了。
double coreAnchorPx(const GridPack& gp, const GridZoneDir& gz, const WorldPoint& p)
{
    const double cs = gp.cellSize();
    const double reach = kSnapRadius * 2.0;
    const auto cx = static_cast<int64_t>(std::floor(p.x / cs));
    const auto cy = static_cast<int64_t>(std::floor(p.y / cs));
    const auto rad = static_cast<int64_t>(std::ceil(reach / cs));
    int64_t best = -1;
    GridTile tile;
    for (const GridTileRef* t : GridTilesInRect(gz, cx - rad, cy - rad, cx + rad, cy + rad)) {
        if (t->records == 0 || !gp.decodeTile(*t, tile)) {
            continue;
        }
        for (const GridSpanRec& r : tile.rec) {
            if ((r.flags & kGridFlagCore) == 0) {
                continue;
            }
            const int64_t ix = r.cell % t->nx;
            const int64_t iy = r.cell / t->nx;
            if (ix < t->px0 || ix > t->px1 || iy < t->py0 || iy > t->py1) {
                continue;
            }
            const int64_t dx = t->gx0 + ix - cx;
            const int64_t dy = t->gy0 + iy - cy;
            const int64_t d = dx * dx + dy * dy;
            if (best < 0 || d < best) {
                best = d;
            }
        }
    }
    return best < 0 ? reach : std::sqrt(static_cast<double>(best)) * cs;
}

std::optional<WindowInfo> buildWindow(
    WallOracle& wo,
    const GridPack& gp,
    const GridZoneDir& gz,
    const ZoneClean& zc,
    const WorldPoint& s,
    const WorldPoint& s_snap,
    const WorldPoint& g,
    double h0,
    std::optional<double> goal_deck,
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
    // 窗口原点是对齐过的,所以它落在全局格线上,窗口格与烘焙格一一对上
    const int64_t wgx0 = std::llround(x0 / kCS);
    const int64_t wgy0 = std::llround(y0 / kCS);
    GridWindow gw;
    if (!loadGridWindow(gp, gz, wgx0, wgy0, nx, ny, gw)) {
        err = "预烘格图解不开";
        return std::nullopt;
    }

    const auto cell_at = [&](int64_t cx, int64_t cy) {
        return cx < 0 || cx >= nx || cy < 0 || cy >= ny ? -1 : cy * nx + cx;
    };
    int64_t gx = static_cast<int64_t>((s.x - x0) / kCS);
    int64_t gy = static_cast<int64_t>((s.y - y0) / kCS);
    int64_t cell0 = cell_at(gx, gy);
    int64_t start_rec = cell0 >= 0 ? pickStartRec(gw, cell0, h0) : -1;
    if (start_rec < 0) {
        // 起点离网时其所在格无体素,退用按楼层吸附过的起点定种子
        gx = static_cast<int64_t>((s_snap.x - x0) / kCS);
        gy = static_cast<int64_t>((s_snap.y - y0) / kCS);
        cell0 = cell_at(gx, gy);
        start_rec = cell0 >= 0 ? pickStartRec(gw, cell0, h0) : -1;
    }
    if (start_rec < 0) {
        err = "起点格无体素 (gx=" + std::to_string(gx) + ",gy=" + std::to_string(gy) + ")";
        return std::nullopt;
    }
    uint32_t region = gw.rec[static_cast<size_t>(start_rec)].rid;
    if (goal_deck.has_value()) {
        const int64_t deck_rec =
            pickDeckRec(gw, nx, ny, static_cast<int64_t>((g.x - x0) / kCS), static_cast<int64_t>((g.y - y0) / kCS), *goal_deck);
        if (deck_rec < 0) {
            err = "终点附近没有声明的面 (deck=" + std::to_string(*goal_deck) + ")";
            return std::nullopt;
        }
        region = gw.rec[static_cast<size_t>(deck_rec)].rid;
    }

    WindowInfo info;
    info.x0 = x0;
    info.y0 = y0;
    info.nx = nx;
    info.ny = ny;
    info.lay = Mask(nx, ny, 0);
    info.core = Mask(nx, ny, 0);
    info.dist = Grid<float>(nx, ny, 0.0F);
    info.lh = Grid<float>(nx, ny, std::numeric_limits<float>::quiet_NaN());
    std::vector<uint8_t> stepbits(static_cast<size_t>(nx * ny), 0);
    std::vector<int64_t> sp_cell;
    std::vector<float> sp_h;
    // 表里留着别的类的 span:层判据要看整列,少一层就会从楼板底下穿过去
    for (const GridSpanRec& r : gw.rec) {
        const bool ghost = (r.flags & kGridFlagGhost) != 0;
        const bool fill = (r.flags & kGridFlagFill) != 0;
        const auto cell = static_cast<size_t>(r.cell);
        if (r.rid == region) {
            const bool core = (r.flags & kGridFlagCore) != 0;
            if ((r.flags & kGridFlagWalk) != 0 || !core) {
                info.lay.v[cell] = 1;
            }
            if (core) {
                info.core.v[cell] = 1;
            }
            info.dist.v[cell] = GridClearance(r.clr);
            stepbits[cell] |= r.steps;
            if (!ghost && !fill && (std::isnan(info.lh.v[cell]) || r.h > info.lh.v[cell])) {
                info.lh.v[cell] = r.h;
            }
        }
        if (fill || (ghost && r.rid != region)) {
            continue;
        }
        sp_cell.push_back(r.cell);
        sp_h.push_back(r.h);
        info.vis3.push_back(static_cast<uint8_t>(r.rid == region));
    }

    const BakedWalls walls = BakeWalls(wo, x0, y0, nx, ny);
    const std::vector<uint8_t> keep = WallsAtLayer(walls.p0, walls.p1, walls.hh, info.lh, x0, y0);
    for (size_t i = 0; i < keep.size(); ++i) {
        if (keep[i] != 0) {
            info.wP0.push_back(walls.p0[i]);
            info.wP1.push_back(walls.p1[i]);
        }
    }
    info.wcsr = BuildWallIndex(info.wP0, info.wP1, x0, y0, nx, ny);

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

    // 禁步面按烘出来的位还原。位序与方向表是写入方定的,方向倒序的那一位对应反向键;
    // 只有正交两向出线段,对角步不挡视线。封堵盖掉的格不再出面,与它被移出可走层一致。
    const int64_t nc = nx * ny;
    for (int i = 0; i < 4; ++i) {
        const int64_t dx = kGridStepDx[i];
        const int64_t dy = kGridStepDy[i];
        for (int64_t c = 0; c < nc; ++c) {
            const uint8_t bits = static_cast<uint8_t>(stepbits[static_cast<size_t>(c)] >> (2 * i)) & 0x03U;
            if (bits == 0 || info.lay.v[static_cast<size_t>(c)] == 0) {
                continue;
            }
            const int64_t ax = c % nx + dx;
            const int64_t ay = c / nx + dy;
            if (ax < 0 || ax >= nx || ay < 0 || ay >= ny) {
                continue;
            }
            const int64_t b = ay * nx + ax;
            if ((bits & 0x01U) != 0) {
                info.sev.steps.insert(c * nc + b);
            }
            if ((bits & 0x02U) != 0) {
                info.sev.steps.insert(b * nc + c);
            }
            if (dx != 0 && dy != 0) {
                continue;
            }
            const double px = x0 + static_cast<double>(c % nx + dx) * kCS;
            const double py = y0 + static_cast<double>(c / nx + dy) * kCS;
            info.sev.p0.push_back({ px, py });
            info.sev.p1.push_back({ px + static_cast<double>(dy) * kCS, py + static_cast<double>(dx) * kCS });
        }
    }

    info.h0 = h0;
    info.st3 = PackSpans(std::move(sp_cell), std::move(sp_h), &info.vis3);
    info.cidx.assign(static_cast<size_t>(nc), -1);
    for (size_t ci = 0; ci < info.st3.occ.size(); ++ci) {
        info.cidx[static_cast<size_t>(info.st3.occ[ci])] = static_cast<int64_t>(ci);
    }
    const int64_t sj = info.cidx[static_cast<size_t>(cell0)];
    int64_t seed3 = -1;
    float best3 = 0.0F;
    for (int64_t k = 0; k < info.st3.K; ++k) {
        const int64_t sid = info.st3.IK[static_cast<size_t>(sj * info.st3.K + k)];
        if (sid < 0 || info.vis3[static_cast<size_t>(sid)] == 0) {
            continue;
        }
        const float d = std::fabs(info.st3.sp_h[static_cast<size_t>(sid)] - static_cast<float>(h0));
        if (seed3 < 0 || d < best3) {
            seed3 = sid;
            best3 = d;
        }
    }
    if (seed3 < 0) {
        err = "起点格没有与终点同类的面";
        return std::nullopt;
    }
    info.segA = info.wP0;
    info.segA.insert(info.segA.end(), info.sev.p0.begin(), info.sev.p0.end());
    info.segB = info.wP1;
    info.segB.insert(info.segB.end(), info.sev.p1.begin(), info.sev.p1.end());
    // 烘出来的净空是没封堵时的;盖掉格子会让通道变窄,代价场得按盖过的核心重算
    if (!blocked_local.empty() || !blocked_points.empty()) {
        info.dist = Clearance(info.core);
    }
    return info;
}

// 贪心拉直:从上一个提交点出发,沿折线尽量往前够,一条直线走不通就把它停下的那个顶点收进航点。
// 判据两条都要过 —— 网格面高度连续(带起点高度), 以及窗口挡线格图不允许这条弦跨墙。
// 前者单独用会顺着叠层的下一层走通, 后者补的正是那一刀。
void PullWaypoints(
    const std::vector<WorldPoint>& pts,
    RouteDiag& dg,
    const BaseNavPlanner& pl,
    uint16_t zid,
    const Blockers& blk,
    bool has_layer)
{
    if (!has_layer || pts.size() < 2) {
        return;
    }
    // 一次拉直最多吞掉多少个顶点。纯成本上界:每多够一个都要把整条弦重测一遍,不封顶就是平方级。
    // 撞到上界只是把顶点留在原地,是安全的那一侧。
    constexpr size_t kMaxPullSpan = 64;
    const size_t anchor = pts.size() - 1;
    size_t cursor = 0;
    while (cursor + 1 < anchor) {
        // 捷径不得比它吞掉的最窄处更窄:那个宽度是路线自己判定这段通道需要的,拉直这一层不比它更懂。
        double swallowed = std::numeric_limits<double>::infinity();
        const std::optional<double> seed = cursor < dg.height.size() ? std::optional<double>(dg.height[cursor]) : std::nullopt;
        size_t reach = cursor;
        const size_t reach_limit = std::min(anchor, cursor + kMaxPullSpan);
        while (reach < reach_limit) {
            const WorldPoint& a = pts[cursor];
            const WorldPoint& c = pts[reach + 1];
            const double required = std::isfinite(swallowed) ? swallowed : 0.0;
            if (!pl.isRouteSegmentDrivable(zid, a, c, required, seed) || blk.blocked(a, c)) {
                break;
            }
            swallowed = std::min(swallowed, reach + 1 < dg.clearance.size() ? dg.clearance[reach + 1] : 0.0);
            ++reach;
        }
        // 连折线自己的下一条边都过不了判据时,把那个顶点原样留下仍是手上最好的答案,也保证循环往前走。
        if (reach == cursor) {
            ++reach;
        }
        if (reach >= anchor) {
            break;
        }
        dg.waypoints.push_back(reach);
        cursor = reach;
    }
    dg.waypoints.push_back(anchor);
}

// goal_deck: 终点所在面的高度。不声明时终点集是该格全部 span,先够到哪张停哪张
std::optional<std::vector<WorldPoint>> routeWindow(
    const WindowInfo& info,
    const WorldPoint& s,
    const WorldPoint& g,
    RouteDiag& dg,
    std::optional<double> goal_deck,
    bool capture_gap,
    const BaseNavPlanner& pl,
    uint16_t zid,
    bool exact_slim)
{
    const int64_t nx = info.nx;
    const int64_t ny = info.ny;
    const double x0 = info.x0;
    const double y0 = info.y0;
    Mask walk(nx, ny, 0);
    for (size_t i = 0; i < walk.v.size(); ++i) {
        walk.v[i] = static_cast<uint8_t>(info.core.v[i] != 0 && info.lay.v[i] != 0);
    }
    // 边界边只用来算余量, 不用来禁步: 补洞封缝那一步已经判定这些细缝可以跨,
    // 回头再拿同一批边禁掉跨缝的一步, 等于在每道接缝上凭空立一堵墙
    std::unordered_set<int64_t> blocked_steps(info.sev.steps.begin(), info.sev.steps.end());
    // 掩膜距离场对跨越边界边无感, 取到边界的距离的下确界补上
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
            if (info.vis3[i] != 0 && m2.v[cell] != 0) {
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
    // 终点声明是硬的: 收敛到单张 span, 匹配不上交空集让本级失败
    const auto goalsOf = [&](const std::vector<int64_t>& vs) {
        if (!goal_deck.has_value()) {
            return vs;
        }
        const int64_t v = atDeck(vs, *goal_deck);
        return v >= 0 ? std::vector<int64_t> { v } : std::vector<int64_t> {};
    };

    const auto nearGoal = [&](const std::vector<uint8_t>& use, const Mask& cells) -> std::pair<std::optional<CellPt>, double> {
        if (!goal_deck.has_value()) {
            return near(cells, gc);
        }
        bool have = false;
        int64_t best_distance = 0;
        double best_height = 0.0;
        CellPt best_cell;
        for (size_t i = 0; i < use.size(); ++i) {
            if (use[i] == 0) {
                continue;
            }
            const double height_distance = std::fabs(static_cast<double>(st3.sp_h[i]) - *goal_deck);
            if (height_distance > kDeckBand) {
                continue;
            }
            const int64_t cell = st3.sp_cell[i];
            const int64_t x = cell % nx;
            const int64_t y = cell / nx;
            const int64_t distance = (x - gc.x) * (x - gc.x) + (y - gc.y) * (y - gc.y);
            if (!have || distance < best_distance || (distance == best_distance && height_distance < best_height)) {
                have = true;
                best_distance = distance;
                best_height = height_distance;
                best_cell = { x, y };
            }
        }
        if (!have) {
            return { std::nullopt, 0.0 };
        }
        return { best_cell, std::sqrt(static_cast<double>(best_distance)) * kCS };
    };

    auto [as_, dsa] = near(cw3, sc);
    auto [ag_, dga] = nearGoal(useW, cw3);
    if (!as_.has_value()) {
        dg.err = "walk 掩膜为空";
        return std::nullopt;
    }
    if (!ag_.has_value()) {
        dg.err = goal_deck.has_value() ? "目标附近没有未封堵的声明面" : "walk 掩膜为空";
        return std::nullopt;
    }

    const std::unordered_set<int64_t>* faces = &info.sev.steps;
    const std::unordered_set<int64_t>* soft = nullptr;
    const double* soft_penalty = nullptr;
    Mask on3 = cw3;
    // 可走集已经限死在终点那张面所属的类里, 起点这侧只剩层内挑高度
    const auto search = [&](const std::vector<uint8_t>& use,
                            const Mask& c3,
                            const std::vector<int64_t>& svs,
                            const std::vector<int64_t>& gs) -> std::optional<std::vector<int64_t>> {
        const int64_t sd = atSeedLayer(svs);
        if (sd < 0) {
            return std::nullopt;
        }
        return SpanAstar(st3, use, info.cidx, c3, sd, gs, mult, soft, soft_penalty, faces);
    };
    const auto findGap = [&](const std::vector<uint8_t>& use, const Mask& cells, const CellPt& start_cell, const CellPt& goal_cell) {
        const int64_t start_span = atSeedLayer(pick(start_cell, use));
        const std::vector<int64_t> goal_spans = goalsOf(pick(goal_cell, use));
        if (start_span < 0 || goal_spans.empty()) {
            return std::optional<ReachGap> {};
        }
        const std::vector<uint8_t> start_reach = topologyReach(st3, use, info.cidx, cells, { start_span }, faces, false);
        const std::vector<uint8_t> goal_reach = topologyReach(st3, use, info.cidx, cells, goal_spans, faces, true);
        return closestReachGap(st3, start_reach, goal_reach, nx, ny, x0, y0);
    };

    const auto astar_started_at = std::chrono::steady_clock::now();
    const std::vector<int64_t> walk_goals = goalsOf(pick(*ag_, useW));
    std::optional<std::vector<int64_t>> qs = search(useW, cw3, pick(*as_, useW), walk_goals);
    std::optional<ReachGap> failed_gap;
    if (!qs.has_value()) {
        const auto [ac_, dc_] = near(cc3, sc);
        const auto [ag2, dg2] = nearGoal(useC, cc3);
        if (ac_.has_value() && ag2.has_value()) {
            const std::vector<int64_t> core_goals = goalsOf(pick(*ag2, useC));
            qs = search(useC, cc3, pick(*ac_, useC), core_goals);
            if (qs.has_value()) {
                on3 = cc3;
                as_ = ac_;
                dsa = dc_;
                ag_ = ag2;
                dga = dg2;
                dg.warn.push_back("walk 断开→退回 core");
            }
            else if (capture_gap) {
                failed_gap = findGap(useC, cc3, *ac_, *ag2);
            }
        }
    }
    if (!qs.has_value() && !failed_gap.has_value() && capture_gap) {
        failed_gap = findGap(useW, cw3, *as_, *ag_);
    }
    std::optional<std::vector<CellPt>> q;
    dg.timing.astar_ms = ElapsedMs(astar_started_at);
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
            char buf[32];
            if (failed_gap.has_value()) {
                dg.gap_start = failed_gap->start;
                dg.gap_goal = failed_gap->goal;
                dg.gap_distance = failed_gap->distance;
                std::snprintf(buf, sizeof(buf), "%.1f", failed_gap->distance);
                dg.err = "目标面与起点不连通 (最近断口 " + std::string(buf) + "px)";
            }
            else {
                std::snprintf(buf, sizeof(buf), "%.2f", *goal_deck);
                dg.err = "目标面不可达 (声明 " + std::string(buf) + ")";
            }
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
            if (failed_gap.has_value()) {
                dg.gap_start = failed_gap->start;
                dg.gap_goal = failed_gap->goal;
                dg.gap_distance = failed_gap->distance;
            }
            dg.err = "不连通";
            return std::nullopt;
        }
        dg.warn.push_back("层不连通→退回格级");
    }
    dg.x0 = x0;
    dg.y0 = y0;
    dg.nx = nx;
    dg.ny = ny;
    dg.astar_cells.reserve(q->size());
    dg.astar_heights.reserve(q->size());
    for (size_t i = 0; i < q->size(); ++i) {
        const CellPt& c = (*q)[i];
        dg.astar_cells.push_back({ x0 + (static_cast<double>(c.x) + 0.5) * kCS, y0 + (static_cast<double>(c.y) + 0.5) * kCS });
        if (qs.has_value()) {
            dg.astar_heights.push_back(static_cast<double>(st3.sp_h[static_cast<size_t>((*qs)[i])]));
        }
        else {
            dg.astar_heights.push_back(static_cast<double>(info.lh.at(c.y, c.x)));
        }
    }
    dg.snap_start = dsa;
    dg.snap_goal = dga;
    const int64_t NC = nx * ny;
    std::vector<size_t> bad;
    for (size_t k = 1; k < q->size(); ++k) {
        const int64_t ca = (*q)[k - 1].y * nx + (*q)[k - 1].x;
        const int64_t cb = (*q)[k].y * nx + (*q)[k].x;
        if (blocked_steps.contains(ca * NC + cb)) {
            bad.push_back(k);
        }
    }
    if (!bad.empty()) {
        dg.warn.push_back("不可避立面 " + std::to_string(bad.size()) + " 步");
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

    double rerouted_ms = 0.0;
    double string_pull_ms = 0.0;
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
                const auto rerouted_started_at = std::chrono::steady_clock::now();
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
                        for (const auto& p : pp) {
                            if (dg.rerouted_points.empty()
                                || std::hypot(p.x - dg.rerouted_points.back().x, p.y - dg.rerouted_points.back().y) > 1e-9) {
                                dg.rerouted_points.push_back(p);
                            }
                        }
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
                rerouted_ms += ElapsedMs(rerouted_started_at);
            }
            const auto string_pull_started_at = std::chrono::steady_clock::now();
            pp = StringPull(
                pp,
                blk_green.has_value() ? *blk_green : blk_gray,
                &cfl,
                hs.empty() ? nullptr : &lyo,
                hs.empty() ? nullptr : &hs);
            string_pull_ms += ElapsedMs(string_pull_started_at);
        }
        if (!taut.empty() && !pp.empty() && std::hypot(pp.front().x - taut.back().x, pp.front().y - taut.back().y) < 1e-9) {
            pp.erase(pp.begin());
        }
        taut.insert(taut.end(), pp.begin(), pp.end());
    }
    dg.timing.rerouted_ms = rerouted_ms;
    dg.string_pull_points = taut;
    dg.timing.string_pull_ms = string_pull_ms;

    const auto assembled_started_at = std::chrono::steady_clock::now();
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
    dg.assembled_points = ded;
    dg.timing.assembled_ms = ElapsedMs(assembled_started_at);
    const auto loop_fixed_started_at = std::chrono::steady_clock::now();
    std::vector<WorldPoint> out = DropLoops(ded);
    dg.loop_fixed_points = out;
    dg.timing.loop_fixed_ms = ElapsedMs(loop_fixed_started_at);
    const LayerOracle* lyo_p = qs.has_value() ? &lyo : nullptr;
    const float lyo_h = qs.has_value() ? st3.sp_h[static_cast<size_t>(qs->front())] : 0.0F;
    const auto slim_started_at = std::chrono::steady_clock::now();
    if (out.size() > 2) {
        out = Slim(out, blk_gray, &cfl, lyo_p, lyo_h, true, exact_slim ? out.size() : 0);
    }
    dg.slim_points = out;
    dg.timing.slim_ms = ElapsedMs(slim_started_at);
    const auto widened_started_at = std::chrono::steady_clock::now();
    if (out.size() > 2) {
        out = WidenCorners(out, blk_gray, dist, info.x0, info.y0, kCS, &cfl, lyo_p, lyo_h);
    }
    dg.widened_points = out;
    dg.timing.widened_ms = ElapsedMs(widened_started_at);
    const auto final_started_at = std::chrono::steady_clock::now();
    dg.clearance.reserve(out.size());
    for (const auto& p : out) {
        const int64_t cx = std::min(std::max(static_cast<int64_t>(std::floor((p.x - info.x0) / kCS)), int64_t { 0 }), nx - 1);
        const int64_t cy = std::min(std::max(static_cast<int64_t>(std::floor((p.y - info.y0) / kCS)), int64_t { 0 }), ny - 1);
        dg.clearance.push_back(static_cast<double>(dist.at(cy, cx)));
    }
    // 逐点所在面的高度:从起点那张 span 出发,沿线段链式游走,每步在游走到的候选里取与上一点最近的一张。
    // 起点高度是唯一的外部输入,后面全由它推出来,叠层处不会串到楼下那层。
    if (lyo_p != nullptr && !out.empty()) {
        std::vector<float> cur { lyo_h };
        dg.height.push_back(static_cast<double>(lyo_h));
        for (size_t i = 1; i < out.size(); ++i) {
            const auto nxt = lyo_p->walk({ out[i - 1], out[i] }, cur);
            if (!nxt.has_value() || nxt->empty()) {
                dg.height.clear();
                break;
            }
            cur = *nxt;
            const double ref = dg.height.back();
            double nearest_h = static_cast<double>(cur.front());
            for (const float v : cur) {
                if (std::fabs(static_cast<double>(v) - ref) < std::fabs(nearest_h - ref)) {
                    nearest_h = static_cast<double>(v);
                }
            }
            dg.height.push_back(nearest_h);
        }
    }
    PullWaypoints(out, dg, pl, zid, blk_gray, lyo_p != nullptr);
    dg.planned_points = out;
    dg.timing.final_ms = ElapsedMs(final_started_at);
    return out;
}

} // namespace

RecastNavEngine::RecastNavEngine(const BaseNavPack& pack, const BaseNavPlanner& planner)
    : pack_(pack)
    , planner_(planner)
{
    const BaseNavSection* sec = pack_.section(kGridSectionTag);
    if (sec == nullptr) {
        grid_error_ = "包里没有预烘格图段";
        return;
    }
    if (!grid_.parse(sec->bytes.data(), sec->bytes.size(), grid_error_)) {
        grid_ = GridPack();
    }
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
    const std::function<bool()>& should_stop,
    bool exact_slim)
{
    const std::lock_guard<std::mutex> lock(mutex_);
    return planLocked(zone_name, start, goal, start_floor_y, goal_floor_y, goal_deck_y, blocked, blocked_points, should_stop, exact_slim);
}

void RecastNavEngine::warm(const std::string& zone_name)
{
    const std::lock_guard<std::mutex> lock(mutex_);
    zoneEntry(zone_name);
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
    const std::function<bool()>& should_stop,
    bool exact_slim)
{
    RecastPlanResult res;
    if (!grid_.valid()) {
        res.error = grid_error_;
        return res;
    }
    const GridZoneDir* gz = grid_.findZone(zone_name);
    if (gz == nullptr) {
        res.error = "区没有预烘格图 (" + zone_name + ")";
        return res;
    }
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

    // 端点接不上可走层的腿在全区图上就能判掉。全区核心是任何窗口内核心的超集,
    // 量出来的锚距是窗口里那把尺子的下界,过不了这道闸的腿换多大的窗口也接不上。
    const double zsa = coreAnchorPx(grid_, *gz, start);
    const double zga = coreAnchorPx(grid_, *gz, goal);
    if (zsa > kSnapRadius || zga > kSnapRadius) {
        char buf[128];
        std::snprintf(buf, sizeof(buf), "端点接不上可走层 (起 %.1fpx / 终 %.1fpx, 疑似不连通)", zsa, zga);
        res.error = buf;
        return res;
    }

    // 扩窗只留两档。59 条生产腿里 ×100 与 ×200 零成功,只在必败腿上把时间烧掉。
    constexpr int kWidenSteps = 2;
    const double margins[kWidenSteps] = { kMargin, kMargin * 2 };
    const int pass_count = (blocked.empty() && blocked_points.empty()) ? kWidenSteps : 1;
    std::string last_err;
    const auto storeDebug = [&](RouteDiag& dg) {
        res.debug.x0 = dg.x0;
        res.debug.y0 = dg.y0;
        res.debug.nx = dg.nx;
        res.debug.ny = dg.ny;
        res.debug.cell_size = kCS;
        res.debug.timing = dg.timing;
        res.debug.astar_cells = std::move(dg.astar_cells);
        res.debug.astar_heights = std::move(dg.astar_heights);
        res.debug.rerouted_points = std::move(dg.rerouted_points);
        res.debug.string_pull_points = std::move(dg.string_pull_points);
        res.debug.assembled_points = std::move(dg.assembled_points);
        res.debug.loop_fixed_points = std::move(dg.loop_fixed_points);
        res.debug.slim_points = std::move(dg.slim_points);
        res.debug.widened_points = std::move(dg.widened_points);
        res.debug.planned_points = std::move(dg.planned_points);
        res.debug.gap_start = dg.gap_start;
        res.debug.gap_goal = dg.gap_goal;
        res.debug.gap_distance = dg.gap_distance;
        res.debug.warnings = dg.warn;
    };
    for (int pass = 0; pass < pass_count; ++pass) {
        const bool last_margin = pass + 1 == kWidenSteps;
        const bool capture_gap = pass + 1 == pass_count;
        // 窗口边界对齐到全局网格。原点直接取 min-margin 时, 起点差 0.06px 就换一套体素相位,
        // 同一段路两次规划得到的格子划分不同; 对齐后相位只由世界坐标决定, 与起终点无关。
        const double x0 = std::floor((std::min(start.x, goal.x) - margins[pass]) / kCS) * kCS;
        const double y0 = std::floor((std::min(start.y, goal.y) - margins[pass]) / kCS) * kCS;
        const double x1 = std::ceil((std::max(start.x, goal.x) + margins[pass]) / kCS) * kCS;
        const double y1 = std::ceil((std::max(start.y, goal.y) + margins[pass]) / kCS) * kCS;
        const int64_t nx = static_cast<int64_t>(std::ceil((x1 - x0) / kCS));
        const int64_t ny = static_cast<int64_t>(std::ceil((y1 - y0) / kCS));
        if (nx * ny > kMaxCells) {
            res.error = "窗口过大 (" + std::to_string(nx) + "×" + std::to_string(ny) + " 格)";
            return res;
        }
        if (pass > 0 && should_stop && should_stop()) {
            res.error = "规划已取消";
            return res;
        }
        std::string err;
        auto info = buildWindow(wo, grid_, *gz, zc, start, ss->point, goal, h0, gdk, x0, y0, x1, y1, blocked_local, blocked_points, err);
        if (info.has_value()) {
            RouteDiag dg;
            auto line = routeWindow(*info, start, goal, dg, gdk, capture_gap, planner_, zc.zone_id, exact_slim);
            if (line.has_value()) {
                // 锚点远 = 走廊出窗,同触界扩窗,否则末段盲跳穿墙
                if (std::max(dg.snap_start, dg.snap_goal) > kSnapRadius) {
                    if (last_margin) {
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
                    if (last_margin || (mnx > x0 + pad && mxx < x1 - pad && mny > y0 + pad && mxy < y1 - pad)) {
                        res.ok = true;
                        res.points = *line;
                        for (size_t i = 1; i < line->size(); ++i) {
                            res.length += std::hypot((*line)[i].x - (*line)[i - 1].x, (*line)[i].y - (*line)[i - 1].y);
                        }
                        res.warnings = dg.warn;
                        res.clearance = dg.clearance;
                        res.snap_start = dg.snap_start;
                        res.snap_goal = dg.snap_goal;
                        res.waypoints = std::move(dg.waypoints);
                        storeDebug(dg);
                        return res;
                    }
                    err = "终线触界,扩窗重跑";
                }
            }
            else {
                err = dg.err.empty() ? "路线失败" : dg.err;
                storeDebug(dg);
            }
        }
        last_err = err;
    }
    res.error = last_err.empty() ? "路线失败" : last_err;
    return res;
}

} // namespace navmesh::recast
