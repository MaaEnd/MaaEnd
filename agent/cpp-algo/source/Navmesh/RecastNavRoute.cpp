#include "RecastNavRoute.h"

#include "NavParallel.h"
#include "RecastNavBake.h"

#include <algorithm>
#include <atomic>
#include <bit>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <queue>
#include <tuple>

#include <cstdio>
#include <cstdlib>

namespace navmesh::recast
{

namespace
{

double triHeightOf(const PolyMesh& mesh, int32_t t)
{
    const auto& tri = mesh.T[static_cast<size_t>(t)];
    return (mesh.h(tri[0]) + mesh.h(tri[1]) + mesh.h(tri[2])) / 3.0;
}

// 净空截断的格数(含取整余量), 与 Clearance 的扫描半径同式。小窗档的可信余量要盖过它。
constexpr int64_t kEdtCells = static_cast<int64_t>(kEdtCap / kCS) + 1;

struct WindowInfo
{
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
    // 只留跨过 buildWindow 边界还有读者的表。层高图与挑剩的墙段都是建窗内部量, 留在结构里
    // 就是让两张全窗口的图白白活过整个 routeWindow。
    Mask lay;
    Mask core;
    Grid<float> dist; // 无封堵: 旁包烘好的封缝净空; 有封堵: 按盖过的核心重算
    Mask whit; // 只在有封堵时算, 无封堵的腿不需要它
    Mask medial; // 旁包烘好的中轴, 有封堵时不可信、留空
    EdgeBits step_edges; // 旁包烘好的台阶税边, 与封堵无关
    bool blocked = false;
    StepBarrier sev;
    std::vector<WorldPoint> segA;
    std::vector<WorldPoint> segB;
    double h0 = 0.0;
    SpanTable st3;
    std::vector<uint8_t> vis3;
    std::vector<uint8_t> reach3;
};

struct RouteDiag
{
    std::string err;
    std::vector<std::string> warn;
    std::vector<double> clearance;
    std::vector<double> height; // 逐点所在面的高度; 层预言机走不通时清空
    std::vector<size_t> waypoints;
    bool crossed_barrier = false;
    bool hop_barrier = false; // 端点接线的那一跳跨了禁行边
    double snap_start = 0.0;
    double snap_goal = 0.0;

    // 小窗档的验收。margin > 0 时窗口只是端点附近的一块, 凡是结果可能被窗口边切过的判据
    // (搜索碰边、吸附受可达域限制、任何退档分支) 都置 escalate 立即返回, 由调用方换大窗重算。
    // margin = 0 是整类窗口, 所有验收关掉, 逐字走原路。
    int64_t margin = 0;
    // 封顶档: 没有更大的小窗可升, 只与窗口大小有关的验收(碰边类)关掉, 结果照采
    bool final = false;
    bool escalate = false;
    std::string escalate_why;

    // 诊断埋点。只读各阶段的出口, 不参与任何判据, 摘掉它们路线逐点不变。
    RecastPlanResult::Debug::Timing timing;
    std::vector<WorldPoint> topology_cells;
    std::vector<double> topology_heights;
    std::vector<WorldPoint> taut_points;
    std::vector<WorldPoint> pulled_points;
    std::vector<WorldPoint> assembled_points;
    std::optional<WorldPoint> gap_start;
    std::optional<WorldPoint> gap_goal;
    std::optional<double> gap_distance;
};

// 单调钟读数, 单位毫秒
double nowMs()
{
    return std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now().time_since_epoch()).count();
}

// 窗口里从预烘图解出来的记录。格号已换成窗口格号,窗外的与不属于本瓦自有矩形的
// 都已剔除;一格只归一块瓦,所以同格的记录必然来自同一块瓦、按 (类号, 高) 排好。
struct GridWindow
{
    std::vector<GridSpanRec> rec;
    std::vector<int32_t> head; // 逐格: 记录链表头,无记录为 -1
    std::vector<int32_t> next; // 逐记录: 同格的下一条
    // 旁包的列, 与 rec 逐条对位; 不带旁包解的窗口留空。建窗那一刻记录表是内存峰, 所以能并
    // 就并: 封缝净空直接写回 rec.clr (窗口里没人再读原值), 中轴位放在 seg 的 bit7。
    std::vector<uint32_t> scc;
    std::vector<uint8_t> steps2;
    std::vector<uint8_t> seg; // bit0/1 = 立面段, bit7 = 中轴
    std::vector<uint8_t> tax;
};
constexpr uint8_t kGwMedialBit = 0x80U;

// fp/fz 给了就连旁包的六列一起解; 定类那两小块不需要它们, 传空。
bool loadGridWindow(
    const GridPack& gp,
    const GridZoneDir& gz,
    const FieldsPack* fp,
    const FieldsZoneDir* fz,
    int64_t wgx0,
    int64_t wgy0,
    int64_t nx,
    int64_t ny,
    GridWindow& out)
{
    const std::vector<const GridTileRef*> tiles = GridTilesInRect(gz, wgx0, wgy0, wgx0 + nx - 1, wgy0 + ny - 1);
    // 先按瓦目录里的记录数开够。窗外与非自有矩形的记录会被滤掉, 所以这是个上界。
    // 解瓦是纯读: 每块领一段瓦, 按同一个上界给它划一段互不相交的写区直写, 收工按块序压紧。
    // 写区起点与压紧次序都只由瓦下标定, 于是 rec 的次序与线程数无关, 表也始终只有一份。
    const bool with_fields = fp != nullptr && fz != nullptr;
    const auto nt = static_cast<int64_t>(tiles.size());
    const size_t nw = NavWorkerCountForBlocks(nt);
    size_t cap = 0;
    for (const GridTileRef* t : tiles) {
        cap += t->records;
    }
    out.rec.assign(cap, GridSpanRec {});
    if (with_fields) {
        out.scc.assign(cap, 0);
        out.steps2.assign(cap, 0);
        out.seg.assign(cap, 0);
        out.tax.assign(cap, 0);
    }
    std::vector<size_t> beg(nw, 0);
    std::vector<size_t> cnt(nw, 0);
    std::atomic<bool> ok { true };
    ParallelChunks(nt, nw, [&](size_t w, int64_t lo, int64_t hi) {
        size_t at = 0;
        for (int64_t i = 0; i < lo; ++i) {
            at += tiles[static_cast<size_t>(i)]->records;
        }
        beg[w] = at;
        GridTile tile;
        FieldsTile ft;
        for (int64_t i = lo; i < hi; ++i) {
            const GridTileRef* t = tiles[static_cast<size_t>(i)];
            if (t->records == 0) {
                continue;
            }
            if (!gp.decodeTile(*t, tile)) {
                ok.store(false);
                return;
            }
            if (with_fields) {
                // 旁包的瓦表与格图同区同序, 记录按解出的次序逐条对位
                const auto ti = static_cast<size_t>(t - gz.tiles.data());
                if (ti >= fz->tiles.size() || !fp->decodeTile(fz->tiles[ti], ft) || ft.scc.size() != tile.rec.size()) {
                    ok.store(false);
                    return;
                }
            }
            for (size_t k = 0; k < tile.rec.size(); ++k) {
                GridSpanRec& r = tile.rec[k];
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
                r.cell = static_cast<int32_t>(wy * nx + wx);
                if (with_fields) {
                    // 封缝只会让净空变小, 差值超过主包净空就是旁包烘错了, 不猜
                    if (ft.clr2d[k] > r.clr) {
                        ok.store(false);
                        return;
                    }
                    out.scc[at] = ft.scc[k];
                    out.steps2[at] = static_cast<uint8_t>(ft.steps2x[k] ^ r.steps);
                    out.seg[at] = static_cast<uint8_t>(ft.seg[k] | ((ft.medial[k] & 0x01U) != 0 ? kGwMedialBit : 0U));
                    out.tax[at] = ft.tax[k];
                    r.clr = static_cast<uint16_t>(r.clr - ft.clr2d[k]);
                }
                out.rec[at++] = r;
            }
        }
        cnt[w] = at - beg[w];
    });
    if (!ok.load()) {
        out.rec.clear();
        return false;
    }
    size_t kept = cnt[0];
    for (size_t w = 1; w < nw; ++w) {
        if (cnt[w] != 0 && beg[w] != kept) {
            const auto b = static_cast<int64_t>(beg[w]);
            const auto e = static_cast<int64_t>(beg[w] + cnt[w]);
            const auto k = static_cast<int64_t>(kept);
            std::move(out.rec.begin() + b, out.rec.begin() + e, out.rec.begin() + k);
            if (with_fields) {
                std::move(out.scc.begin() + b, out.scc.begin() + e, out.scc.begin() + k);
                std::move(out.steps2.begin() + b, out.steps2.begin() + e, out.steps2.begin() + k);
                std::move(out.seg.begin() + b, out.seg.begin() + e, out.seg.begin() + k);
                std::move(out.tax.begin() + b, out.tax.begin() + e, out.tax.begin() + k);
            }
        }
        kept += cnt[w];
    }
    out.rec.resize(kept);
    if (with_fields) {
        out.scc.resize(kept);
        out.steps2.resize(kept);
        out.seg.resize(kept);
        out.tax.resize(kept);
    }
    out.head.assign(static_cast<size_t>(nx * ny), -1);
    out.next.assign(out.rec.size(), -1);
    for (size_t i = out.rec.size(); i-- > 0;) {
        const auto c = static_cast<size_t>(out.rec[i].cell);
        out.next[i] = out.head[c];
        out.head[c] = static_cast<int32_t>(i);
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

// 点到最近核心格的格距 × kCS,与窗口里的 nearestCell() 同口径,只是在全区图上量。
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

// 一块解开的格图连同它的原点与尺寸。原点落在全局格线上, 所以窗口格号与烘焙格号一一对上。
struct GridPatch
{
    GridWindow gw;
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
};

// 定这条腿走哪一类。起点那一格定类; 终点声明了面时改由终点定, 免得起点二维吸附落在屋顶上
// 把线拉到别层去。只读起点格与终点吸附半径内的格, 所以在覆盖了这两处的任何一块格图上定,
// 结果都一样 —— 先在小块上定类再按类开图, 与直接在整区图上定类逐位相同。
bool pickRegion(
    const GridPatch& ps,
    const GridPatch& pg,
    const WorldPoint& s,
    const WorldPoint& s_snap,
    const WorldPoint& g,
    double h0,
    std::optional<double> goal_deck,
    uint32_t& region,
    int64_t& start_cell,
    std::string& err)
{
    const auto cell_at = [](const GridPatch& p, int64_t cx, int64_t cy) {
        return cx < 0 || cx >= p.nx || cy < 0 || cy >= p.ny ? -1 : cy * p.nx + cx;
    };
    int64_t gx = static_cast<int64_t>((s.x - ps.x0) / kCS);
    int64_t gy = static_cast<int64_t>((s.y - ps.y0) / kCS);
    int64_t cell0 = cell_at(ps, gx, gy);
    int64_t start_rec = cell0 >= 0 ? pickStartRec(ps.gw, cell0, h0) : -1;
    if (start_rec < 0) {
        // 起点离网时其所在格无体素, 退用按楼层吸附过的起点定种子
        gx = static_cast<int64_t>((s_snap.x - ps.x0) / kCS);
        gy = static_cast<int64_t>((s_snap.y - ps.y0) / kCS);
        cell0 = cell_at(ps, gx, gy);
        start_rec = cell0 >= 0 ? pickStartRec(ps.gw, cell0, h0) : -1;
    }
    if (start_rec < 0) {
        err = "起点格无体素 (gx=" + std::to_string(gx) + ",gy=" + std::to_string(gy) + ")";
        return false;
    }
    region = ps.gw.rec[static_cast<size_t>(start_rec)].rid;
    start_cell = cell0;
    if (goal_deck.has_value()) {
        const int64_t deck_rec = pickDeckRec(
            pg.gw,
            pg.nx,
            pg.ny,
            static_cast<int64_t>((g.x - pg.x0) / kCS),
            static_cast<int64_t>((g.y - pg.y0) / kCS),
            *goal_deck);
        if (deck_rec < 0) {
            err = "终点附近没有声明的面 (deck=" + std::to_string(*goal_deck) + ")";
            return false;
        }
        region = pg.gw.rec[static_cast<size_t>(deck_rec)].rid;
    }
    return true;
}

// 一个类占的格范围。类是可走面的连通片, 类外的格进不了规划图, 所以按类开图与铺满整区等价。
ZoneBoundsPx regionBounds(const GridPack& gp, const GridZoneDir& gz, uint32_t region)
{
    ZoneBoundsPx b;
    b.x0 = std::numeric_limits<int64_t>::max();
    b.y0 = std::numeric_limits<int64_t>::max();
    b.x1 = std::numeric_limits<int64_t>::min();
    b.y1 = std::numeric_limits<int64_t>::min();
    // 先看每块瓦的类号字典。一个类只落在少数几块瓦里, 其余的整块跳过, 不用解开。
    // 挑完再并行解剩下的, 静态分块才不会有人整块领到空活。
    std::vector<const GridTileRef*> hits;
    {
        const auto ntl = static_cast<int64_t>(gz.tiles.size());
        // 每块只写自己那几个下标位, 收工再按下标序把中标的挑出来。
        std::vector<uint8_t> hit(static_cast<size_t>(ntl), 0);
        ParallelChunks(ntl, NavWorkerCountForBlocks(ntl), [&](size_t, int64_t lo, int64_t hi) {
            std::vector<uint32_t> rids;
            for (int64_t i = lo; i < hi; ++i) {
                const GridTileRef& t = gz.tiles[static_cast<size_t>(i)];
                if (t.records == 0) {
                    continue;
                }
                if (gp.tileRegions(t, rids) && std::find(rids.begin(), rids.end(), region) != rids.end()) {
                    hit[static_cast<size_t>(i)] = 1;
                }
            }
        });
        for (int64_t i = 0; i < ntl; ++i) {
            if (hit[static_cast<size_t>(i)] != 0) {
                hits.push_back(&gz.tiles[static_cast<size_t>(i)]);
            }
        }
    }
    // 每块自己收一个包围盒, 收工再取并。取极值与次序无关, 于是与线程数无关。
    const auto nh = static_cast<int64_t>(hits.size());
    const size_t nw = NavWorkerCountForBlocks(nh);
    std::vector<ZoneBoundsPx> part(nw, b);
    ParallelChunks(nh, nw, [&](size_t w, int64_t lo, int64_t hi) {
        ZoneBoundsPx& p = part[w];
        GridTile tile;
        for (int64_t i = lo; i < hi; ++i) {
            const GridTileRef& t = *hits[static_cast<size_t>(i)];
            if (!gp.decodeTile(t, tile)) {
                continue;
            }
            for (const GridSpanRec& r : tile.rec) {
                if (r.rid != region) {
                    continue;
                }
                const int64_t ix = r.cell % t.nx;
                const int64_t iy = r.cell / t.nx;
                if (ix < t.px0 || ix > t.px1 || iy < t.py0 || iy > t.py1) {
                    continue;
                }
                p.x0 = std::min<int64_t>(p.x0, t.gx0 + ix);
                p.y0 = std::min<int64_t>(p.y0, t.gy0 + iy);
                p.x1 = std::max<int64_t>(p.x1, t.gx0 + ix);
                p.y1 = std::max<int64_t>(p.y1, t.gy0 + iy);
            }
        }
    });
    for (const ZoneBoundsPx& p : part) {
        b.x0 = std::min(b.x0, p.x0);
        b.y0 = std::min(b.y0, p.y0);
        b.x1 = std::max(b.x1, p.x1);
        b.y1 = std::max(b.y1, p.y1);
    }
    return b;
}

// 起点格里离 h0 最近的本类面, 它是 span 可达域的种子。
int64_t seedSpan(const SpanTable& st, const std::vector<uint8_t>& vis, int64_t cell, double h0)
{
    const int64_t sj = st.j(cell);
    if (sj < 0) {
        return -1;
    }
    int64_t seed = -1;
    float best = 0.0F;
    const int64_t b = st.cstart(sj);
    for (int64_t k = 0, kn = st.ccnt(sj); k < kn; ++k) {
        const int64_t sid = b + k;
        if (vis[static_cast<size_t>(sid)] == 0) {
            continue;
        }
        const float d = std::fabs(st.sp_h[static_cast<size_t>(sid)] - static_cast<float>(h0));
        if (seed < 0 || d < best) {
            seed = sid;
            best = d;
        }
    }
    return seed;
}

// 建窗。可达域、禁步重判、挑墙三样都是整类量, 全从旁包读: 小窗与整类窗口在这三样上逐位相同,
// 窗口大小只影响场(净空、通道)与搜索范围, 那是小窗验收管的事。
std::optional<WindowInfo> buildWindow(
    const GridPack& gp,
    const GridZoneDir& gz,
    const FieldsPack& fp,
    const FieldsZoneDir& fzd,
    const FieldsZone& fz,
    ZoneClean& zc,
    const WorldPoint& s,
    const WorldPoint& s_snap,
    const WorldPoint& g,
    double h0,
    uint32_t region,
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

    // 区网格只有取墙与盖封堵面两个读者, 两者都只认窗口矩形, 与格图无关。
    BakedWalls walls = BakeWalls(zc, x0, y0, nx, ny);
    RasterCells brc;
    if (!blocked_local.empty()) {
        std::vector<std::array<int32_t, 3>> bt;
        bt.reserve(blocked_local.size());
        for (const int32_t t : blocked_local) {
            bt.push_back(zc.mesh.T[static_cast<size_t>(t)]);
        }
        brc = Rasterize(zc.mesh.verts(), bt, x0, y0, nx, ny);
    }
    GridPatch pw;
    pw.x0 = x0;
    pw.y0 = y0;
    pw.nx = nx;
    pw.ny = ny;
    GridWindow& gw = pw.gw;
    if (!loadGridWindow(gp, gz, &fp, &fzd, wgx0, wgy0, nx, ny, gw)) {
        err = "预烘格图或旁包解不开";
        return std::nullopt;
    }

    // 类由调用方定好传进来。这里只要起点格 —— 它是 span 可达域的种子, 与走哪一类无关,
    // 所以定类的那一路参数传空。
    uint32_t seed_region = 0;
    int64_t cell0 = -1;
    if (!pickRegion(pw, pw, s, s_snap, g, h0, std::nullopt, seed_region, cell0, err)) {
        return std::nullopt;
    }
    // 逐格链表只服务于起点格查询, 到这里就没有读者了; 下面是顺着记录表走一遍。
    gw.head = std::vector<int32_t>();
    gw.next = std::vector<int32_t>();

    WindowInfo info;
    info.x0 = x0;
    info.y0 = y0;
    info.nx = nx;
    info.ny = ny;
    info.lay = Mask(nx, ny, 0);
    info.core = Mask(nx, ny, 0);
    info.dist = Grid<float>(nx, ny, 0.0F);
    info.blocked = !blocked_local.empty() || !blocked_points.empty();
    // 中轴是从封缝净空推出来的窗口量, 封堵会改净空, 所以只有无封堵的腿才采烘好的
    if (!info.blocked) {
        info.medial = Mask(nx, ny, 0);
    }
    info.step_edges.resize(nx, ny);
    Grid<float> lh(nx, ny, std::numeric_limits<float>::quiet_NaN());
    std::vector<uint8_t> stepbits(static_cast<size_t>(nx * ny), 0);
    std::vector<uint8_t> stepbits2(static_cast<size_t>(nx * ny), 0);
    std::vector<uint8_t> segbits(static_cast<size_t>(nx * ny), 0);
    // 记录数就是 span 表的上界。让它自己长的话, 扩容那一刻新旧两份缓冲同时活着,
    // 而这一刻正是建窗内存最高的时候。
    std::vector<int32_t> sp_cell;
    std::vector<float> sp_h;
    std::vector<uint32_t> sp_scc;
    sp_cell.reserve(gw.rec.size());
    sp_h.reserve(gw.rec.size());
    sp_scc.reserve(gw.rec.size());
    info.vis3.reserve(gw.rec.size());
    // 表里留着别的类的 span:层判据要看整列,少一层就会从楼板底下穿过去
    for (size_t ri = 0; ri < gw.rec.size(); ++ri) {
        const GridSpanRec& r = gw.rec[ri];
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
            // r.clr 已在解瓦时换成封缝净空; 按记录序后写覆盖, 与老路 min(烘净空, 接缝净空) 的取值次序一致
            info.dist.v[cell] = GridClearance(r.clr);
            stepbits[cell] |= r.steps;
            stepbits2[cell] |= gw.steps2[ri];
            segbits[cell] |= static_cast<uint8_t>(gw.seg[ri] & ~kGwMedialBit);
            if (!info.blocked && (gw.seg[ri] & kGwMedialBit) != 0) {
                info.medial.v[cell] = 1;
            }
            // 台阶税边: 方向 i 正向在 bit 2i, 反向在 bit 2i+1。EdgeBits 的反向位记在对端格上,
            // 所以不能整字节搬, 逐位 set; 窗边格的记录可能带出窗的边, 越界的丢
            if (const uint8_t tb = gw.tax[ri]; tb != 0) {
                const int64_t cx = static_cast<int64_t>(cell) % nx;
                const int64_t cy = static_cast<int64_t>(cell) / nx;
                for (int i = 0; i < 4; ++i) {
                    for (const int64_t sg : { int64_t { 1 }, int64_t { -1 } }) {
                        if (((tb >> (2 * i + (sg < 0 ? 1 : 0))) & 0x01U) == 0) {
                            continue;
                        }
                        const int64_t bx = cx + sg * kGridStepDx[i];
                        const int64_t by = cy + sg * kGridStepDy[i];
                        if (bx < 0 || by < 0 || bx >= nx || by >= ny) {
                            continue;
                        }
                        info.step_edges.set(static_cast<int64_t>(cell), by * nx + bx);
                    }
                }
            }
            if (!ghost && !fill && (std::isnan(lh.v[cell]) || r.h > lh.v[cell])) {
                lh.v[cell] = r.h;
            }
        }
        if (fill || (ghost && r.rid != region)) {
            continue;
        }
        sp_cell.push_back(static_cast<int32_t>(r.cell));
        sp_h.push_back(r.h);
        sp_scc.push_back(gw.scc[ri]);
        info.vis3.push_back(static_cast<uint8_t>(r.rid == region));
    }
    // 记录表到此已经摊进上面这几张图, 后面再没人读它。建 span 表是建窗最耗内存的一步,
    // 这份表是其中最大的一块, 不该一直占到那时。
    pw.gw = GridWindow();

    // 挑墙: 这条边在本类的层上留不留, 是按整区采样烘好的整类量, 直接查表。表里没有的边
    // 说明旁包与区网格对不上, 不猜。
    std::vector<WorldPoint> wP0;
    std::vector<WorldPoint> wP1;
    for (size_t i = 0; i < walls.p0.size(); ++i) {
        bool known = false;
        const bool keep = fz.wallKeep(walls.tri[i], walls.k[i], region, known);
        if (!known) {
            err = "旁包留墙表没有这条边 (tri " + std::to_string(walls.tri[i]) + ")";
            return std::nullopt;
        }
        if (keep) {
            wP0.push_back(walls.p0[i]);
            wP1.push_back(walls.p1[i]);
        }
    }
    walls = BakedWalls();
    // 挡线格图只喂接缝净空那一步, 而无封堵时净空直接采旁包, 不必再算
    if (info.blocked) {
        info.whit = WallHits(wP0, wP1, x0, y0, nx, ny);
    }

    for (size_t ci = 0; ci < brc.cell.size(); ++ci) {
        const auto cell = static_cast<size_t>(brc.cell[ci]);
        const float lf = lh.v[cell];
        // 层高带内才盖掉,免得误伤其他楼层的格
        if (!std::isnan(lf) && std::fabs(brc.h[ci] - lf) <= static_cast<float>(kClimb)) {
            info.core.v[cell] = 0;
            info.lay.v[cell] = 0;
        }
    }
    brc = RasterCells();
    lh = Grid<float>();

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

    const int64_t nc = nx * ny;
    info.h0 = h0;
    info.sev.steps.resize(nx, ny);
    info.st3 = PackSpans(std::move(sp_cell), std::move(sp_h), &info.vis3, &sp_scc);

    // 禁步面按烘出来的位还原。位序与方向表是写入方定的,方向倒序的那一位对应反向键;
    // 只有正交两向出线段,对角步不挡视线。封堵盖掉的格不再出面,与它被移出可走层一致。
    // 主包的位是按老口径烘的门, 旁包的重判位与立面段位是整区口径算好的答案: 门开着才读,
    // 读到什么就是什么, 这里不再看 span。
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
            const uint8_t bits2 = static_cast<uint8_t>(stepbits2[static_cast<size_t>(c)] >> (2 * i)) & 0x03U;
            if ((bits2 & 0x01U) != 0) {
                info.sev.steps.set(c, b);
            }
            if ((bits2 & 0x02U) != 0) {
                info.sev.steps.set(b, c);
            }
            if (i >= 2 || ((segbits[static_cast<size_t>(c)] >> i) & 0x01U) == 0) {
                continue;
            }
            const double px = x0 + static_cast<double>(c % nx + dx) * kCS;
            const double py = y0 + static_cast<double>(c / nx + dy) * kCS;
            info.sev.p0.push_back({ px, py });
            info.sev.p1.push_back({ px + static_cast<double>(dy) * kCS, py + static_cast<double>(dx) * kCS });
        }
    }
    stepbits = std::vector<uint8_t>();
    stepbits2 = std::vector<uint8_t>();
    segbits = std::vector<uint8_t>();
    const int64_t seed3 = seedSpan(info.st3, info.vis3, cell0, h0);
    if (seed3 < 0) {
        err = "起点格没有与终点同类的面";
        return std::nullopt;
    }
    // 可达域: 起点面所在分量在类的分量图上能到的分量集, 与整类洪水逐位相同, 窗口切不到它。
    {
        const std::vector<uint8_t> hit = fz.reachFrom(region, sp_scc[static_cast<size_t>(seed3)]);
        if (hit.empty()) {
            err = "旁包分量图里没有起点面所在的分量";
            return std::nullopt;
        }
        info.reach3.assign(info.vis3.size(), 0);
        for (size_t sid = 0; sid < info.vis3.size(); ++sid) {
            if (info.vis3[sid] == 0) {
                continue;
            }
            const uint32_t comp = sp_scc[sid];
            if (comp == 0 || comp >= hit.size()) {
                err = "旁包分量号越界";
                return std::nullopt;
            }
            info.reach3[sid] = hit[comp];
        }
    }

    // 段表就此定型。挑剩的墙段与立面禁步段都整份进了 segA/segB, 源表留着只是同一批点的第二份。
    info.segA = std::move(wP0);
    info.segA.insert(info.segA.end(), info.sev.p0.begin(), info.sev.p0.end());
    info.segB = std::move(wP1);
    info.segB.insert(info.segB.end(), info.sev.p1.begin(), info.sev.p1.end());
    info.sev.p0 = {};
    info.sev.p1 = {};
    // 烘出来的净空是没封堵时的;盖掉格子会让通道变窄,代价场得按盖过的核心重算
    if (info.blocked) {
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

// 弦沿线的最小净空。取样与层走查同一套整数插值, 于是"弦经过哪些格"在两处判据里是同一个答案。
double SegMinClr(const Grid<float>& d, double x0, double y0, const WorldPoint& a, const WorldPoint& b)
{
    const int64_t ax = static_cast<int64_t>((a.x - x0) / kCS);
    const int64_t ay = static_cast<int64_t>((a.y - y0) / kCS);
    const int64_t bx = static_cast<int64_t>((b.x - x0) / kCS);
    const int64_t by = static_cast<int64_t>((b.y - y0) / kCS);
    const int64_t n = std::max<int64_t>(std::max(std::abs(bx - ax), std::abs(by - ay)), 1);
    double m = std::numeric_limits<double>::infinity();
    for (int64_t k = 0; k <= n; ++k) {
        const int64_t cx = ax + static_cast<int64_t>(std::nearbyint(static_cast<double>(bx - ax) * static_cast<double>(k) / static_cast<double>(n)));
        const int64_t cy = ay + static_cast<int64_t>(std::nearbyint(static_cast<double>(by - ay) * static_cast<double>(k) / static_cast<double>(n)));
        if (cx < 0 || cy < 0 || cx >= d.nx || cy >= d.ny) {
            continue;
        }
        m = std::min(m, static_cast<double>(d.at(cy, cx)));
    }
    return m;
}

bool SegCross(const WorldPoint& p, const WorldPoint& q, const WorldPoint& r, const WorldPoint& s)
{
    const auto side = [](const WorldPoint& a, const WorldPoint& b, const WorldPoint& c) {
        return (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x);
    };
    return (side(r, s, p) > 0.0) != (side(r, s, q) > 0.0) && (side(p, q, r) > 0.0) != (side(p, q, s) > 0.0);
}

// 拐角抬升:把拐点朝净空更高的一侧挪不超过 lift, 两条新弦过与搜索同一道视线判据, 并要求
// 两段的段内最小净空都不低于原值 —— 穿缝必然让段内极小值下探, 这一条接住了通道掩膜那层保护。
// 净空严格变大才收, 落在净空极大线上的拐点因此一格不动: 那种贴墙是通道本身的宽度决定的。
void LiftCorners(
    std::vector<WorldPoint>& P,
    const Grid<float>& d,
    double x0,
    double y0,
    const Visibility& vis,
    const LayerOracle& lyo,
    float h0,
    double lift)
{
    const int64_t rad = static_cast<int64_t>(std::lround(lift / kCS));
    if (P.size() < 3 || rad <= 0) {
        return;
    }
    const auto nearest = [](const std::vector<float>& hs, float ref) {
        float best = hs.front();
        for (const float v : hs) {
            if (std::fabs(v - ref) < std::fabs(best - ref)) {
                best = v;
            }
        }
        return best;
    };
    float ha = h0;
    for (size_t i = 1; i + 1 < P.size(); ++i) {
        const auto hb = lyo.walk({ P[i - 1], P[i] }, ha);
        if (!hb.has_value() || hb->empty()) {
            return;
        }
        const float hv0 = nearest(*hb, ha);
        const int64_t cx0 = static_cast<int64_t>((P[i].x - x0) / kCS);
        const int64_t cy0 = static_cast<int64_t>((P[i].y - y0) / kCS);
        if (cx0 < 0 || cy0 < 0 || cx0 >= d.nx || cy0 >= d.ny) {
            ha = hv0;
            continue;
        }
        const double here = static_cast<double>(d.at(cy0, cx0));
        const double keep = std::min(
            SegMinClr(d, x0, y0, P[i - 1], P[i]),
            SegMinClr(d, x0, y0, P[i], P[i + 1]));
        // 候选按净空降序, 同净空按线性格序破平 —— 顺序全序, 出线因此逐位可复现
        std::vector<std::pair<double, int64_t>> cand;
        for (int64_t dy = -rad; dy <= rad; ++dy) {
            for (int64_t dx = -rad; dx <= rad; ++dx) {
                if (dx * dx + dy * dy > rad * rad || (dx == 0 && dy == 0)) {
                    continue;
                }
                const int64_t cx = cx0 + dx;
                const int64_t cy = cy0 + dy;
                if (cx < 0 || cy < 0 || cx >= d.nx || cy >= d.ny) {
                    continue;
                }
                if (static_cast<double>(d.at(cy, cx)) > here) {
                    cand.emplace_back(-static_cast<double>(d.at(cy, cx)), cy * d.nx + cx);
                }
            }
        }
        std::sort(cand.begin(), cand.end());
        float hnext = hv0;
        for (const auto& c : cand) {
            const WorldPoint w {
                x0 + (static_cast<double>(c.second % d.nx) + 0.5) * kCS,
                y0 + (static_cast<double>(c.second / d.nx) + 0.5) * kCS
            };
            // 挪到与邻点重合就出零长段, 跟随层从零长段上取不到方向。阈值取半格: 严格小于
            // 任意两个不同格心的间距, 于是拦得住重合又碰不到合法的一格位移。
            if (std::hypot(w.x - P[i - 1].x, w.y - P[i - 1].y) < kCS * 0.5
                || std::hypot(w.x - P[i + 1].x, w.y - P[i + 1].y) < kCS * 0.5) {
                continue;
            }
            if (std::min(SegMinClr(d, x0, y0, P[i - 1], w), SegMinClr(d, x0, y0, w, P[i + 1])) < keep - 1e-9) {
                continue;
            }
            // 隔一段的自交:中间那一小段比位移还短时, 挪一下就把它翻了过去。父链本身是树不自交,
            // 只有这一类新交点需要挡, 挡在这里比事后去环便宜, 也不用再引一段几何工序。
            if ((i >= 2 && SegCross(w, P[i + 1], P[i - 2], P[i - 1]))
                || (i + 2 < P.size() && SegCross(P[i - 1], w, P[i + 1], P[i + 2]))) {
                continue;
            }
            const auto h1 = lyo.walk({ P[i - 1], w }, ha);
            if (!h1.has_value() || h1->empty()) {
                continue;
            }
            const float hw = nearest(*h1, ha);
            const auto h2 = lyo.walk({ w, P[i + 1] }, hw);
            if (!h2.has_value() || h2->empty()) {
                continue;
            }
            if (!vis.ok(P[i - 1], w, ha, hw) || !vis.ok(w, P[i + 1], hw, nearest(*h2, hw))) {
                continue;
            }
            P[i] = w;
            hnext = hw;
            break;
        }
        ha = hnext;
    }
}

// goal_deck: 终点所在面的高度。不声明时终点集是该格全部 span,先够到哪张停哪张
std::optional<std::vector<WorldPoint>> routeWindow(
    WindowInfo& info,
    const WorldPoint& s,
    const WorldPoint& g,
    RouteDiag& dg,
    std::optional<double> goal_deck,
    const BaseNavPlanner& pl,
    uint16_t zid)
{
    const double t_topo0 = nowMs();
    const int64_t nx = info.nx;
    const int64_t ny = info.ny;
    const double x0 = info.x0;
    const double y0 = info.y0;
    // 全窗口的图按最后一个读者就地释放: 早就没人读的表不该陪着活到最耗内存的那一刻。释放后再
    // 取值是越界而不是错值, 逐腿比对因此能当场抓住漏算的读者 —— lambda 的调用点才算读者。
    Mask walk(nx, ny, 0);
    for (size_t i = 0; i < walk.v.size(); ++i) {
        walk.v[i] = static_cast<uint8_t>(info.core.v[i] != 0 && info.lay.v[i] != 0);
    }
    info.lay = Mask();
    // 边界边只用来算余量, 不用来禁步: 补洞封缝那一步已经判定这些细缝可以跨,
    // 回头再拿同一批边禁掉跨缝的一步, 等于在每道接缝上凭空立一堵墙
    const EdgeBits& blocked_steps = info.sev.steps;
    // 无封堵的腿: 封缝净空与中轴都是旁包按整类窗口烘好的, 直接采。
    // 有封堵的腿: 净空已按盖过的核心重算, 接缝补偿与中轴只能在这里现算。
    Grid<float> dist;
    Mask rdg;
    if (!info.blocked) {
        dist = std::move(info.dist);
        info.dist = Grid<float>();
        rdg = std::move(info.medial);
        info.medial = Mask();
    }
    else {
        // 掩膜距离场对跨越边界边无感, 取到边界的距离的下确界补上
        Mask wfree(nx, ny, 0);
        for (size_t i = 0; i < wfree.v.size(); ++i) {
            wfree.v[i] = info.whit.v[i] != 0 ? 0 : 1;
        }
        info.whit = Mask();
        // 共面重叠片各自留着自己的边界, 落到格上是间距约 1px 的栅格, 开阔广场因此与窄巷读出同样的
        // 宽度, 按宽度定价的拓扑层于是分辨不出宽路。摘法只放不加: 四邻全可走、且这四步都没被禁的
        // 格子才回自由集, 建筑外轮廓恒有一侧没有面, 一根真墙边都摘不掉。
        for (int64_t y = 1; y + 1 < ny; ++y) {
            for (int64_t x = 1; x + 1 < nx; ++x) {
                const int64_t c = y * nx + x;
                if (wfree.v[static_cast<size_t>(c)] != 0 || walk.v[static_cast<size_t>(c)] == 0) {
                    continue;
                }
                bool seam = true;
                for (const int64_t d : { int64_t { 1 }, int64_t { -1 }, nx, -nx }) {
                    const int64_t b = c + d;
                    if (walk.v[static_cast<size_t>(b)] == 0 || blocked_steps.has(c, b) || blocked_steps.has(b, c)) {
                        seam = false;
                        break;
                    }
                }
                if (seam) {
                    wfree.v[static_cast<size_t>(c)] = 1;
                }
            }
        }
        dist = Clearance(wfree);
        wfree = Mask();
        // 取小就地写回接缝净空那张表: 另开一张同尺寸的只是让两张 36MB 的图在整个 routeWindow 里同时活着。
        for (size_t i = 0; i < dist.v.size(); ++i) {
            dist.v[i] = std::min(info.dist.v[i], dist.v[i]);
        }
        info.dist = Grid<float>();
        // VV(c): 障碍按期望净空 c 膨胀后仍自由的格走可见图那一侧, 膨胀后被吃掉的窄处只留中脊,
        // 对应论文里 V∩M(c) 的那段 Voronoi 弧。净空在这一层是掩膜: 开阔地没有贴墙这个选项, 窄缝
        // 里没有偏一侧这个选项, 中途钻的一小段窄缝也就无法被整条路长平均掉。
        rdg = MedialAxis(dist, kClrLambda);
    }
    const double cpref = kClrPref;
    // 通道 = 障碍按 c 膨胀后仍自由的格, 并上中轴带。
    const auto chan = [&](double cc, const Mask& band) {
        Mask w(nx, ny, 0);
        for (size_t i = 0; i < w.v.size(); ++i) {
            w.v[i] = static_cast<uint8_t>(static_cast<double>(dist.v[i]) >= cc || band.v[i] != 0);
        }
        return w;
    };
    // 中脊单价按净空亏欠比例上浮, 可见图一侧恒为一。这道价只在几条窄缝之间做取舍。
    const PriceField mult { .dist = &dist, .lo = kCS, .hi = cpref };
    // 定通道那一层的单价同式, 但取值区间换成 [kClrNarrow, kClrWide]。封在 cpref 的话中轴上处处
    // 够宽的格单价一律为一, 平行分支里最短的那条必然中标 —— 而窄缝总比宽道短, 线于是钻缝。
    // 基准取在最宽处, 单价因此恒不低于一, 搜索拿欧氏距离当下界才成立; 基准落在窄处则它高估
    // 剩余代价, 反复重开已定好的节点。整体抬价保持逐格相对贵贱不变, 最优路径集合照旧。
    const PriceField multw { .dist = &dist, .lo = kClrNarrow, .hi = kClrWide };

    const CellPt sc { static_cast<int64_t>((s.x - x0) / kCS), static_cast<int64_t>((s.y - y0) / kCS) };
    const CellPt gc { static_cast<int64_t>((g.x - x0) / kCS), static_cast<int64_t>((g.y - y0) / kCS) };

    // 小窗验收的尺子: 端点离窗边最近的格数减去可信余量。单价恒 ≥1, 所以累计代价是走过格数的
    // 上界; 代价小于这把尺子的搜索碰不到可信区外的格, 弹出序列与整类窗口里的逐步相同。
    // 带启发式的搜索更紧: 弹出的格满足 g+h ≤ 终代价, 而 g ≥ 到起点格数、h = 到终点欧氏格数,
    // 于是只在两端为焦点、和为终代价的椭圆里; limit2 取可信区外的格两距之和的下界。
    const bool tentative = dg.margin > 0;
    // 只与窗口大小有关的验收。可达域来自旁包, 小窗就已是整类口径, 所以只有碰边类要升档;
    // 封顶档没有更大的窗可换, 不通类按整类窗口的规则就地退档或报断。
    const bool bounded = tentative && !dg.final;
    const bool strict = bounded;
    const int64_t edge_s = std::min({ sc.x, sc.y, nx - 1 - sc.x, ny - 1 - sc.y });
    const int64_t edge_g = std::min({ gc.x, gc.y, nx - 1 - gc.x, ny - 1 - gc.y });
    const int64_t edge = std::min(edge_s, edge_g);
    const double limit = static_cast<double>(edge - dg.margin);
    // 两距之和是凸函数且最小值在可信区里, 区外任一点连向最小点的线段穿过可信区边界, 凸性
    // 使穿越点不比它大; 所以下界就是边界四条边上的最小值, 每条边上三分求。可信区外的格落在
    // x ≤ margin-1 或 x ≥ nx-margin 那圈上, 矩形按这两条取; 再让一格给取整。
    const auto ellipseFloor = [&]() {
        const auto sum2 = [&](double x, double y) {
            return std::hypot(x - static_cast<double>(sc.x), y - static_cast<double>(sc.y))
                   + std::hypot(x - static_cast<double>(gc.x), y - static_cast<double>(gc.y));
        };
        const auto onSeg = [&](double ax, double ay, double bx, double by) {
            double lo = 0.0;
            double hi = 1.0;
            for (int i = 0; i < 100; ++i) {
                const double m1 = lo + (hi - lo) / 3.0;
                const double m2 = hi - (hi - lo) / 3.0;
                if (sum2(ax + (bx - ax) * m1, ay + (by - ay) * m1) < sum2(ax + (bx - ax) * m2, ay + (by - ay) * m2)) {
                    hi = m2;
                }
                else {
                    lo = m1;
                }
            }
            return sum2(ax + (bx - ax) * lo, ay + (by - ay) * lo);
        };
        const double rx0 = static_cast<double>(dg.margin - 1);
        const double ry0 = static_cast<double>(dg.margin - 1);
        const double rx1 = static_cast<double>(nx - dg.margin);
        const double ry1 = static_cast<double>(ny - dg.margin);
        return std::min({ onSeg(rx0, ry0, rx1, ry0), onSeg(rx1, ry0, rx1, ry1), onSeg(rx1, ry1, rx0, ry1), onSeg(rx0, ry1, rx0, ry0) })
               - 1.0;
    };
    const double limit2 = tentative ? ellipseFloor() : 0.0;
    const auto esc = [&](const char* why) {
        if (!dg.escalate) {
            dg.escalate = true;
            dg.escalate_why = why;
        }
    };
    if (tentative && limit <= 0.0) {
        esc("端点离窗边不足");
        return std::nullopt;
    }

    const auto nearestCell = [&](const Mask& mask, const CellPt& p) -> std::pair<std::optional<CellPt>, double> {
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
    const LayerOracle lyo(&st3, nx, ny, x0, y0);
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
    Mask cw3;
    mk(walk, useW, cw3);
    const auto pick = [&](const CellPt& c, const std::vector<uint8_t>& use) {
        std::vector<int64_t> out;
        const int64_t j = info.st3.j(c.y * nx + c.x);
        if (j < 0) {
            return out;
        }
        const int64_t jb = st3.cstart(j);
        for (int64_t k = 0, kn = st3.ccnt(j); k < kn; ++k) {
            const int64_t v = jb + k;
            if (use[static_cast<size_t>(v)] != 0) {
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

    // 声明了终点面就按面吸附: 最近的可走格未必带着这张面, 吸上去 goalsOf 会交空集。同距再比
    // 高度差, 让吸附结果跟 atDeck 选的那张 span 一致。
    const auto nearGoal = [&](const std::vector<uint8_t>& use, const Mask& cells) -> std::pair<std::optional<CellPt>, double> {
        if (!goal_deck.has_value()) {
            return nearestCell(cells, gc);
        }
        bool have = false;
        int64_t bd = 0;
        double bh = 0.0;
        CellPt bc;
        for (size_t i = 0; i < use.size(); ++i) {
            if (use[i] == 0) {
                continue;
            }
            const double dh = std::fabs(static_cast<double>(st3.sp_h[i]) - *goal_deck);
            if (dh > kDeckBand) {
                continue;
            }
            const int64_t cell = st3.sp_cell[i];
            const int64_t x = cell % nx;
            const int64_t y = cell / nx;
            const int64_t d = (x - gc.x) * (x - gc.x) + (y - gc.y) * (y - gc.y);
            if (!have || d < bd || (d == bd && dh < bh)) {
                have = true;
                bd = d;
                bh = dh;
                bc = { x, y };
            }
        }
        if (!have) {
            return { std::nullopt, 0.0 };
        }
        return { bc, std::sqrt(static_cast<double>(bd)) * kCS };
    };

    const auto snap0 = nearestCell(cw3, sc);
    const auto snap1 = nearGoal(useW, cw3);
    if (!snap0.first.has_value()) {
        dg.err = "walk 掩膜为空";
        return std::nullopt;
    }
    if (!snap1.first.has_value()) {
        dg.err = goal_deck.has_value() ? "目标附近没有未封堵的声明面" : "walk 掩膜为空";
        return std::nullopt;
    }
    std::optional<CellPt> as_ = snap0.first;
    std::optional<CellPt> ag_ = snap1.first;
    double dsa = snap0.second;
    double dga = snap1.second;
    if (bounded && (dsa / kCS >= limit || dga / kCS >= limit)) {
        esc("吸附距离碰边");
        return std::nullopt;
    }

    // VV(c) 的端点接入。作者点位常贴着墙放, 净空低于 c 又不在中脊上, 于是根本不在通道里。
    // 论文里起终点是单独接进图的: 这里在可走面上求端点到通道的最短接入链, 只放开这一条。接入
    // 是退化连接而不是路线, 取最短即最小开口; 同长再取瓶颈最高的一条, 最后按格序定全序。八角
    // 距离取整数以免浮点累加出不确定的序。搜索遍历整片可走面, 口袋与量化平台都困不住它 ——
    // 端点贴墙因此不再把整条腿的准入等级拖下去, 降档只留给中段真正的窄缝。
    const auto access = [&](const Mask& lim, const std::optional<CellPt>& a) -> std::optional<std::vector<int64_t>> {
        if (!a.has_value()) {
            return std::vector<int64_t> {};
        }
        const size_t n = static_cast<size_t>(nx * ny);
        const size_t s0 = static_cast<size_t>(a->y * nx + a->x);
        if (lim.v[s0] != 0) {
            return std::vector<int64_t> {};
        }
        std::vector<int32_t> cs(n, std::numeric_limits<int32_t>::max());
        std::vector<float> bw(n, -1.0F);
        // 父链存的是格号, 上界 kMaxCells 装得进 32 位; 比较里它会提回 64 位, 全序照旧。
        std::vector<int32_t> pv(n, -1);
        std::priority_queue<std::tuple<int32_t, float, int64_t>> pq;
        cs[s0] = 0;
        bw[s0] = dist.v[s0];
        pq.emplace(0, bw[s0], -static_cast<int64_t>(s0));
        int64_t hit = -1;
        while (!pq.empty()) {
            const int32_t cc = -std::get<0>(pq.top());
            const float b = std::get<1>(pq.top());
            const int64_t c = -std::get<2>(pq.top());
            pq.pop();
            if (cc != cs[static_cast<size_t>(c)] || b != bw[static_cast<size_t>(c)]) {
                continue;
            }
            if (lim.v[static_cast<size_t>(c)] != 0) {
                hit = c;
                break;
            }
            const int64_t cx = c % nx;
            const int64_t cy = c / nx;
            for (int64_t dy = -1; dy <= 1; ++dy) {
                for (int64_t dx = -1; dx <= 1; ++dx) {
                    const int64_t bx = cx + dx;
                    const int64_t by = cy + dy;
                    if ((dx == 0 && dy == 0) || bx < 0 || by < 0 || bx >= nx || by >= ny) {
                        continue;
                    }
                    const size_t k = static_cast<size_t>(by * nx + bx);
                    if (cw3.v[k] == 0) {
                        continue;
                    }
                    const int32_t kc = cc + (dx != 0 && dy != 0 ? 141 : 100);
                    const float kb = std::min(b, dist.v[k]);
                    if (kc < cs[k] || (kc == cs[k] && (kb > bw[k] || (kb == bw[k] && c < pv[k])))) {
                        cs[k] = kc;
                        bw[k] = kb;
                        pv[k] = static_cast<int32_t>(c);
                        pq.emplace(-kc, kb, -static_cast<int64_t>(k));
                    }
                }
            }
        }
        if (hit < 0) {
            if (strict) {
                esc("接入链接不通");
            }
            return std::nullopt;
        }
        const double chain_len = static_cast<double>(cs[static_cast<size_t>(hit)]) / 100.0;
        if (bounded && chain_len >= limit) {
            esc("接入链碰边");
            return std::nullopt;
        }
        std::vector<int64_t> ch;
        for (int64_t c = hit; c >= 0; c = pv[static_cast<size_t>(c)]) {
            ch.push_back(c);
        }
        return ch;
    };
    const auto openc = [&](Mask& lim, const std::vector<int64_t>& ch) {
        for (const int64_t c : ch) {
            lim.v[static_cast<size_t>(c)] = 1;
        }
    };


    const EdgeBits* faces = &info.sev.steps;

    struct Topo
    {
        std::vector<CellPt> q;
        std::optional<std::vector<int64_t>> qs;
        // 搜索交出的父链。带视线判据那一路才有, 它就是几何要走的折线本身。
        std::vector<int64_t> corn;
        Mask on3;
        std::vector<std::string> warn;
    };

    // 掩膜内两端是否八连通。span 图上的解投到格上必是掩膜内的一条八连通链, 因此这是必要条件:
    // 判否时 solve 一定失败, 通道阶梯就不必为接不通的那些档建图。取 core∧lim, 它是 solve 两次
    // 尝试里最宽松的集合。
    std::vector<uint8_t> seen(static_cast<size_t>(nx * ny), 0);
    std::vector<int64_t> stk;
    const auto linked = [&](const Mask& lim) {
        const int64_t sc = as_->y * nx + as_->x;
        const int64_t gc = ag_->y * nx + ag_->x;
        const auto in = [&](int64_t c) { return info.core.v[static_cast<size_t>(c)] != 0 && lim.v[static_cast<size_t>(c)] != 0; };
        if (!in(sc) || !in(gc)) {
            return false;
        }
        std::fill(seen.begin(), seen.end(), static_cast<uint8_t>(0));
        stk.clear();
        stk.push_back(sc);
        seen[static_cast<size_t>(sc)] = 1;
        while (!stk.empty()) {
            const int64_t c = stk.back();
            stk.pop_back();
            if (c == gc) {
                return true;
            }
            const int64_t cx = c % nx;
            const int64_t cy = c / nx;
            for (int64_t dy = -1; dy <= 1; ++dy) {
                for (int64_t dx = -1; dx <= 1; ++dx) {
                    const int64_t bx = cx + dx;
                    const int64_t by = cy + dy;
                    if (bx < 0 || by < 0 || bx >= nx || by >= ny) {
                        continue;
                    }
                    const int64_t b = by * nx + bx;
                    if (seen[static_cast<size_t>(b)] == 0 && in(b)) {
                        seen[static_cast<size_t>(b)] = 1;
                        stk.push_back(b);
                    }
                }
            }
        }
        return false;
    };

    // 一次拓扑求解。硬可达口径逐字不变: 掩膜按 walk→core 退, 层不通再退格级, RiseOk、立面禁步、
    // 目标面声明与 LayerOracle 全部原样。lim 只做减法, 无权放宽其中任何一条, 舒适选路因此造不出
    // unreachable。
    const auto solve = [&](const Mask& lim,
                           const PriceField& price,
                           const EdgeBits* banned,
                           const double* bnp,
                           bool resnap,
                           const Visibility* vis = nullptr) -> std::optional<Topo> {
        // 接不通的掩膜不必建图。resnap 那一路会重挑吸附锚点, 判据里的两端就不再成立, 因此不查。
        if (!resnap && !linked(lim)) {
            if (strict) {
                esc("掩膜内两端不连通");
            }
            return std::nullopt;
        }
        Mask wl(nx, ny, 0);
        Mask cr(nx, ny, 0);
        for (size_t i = 0; i < wl.v.size(); ++i) {
            wl.v[i] = static_cast<uint8_t>(walk.v[i] != 0 && lim.v[i] != 0);
            cr.v[i] = static_cast<uint8_t>(info.core.v[i] != 0 && lim.v[i] != 0);
        }
        std::vector<uint8_t> uw;
        std::vector<uint8_t> uc;
        Mask w3;
        Mask c3;
        mk(wl, uw, w3);
        mk(cr, uc, c3);
        std::vector<int64_t> corn;
        double cost = 0.0;
        const auto run = [&](const std::vector<uint8_t>& use, const Mask& m3) -> std::optional<std::vector<int64_t>> {
            corn.clear();
            cost = 0.0;
            if (m3.at(as_->y, as_->x) == 0 || m3.at(ag_->y, ag_->x) == 0) {
                return std::nullopt;
            }
            const std::vector<int64_t> gs = goalsOf(pick(*ag_, use));
            const int64_t sd = atSeedLayer(pick(*as_, use));
            if (as_->x == ag_->x && as_->y == ag_->y) {
                if (!goal_deck.has_value()) {
                    return sd >= 0 ? std::optional<std::vector<int64_t>> { { sd } } : std::nullopt;
                }
                return gs.empty() ? std::nullopt : std::optional<std::vector<int64_t>> { { gs.front() } };
            }
            if (sd < 0 || (goal_deck.has_value() && gs.empty())) {
                return std::nullopt;
            }
            return SpanAstar(st3, use, m3, sd, gs, price, banned, bnp, faces, vis, vis != nullptr ? &corn : nullptr, &cost);
        };
        Topo t;
        t.on3 = w3;
        std::optional<std::vector<int64_t>> sq = run(uw, w3);
        if (!sq.has_value()) {
            // 小窗里的任何退档都可能是被窗口切出来的假失败, 不采信。
            if (strict) {
                esc("walk 断开");
                return std::nullopt;
            }
            sq = run(uc, c3);
            if (sq.has_value()) {
                t.on3 = c3;
                t.warn.push_back("walk 断开→退回 core");
            }
        }
        if (sq.has_value()) {
            // 搜索从吸附锚点出发, 焦点却取端点格, 两段吸附偏移并入代价
            if (bounded && cost + (dsa + dga) / kCS >= limit2) {
                esc("搜索碰边");
                return std::nullopt;
            }
            t.q.reserve(sq->size());
            for (const int64_t v : *sq) {
                const int64_t c = st3.sp_cell[static_cast<size_t>(v)];
                t.q.push_back({ c % nx, c / nx });
            }
            t.qs = std::move(sq);
            t.corn = std::move(corn);
            return t;
        }
        // 格级搜索连 span 都不看, 退到这一级等于把选层交回给楼层盲的那一级
        if (goal_deck.has_value()) {
            return std::nullopt;
        }
        // 吸附锚点是硬可达的判定, 舒适选路无权改它: 够不着就报断开, 让基线并集那一轮接手
        if (!resnap) {
            return std::nullopt;
        }
        if (strict) {
            esc("层不连通");
            return std::nullopt;
        }
        std::tie(as_, dsa) = nearestCell(wl, sc);
        std::tie(ag_, dga) = nearestCell(wl, gc);
        if (!as_.has_value() || !ag_.has_value()) {
            return std::nullopt;
        }
        t.on3 = wl;
        std::optional<std::vector<CellPt>> qc;
        if (as_->x == ag_->x && as_->y == ag_->y) {
            qc = std::vector<CellPt> { *as_ };
        }
        else {
            qc = CostAstar(wl, *as_, *ag_, price, banned, bnp, faces);
        }
        if (!qc.has_value()) {
            t.on3 = cr;
            qc = CostAstar(cr, *as_, *ag_, price, banned, bnp, faces);
            if (qc.has_value()) {
                t.warn.push_back("walk 断开→退回 core");
            }
        }
        if (!qc.has_value()) {
            return std::nullopt;
        }
        t.warn.push_back("层不连通→退回格级");
        t.q = std::move(*qc);
        return t;
    };

    // 一端的可达 span 集。展开判据与 SpanAstar 逐字相同: 格掩膜、对角切角、立面禁步、RiseOk。
    // backward 那一路走的是 v→u 这个方向 —— 禁行边与抬升判据都是有向的, 拿正向去问会把单向的
    // 台阶说成两边都能过。
    const auto reachFrom = [&](const std::vector<int64_t>& seeds, const std::vector<uint8_t>& use, const Mask& ok2,
                               bool backward) {
        std::vector<uint8_t> seen(st3.sp_h.size(), 0);
        std::vector<int64_t> frontier;
        for (const int64_t v : seeds) {
            if (v >= 0 && use[static_cast<size_t>(v)] != 0 && seen[static_cast<size_t>(v)] == 0) {
                seen[static_cast<size_t>(v)] = 1;
                frontier.push_back(v);
            }
        }
        while (!frontier.empty()) {
            std::vector<int64_t> next;
            for (const int64_t u : frontier) {
                const int64_t cu = st3.sp_cell[static_cast<size_t>(u)];
                const int64_t ux = cu % nx;
                const int64_t uy = cu / nx;
                const float hu = st3.sp_h[static_cast<size_t>(u)];
                for (int64_t dy = -1; dy <= 1; ++dy) {
                    for (int64_t dx = -1; dx <= 1; ++dx) {
                        const int64_t vx = ux + dx;
                        const int64_t vy = uy + dy;
                        if ((dx == 0 && dy == 0) || vx < 0 || vy < 0 || vx >= nx || vy >= ny) {
                            continue;
                        }
                        const int64_t cv = vy * nx + vx;
                        if (ok2.v[static_cast<size_t>(cv)] == 0) {
                            continue;
                        }
                        if (dx != 0 && dy != 0 && !(ok2.at(uy, vx) && ok2.at(vy, ux))) {
                            continue;
                        }
                        if (info.st3.j(cv) < 0) {
                            continue;
                        }
                        if (faces->has(backward ? cv : cu, backward ? cu : cv)) {
                            continue;
                        }
                        const int64_t j = info.st3.j(cv);
                        const int64_t jb = st3.cstart(j);
                        for (int64_t k = 0, kn = st3.ccnt(j); k < kn; ++k) {
                            const int64_t v = jb + k;
                            if (use[static_cast<size_t>(v)] == 0 || seen[static_cast<size_t>(v)] != 0) {
                                continue;
                            }
                            const float hv = st3.sp_h[static_cast<size_t>(v)];
                            if (!(backward ? RiseOk(st3, nx, ny, cv, -dx, -dy, hv, hu)
                                           : RiseOk(st3, nx, ny, cu, dx, dy, hu, hv))) {
                                continue;
                            }
                            seen[static_cast<size_t>(v)] = 1;
                            next.push_back(v);
                        }
                    }
                }
            }
            frontier = std::move(next);
        }
        return seen;
    };
    // 断开时报缝: 两端各自泛洪, 取两片可达集之间最近的一对格。倒角距离场从终点侧铺开、起点侧
    // 扫一遍取最小, 与逐对比较同解而只花一遍网格。判在最宽松的 core 上, 缝因此是真的缝。
    const auto reportGap = [&] {
        if (!as_.has_value() || !ag_.has_value()) {
            return;
        }
        std::vector<uint8_t> useC;
        Mask cc3;
        mk(info.core, useC, cc3);
        const int64_t sd = atSeedLayer(pick(*as_, useC));
        const std::vector<int64_t> gs = goalsOf(pick(*ag_, useC));
        if (sd < 0 || gs.empty()) {
            return;
        }
        const std::vector<uint8_t> ra = reachFrom({ sd }, useC, cc3, false);
        const std::vector<uint8_t> rb = reachFrom(gs, useC, cc3, true);
        const size_t n = static_cast<size_t>(nx * ny);
        constexpr int32_t kBig = std::numeric_limits<int32_t>::max() / 4;
        std::vector<int32_t> dc(n, kBig);
        std::vector<int32_t> src(n, -1);
        std::vector<uint8_t> ina(n, 0);
        for (size_t i = 0; i < ra.size(); ++i) {
            const auto c = static_cast<size_t>(st3.sp_cell[i]);
            ina[c] = static_cast<uint8_t>(ina[c] | ra[i]);
            if (rb[i] != 0) {
                dc[c] = 0;
                src[c] = static_cast<int32_t>(c);
            }
        }
        const auto relax = [&](size_t c, int64_t bx, int64_t by, int32_t w) {
            if (bx < 0 || by < 0 || bx >= nx || by >= ny) {
                return;
            }
            const auto b = static_cast<size_t>(by * nx + bx);
            if (dc[b] < kBig && dc[b] + w < dc[c]) {
                dc[c] = dc[b] + w;
                src[c] = src[b];
            }
        };
        for (int64_t y = 0; y < ny; ++y) {
            for (int64_t x = 0; x < nx; ++x) {
                const auto c = static_cast<size_t>(y * nx + x);
                relax(c, x - 1, y - 1, 141);
                relax(c, x, y - 1, 100);
                relax(c, x + 1, y - 1, 141);
                relax(c, x - 1, y, 100);
            }
        }
        for (int64_t y = ny - 1; y >= 0; --y) {
            for (int64_t x = nx - 1; x >= 0; --x) {
                const auto c = static_cast<size_t>(y * nx + x);
                relax(c, x + 1, y + 1, 141);
                relax(c, x, y + 1, 100);
                relax(c, x - 1, y + 1, 141);
                relax(c, x + 1, y, 100);
            }
        }
        int64_t best = -1;
        for (size_t c = 0; c < n; ++c) {
            if (ina[c] != 0 && src[c] >= 0 && (best < 0 || dc[c] < dc[static_cast<size_t>(best)])) {
                best = static_cast<int64_t>(c);
            }
        }
        if (best < 0) {
            return;
        }
        const int64_t peer = src[static_cast<size_t>(best)];
        const WorldPoint a { x0 + (static_cast<double>(best % nx) + 0.5) * kCS, y0 + (static_cast<double>(best / nx) + 0.5) * kCS };
        const WorldPoint b { x0 + (static_cast<double>(peer % nx) + 0.5) * kCS, y0 + (static_cast<double>(peer / nx) + 0.5) * kCS };
        dg.gap_start = a;
        dg.gap_goal = b;
        dg.gap_distance = std::hypot(a.x - b.x, a.y - b.y);
    };

    // 硬约束基线: 原始硬图上的纯长度最短路。硬图可达它就一定存在, 于是舒适选路永远不会把一条
    // 走得通的腿判成不可达。它同时是端点的回缩通道 —— 起终点天然贴墙时, 搜索沿这条按亏欠计价
    // 的线走几格就自然汇入 VV 图, 端点附近不需要任何放宽半径。
    const Mask all(nx, ny, 1);
    const PriceField unit {};
    const std::optional<Topo> base = solve(all, unit, nullptr, nullptr, true);
    if (strict && !base.has_value()) {
        esc("基线不通");
        return std::nullopt;
    }
    if (!base.has_value()) {
        reportGap();
        dg.timing.topology_ms = nowMs() - t_topo0;
        if (goal_deck.has_value()) {
            const std::vector<int64_t> gv = pick(*ag_, useW);
            std::vector<float> hv;
            hv.reserve(gv.size());
            for (const int64_t v : gv) {
                hv.push_back(st3.sp_h[static_cast<size_t>(v)]);
            }
            std::sort(hv.begin(), hv.end());
            hv.erase(std::unique(hv.begin(), hv.end()), hv.end());
            std::string list;
            char buf[32];
            for (const float h : hv) {
                std::snprintf(buf, sizeof(buf), "%.2f", static_cast<double>(h));
                list += (list.empty() ? "" : ", ") + std::string(buf);
            }
            std::snprintf(buf, sizeof(buf), "%.2f", *goal_deck);
            dg.err =
                "目标面不可达 (声明 " + std::string(buf) + ", 终点格里的面 " + (list.empty() ? std::string("无") : "[" + list + "]") + ")";
            return std::nullopt;
        }
        dg.err = "不连通";
        return std::nullopt;
    }
    // 走通了就再没人查这张逐 span 的可用表, 上面那两条出口都直接返回。
    useW = std::vector<uint8_t>();

    // 台阶税边: 跨越两侧找不到任何一对高差在坡度内的面才算一次台阶, 连续缓坡不计。它是整类量,
    // 旁包烘好了直接用。不参与硬连通性, 只在同样走得通的两条线之间偏向不必迈的那条。
    const double tax = kStepTax;

    // 自由集是膨胀后仍自由的实心区并上中脊: 宽处走净空 ≥c 的实心区, 窄段走中轴弧,
    // 缝的准入因此由中轴自己定, 不必再拿一串阈值去试。
    const EdgeBits* bn = tax > 0.0 ? &info.step_edges : nullptr;
    const double* bp = tax > 0.0 ? &tax : nullptr;
    // 立面这笔税以普通路面的一格为单位报价, 而定通道那层把普通路面抬了价, 汇率得跟着换,
    // 否则同一道立面在拓扑层变便宜, 花两格路就能买过去。
    const double taxw = tax * kClrWide / cpref;
    const double* bpw = tax > 0.0 ? &taxw : nullptr;
    // 接入链只算一次: 目标取实心通道 chan0, 它是自由集的子集, 接到它就等于接进了自由集。
    // 两端各自对着 chan0 算, 谁先算不影响结果。够不到 chan0 的那一端才对着自由集重算。
    Mask chan0 = chan(cpref, Mask(nx, ny, 0));
    const std::optional<std::vector<int64_t>> ac_s = access(chan0, as_);
    const std::optional<std::vector<int64_t>> ac_g = access(chan0, ag_);
    chan0 = Mask();
    if (dg.escalate) {
        return std::nullopt;
    }
    const auto join = [&](Mask& lim) {
        const std::optional<std::vector<int64_t>> s = ac_s.has_value() ? ac_s : access(lim, as_);
        const std::optional<std::vector<int64_t>> g = ac_g.has_value() ? ac_g : access(lim, ag_);
        if (s.has_value()) {
            openc(lim, *s);
        }
        if (g.has_value()) {
            openc(lim, *g);
        }
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

    Mask limw = chan(cpref, rdg);
    rdg = Mask();
    join(limw);
    if (dg.escalate) {
        return std::nullopt;
    }
    std::optional<Topo> vv = solve(limw, multw, bn, bpw, false);
    if (strict && !vv.has_value()) {
        esc("通道未接通");
        return std::nullopt;
    }
    if (!vv.has_value()) {
        limw = Mask(nx, ny, 1);
        vv = solve(limw, multw, bn, bpw, false);
        dg.warn.emplace_back("通道未接通, 退到全图亏欠计价");
    }
    {
        const Topo& tp = vv.has_value() ? *vv : *base;
        dg.topology_cells.reserve(tp.q.size());
        for (const CellPt& c : tp.q) {
            dg.topology_cells.push_back({ x0 + (static_cast<double>(c.x) + 0.5) * kCS, y0 + (static_cast<double>(c.y) + 0.5) * kCS });
        }
        if (tp.qs.has_value()) {
            dg.topology_heights.reserve(tp.qs->size());
            for (const int64_t v : *tp.qs) {
                dg.topology_heights.push_back(static_cast<double>(st3.sp_h[static_cast<size_t>(v)]));
            }
        }
    }
    dg.timing.topology_ms = nowMs() - t_topo0;
    const double t_geo0 = nowMs();
    // 通道定了才铺几何: 同一张中标掩膜上再解一次, 这次带视线判据, 出来的父链已经是紧绷折线。
    // 弦只许落在 cpref 的实心区内 —— 中脊是净空极大线, 对它取直等于把线拽向墙, 那一段照旧逐格走;
    // 实心区里单价恒为一, 弦的欧氏长度因此就是精确代价。拐角余量往上收窄这个自由集: 弦贴着障碍角
    // 切过去时净空恰好等于这道阈值, 收一点拐角就离墙远一点, 且收后仍是单价恒一的子集, 计价不受影响。
    const double solid_c = cpref + kGeoMargin;
    // 走廊以中标的那条逐格路径为骨干张开, 半宽照它自己的净空取。按全局中轴取则会隔墙认到旁边
    // 那条窄缝上去: 宽处的半宽被压回地板, 该收紧的地方一点没收紧。
    Mask band;
    Mask solidc(nx, ny, 0);
    {
        Mask bb(nx, ny, 0);
        for (const CellPt& c : (vv.has_value() ? vv->q : base->q)) {
            bb.at(c.y, c.x) = 1;
        }
        const Grid<float> cw = CorridorWidth(dist, bb, &band);
        for (size_t i = 0; i < solidc.v.size(); ++i) {
            const double need = std::max(solid_c, kChordFrac * static_cast<double>(cw.v[i]));
            solidc.v[i] = static_cast<uint8_t>(static_cast<double>(dist.v[i]) >= need);
        }
        // 走廊读的是净空场, 它只在可信区里与整类窗口相同。
        if (bounded) {
            const int64_t m = dg.margin;
            for (int64_t y = 0; y < ny; ++y) {
                for (int64_t x = 0; x < nx; ++x) {
                    if (band.at(y, x) != 0 && (x < m || y < m || x >= nx - m || y >= ny - m)) {
                        esc("走廊碰边");
                        return std::nullopt;
                    }
                }
            }
        }
    }
    const Blockers::OnMask onv { &solidc, x0, y0, kCS };
    // 搜索期的视线只靠 on 掩膜兜底, 不带轮廓挡线。
    const BlockerSegments no_segs;
    const Blockers blk_vis(no_segs, onv);
    const Visibility vis(&blk_vis, &lyo, faces, bn, nx, ny, x0, y0);
    if (vv.has_value()) {
        // 几何这一次锁在走廊里。放开的话它按恒一的单价重解一遍, 又会挑回拓扑刚花钱绕开的那条
        // 窄分支 —— 加权白做。走廊含整条骨干, 两端锚点也在上面, 接通性因此照旧成立。
        Mask limg = limw;
        for (size_t i = 0; i < limg.v.size(); ++i) {
            limg.v[i] = static_cast<uint8_t>(limg.v[i] != 0 && band.v[i] != 0);
        }
        band = Mask();
        std::optional<Topo> tt = solve(limg, mult, bn, bp, false, &vis);
        if (strict && !tt.has_value()) {
            esc("走廊内几何不通");
            return std::nullopt;
        }
        if (!tt.has_value()) {
            // 走廊是偏好, 接通性不是。二维骨干必在走廊里, 层间接不通才会走到这里; 放开重解, 拿回
            // 的仍是紧绷折线, 只是不再受宽度约束。
            tt = solve(limw, mult, bn, bp, false, &vis);
        }
        if (tt.has_value()) {
            vv = std::move(tt);
        }
    }
    dg.timing.geometry_ms = nowMs() - t_geo0;
    const Topo& win = vv.has_value() ? *vv : *base;
    for (const std::string& w : win.warn) {
        dg.warn.push_back(w);
    }
    Mask on3 = win.on3;
    const std::optional<std::vector<CellPt>> q = win.q;
    const std::optional<std::vector<int64_t>>& qs = win.qs;
    dg.snap_start = dsa;
    dg.snap_goal = dga;
    // 末段是从锚点直连终点的,不走阻挡检查。终点自己就踩在可走面上却没进可达集时,
    // 这一跳等于穿墙;终点没有面才是作者点位的容差,那种照旧直连。
    if (gc.x >= 0 && gc.y >= 0 && gc.x < nx && gc.y < ny) {
        // 判据是硬可达集: on3 现在是舒适通道, 终点天然贴墙就不在里面, 拿它判等于把贴墙的
        // 终点一律说成被禁行边隔开
        dg.hop_barrier = walk.at(gc.y, gc.x) != 0 && base->on3.at(gc.y, gc.x) == 0;
    }
    std::vector<size_t> bad;
    for (size_t k = 1; k < q->size(); ++k) {
        const int64_t ca = (*q)[k - 1].y * nx + (*q)[k - 1].x;
        const int64_t cb = (*q)[k].y * nx + (*q)[k].x;
        if (blocked_steps.has(ca, cb)) {
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
    // 取直与计价共用一道挡线。两处判据分家的话, 搜索挑出来的居中拐点会被取直按另一套阈值切回墙边。
    // 它与这条腿有没有放宽无关: 放宽只为接通拓扑, 几何跟着放宽等于把钻缝从拓扑挪到几何。合不成弦
    // 的段退回逐格路径, 那条路径走的是中轴。再并上选定路径自身放宽一格, 端点接入链与格级回退路径
    // 因此不会被自己的挡线判掉。
    Mask tm = solidc;
    join(tm);
    for (size_t i = 0; i < tm.v.size(); ++i) {
        tm.v[i] = static_cast<uint8_t>(tm.v[i] != 0 && on3.v[i] != 0);
    }
    for (const CellPt& c : *q) {
        if (on3.at(c.y, c.x) == 0) {
            continue;
        }
        for (int64_t dy = -1; dy <= 1; ++dy) {
            for (int64_t dx = -1; dx <= 1; ++dx) {
                const int64_t bx = c.x + dx;
                const int64_t by = c.y + dy;
                if (bx >= 0 && by >= 0 && bx < nx && by < ny && on3.at(by, bx) != 0) {
                    tm.at(by, bx) = 1;
                }
            }
        }
    }
    const Blockers::OnMask onm { &tm, x0, y0, kCS };
    // 灰、硬两个视图的挡线几何逐字相同, 段表与桶索引共享一份, 两遍构建省成一遍。
    const BlockerSegments core_segs(loops_core, &info.segA, &info.segB);
    const Blockers blk_gray(core_segs, onm);
    // 几何用的视线判据: 跨步禁行、立面、层判据与搜索那一个逐字相同, 只把自由区从计价用的实心区
    // 换成这条路自己的管道。实心区那道限制是为让弦的欧氏长度等于代价, 只约束搜索; 终线取直不计价,
    // 该由能不能走过去决定。管道只比路径宽一格, 拉直因此仍出不了这条通道。
    const Visibility vis_geo(&blk_gray, &lyo, faces, bn, nx, ny, x0, y0);

    // 全局取直: 挡线用同一张选定通道掩膜, 取直因此出不了通道。不再切绿灰段, 也不再把落脚处限在
    // 原路径两侧 —— 那两件事把几何锁死在拓扑之后, 整段本可直走也留着折线。积分守卫一并撤掉:
    // 通道是掩膜, 直不直走该由能不能看见对端决定, 不由两条线的积分代价谁便宜决定。
    // 几何取搜索交出的父链: 拐点是搜索自己挑的, 弦是搜索自己验过视线的。逐格路径只留给拓扑
    // 判据读。终线仍走一道最远可见 —— Theta* 只出近紧线 —— 判据用几何那一个, 中脊带里搜索
    // 逐格走出来的点因此还能并成弦。
    const bool by_corn = win.corn.size() >= 2;
    const std::vector<int64_t>& gsrc = by_corn ? win.corn : (qs.has_value() ? *qs : win.corn);
    std::vector<float> hs;
    if (by_corn || qs.has_value()) {
        hs.reserve(gsrc.size());
        for (const int64_t v : gsrc) {
            hs.push_back(st3.sp_h[static_cast<size_t>(v)]);
        }
    }
    std::vector<CellPt> gq;
    if (by_corn) {
        gq.reserve(gsrc.size());
        for (const int64_t v : gsrc) {
            const int64_t c = st3.sp_cell[static_cast<size_t>(v)];
            gq.push_back({ c % nx, c / nx });
        }
    }
    const double t_pull0 = nowMs();
    std::vector<WorldPoint> taut = cen(by_corn ? gq : *q);
    dg.taut_points = taut;
    if (taut.size() >= 2) {
        taut = StringPull(taut, blk_gray, hs.empty() ? nullptr : &lyo, hs.empty() ? nullptr : &hs,
            by_corn ? &vis_geo : nullptr);
    }
    dg.pulled_points = taut;
    dg.timing.pull_ms = nowMs() - t_pull0;

    const double t_asm0 = nowMs();
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
    const LayerOracle* lyo_p = (by_corn || qs.has_value()) ? &lyo : nullptr;
    const float lyo_h = hs.empty() ? 0.0F : hs.front();
    std::vector<WorldPoint> out;
    if (by_corn && ded.size() >= 2) {
        // 父链是树, 自身不自交, 去环无事可做; 落点已经是拐点, 抽稀与拐角外扩同理。剩下的只有
        // 共线冗余点: a-b 与 b-c 都过视线且三点共线, a-c 必过, 删 b 是纯几何恒等。
        out.push_back(ded.front());
        for (size_t i = 1; i + 1 < ded.size(); ++i) {
            const WorldPoint& a = out.back();
            const WorldPoint& b = ded[i];
            const WorldPoint& c = ded[i + 1];
            const double cr = (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x);
            if (std::fabs(cr) > 1e-9 * std::max(1.0, std::hypot(c.x - a.x, c.y - a.y))) {
                out.push_back(b);
            }
        }
        out.push_back(ded.back());
    }
    else if (by_corn) {
        out = ded;
    }
    else {
        out = ded;
    }
    dg.assembled_points = out;
    dg.timing.assemble_ms = nowMs() - t_asm0;

    const double t_lift0 = nowMs();
    // 抬升放在取直之后: 放前面的话抬起来的拐点让两侧更容易连通, 取直一刀就把它跳过去了。
    // 判据换成硬墙那一套 —— 通道掩膜只比路径宽一格, 拿它判等于禁止拐点离开原路径。
    if (lyo_p != nullptr && out.size() >= 3) {
        const Blockers blk_hard(core_segs, std::nullopt);
        const Visibility vis_hard(&blk_hard, &lyo, faces, bn, nx, ny, x0, y0);
        LiftCorners(out, dist, x0, y0, vis_hard, lyo, lyo_h, kLiftMax);
    }
    dg.timing.lift_ms = nowMs() - t_lift0;
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
    return out;
}

}

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
        return;
    }
    // 旁包与主包同目录同名配对; 缺了或对不上就整个引擎不可用, 不退回运行期重建。
    if (!fields_.load(FieldsSidecarPath(pack_.path()), pack_, grid_, grid_error_)) {
        grid_ = GridPack();
    }
}

RecastNavEngine::ZoneEntry& RecastNavEngine::zoneEntry(const std::string& name)
{
    // 返回的引用在下一次 zoneEntry/warm 之后失效(会淘汰), 调用方不得跨调用持有。
    auto it = zones_.find(name);
    if (it == zones_.end()) {
        // 先腾位再建: 建的过程本身还要一份临时内存, 别让新旧两个大区叠在一起。
        // 包里没有的区只会得到一条错误串, 不占位; 淘汰时也先淘汰这种空条目。
        const BaseNavZone* zone = pack_.findZoneByName(name);
        const bool real = zone != nullptr && zone->triangle_count > 0;
        auto rank = [](const ZoneEntry& e) { return e.zc->valid() ? e.used_at : 0; };
        while (real && zones_.size() >= kZoneCacheMax) {
            auto victim = zones_.begin();
            for (auto z = zones_.begin(); z != zones_.end(); ++z) {
                if (rank(z->second) < rank(victim->second)) {
                    victim = z;
                }
            }
            zones_.erase(victim);
        }
        ZoneEntry e;
        e.zc = std::make_unique<ZoneClean>(pack_, planner_, name);
        if (const FieldsZoneDir* fzd = fields_.findZone(name); fzd != nullptr) {
            e.fz = std::make_unique<FieldsZone>();
            if (!fields_.loadZone(*fzd, *e.fz, e.fields_error)) {
                e.fz.reset();
            }
        }
        else {
            e.fields_error = "旁包里没有这个区";
        }
        it = zones_.emplace(name, std::move(e)).first;
    }
    it->second.used_at = ++zone_clock_;
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
    const std::function<bool()>& should_stop)
{
    const std::lock_guard<std::mutex> lock(mutex_);
    return planLocked(zone_name, start, goal, start_floor_y, goal_floor_y, goal_deck_y, blocked, blocked_points, should_stop);
}

void RecastNavEngine::warm(const std::string& zone_name)
{
    const std::lock_guard<std::mutex> lock(mutex_);
    zoneEntry(zone_name);
}

std::vector<std::vector<uint32_t>> RecastNavEngine::regionsNear(const std::string& zone_name, const std::vector<WorldPoint>& points)
{
    const std::lock_guard<std::mutex> lock(mutex_);
    std::vector<std::vector<uint32_t>> out(points.size());
    const GridZoneDir* gz = grid_.valid() ? grid_.findZone(zone_name) : nullptr;
    if (gz == nullptr || points.empty()) {
        return out;
    }
    const double cs = grid_.cellSize();
    const auto rad = static_cast<int64_t>(std::ceil(kSnapRadius / cs));
    // 一批点常挤在同几张瓦上,而解瓦不带缓存,逐点各解一遍就是这道闸的全部开销。
    std::unordered_map<const GridTileRef*, std::vector<size_t>> by_tile;
    std::vector<int64_t> pcx(points.size());
    std::vector<int64_t> pcy(points.size());
    for (size_t i = 0; i < points.size(); ++i) {
        pcx[i] = static_cast<int64_t>(std::floor(points[i].x / cs));
        pcy[i] = static_cast<int64_t>(std::floor(points[i].y / cs));
        for (const GridTileRef* t : GridTilesInRect(*gz, pcx[i] - rad, pcy[i] - rad, pcx[i] + rad, pcy[i] + rad)) {
            if (t->records != 0) {
                by_tile[t].push_back(i);
            }
        }
    }
    GridTile tile;
    for (const auto& [t, idx] : by_tile) {
        if (!grid_.decodeTile(*t, tile)) {
            continue;
        }
        for (const GridSpanRec& r : tile.rec) {
            // 无高度可传播的补格两个选类器都不会挑;别的一律算进来,宁可多算不可漏。
            if ((r.flags & kGridFlagFill) != 0) {
                continue;
            }
            const int64_t gx = t->gx0 + r.cell % t->nx;
            const int64_t gy = t->gy0 + r.cell / t->nx;
            for (const size_t i : idx) {
                if (std::llabs(gx - pcx[i]) > rad || std::llabs(gy - pcy[i]) > rad) {
                    continue;
                }
                const double dx = (static_cast<double>(gx) + 0.5) * cs - points[i].x;
                const double dy = (static_cast<double>(gy) + 0.5) * cs - points[i].y;
                if (dx * dx + dy * dy <= kSnapRadius * kSnapRadius) {
                    out[i].push_back(r.rid);
                }
            }
        }
    }
    for (std::vector<uint32_t>& v : out) {
        std::sort(v.begin(), v.end());
        v.erase(std::unique(v.begin(), v.end()), v.end());
    }
    return out;
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
    const std::function<bool()>& should_stop)
{
    const double t_all0 = nowMs();
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
    const FieldsZoneDir* fzd = fields_.findZone(zone_name);
    if (fzd == nullptr || ze.fz == nullptr) {
        res.error = "旁包读不出这个区 (" + zone_name + "): " + ze.fields_error;
        return res;
    }
    const FieldsZone* fz = ze.fz.get();
    ZoneClean& zc = *ze.zc;
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
        std::snprintf(buf, sizeof(buf), "端点接不上可走层 (起 %.1fpx / 终 %.1fpx)", zsa, zga);
        res.error = buf;
        return res;
    }

    // 规划范围是这条腿走的那一类占的格。按端点包围盒开窗、失败再逐档扩大的老做法有两处死结:
    // 绕行只要落在盒外就等不到更大的窗口, 连通的腿被判成不连通; 升档又把每次失败的代价乘上档数。
    // 类是可走面的连通片, 类外的格进不了规划图, 所以按类开图与铺满整区拿到的是同一张图 ——
    // 绕多远都在图里, 而不必为区里另外几千个类白铺内存。
    const auto loadPatch = [&](const WorldPoint& a, const WorldPoint& b, double pad, GridPatch& p) {
        const auto r = static_cast<int64_t>(std::ceil(pad / kCS));
        const int64_t ax = static_cast<int64_t>(std::floor(a.x / kCS));
        const int64_t ay = static_cast<int64_t>(std::floor(a.y / kCS));
        const int64_t bx = static_cast<int64_t>(std::floor(b.x / kCS));
        const int64_t by = static_cast<int64_t>(std::floor(b.y / kCS));
        const int64_t gx0 = std::min(ax, bx) - r;
        const int64_t gy0 = std::min(ay, by) - r;
        p.nx = std::max(ax, bx) + r - gx0 + 1;
        p.ny = std::max(ay, by) + r - gy0 + 1;
        p.x0 = static_cast<double>(gx0) * kCS;
        p.y0 = static_cast<double>(gy0) * kCS;
        return loadGridWindow(grid_, *gz, nullptr, nullptr, gx0, gy0, p.nx, p.ny, p.gw);
    };
    // 定类只读起点那一格与终点吸附半径内的格, 两小块解开就够。
    GridPatch ps;
    GridPatch pg;
    if (!loadPatch(start, ss->point, kCS, ps) || (gdk.has_value() && !loadPatch(goal, goal, kSnapRadius, pg))) {
        res.error = "预烘格图解不开";
        return res;
    }
    std::string err;
    uint32_t region = 0;
    int64_t seed_cell = -1;
    if (!pickRegion(ps, pg, start, ss->point, goal, h0, gdk, region, seed_cell, err)) {
        res.error = err;
        return res;
    }
    ps = GridPatch {};
    pg = GridPatch {};

    // 窗口分档。整类窗口是精确答案, 但它的代价与腿长无关; 先按搜索椭圆开小窗, 小窗只在证明了
    // 答案与整类逐位相同时才采信: 场是局部算子, 可信余量盖过它们的依赖半径; 搜索单价恒 ≥1,
    // 弹出的格都在两端为焦点、和为终代价的椭圆里, 椭圆没碰可信区边就碰不到窗外; 吸附与可达域
    // 另与不限可达域的版本对照。任一条不成立就把允许的代价翻倍, 椭圆随之长大。
    // 面积到上限那一档是最后一档: 与窗口大小有关的验收不再做, 结果照采; 可达域相关的差异用
    // 整类洪水补齐; 只有"窗内不通而整类可达"才退到整类窗口。最坏耗时与内存由上限定住。
    constexpr int64_t kTrustMargin = 64; // 可信余量(格), 须盖过净空截断 kEdtCells 与中轴邻域
    constexpr int64_t kLocalMaxCells = 3'000'000;
    constexpr double kRatio0 = 2.0; // 首档允许的 代价/直线 比; 语料里过半的腿一档就过
    constexpr double kRatioStep = 2.0;
    constexpr double kCostSlack = 200.0; // 短腿的固定余裕(格): 吸附偏移、接入链、栅格化
    static_assert(kTrustMargin > kEdtCells + 8);
    const auto cellOf = [](double v) { return static_cast<int64_t>(std::floor(v / kCS)); };
    const int64_t bx0 = std::min({ cellOf(start.x), cellOf(ss->point.x), cellOf(goal.x) });
    const int64_t bx1 = std::max({ cellOf(start.x), cellOf(ss->point.x), cellOf(goal.x) });
    const int64_t by0 = std::min({ cellOf(start.y), cellOf(ss->point.y), cellOf(goal.y) });
    const int64_t by1 = std::max({ cellOf(start.y), cellOf(ss->point.y), cellOf(goal.y) });
    // 整类窗口矩形。场都是局部算子, 依赖半径合起来不到 kFieldHalo 这一圈; 留出它, 类边缘
    // 那几格算出来的场就与在整区图上算的逐位相同。旁包的整类量也是按这个矩形烘的。
    // 类范围是整类量, 每区每类只扫一次瓦, 之后的腿直接查。
    auto zbit = ze.bounds.find(region);
    if (zbit == ze.bounds.end()) {
        zbit = ze.bounds.emplace(region, regionBounds(grid_, *gz, region)).first;
    }
    const ZoneBoundsPx zb = zbit->second;
    if (zb.empty()) {
        res.error = "类内没有格图";
        return res;
    }
    const int64_t rgx0 = zb.x0 - kFieldHalo;
    const int64_t rgy0 = zb.y0 - kFieldHalo;
    const int64_t rnx = zb.x1 - zb.x0 + 1 + 2 * kFieldHalo;
    const int64_t rny = zb.y1 - zb.y0 + 1 + 2 * kFieldHalo;
    // 搜索椭圆的外接矩形: 焦点取起终点, 半长轴 C/2, 半短轴 sqrt(C²-L²)/2, 按腿的方向投影到两轴;
    // 再并上端点包围盒, 外加可信余量。直腿的椭圆是细长条, 窗口跟着细, 不再按腿长开正方形。
    const double fsx = start.x / kCS;
    const double fsy = start.y / kCS;
    const double fgx = goal.x / kCS;
    const double fgy = goal.y / kCS;
    const double leg = std::hypot(fgx - fsx, fgy - fsy);
    const double ang = std::atan2(fgy - fsy, fgx - fsx);
    struct Rect
    {
        int64_t x0 = 0, y0 = 0, nx = 0, ny = 0;
        int64_t cells() const { return nx * ny; }
    };
    const auto rectFor = [&](double cost) {
        const double a = cost / 2.0;
        const double b = std::sqrt(std::max(cost * cost - leg * leg, 0.0)) / 2.0;
        const double c = std::cos(ang);
        const double s = std::sin(ang);
        const double ex = std::sqrt(a * a * c * c + b * b * s * s);
        const double ey = std::sqrt(a * a * s * s + b * b * c * c);
        const double cx = (fsx + fgx) / 2.0;
        const double cy = (fsy + fgy) / 2.0;
        constexpr int64_t halo = kTrustMargin + 8;
        const int64_t x0 = std::min(bx0, static_cast<int64_t>(std::floor(cx - ex))) - halo;
        const int64_t x1 = std::max(bx1, static_cast<int64_t>(std::ceil(cx + ex))) + halo;
        const int64_t y0 = std::min(by0, static_cast<int64_t>(std::floor(cy - ey))) - halo;
        const int64_t y1 = std::max(by1, static_cast<int64_t>(std::ceil(cy + ey))) + halo;
        return Rect { x0, y0, x1 - x0 + 1, y1 - y0 + 1 };
    };
    enum class Kind
    {
        Tentative, // 小窗, 全套验收
        Capped, // 封顶档: 面积到上限, 窗口大小相关的验收关掉
        Full // 整类窗口, 不做验收
    };
    const Rect region_rect { rgx0, rgy0, rnx, rny };
    const auto tierRect = [&](int tier) -> std::pair<Rect, Kind> {
        const double cost = leg * kRatio0 * std::pow(kRatioStep, tier) + kCostSlack;
        Rect r = rectFor(cost);
        const bool covers = r.x0 <= rgx0 && r.y0 <= rgy0 && r.x0 + r.nx >= rgx0 + rnx && r.y0 + r.ny >= rgy0 + rny;
        // 小类: 椭圆盖住整类, 或差不到两成, 直接走整类那一档, 精确且不比小窗贵
        if (region_rect.cells() <= kLocalMaxCells && (covers || r.cells() * 5 >= region_rect.cells() * 4)) {
            return { region_rect, Kind::Full };
        }
        if (r.cells() <= kLocalMaxCells) {
            return { r, Kind::Tentative };
        }
        // 超上限: 在 [直线, cost] 里二分出面积刚好不超的椭圆; 连直线的外接矩形都超就只能用它
        double lo = leg;
        double hi = cost;
        if (rectFor(lo).cells() > kLocalMaxCells) {
            return { rectFor(lo), Kind::Capped };
        }
        for (int i = 0; i < 40; ++i) {
            const double mid = (lo + hi) / 2.0;
            (rectFor(mid).cells() <= kLocalMaxCells ? lo : hi) = mid;
        }
        return { rectFor(lo), Kind::Capped };
    };
    // 可达域、禁步、留墙都从旁包读, 小窗与整类窗口在这三样上逐位相同; 小窗只剩与窗口大小
    // 有关的验收(搜索碰边、场的可信余量), 不过就扩一档, 封顶档的答案照采, 只有窗内不通才退整类。
    std::optional<WindowInfo> info;
    int level = 0; // 椭圆档位
    bool last_resort = false;
    Rect cur;
    Rect prev { 0, 0, 0, 0 };
    Kind kind = Kind::Tentative;
    for (int tier = 0;; ++tier) {
        if (last_resort) {
            cur = region_rect;
            kind = Kind::Full;
        }
        else {
            std::tie(cur, kind) = tierRect(level);
            // 极短腿的椭圆一档长不出一格, 腿长为 0 时永远不长: 同一窗口不重跑, 跳到变大的那一档, 跳不动就整类
            constexpr int kMaxLevel = 64;
            while (kind == Kind::Tentative && tier > 0 && cur.x0 == prev.x0 && cur.y0 == prev.y0 && cur.nx == prev.nx
                   && cur.ny == prev.ny) {
                if (++level > kMaxLevel) {
                    cur = region_rect;
                    kind = Kind::Full;
                    last_resort = true;
                    break;
                }
                std::tie(cur, kind) = tierRect(level);
            }
        }
        prev = cur;
        const bool local = kind != Kind::Full;
        const bool capped = kind == Kind::Capped;
        if (!local && region_rect.cells() > kMaxCells) {
            res.error = "类过大 (" + std::to_string(rnx) + "×" + std::to_string(rny) + " 格)";
            return res;
        }
        const int64_t gx0 = cur.x0;
        const int64_t gy0 = cur.y0;
        const int64_t nx = cur.nx;
        const int64_t ny = cur.ny;
        if (should_stop && should_stop()) {
            res.error = "规划已取消";
            return res;
        }
        const double x0 = static_cast<double>(gx0) * kCS;
        const double y0 = static_cast<double>(gy0) * kCS;
        const double x1 = static_cast<double>(gx0 + nx) * kCS;
        const double y1 = static_cast<double>(gy0 + ny) * kCS;
        const int64_t margin = local ? kTrustMargin : 0;

        const double t_win0 = nowMs();
        info = buildWindow(grid_, *gz, fields_, *fzd, *fz, zc, start, ss->point, goal, h0, region, x0, y0, x1, y1,
            blocked_local, blocked_points, err);
        const double window_ms = nowMs() - t_win0;
        const uint16_t zone_id = zc.zone_id;
        if (!info.has_value()) {
            if (local && !capped) {
                res.debug.tier_notes.push_back(err);
                ++level;
                continue;
            }
            if (local) {
                res.debug.tier_notes.push_back(err + "→整类窗口");
                last_resort = true;
                continue;
            }
            res.error = err.empty() ? "路线失败" : err;
            return res;
        }
        RouteDiag dg;
        dg.margin = margin;
        dg.final = capped;
        auto line = routeWindow(*info, start, goal, dg, gdk, planner_, zone_id);
        // 封顶档就是最终答案, 与整类窗口同一套出口
        if (local && !capped) {
            std::string why;
            if (dg.escalate) {
                why = dg.escalate_why;
            }
            else if (!line.has_value()) {
                why = dg.err.empty() ? "路线失败" : dg.err;
            }
            if (!why.empty()) {
                info.reset();
                res.debug.tier_notes.push_back(why);
                ++level;
                continue;
            }
        }
        // 封顶档关掉了碰边验收, 窗内走不通分不清是真不通还是绕路出了窗: 整类窗口买得起就退整类再下结论
        if (local && capped && !line.has_value() && region_rect.cells() <= kMaxCells) {
            info.reset();
            res.debug.tier_notes.push_back((dg.err.empty() ? std::string("路线失败") : dg.err) + "→整类窗口");
            last_resort = true;
            continue;
        }
        // 失败的腿才最需要诊断: 断开时的缝、窗口范围、各阶段耗时全在这里, 两条出口都得带上。
        const auto dump = [&] {
            res.debug.timing = dg.timing;
            res.debug.timing.window_ms = window_ms;
            res.debug.timing.total_ms = nowMs() - t_all0;
            res.debug.x0 = x0;
            res.debug.y0 = y0;
            res.debug.nx = nx;
            res.debug.ny = ny;
            res.debug.cell_size = kCS;
            res.debug.tier = tier;
            res.debug.topology_cells = std::move(dg.topology_cells);
            res.debug.topology_heights = std::move(dg.topology_heights);
            res.debug.taut_points = std::move(dg.taut_points);
            res.debug.pulled_points = std::move(dg.pulled_points);
            res.debug.assembled_points = std::move(dg.assembled_points);
            res.debug.gap_start = dg.gap_start;
            res.debug.gap_goal = dg.gap_goal;
            res.debug.gap_distance = dg.gap_distance;
            res.debug.warnings = dg.warn;
        };
        if (!line.has_value()) {
            res.error = dg.err.empty() ? "路线失败" : dg.err;
            dump();
            return res;
        }
        if (std::max(dg.snap_start, dg.snap_goal) > kSnapRadius || dg.hop_barrier) {
            // 两端都已过全区核心闸, 端点确实在可走面上, 差的是从起点出发的可达域够不着它。图铺满了
            // 整区, 这就是最终结论。
            char buf[160];
            std::snprintf(
                buf,
                sizeof(buf),
                "从起点走不到终点 (端点%s, 可达区距 起 %.1fpx / 终 %.1fpx)",
                dg.hop_barrier ? "被禁行边隔开" : "在可走面上",
                dg.snap_start,
                dg.snap_goal);
            res.error = buf;
            dump();
            return res;
        }
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
        dump();
        res.debug.planned_points = res.points;
        return res;
    }
}

}
