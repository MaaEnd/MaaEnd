#include "RecastNavRoute.h"

#include "BaseNavGeometry.h"
#include "NavParallel.h"
#include "OccluderPack.h"
#include "RecastNavBake.h"

#include <algorithm>
#include <atomic>
#include <bit>
#include <chrono>
#include <cmath>
#include <functional>
#include <limits>
#include <map>
#include <numeric>
#include <optional>
#include <queue>
#include <set>
#include <tuple>

#include <cstdio>
#include <cstdlib>

#include <MaaFramework/MaaAPI.h>

#if defined(__APPLE__)
#include <malloc/malloc.h>
#elif defined(_WIN32)
#include <malloc.h>
#endif

namespace navmesh::recast
{

namespace
{

double tri_height_of(const PolyMesh& mesh, int32_t t)
{
    const auto& tri = mesh.T[static_cast<size_t>(t)];
    return (MeshHeight(mesh, tri[0]) + MeshHeight(mesh, tri[1]) + MeshHeight(mesh, tri[2])) / 3.0;
}

// 净空截断的格数, 含取整余量, 须与 Clearance 的扫描半径同式
constexpr int64_t kEdtCells = static_cast<int64_t>(kEdtCap / kCS) + 1;

// 台沿下落用的角色尺寸, 米
constexpr double kFallClimb = 0.39;
constexpr double kFallHeight = 2.1;
constexpr double kFallRadius = 0.41;

double tri_height_at(const PolyMesh& mesh, int32_t t, const WorldPoint& p)
{
    const auto& tri = mesh.T[static_cast<size_t>(t)];
    const WorldPoint a = MeshPoint(mesh, tri[0]);
    const WorldPoint b = MeshPoint(mesh, tri[1]);
    const WorldPoint c = MeshPoint(mesh, tri[2]);
    const double det = (b.y - c.y) * (a.x - c.x) + (c.x - b.x) * (a.y - c.y);
    if (det == 0.0) {
        return tri_height_of(mesh, t);
    }
    const double l0 = ((b.y - c.y) * (p.x - c.x) + (c.x - b.x) * (p.y - c.y)) / det;
    const double l1 = ((c.y - a.y) * (p.x - c.x) + (a.x - c.x) * (p.y - c.y)) / det;
    return l0 * MeshHeight(mesh, tri[0]) + l1 * MeshHeight(mesh, tri[1]) + (1.0 - l0 - l1) * MeshHeight(mesh, tri[2]);
}

std::optional<std::pair<WorldPoint, double>> mesh_point_at(const ZoneClean& zc, const WorldPoint& p, double h)
{
    bool have = false;
    std::pair<double, double> bk {};
    std::pair<WorldPoint, double> best {};
    for (const int32_t t : TrisNear(zc.mesh, p, kCS)) {
        if (zc.walkable[static_cast<size_t>(t)] == 0) {
            continue;
        }
        const auto& tri = zc.mesh.T[static_cast<size_t>(t)];
        const double ha = MeshHeight(zc.mesh, tri[0]);
        const double hb = MeshHeight(zc.mesh, tri[1]);
        const double hc = MeshHeight(zc.mesh, tri[2]);
        if (std::fmin(ha, std::fmin(hb, hc)) > h + kDeckBand + 1e-6 || std::fmax(ha, std::fmax(hb, hc)) < h - kDeckBand - 1e-6) {
            continue;
        }
        const WorldPoint q =
            detail::ClosestPointOnTriangle(p, { MeshPoint(zc.mesh, tri[0]), MeshPoint(zc.mesh, tri[1]), MeshPoint(zc.mesh, tri[2]) });
        const double dist = detail::Distance(q, p);
        if (dist > kCS) {
            continue;
        }
        const double y = tri_height_at(zc.mesh, t, q);
        if (std::fabs(y - h) > kDeckBand) {
            continue;
        }
        const std::pair<double, double> k { dist, std::fabs(y - h) };
        if (!have || k < bk) {
            have = true;
            bk = k;
            best = { q, y };
        }
    }
    if (!have) {
        return std::nullopt;
    }
    return best;
}

uint64_t fall_mix(uint64_t x)
{
    x ^= x >> 30;
    x *= 0xbf58476d1ce4e5b9ULL;
    x ^= x >> 27;
    x *= 0x94d049bb133111ebULL;
    return x ^ (x >> 31);
}

struct FallPointKey
{
    int64_t gx;
    int64_t gy;
    float h;

    bool operator==(const FallPointKey& o) const { return gx == o.gx && gy == o.gy && h == o.h; }
};

struct FallEdgeKey
{
    FallPointKey a;
    FallPointKey b;

    bool operator==(const FallEdgeKey& o) const { return a == o.a && b == o.b; }
};

// ±0 相等, 散列前并成 +0
uint64_t fall_hash_point(const FallPointKey& k)
{
    const float h = k.h == 0.0F ? 0.0F : k.h;
    return fall_mix(
        static_cast<uint64_t>(k.gx) * 0x9E3779B97F4A7C15ULL ^ fall_mix(static_cast<uint64_t>(k.gy)) ^ std::bit_cast<uint32_t>(h));
}

struct FallPointHash
{
    size_t operator()(const FallPointKey& k) const { return static_cast<size_t>(fall_hash_point(k)); }
};

struct FallEdgeHash
{
    size_t operator()(const FallEdgeKey& k) const
    {
        return static_cast<size_t>(fall_mix(fall_hash_point(k.a) + 0x9E3779B97F4A7C15ULL * fall_hash_point(k.b)));
    }
};

// 按全局格记, 一次规划的各档窗口共用
struct FallCache
{
    std::unordered_map<FallPointKey, OccluderPoint, FallPointHash> points;
    std::unordered_map<FallEdgeKey, bool, FallEdgeHash> memo;
};

}

// 在遮挡体里查台沿下落那一步有没有被挡, 窗口格号按 gx0/gy0/nx 换成全局格
struct FallGate
{
    std::shared_ptr<const OccluderScene> solid;
    const ZoneClean* zc = nullptr;
    double sx = 0.0;
    double tx = 0.0;
    double sy = 0.0;
    double ty = 0.0;
    int64_t gx0 = 0;
    int64_t gy0 = 0;
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    std::shared_ptr<FallCache> cache;
};

namespace
{

// 遮挡体为空或仿射退化时不另判
std::optional<FallGate> make_fall_gate(
    std::shared_ptr<const OccluderScene> solid,
    const ZoneClean& zc,
    const std::array<float, 4>& tf,
    int64_t gx0,
    int64_t gy0,
    int64_t nx,
    std::shared_ptr<FallCache> cache)
{
    const double sx = tf[0];
    const double tx = tf[1];
    const double sy = tf[2];
    const double ty = tf[3];
    if (solid == nullptr || sx == 0.0 || sy == 0.0) {
        return std::nullopt;
    }
    const double x0 = static_cast<double>(gx0) * kCS;
    const double y0 = static_cast<double>(gy0) * kCS;
    return FallGate {
        .solid = std::move(solid),
        .zc = &zc,
        .sx = sx,
        .tx = tx,
        .sy = sy,
        .ty = ty,
        .gx0 = gx0,
        .gy0 = gy0,
        .x0 = x0,
        .y0 = y0,
        .nx = nx,
        .cache = std::move(cache),
    };
}

OccluderPoint fall_point(FallGate* gate, int64_t c, const FallPointKey& pk)
{
    if (const auto hit = gate->cache->points.find(pk); hit != gate->cache->points.end()) {
        return hit->second;
    }
    const WorldPoint p { gate->x0 + (static_cast<double>(c % gate->nx) + 0.5) * kCS,
                         gate->y0 + (static_cast<double>(c / gate->nx) + 0.5) * kCS };
    const auto m = mesh_point_at(*gate->zc, p, static_cast<double>(pk.h)).value_or(std::make_pair(p, static_cast<double>(pk.h)));
    const OccluderPoint q { .x = (m.first.x - gate->tx) / gate->sx, .y = m.second, .z = (gate->ty - m.first.y) / gate->sy };
    gate->cache->points.emplace(pk, q);
    return q;
}

struct WindowInfo
{
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
    Mask lay;
    Mask core;
    Grid<float> dist; // 封缝净空, 禁区内清零
    Mask medial;
    EdgeBits step_edges;
    StepBarrier sev;
    std::vector<WorldPoint> segA;
    std::vector<WorldPoint> segB;
    double h0 = 0.0;
    double fall_r = 0.0; // 角色半径 px, 为 0 时落点不外推
    SpanTable st3;
    std::vector<uint8_t> vis3;
    std::vector<uint8_t> reach3;
    JumpEdges links_cell;
    JumpEdges links_span;
    JumpEdges links_span_rev;
    uint32_t links_dropped = 0; // 两端在窗内但选不出落脚 span 的条数
};

struct RouteDiag
{
    std::string err;
    std::vector<std::string> warn;
    std::vector<double> clearance;
    std::vector<double> height;      // 逐点所在面的高度, 逐层追踪失败时清空
    std::vector<double> span_height; // 逐点取自搜索选中的面, 对不回父链时为空
    std::vector<size_t> waypoints;
    std::vector<DropLanding> drops;
    bool crossed_barrier = false;
    bool hop_barrier = false; // 端点接线的那一跳跨了禁行边
    double snap_start = 0.0;
    double snap_goal = 0.0;

    // margin > 0 是端点附近的小窗, 结果可能被窗口边切过时置 escalate, margin = 0 是整类窗口
    int64_t margin = 0;
    // 已是最大的小窗, 碰边类验收关掉
    bool final = false;
    bool escalate = false;
    std::string escalate_why;

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

double now_ms()
{
    return std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now().time_since_epoch()).count();
}

// 格号是窗口格号, 同格记录按 (类号, 高) 排好
struct GridWindow
{
    std::vector<GridSpanRec> rec;
    std::vector<int32_t> head; // 逐格的记录链表头, 无记录为 -1
    std::vector<int32_t> next;
    // 与 rec 逐条对位, 不带场数据解时为空, 封缝净空已写回 rec.clr
    std::vector<uint32_t> scc;
    std::vector<uint8_t> steps2;
    std::vector<uint8_t> seg; // bit0/1 是立面段, bit7 是中轴
    std::vector<uint8_t> tax;
};

constexpr uint8_t kGwMedialBit = 0x80U;

// fz 为空时只解格图, opn 命中的死记录并入标志位并取较大净空
bool load_grid_window(
    GridWindow* window,
    const GridPack& gp,
    const GridZoneDir& gz,
    const FieldsZoneDir* fz,
    const FieldsOpenRec* opn,
    size_t n_opn,
    int64_t wgx0,
    int64_t wgy0,
    int64_t nx,
    int64_t ny)
{
    GridWindow& out = *window;
    const std::vector<const GridTileRef*> tiles = GridTilesInRect(gz, wgx0, wgy0, wgx0 + nx - 1, wgy0 + ny - 1);
    // 每块瓦按记录数上界独占一段写区, 收工按瓦序压紧, rec 次序与线程数无关
    const bool with_fields = fz != nullptr;
    const auto nt = static_cast<int64_t>(tiles.size());
    const size_t nw = NavWorkerCountForBlocks(nt);
    std::vector<size_t> beg(static_cast<size_t>(nt) + 1, 0);
    for (int64_t i = 0; i < nt; ++i) {
        beg[static_cast<size_t>(i) + 1] = beg[static_cast<size_t>(i)] + tiles[static_cast<size_t>(i)]->records;
    }
    const size_t cap = beg.back();
    out.rec.assign(cap, GridSpanRec {});
    if (with_fields) {
        out.scc.assign(cap, 0);
        out.steps2.assign(cap, 0);
        out.seg.assign(cap, 0);
        out.tax.assign(cap, 0);
    }
    std::vector<int64_t> order(static_cast<size_t>(nt));
    std::iota(order.begin(), order.end(), int64_t { 0 });
    std::stable_sort(order.begin(), order.end(), [&](int64_t a, int64_t b) {
        return tiles[static_cast<size_t>(a)]->records > tiles[static_cast<size_t>(b)]->records;
    });
    std::vector<size_t> cnt(static_cast<size_t>(nt), 0);
    std::atomic<int64_t> next { 0 };
    std::atomic<bool> ok { true };
    ParallelChunks(static_cast<int64_t>(nw), nw, [&](size_t, int64_t, int64_t) {
        GridTile tile;
        FieldsTile ft;
        for (int64_t q = next.fetch_add(1); q < nt && ok.load(); q = next.fetch_add(1)) {
            const int64_t i = order[static_cast<size_t>(q)];
            const GridTileRef* t = tiles[static_cast<size_t>(i)];
            if (t->records == 0) {
                continue;
            }
            size_t at = beg[static_cast<size_t>(i)];
            if (!DecodeGridPackTile(&tile, gp, *t)) {
                ok.store(false);
                return;
            }
            if (with_fields) {
                // 场数据瓦表与格图同区同序, 记录逐条对位
                const auto ti = static_cast<size_t>(t - gz.tiles.data());
                if (ti >= fz->tiles.size() || !DecodeFieldsTile(&ft, fz->tiles[ti]) || ft.scc.size() != tile.rec.size()) {
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
                    // 封缝只会让净空变小, 差值超过原净空说明数据有误
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
                // 须在扣掉封缝净空之后取较大值, 否则会被压回 0
                if (n_opn != 0 && (r.flags & kGridFlagCore) == 0) {
                    if (const FieldsOpenRec* o =
                            FindOpen(opn, n_opn, static_cast<int32_t>(t->gx0 + ix), static_cast<int32_t>(t->gy0 + iy), r.h)) {
                        r.flags |= o->flags;
                        r.clr = std::max(r.clr, o->clr);
                    }
                }
                out.rec[at++] = r;
            }
            cnt[static_cast<size_t>(i)] = at - beg[static_cast<size_t>(i)];
        }
    });
    if (!ok.load()) {
        out.rec.clear();
        return false;
    }
    size_t kept = 0;
    for (size_t w = 0; w < static_cast<size_t>(nt); ++w) {
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

// 只在层带内的可走 span 里挑, 贴崖站立时起点格常只有被墙盖掉的 span
int64_t pick_start_rec(const GridWindow& gw, int64_t nx, int64_t ny, int64_t gcx, int64_t gcy, double h0)
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
                if ((r.flags & kGridFlagWalk) == 0 || (r.flags & (kGridFlagGhost | kGridFlagFill)) != 0) {
                    continue;
                }
                const double hd = std::fabs(static_cast<double>(r.h) - h0);
                if (hd > kClimb) {
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

int64_t pick_deck_rec(const GridWindow& gw, int64_t nx, int64_t ny, int64_t gcx, int64_t gcy, double deck)
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

// 两点到最近核心格的距离 px, 搜不到时返回搜索半径作下界
std::array<double, 2> core_anchor_px(const GridPack& gp, const GridZoneDir& gz, const std::array<WorldPoint, 2>& pts)
{
    const double cs = gp.cell_size;
    const double reach = kSnapRadius * 2.0;
    const auto rad = static_cast<int64_t>(std::ceil(reach / cs));
    std::array<int64_t, 2> cx {};
    std::array<int64_t, 2> cy {};
    std::vector<const GridTileRef*> tiles;
    std::vector<uint8_t> owners;
    for (size_t k = 0; k < 2; ++k) {
        cx[k] = static_cast<int64_t>(std::floor(pts[k].x / cs));
        cy[k] = static_cast<int64_t>(std::floor(pts[k].y / cs));
        for (const GridTileRef* t : GridTilesInRect(gz, cx[k] - rad, cy[k] - rad, cx[k] + rad, cy[k] + rad)) {
            if (t->records == 0) {
                continue;
            }
            const auto it = std::find(tiles.begin(), tiles.end(), t);
            if (it == tiles.end()) {
                tiles.push_back(t);
                owners.push_back(static_cast<uint8_t>(1U << k));
            }
            else {
                owners[static_cast<size_t>(it - tiles.begin())] |= static_cast<uint8_t>(1U << k);
            }
        }
    }
    const auto nt = static_cast<int64_t>(tiles.size());
    std::vector<std::array<int64_t, 2>> best(tiles.size(), { -1, -1 });
    ParallelChunks(nt, NavWorkerCountForBlocks(nt), [&](size_t, int64_t lo, int64_t hi) {
        GridTile tile;
        for (int64_t i = lo; i < hi; ++i) {
            const GridTileRef* t = tiles[static_cast<size_t>(i)];
            if (!DecodeGridPackTile(&tile, gp, *t)) {
                continue;
            }
            const uint8_t own = owners[static_cast<size_t>(i)];
            std::array<int64_t, 2>& b = best[static_cast<size_t>(i)];
            for (const GridSpanRec& r : tile.rec) {
                if ((r.flags & kGridFlagCore) == 0) {
                    continue;
                }
                const int64_t ix = r.cell % t->nx;
                const int64_t iy = r.cell / t->nx;
                if (ix < t->px0 || ix > t->px1 || iy < t->py0 || iy > t->py1) {
                    continue;
                }
                for (size_t k = 0; k < 2; ++k) {
                    if ((own & (1U << k)) == 0) {
                        continue;
                    }
                    const int64_t dx = t->gx0 + ix - cx[k];
                    const int64_t dy = t->gy0 + iy - cy[k];
                    const int64_t d = dx * dx + dy * dy;
                    if (b[k] < 0 || d < b[k]) {
                        b[k] = d;
                    }
                }
            }
        }
    });
    std::array<double, 2> out {};
    for (size_t k = 0; k < 2; ++k) {
        int64_t m = -1;
        for (const std::array<int64_t, 2>& b : best) {
            if (b[k] >= 0 && (m < 0 || b[k] < m)) {
                m = b[k];
            }
        }
        out[k] = m < 0 ? reach : std::sqrt(static_cast<double>(m)) * cs;
    }
    return out;
}

// 原点落在全局格线上
struct GridPatch
{
    GridWindow gw;
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
};

int64_t seed_rec(int64_t* gx, int64_t* gy, const GridPatch& ps, const WorldPoint& s, const WorldPoint& s_snap, double h0)
{
    *gx = static_cast<int64_t>((s.x - ps.x0) / kCS);
    *gy = static_cast<int64_t>((s.y - ps.y0) / kCS);
    const int64_t rec = pick_start_rec(ps.gw, ps.nx, ps.ny, *gx, *gy, h0);
    if (rec >= 0) {
        return rec;
    }
    *gx = static_cast<int64_t>((s_snap.x - ps.x0) / kCS);
    *gy = static_cast<int64_t>((s_snap.y - ps.y0) / kCS);
    return pick_start_rec(ps.gw, ps.nx, ps.ny, *gx, *gy, h0);
}

// 起点吸到 sp 后定出的种子格里有 region 的非填充面
bool seeds_in_region(const GridPatch& ps, uint32_t region, const WorldPoint& start, const WorldPoint& sp, double h)
{
    int64_t gx = 0;
    int64_t gy = 0;
    const int64_t rec = seed_rec(&gx, &gy, ps, start, sp, h);
    if (rec < 0) {
        return false;
    }
    const auto cell = static_cast<size_t>(ps.gw.rec[static_cast<size_t>(rec)].cell);
    for (int64_t i = ps.gw.head[cell]; i >= 0; i = ps.gw.next[static_cast<size_t>(i)]) {
        const GridSpanRec& r = ps.gw.rec[static_cast<size_t>(i)];
        if (r.rid == region && (r.flags & kGridFlagFill) == 0) {
            return true;
        }
    }
    return false;
}

std::optional<ZoneSnapHit>
    snap_seeding_region(const ZoneClean& zc, const GridPatch& ps, uint32_t region, const WorldPoint& start, std::optional<double> floor_y)
{
    for (const ZoneSnapHit& hit : ZoneSnapCandidates(zc, start, kSnapRadius, floor_y)) {
        if (seeds_in_region(ps, region, start, hit.point, tri_height_of(zc.mesh, hit.tri))) {
            return hit;
        }
    }
    return std::nullopt;
}

struct RegionSeed
{
    uint32_t region = 0;
    int64_t gx = 0;
    int64_t gy = 0;
    double h = 0.0;
};

// 终点声明了面时改由终点定类, 免得起点二维吸附落在屋顶把线拉到别层
std::optional<RegionSeed> pick_region(
    std::string* err,
    const GridPatch& ps,
    const GridPatch& pg,
    const WorldPoint& s,
    const WorldPoint& s_snap,
    const WorldPoint& g,
    double h0,
    std::optional<double> goal_deck)
{
    int64_t gx = 0;
    int64_t gy = 0;
    const int64_t start_rec = seed_rec(&gx, &gy, ps, s, s_snap, h0);
    if (start_rec < 0) {
        *err = "起点附近无可走体素 (gx=" + std::to_string(gx) + ",gy=" + std::to_string(gy) + ")";
        return std::nullopt;
    }
    const GridSpanRec& sr = ps.gw.rec[static_cast<size_t>(start_rec)];
    RegionSeed seed {
        .region = sr.rid,
        .gx = std::llround(ps.x0 / kCS) + sr.cell % ps.nx,
        .gy = std::llround(ps.y0 / kCS) + sr.cell / ps.nx,
        .h = static_cast<double>(sr.h),
    };
    if (goal_deck.has_value()) {
        const int64_t deck_rec = pick_deck_rec(
            pg.gw,
            pg.nx,
            pg.ny,
            static_cast<int64_t>((g.x - pg.x0) / kCS),
            static_cast<int64_t>((g.y - pg.y0) / kCS),
            *goal_deck);
        if (deck_rec < 0) {
            *err = "终点附近没有声明的面 (deck=" + std::to_string(*goal_deck) + ")";
            return std::nullopt;
        }
        seed.region = pg.gw.rec[static_cast<size_t>(deck_rec)].rid;
    }
    return seed;
}

ZoneBoundsPx region_bounds(const GridPack& gp, const GridZoneDir& gz, uint32_t region)
{
    ZoneBoundsPx b;
    b.x0 = std::numeric_limits<int64_t>::max();
    b.y0 = std::numeric_limits<int64_t>::max();
    b.x1 = std::numeric_limits<int64_t>::min();
    b.y1 = std::numeric_limits<int64_t>::min();
    std::vector<const GridTileRef*> hits;
    {
        const auto ntl = static_cast<int64_t>(gz.tiles.size());
        std::vector<uint8_t> hit(static_cast<size_t>(ntl), 0);
        ParallelChunks(ntl, NavWorkerCountForBlocks(ntl), [&](size_t, int64_t lo, int64_t hi) {
            std::vector<uint32_t> rids;
            for (int64_t i = lo; i < hi; ++i) {
                const GridTileRef& t = gz.tiles[static_cast<size_t>(i)];
                if (t.records == 0) {
                    continue;
                }
                if (GridPackTileRegions(&rids, gp, t) && std::find(rids.begin(), rids.end(), region) != rids.end()) {
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
    const auto nh = static_cast<int64_t>(hits.size());
    const size_t nw = NavWorkerCountForBlocks(nh);
    std::vector<ZoneBoundsPx> part(nw, b);
    ParallelChunks(nh, nw, [&](size_t w, int64_t lo, int64_t hi) {
        ZoneBoundsPx& p = part[w];
        GridTile tile;
        for (int64_t i = lo; i < hi; ++i) {
            const GridTileRef& t = *hits[static_cast<size_t>(i)];
            if (!DecodeGridPackTile(&tile, gp, t)) {
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

int64_t seed_span(const SpanTable& st, const std::vector<uint8_t>& vis, int64_t cell, double h0)
{
    const int64_t sj = OccupiedIndex(st, cell);
    if (sj < 0) {
        return -1;
    }
    int64_t seed = -1;
    float best = 0.0F;
    const int64_t b = CellSpanStart(st, sj);
    for (int64_t k = 0, kn = CellSpanCount(st, sj); k < kn; ++k) {
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

// 格里与 h 相差 kMcHBand 内、可见的 span 中最近的一个, 没有返回 -1
int64_t link_span(const WindowInfo& info, int64_t cell, float h)
{
    const int64_t j = OccupiedIndex(info.st3, cell);
    if (j < 0) {
        return -1;
    }
    int64_t best = -1;
    float bd = 0.0F;
    for (int64_t k = CellSpanStart(info.st3, j), kn = k + CellSpanCount(info.st3, j); k < kn; ++k) {
        if (info.vis3[static_cast<size_t>(k)] == 0) {
            continue;
        }
        const float dh = std::fabs(info.st3.sp_h[static_cast<size_t>(k)] - h);
        if (static_cast<double>(dh) > kMcHBand) {
            continue;
        }
        if (best < 0 || dh < bd) {
            best = k;
            bd = dh;
        }
    }
    return best;
}

std::optional<WindowInfo> build_window(
    std::string* err,
    const GridPack& gp,
    const GridZoneDir& gz,
    const FieldsPack& fp,
    const FieldsZoneDir& fzd,
    const FieldsZone& fz,
    const ZoneClean& zc,
    const RegionSeed& seed,
    double h0,
    double x0,
    double y0,
    double x1,
    double y1,
    const std::vector<NoGoPoly>* no_go,
    const std::vector<BaseNavNoGoDisc>& no_go_discs)
{
    const int64_t seed_gx = seed.gx;
    const int64_t seed_gy = seed.gy;
    const double seed_h = seed.h;
    const uint32_t region = seed.region;
    const int64_t nx = static_cast<int64_t>(std::ceil((x1 - x0) / kCS));
    const int64_t ny = static_cast<int64_t>(std::ceil((y1 - y0) / kCS));
    // 窗口原点已对齐到全局格线
    const int64_t wgx0 = std::llround(x0 / kCS);
    const int64_t wgy0 = std::llround(y0 / kCS);

    BakedWalls walls = BakeWalls(zc, x0, y0, nx, ny);
    GridPatch pw;
    pw.x0 = x0;
    pw.y0 = y0;
    pw.nx = nx;
    pw.ny = ny;
    GridWindow& gw = pw.gw;
    const std::span<const FieldsOpenRec> opn = FieldsOpensOfZone(fp, zc.zone_id);
    if (!load_grid_window(&gw, gp, gz, &fzd, opn.data(), opn.size(), wgx0, wgy0, nx, ny)) {
        *err = "预烘格图或旁包解不开";
        return std::nullopt;
    }

    const int64_t cell0 = (seed_gy - wgy0) * nx + (seed_gx - wgx0);
    if (seed_gx < wgx0 || seed_gx >= wgx0 + nx || seed_gy < wgy0 || seed_gy >= wgy0 + ny) {
        *err = "起点种子格落在窗外";
        return std::nullopt;
    }
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
    info.medial = Mask(nx, ny, 0);
    ResetEdgeBits(&info.step_edges, nx, ny);
    Grid<float> lh(nx, ny, std::numeric_limits<float>::quiet_NaN());
    std::vector<uint8_t> stepbits(static_cast<size_t>(nx * ny), 0);
    std::vector<uint8_t> stepbits2(static_cast<size_t>(nx * ny), 0);
    std::vector<uint8_t> segbits(static_cast<size_t>(nx * ny), 0);
    std::vector<int32_t> sp_cell;
    std::vector<float> sp_h;
    std::vector<uint32_t> sp_scc;
    sp_cell.reserve(gw.rec.size());
    sp_h.reserve(gw.rec.size());
    sp_scc.reserve(gw.rec.size());
    info.vis3.reserve(gw.rec.size());
    // 别的类的 span 也要留, 层判据看整列, 少一层就会从楼板底下穿过去
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
            info.dist.v[cell] = GridClearance(r.clr);
            stepbits[cell] |= r.steps;
            stepbits2[cell] |= gw.steps2[ri];
            segbits[cell] |= static_cast<uint8_t>(gw.seg[ri] & ~kGwMedialBit);
            if ((gw.seg[ri] & kGwMedialBit) != 0) {
                info.medial.v[cell] = 1;
            }
            // 方向 i 正向在 bit 2i, 反向在 bit 2i+1, 反向位记在对端格上, 只能逐位 set
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
                        SetEdgeBit(&info.step_edges, static_cast<int64_t>(cell), by * nx + bx);
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
    pw.gw = GridWindow();

    std::vector<WorldPoint> wP0;
    std::vector<WorldPoint> wP1;
    for (size_t i = 0; i < walls.p0.size(); ++i) {
        bool known = false;
        const bool keep = FieldsZoneWallKeep(&known, fz, walls.tri[i], walls.k[i], region);
        if (!known) {
            *err = "旁包留墙表没有这条边 (tri " + std::to_string(walls.tri[i]) + ")";
            return std::nullopt;
        }
        if (keep) {
            wP0.push_back(walls.p0[i]);
            wP1.push_back(walls.p1[i]);
        }
    }
    walls = BakedWalls();
    // 净空也要清零, 否则拐角能从禁区上空拉直过去
    if (no_go != nullptr) {
        StampNoGo(*no_go, x0, y0, nx, ny, lh, info.core, info.lay, info.dist);
    }
    lh = Grid<float>();

    // 运行期禁区不带高度, 窗口层已按起点层高筛过
    for (const BaseNavNoGoDisc& disc : no_go_discs) {
        const int64_t pr = static_cast<int64_t>(std::ceil(disc.radius / kCS));
        const int64_t cgx = static_cast<int64_t>(std::floor((disc.center.x - x0) / kCS));
        const int64_t cgy = static_cast<int64_t>(std::floor((disc.center.y - y0) / kCS));
        for (int64_t by = std::max<int64_t>(cgy - pr, 0); by <= std::min<int64_t>(cgy + pr, ny - 1); ++by) {
            for (int64_t bx = std::max<int64_t>(cgx - pr, 0); bx <= std::min<int64_t>(cgx + pr, nx - 1); ++bx) {
                const double px = x0 + (static_cast<double>(bx) + 0.5) * kCS;
                const double py = y0 + (static_cast<double>(by) + 0.5) * kCS;
                if (std::hypot(px - disc.center.x, py - disc.center.y) > disc.radius) {
                    continue;
                }
                const size_t cell = static_cast<size_t>(by * nx + bx);
                info.core.v[cell] = 0;
                info.lay.v[cell] = 0;
                info.dist.v[cell] = 0.0F;
            }
        }
    }

    const int64_t nc = nx * ny;
    info.h0 = h0;
    ResetEdgeBits(&info.sev.steps, nx, ny);
    info.st3 = PackSpans(std::move(sp_cell), std::move(sp_h), &info.vis3, &sp_scc);

    // 只有正交两向出线段, 对角步不挡视线
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
                SetEdgeBit(&info.sev.steps, c, b);
            }
            if ((bits2 & 0x02U) != 0) {
                SetEdgeBit(&info.sev.steps, b, c);
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
    const int64_t seed3 = seed_span(info.st3, info.vis3, cell0, seed_h);
    if (seed3 < 0) {
        *err = "起点格没有与终点同类的面";
        return std::nullopt;
    }
    // 跳边单价不得低于 1, 否则启发式不可采纳
    {
        const std::span<const FieldsLinkRec> lr = FieldsLinksOfZone(fp, zc.zone_id);
        for (size_t i = 0; i < lr.size(); ++i) {
            const FieldsLinkRec& r = lr[i];
            if (r.valid != 3) {
                continue;
            }
            const int64_t lx = r.lo.gx - wgx0, ly = r.lo.gy - wgy0;
            const int64_t hx = r.hi.gx - wgx0, hy = r.hi.gy - wgy0;
            if (lx < 0 || lx >= nx || ly < 0 || ly >= ny || hx < 0 || hx >= nx || hy < 0 || hy >= ny) {
                continue;
            }
            const int64_t cl = ly * nx + lx, ch = hy * nx + hx;
            const int64_t sl = link_span(info, cl, r.lo.h), sh = link_span(info, ch, r.hi.h);
            if (sl < 0 || sh < 0) {
                if (r.lo.rid == region && r.hi.rid == region) {
                    ++info.links_dropped;
                }
                continue;
            }
            const float cost = std::max(r.cost_modifier, 1.0F)
                               * static_cast<float>(std::hypot(static_cast<double>(hx - lx), static_cast<double>(hy - ly)));
            AddJumpEdge(&info.links_cell, cl, ch, cost);
            AddJumpEdge(&info.links_span, sl, sh, cost);
            AddJumpEdge(&info.links_span_rev, sh, sl, cost);
            if (r.bidirectional != 0) {
                AddJumpEdge(&info.links_cell, ch, cl, cost);
                AddJumpEdge(&info.links_span, sh, sl, cost);
                AddJumpEdge(&info.links_span_rev, sl, sh, cost);
            }
        }
        SortJumpEdges(&info.links_cell);
        SortJumpEdges(&info.links_span);
        SortJumpEdges(&info.links_span_rev);
    }
    {
        const std::vector<uint8_t> hit = FieldsZoneReachFrom(fz, region, sp_scc[static_cast<size_t>(seed3)]);
        if (hit.empty()) {
            *err = "旁包分量图里没有起点面所在的分量";
            return std::nullopt;
        }
        info.reach3.assign(info.vis3.size(), 0);
        for (size_t sid = 0; sid < info.vis3.size(); ++sid) {
            if (info.vis3[sid] == 0) {
                continue;
            }
            const uint32_t comp = sp_scc[sid];
            if (comp == 0 || comp >= hit.size()) {
                *err = "旁包分量号越界";
                return std::nullopt;
            }
            info.reach3[sid] = hit[comp];
        }
        // 分量图不含跳边, 沿跳边把对面分量能到的并进来直到不再增长
        for (bool grew = !JumpEdgesEmpty(info.links_span); grew;) {
            grew = false;
            for (const JumpEdge& e : info.links_span.e) {
                if (info.reach3[static_cast<size_t>(e.src)] == 0 || info.reach3[static_cast<size_t>(e.dst)] != 0) {
                    continue;
                }
                const std::vector<uint8_t> more = FieldsZoneReachFrom(fz, region, sp_scc[static_cast<size_t>(e.dst)]);
                for (size_t sid = 0; sid < info.vis3.size(); ++sid) {
                    const uint32_t comp = sp_scc[sid];
                    if (info.vis3[sid] != 0 && info.reach3[sid] == 0 && comp < more.size() && more[comp] != 0) {
                        info.reach3[sid] = 1;
                        grew = true;
                    }
                }
            }
        }
    }

    info.segA = std::move(wP0);
    info.segA.insert(info.segA.end(), info.sev.p0.begin(), info.sev.p0.end());
    info.segB = std::move(wP1);
    info.segB.insert(info.segB.end(), info.sev.p1.begin(), info.sev.p1.end());
    info.sev.p0 = {};
    info.sev.p1 = {};
    return info;
}

// 只查面高度连续会顺着叠层的下一层走通, 所以还要查挡线
void pull_waypoints(
    RouteDiag* dg,
    Blockers* blk,
    const std::vector<WorldPoint>& pts,
    const BaseNavPlanner& pl,
    const BaseNavPack& pack,
    uint16_t zid)
{
    if (pts.size() < 2) {
        return;
    }
    // 一次拉直最多吞掉的顶点数, 只为限制成本
    constexpr size_t kMaxPullSpan = 64;
    const size_t anchor = pts.size() - 1;
    size_t cursor = 0;
    while (cursor + 1 < anchor) {
        // 捷径不得比它吞掉的最窄处更窄
        double swallowed = std::numeric_limits<double>::infinity();
        const std::optional<double> seed = cursor < dg->height.size() ? std::optional<double>(dg->height[cursor]) : std::nullopt;
        size_t reach = cursor;
        size_t reach_limit = std::min(anchor, cursor + kMaxPullSpan);
        // 台沿下落的落点必须留成航点, 控制端靠它判下落
        for (const DropLanding& drop : dg->drops) {
            if (drop.index > cursor && drop.index < reach_limit) {
                reach_limit = drop.index;
            }
        }
        while (reach < reach_limit) {
            const WorldPoint& a = pts[cursor];
            const WorldPoint& c = pts[reach + 1];
            const double required = std::isfinite(swallowed) ? swallowed : 0.0;
            if (!IsRouteSegmentDrivable(pl, pack, zid, a, c, required, seed) || SegmentBlocked(blk, a, c)) {
                break;
            }
            swallowed = std::min(swallowed, reach + 1 < dg->clearance.size() ? dg->clearance[reach + 1] : 0.0);
            ++reach;
        }
        if (reach == cursor) {
            ++reach;
        }
        if (reach >= anchor) {
            break;
        }
        dg->waypoints.push_back(reach);
        cursor = reach;
    }
    dg->waypoints.push_back(anchor);
}

std::optional<double> span_near(const LayerOracle& lyo, int64_t gx, int64_t gy, double h)
{
    const SpanTable& st = *lyo.st;
    if (gx < 0 || gy < 0 || gx >= lyo.nx || gy >= lyo.ny) {
        return std::nullopt;
    }
    const int64_t j = OccupiedIndex(st, gy * lyo.nx + gx);
    if (j < 0) {
        return std::nullopt;
    }
    std::optional<double> best;
    for (int64_t k = CellSpanStart(st, j), ke = k + CellSpanCount(st, j); k < ke; ++k) {
        const double v = static_cast<double>(st.sp_h[static_cast<size_t>(k)]);
        if (std::fabs(v - h) <= kQH && (!best.has_value() || std::fabs(v - h) < std::fabs(*best - h))) {
            best = v;
        }
    }
    return best;
}

std::pair<int64_t, int64_t> cell_of(const LayerOracle& lyo, const WorldPoint& p)
{
    return std::make_pair(static_cast<int64_t>(std::floor((p.x - lyo.x0) / kCS)), static_cast<int64_t>(std::floor((p.y - lyo.y0) / kCS)));
}

WorldPoint cell_centre(const LayerOracle& lyo, int64_t gx, int64_t gy)
{
    return WorldPoint { lyo.x0 + (static_cast<double>(gx) + 0.5) * kCS, lyo.y0 + (static_cast<double>(gy) + 0.5) * kCS };
}

bool upper_at(const LayerOracle& lyo, int64_t gx, int64_t gy, double h, double h_up)
{
    const SpanTable& st = *lyo.st;
    if (gx < 0 || gy < 0 || gx >= lyo.nx || gy >= lyo.ny) {
        return false;
    }
    const int64_t j = OccupiedIndex(st, gy * lyo.nx + gx);
    if (j < 0) {
        return false;
    }
    for (int64_t k = CellSpanStart(st, j), ke = k + CellSpanCount(st, j); k < ke; ++k) {
        const double v = static_cast<double>(st.sp_h[static_cast<size_t>(k)]);
        if (v > h + kClimb && v <= h_up + kClimb) {
            return true;
        }
    }
    return false;
}

double upper_dist(const LayerOracle& lyo, const WorldPoint& p, double h, double h_up, double reach)
{
    const auto [cx, cy] = cell_of(lyo, p);
    const auto rc = static_cast<int64_t>(std::ceil(reach / kCS));
    double best = reach;
    for (int64_t gy = cy - rc; gy <= cy + rc; ++gy) {
        for (int64_t gx = cx - rc; gx <= cx + rc; ++gx) {
            const double qx = std::clamp(p.x, lyo.x0 + static_cast<double>(gx) * kCS, lyo.x0 + static_cast<double>(gx + 1) * kCS);
            const double qy = std::clamp(p.y, lyo.y0 + static_cast<double>(gy) * kCS, lyo.y0 + static_cast<double>(gy + 1) * kCS);
            const double d = std::hypot(qx - p.x, qy - p.y);
            if (d < best && upper_at(lyo, gx, gy, h, h_up)) {
                best = d;
            }
        }
    }
    return best;
}

// 落点挪到下层离上层可走面四个角色半径处
void push_drop_landings(
    std::vector<WorldPoint>* points,
    std::vector<double>* heights,
    RouteDiag* dg,
    const Grid<float>& dist,
    double radius,
    const LayerOracle& lyo)
{
    std::vector<WorldPoint>& pts = *points;
    std::vector<double>& hz = *heights;
    constexpr double kStep = kCS / 4.0;
    for (size_t i = 1; i + 1 < pts.size(); ++i) {
        if (hz[i - 1] - hz[i] <= kClimb || std::hypot(pts[i].x - pts[i - 1].x, pts[i].y - pts[i - 1].y) <= 0.0) {
            continue;
        }
        // 上层断开处比落点高出一步以上才算台沿, 一路连着的是坡
        double h_up = hz[i - 1];
        bool edge = false;
        std::vector<double> still_up;
        size_t li = i;
        for (; li + 1 < pts.size(); ++li) {
            const WorldPoint take = pts[li - 1];
            const WorldPoint land = pts[li];
            const double len = std::hypot(land.x - take.x, land.y - take.y);
            const double ux = len > 0.0 ? (land.x - take.x) / len : 0.0;
            const double uy = len > 0.0 ? (land.y - take.y) / len : 0.0;
            for (double t = 0.0; t < len; t += kStep) {
                const auto [gx, gy] = cell_of(lyo, { take.x + ux * t, take.y + uy * t });
                const auto v = span_near(lyo, gx, gy, h_up);
                if (!v.has_value()) {
                    edge = true;
                    break;
                }
                h_up = *v;
            }
            if (edge || h_up - hz[li] <= kClimb) {
                break;
            }
            still_up.push_back(h_up);
        }
        if (!edge || h_up - hz[li] <= kClimb) {
            continue;
        }
        for (size_t k = i; k < li; ++k) {
            hz[k] = still_up[k - i];
        }
        i = li;
        const WorldPoint land = pts[i];
        // 直走只查下层逐格连通, 平面挡线在叠层处会连下层的弦一起挡
        double h_low = hz[i];
        WorldPoint at = land;
        double at_clr = -1.0;
        const WorldPoint next = pts[i + 1];
        const auto h_next = static_cast<float>(hz[i + 1]);
        {
            const auto [lx, ly] = cell_of(lyo, land);
            using Node = std::tuple<double, int64_t, int64_t, double>;
            std::priority_queue<Node, std::vector<Node>, std::greater<>> open;
            std::set<std::pair<int64_t, int64_t>> seen { { lx, ly } };
            if (const auto v = span_near(lyo, lx, ly, hz[i])) {
                open.emplace(0.0, lx, ly, *v);
            }
            while (!open.empty()) {
                const auto [d, gx, gy, h] = open.top();
                open.pop();
                const WorldPoint c = d == 0.0 ? land : cell_centre(lyo, gx, gy);
                if (d == 0.0 || LayerStepOk(lyo, c, next, static_cast<float>(h), h_next)) {
                    const double clr = upper_dist(lyo, c, h, h_up, 4.0 * radius);
                    if (clr > at_clr) {
                        at = c;
                        h_low = h;
                        at_clr = clr;
                    }
                    if (clr >= 4.0 * radius) {
                        break;
                    }
                }
                for (const auto& [dx, dy] : { std::pair { 1, 0 }, std::pair { -1, 0 }, std::pair { 0, 1 }, std::pair { 0, -1 } }) {
                    const int64_t qx = gx + dx;
                    const int64_t qy = gy + dy;
                    const WorldPoint q = cell_centre(lyo, qx, qy);
                    const double dq = std::hypot(q.x - land.x, q.y - land.y);
                    if (dq > 8.0 * radius || !seen.insert({ qx, qy }).second) {
                        continue;
                    }
                    if (const auto v = span_near(lyo, qx, qy, h)) {
                        open.emplace(dq, qx, qy, *v);
                    }
                }
            }
        }
        dg->drops.push_back({ .index = i, .height = h_low });
        if (at.x == land.x && at.y == land.y) {
            continue;
        }
        const auto [ax, ay] = cell_of(lyo, at);
        pts[i] = at;
        hz[i] = h_low;
        // 跨类下落试点时净空图已释放
        if (!dist.v.empty()) {
            dg->clearance[i] = static_cast<double>(dist.at(ay, ax));
        }
    }
}

struct DropHop
{
    WorldPoint take;
    double take_h = 0.0;
    WorldPoint land;
    double land_h = 0.0;
};

// 每试一处要多规划两段, 纯成本上界
constexpr size_t kDropHopTries = 3;

// 起点可达的 span u 往四邻跨一格落到不可见的 span k, 落差超过一步
struct CrossDrop
{
    int64_t u = 0;
    int64_t cu = 0;
    int64_t cv = 0;
    int dx = 0;
    int dy = 0;
    int64_t k = 0;
};

std::vector<CrossDrop> cross_drops(const WindowInfo& info)
{
    std::vector<CrossDrop> out;
    const SpanTable& st = info.st3;
    const int64_t nx = info.nx;
    const int64_t ny = info.ny;
    for (size_t u = 0; u < info.reach3.size(); ++u) {
        const int64_t cu = st.sp_cell[u];
        if (info.reach3[u] == 0 || info.core.v[static_cast<size_t>(cu)] == 0) {
            continue;
        }
        const double hu = static_cast<double>(st.sp_h[u]);
        for (const auto& [dx, dy] : { std::pair { 1, 0 }, std::pair { -1, 0 }, std::pair { 0, 1 }, std::pair { 0, -1 } }) {
            const int64_t vx = cu % nx + dx;
            const int64_t vy = cu / nx + dy;
            if (vx < 0 || vy < 0 || vx >= nx || vy >= ny) {
                continue;
            }
            const int64_t cv = vy * nx + vx;
            const int64_t j = OccupiedIndex(st, cv);
            if (j < 0 || HasEdgeBit(info.sev.steps, cu, cv)) {
                continue;
            }
            for (int64_t k = CellSpanStart(st, j), ke = k + CellSpanCount(st, j); k < ke; ++k) {
                if (info.vis3[static_cast<size_t>(k)] == 0 && static_cast<double>(st.sp_h[static_cast<size_t>(k)]) < hu - kClimb) {
                    out.push_back({ .u = static_cast<int64_t>(u), .cu = cu, .cv = cv, .dx = dx, .dy = dy, .k = k });
                }
            }
        }
    }
    return out;
}

// 逐 span 记 peer 类可走面的分量号, 其余记 0
std::vector<uint32_t> peer_spans(
    const GridPack& gp,
    const GridZoneDir& gz,
    const FieldsPack& fp,
    const FieldsZoneDir& fzd,
    uint16_t zone_id,
    const WindowInfo& info,
    uint32_t peer)
{
    const int64_t nx = info.nx;
    int64_t bx0 = nx;
    int64_t by0 = info.ny;
    int64_t bx1 = -1;
    int64_t by1 = -1;
    for (const CrossDrop& drop : cross_drops(info)) {
        bx0 = std::min(bx0, drop.cv % nx);
        by0 = std::min(by0, drop.cv / nx);
        bx1 = std::max(bx1, drop.cv % nx);
        by1 = std::max(by1, drop.cv / nx);
    }
    std::vector<uint32_t> peer3;
    if (bx0 > bx1) {
        return peer3;
    }
    const int64_t pnx = bx1 - bx0 + 1;
    const std::span<const FieldsOpenRec> opn = FieldsOpensOfZone(fp, zone_id);
    GridWindow pw;
    if (!load_grid_window(
            &pw,
            gp,
            gz,
            &fzd,
            opn.data(),
            opn.size(),
            std::llround(info.x0 / kCS) + bx0,
            std::llround(info.y0 / kCS) + by0,
            pnx,
            by1 - by0 + 1)) {
        return peer3;
    }
    const SpanTable& st = info.st3;
    peer3.assign(info.vis3.size(), 0);
    for (size_t i = 0; i < pw.rec.size(); ++i) {
        const GridSpanRec& r = pw.rec[i];
        if (r.rid != peer || (r.flags & kGridFlagWalk) == 0 || (r.flags & (kGridFlagGhost | kGridFlagFill)) != 0) {
            continue;
        }
        const int64_t j = OccupiedIndex(st, (by0 + r.cell / pnx) * nx + bx0 + r.cell % pnx);
        if (j < 0) {
            continue;
        }
        for (int64_t k = CellSpanStart(st, j), ke = k + CellSpanCount(st, j); k < ke; ++k) {
            if (info.vis3[static_cast<size_t>(k)] == 0 && st.sp_h[static_cast<size_t>(k)] == r.h) {
                peer3[static_cast<size_t>(k)] = pw.scc[i];
            }
        }
    }
    return peer3;
}

WorldPoint window_cell_centre(const WindowInfo& info, int64_t c)
{
    return WorldPoint { info.x0 + (static_cast<double>(c % info.nx) + 0.5) * kCS,
                        info.y0 + (static_cast<double>(c / info.nx) + 0.5) * kCS };
}

int drop_dir(int dx, int dy)
{
    if (dx == 1) {
        return 0;
    }
    if (dx == -1) {
        return 1;
    }
    if (dy == 1) {
        return 2;
    }
    return 3;
}

// (格, 跳下方向, 落进哪一分量)
using DropEdgeSet = std::set<std::tuple<int64_t, int, uint32_t>>;

// 沿台沿往左右两侧各数连着几格, 数到 kClrPref 为止; 下一格可前后错开一格, 斜的台沿是一级级台阶
std::pair<int64_t, int64_t> ledge_runs(const DropEdgeSet& edge, const WindowInfo& info, int64_t c, int dx, int dy, uint32_t comp)
{
    const int64_t nx = info.nx;
    const int64_t need = static_cast<int64_t>(std::ceil(kClrPref / kCS));
    const int d = drop_dir(dx, dy);
    const std::array<std::pair<int, int>, 2> sides { std::pair { -dy, dx }, std::pair { dy, -dx } };
    std::array<int64_t, 2> runs {};
    for (size_t side = 0; side < sides.size(); ++side) {
        const auto [sx, sy] = sides[side];
        int64_t x = c % nx;
        int64_t y = c / nx;
        int64_t n = 0;
        while (n <= need) {
            bool next = false;
            for (const int off : { 0, 1, -1 }) {
                const int64_t qx = x + sx + off * dx;
                const int64_t qy = y + sy + off * dy;
                if (qx >= 0 && qy >= 0 && qx < nx && qy < info.ny && edge.contains({ qy * nx + qx, d, comp })) {
                    x = qx;
                    y = qy;
                    next = true;
                    break;
                }
            }
            if (!next) {
                break;
            }
            ++n;
        }
        runs[side] = n;
    }
    return { runs[0], runs[1] };
}

// 同一分量只留离两端最近的一处, 起跳处沿台沿两侧须各连着 kClrPref
std::vector<DropHop> cross_drop_hops(const WindowInfo& info, const std::vector<uint32_t>& peer3, const WorldPoint& s, const WorldPoint& g)
{
    const SpanTable& st = info.st3;
    const int64_t nx = info.nx;

    struct Cand
    {
        double score = 0.0;
        int64_t u = -1;
        int64_t v = -1;
        int dx = 0;
        int dy = 0;
    };

    std::vector<Cand> cand;
    DropEdgeSet edge;
    for (const CrossDrop& drop : cross_drops(info)) {
        const auto k = static_cast<size_t>(drop.k);
        if (peer3[k] == 0 || !RiseOk(st, nx, info.ny, drop.cu, drop.dx, drop.dy, st.sp_h[static_cast<size_t>(drop.u)], st.sp_h[k])) {
            continue;
        }
        const WorldPoint a = window_cell_centre(info, drop.cu);
        const WorldPoint b = window_cell_centre(info, drop.cv);
        cand.push_back({ std::hypot(a.x - s.x, a.y - s.y) + std::hypot(b.x - g.x, b.y - g.y), drop.u, drop.k, drop.dx, drop.dy });
        edge.emplace(drop.cu, drop_dir(drop.dx, drop.dy), peer3[k]);
    }
    const int64_t need = static_cast<int64_t>(std::ceil(kClrPref / kCS));
    // 某一分量一处都不够宽时照旧全收, 不因此丢掉这一分量
    std::vector<uint8_t> tight(cand.size(), 0);
    std::set<uint32_t> roomy;
    for (size_t i = 0; i < cand.size(); ++i) {
        const Cand& c = cand[i];
        const int64_t cu = st.sp_cell[static_cast<size_t>(c.u)];
        const uint32_t comp = peer3[static_cast<size_t>(c.v)];
        const auto [l, r] = ledge_runs(edge, info, cu, c.dx, c.dy, comp);
        tight[i] = std::min(l, r) < need && std::abs(l - r) > 1;
        if (!tight[i]) {
            roomy.insert(comp);
        }
    }
    {
        size_t w = 0;
        for (size_t i = 0; i < cand.size(); ++i) {
            if (!tight[i] || !roomy.contains(peer3[static_cast<size_t>(cand[i].v)])) {
                cand[w++] = cand[i];
            }
        }
        cand.resize(w);
    }
    std::sort(cand.begin(), cand.end(), [](const Cand& p, const Cand& q) {
        return std::tie(p.score, p.u, p.v) < std::tie(q.score, q.u, q.v);
    });
    const LayerOracle lyo { .st = &st, .nx = nx, .ny = info.ny, .x0 = info.x0, .y0 = info.y0 };
    std::vector<DropHop> out;
    std::vector<uint32_t> tried;
    for (const Cand& c : cand) {
        if (out.size() >= kDropHopTries) {
            break;
        }
        const uint32_t comp = peer3[static_cast<size_t>(c.v)];
        if (std::find(tried.begin(), tried.end(), comp) != tried.end()) {
            continue;
        }
        tried.push_back(comp);
        DropHop hop { .take = window_cell_centre(info, st.sp_cell[static_cast<size_t>(c.u)]),
                      .take_h = static_cast<double>(st.sp_h[static_cast<size_t>(c.u)]),
                      .land = window_cell_centre(info, st.sp_cell[static_cast<size_t>(c.v)]),
                      .land_h = static_cast<double>(st.sp_h[static_cast<size_t>(c.v)]) };
        // 朝跳下方向补一个点, 给落点外推留出范围
        if (info.fall_r > 0.0) {
            const double reach = 8.0 * info.fall_r + kCS;
            std::vector<WorldPoint> pts { hop.take, hop.land, { hop.land.x + c.dx * reach, hop.land.y + c.dy * reach } };
            std::vector<double> hz { hop.take_h, hop.land_h, hop.land_h };
            RouteDiag tdg;
            tdg.clearance.assign(pts.size(), 0.0);
            push_drop_landings(&pts, &hz, &tdg, info.dist, info.fall_r, lyo);
            hop.land = pts[1];
            hop.land_h = hz[1];
        }
        out.push_back(hop);
    }
    return out;
}

// 两段都齐全才拼, 否则整列作废
void join_point_values(std::vector<double>* x, const std::vector<double>& y, size_t base, size_t nb)
{
    if (x->size() == base && y.size() == nb) {
        x->insert(x->end(), y.begin(), y.end());
    }
    else {
        x->clear();
    }
}

// 拼点列与逐点量; 接缝那一步由调用方紧接着记, 下标是 a 的点数
RecastPlanResult join_head(RecastPlanResult a, const RecastPlanResult& b)
{
    const size_t base = a.points.size();
    const size_t nb = b.points.size();
    RecastPlanResult r = std::move(a);
    r.length += std::hypot(b.points.front().x - r.points.back().x, b.points.front().y - r.points.back().y) + b.length;
    r.points.insert(r.points.end(), b.points.begin(), b.points.end());
    join_point_values(&r.clearance, b.clearance, base, nb);
    join_point_values(&r.heights, b.heights, base, nb);
    if (!r.waypoints.empty() && !b.waypoints.empty()) {
        r.waypoints.push_back(base);
        for (const size_t w : b.waypoints) {
            r.waypoints.push_back(base + w);
        }
    }
    else {
        r.waypoints.clear();
    }
    return r;
}

void join_tail(RecastPlanResult* joined, const RecastPlanResult& b, size_t base, const char* warning)
{
    RecastPlanResult& r = *joined;
    for (const DropLanding& d : b.drops) {
        r.drops.push_back({ .index = base + d.index, .height = d.height });
    }
    for (RopeRide rope : b.ropes) {
        rope.index += base;
        r.ropes.push_back(rope);
    }
    r.snap_goal = b.snap_goal;
    r.warnings.insert(r.warnings.end(), b.warnings.begin(), b.warnings.end());
    r.warnings.push_back(warning);
    RecastPlanResult::Debug& da = r.debug;
    const RecastPlanResult::Debug& db = b.debug;
    da.tier_notes.insert(da.tier_notes.end(), db.tier_notes.begin(), db.tier_notes.end());
    da.topology_cells.insert(da.topology_cells.end(), db.topology_cells.begin(), db.topology_cells.end());
    da.topology_heights.insert(da.topology_heights.end(), db.topology_heights.begin(), db.topology_heights.end());
    da.taut_points.insert(da.taut_points.end(), db.taut_points.begin(), db.taut_points.end());
    da.pulled_points.insert(da.pulled_points.end(), db.pulled_points.begin(), db.pulled_points.end());
    da.assembled_points.insert(da.assembled_points.end(), db.assembled_points.begin(), db.assembled_points.end());
    da.planned_points = r.points;
    da.warnings = r.warnings;
}

RecastPlanResult join_drop_hop(RecastPlanResult a, const RecastPlanResult& b, double land_h)
{
    const size_t base = a.points.size();
    RecastPlanResult r = join_head(std::move(a), b);
    r.drops.push_back({ .index = base, .height = land_h });
    join_tail(&r, b, base, "台沿下落换类");
    return r;
}

RecastPlanResult join_rope_hop(RecastPlanResult a, const RecastPlanResult& b, const FieldsRopeRec& rope)
{
    const size_t base = a.points.size();
    RecastPlanResult r = join_head(std::move(a), b);
    r.ropes.push_back({ .index = base,
                        .top_height = static_cast<double>(rope.top.h),
                        .uid = rope.uid_bottom,
                        .ride_s = static_cast<double>(rope.ride_s) });
    join_tail(&r, b, base, "升降索换层");
    return r;
}

WorldPoint link_side_centre(const FieldsLinkSide& s)
{
    return { .x = (static_cast<double>(s.gx) + 0.5) * kCS, .y = (static_cast<double>(s.gy) + 0.5) * kCS };
}

// 取样须与层走查同一套整数插值, 两处判据的经过格才一致
double seg_min_clr(const Grid<float>& d, double x0, double y0, const WorldPoint& a, const WorldPoint& b)
{
    const int64_t ax = static_cast<int64_t>((a.x - x0) / kCS);
    const int64_t ay = static_cast<int64_t>((a.y - y0) / kCS);
    const int64_t bx = static_cast<int64_t>((b.x - x0) / kCS);
    const int64_t by = static_cast<int64_t>((b.y - y0) / kCS);
    const int64_t n = std::max<int64_t>(std::max(std::abs(bx - ax), std::abs(by - ay)), 1);
    double m = std::numeric_limits<double>::infinity();
    for (int64_t k = 0; k <= n; ++k) {
        const int64_t cx =
            ax + static_cast<int64_t>(std::nearbyint(static_cast<double>(bx - ax) * static_cast<double>(k) / static_cast<double>(n)));
        const int64_t cy =
            ay + static_cast<int64_t>(std::nearbyint(static_cast<double>(by - ay) * static_cast<double>(k) / static_cast<double>(n)));
        if (cx < 0 || cy < 0 || cx >= d.nx || cy >= d.ny) {
            continue;
        }
        m = std::min(m, static_cast<double>(d.at(cy, cx)));
    }
    return m;
}

// c 在有向线 a→b 的哪一侧, 正为左
double turn_side(const WorldPoint& a, const WorldPoint& b, const WorldPoint& c)
{
    return (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x);
}

bool seg_cross(const WorldPoint& p, const WorldPoint& q, const WorldPoint& r, const WorldPoint& s)
{
    return (turn_side(r, s, p) > 0.0) != (turn_side(r, s, q) > 0.0) && (turn_side(p, q, r) > 0.0) != (turn_side(p, q, s) > 0.0);
}

float nearest_layer_height(const std::vector<float>& hs, float ref)
{
    float best = hs.front();
    for (const float v : hs) {
        if (std::fabs(v - ref) < std::fabs(best - ref)) {
            best = v;
        }
    }
    return best;
}

// 新弦段内最小净空不得低于原值, 防止拐点挪进窄缝
void lift_corners(std::vector<WorldPoint>* path, Visibility* vis, const Grid<float>& d, const LayerOracle& lyo, float h0, double lift)
{
    std::vector<WorldPoint>& P = *path;
    const double x0 = lyo.x0;
    const double y0 = lyo.y0;
    const int64_t rad = static_cast<int64_t>(std::lround(lift / kCS));
    if (P.size() < 3 || rad <= 0) {
        return;
    }
    float ha = h0;
    for (size_t i = 1; i + 1 < P.size(); ++i) {
        const auto hb = LayerWalk(lyo, { P[i - 1], P[i] }, ha);
        if (!hb.has_value() || hb->empty()) {
            return;
        }
        const float hv0 = nearest_layer_height(*hb, ha);
        const int64_t cx0 = static_cast<int64_t>((P[i].x - x0) / kCS);
        const int64_t cy0 = static_cast<int64_t>((P[i].y - y0) / kCS);
        if (cx0 < 0 || cy0 < 0 || cx0 >= d.nx || cy0 >= d.ny) {
            ha = hv0;
            continue;
        }
        const double here = static_cast<double>(d.at(cy0, cx0));
        const double keep = std::min(seg_min_clr(d, x0, y0, P[i - 1], P[i]), seg_min_clr(d, x0, y0, P[i], P[i + 1]));
        // 候选按净空降序, 同净空按格序, 保证结果可复现
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
            const WorldPoint w { x0 + (static_cast<double>(c.second % d.nx) + 0.5) * kCS,
                                 y0 + (static_cast<double>(c.second / d.nx) + 0.5) * kCS };
            // 挪到与邻点重合会出零长段, 跟随层从零长段取不到方向
            if (std::hypot(w.x - P[i - 1].x, w.y - P[i - 1].y) < kCS * 0.5 || std::hypot(w.x - P[i + 1].x, w.y - P[i + 1].y) < kCS * 0.5) {
                continue;
            }
            if (std::min(seg_min_clr(d, x0, y0, P[i - 1], w), seg_min_clr(d, x0, y0, w, P[i + 1])) < keep - 1e-9) {
                continue;
            }
            // 中间一段比位移还短时, 挪一下会与隔一段的弦自交
            if ((i >= 2 && seg_cross(w, P[i + 1], P[i - 2], P[i - 1]))
                || (i + 2 < P.size() && seg_cross(P[i - 1], w, P[i + 1], P[i + 2]))) {
                continue;
            }
            const auto h1 = LayerWalk(lyo, { P[i - 1], w }, ha);
            if (!h1.has_value() || h1->empty()) {
                continue;
            }
            const float hw = nearest_layer_height(*h1, ha);
            const auto h2 = LayerWalk(lyo, { w, P[i + 1] }, hw);
            if (!h2.has_value() || h2->empty()) {
                continue;
            }
            if (!ChordVisible(vis, P[i - 1], w, ha, hw) || !ChordVisible(vis, w, P[i + 1], hw, nearest_layer_height(*h2, hw))) {
                continue;
            }
            P[i] = w;
            hnext = hw;
            break;
        }
        ha = hnext;
    }
}

// route_window 各步共用的窗口状态, 开头一次填好; 吸附锚点在基线退到格级时重挑
struct RouteWindow
{
    const WindowInfo* info = nullptr;
    int64_t nx = 0;
    int64_t ny = 0;
    double x0 = 0.0;
    double y0 = 0.0;
    const SpanTable* st3 = nullptr;
    LayerOracle lyo;
    Mask walk;
    Grid<float> dist;
    CellPt sc;
    CellPt gc;
    bool tentative = false;
    bool bounded = false;
    bool strict = false;
    double limit = 0.0;
    double limit2 = 0.0;
    std::optional<double> goal_deck;
    std::optional<double> start_deck;
    Mask cw3;
    const EdgeBits* faces = nullptr;
    const JumpEdges* jspan = nullptr;
    const JumpEdges* jcell = nullptr;
    std::optional<CellPt> as;
    std::optional<CellPt> ag;
    double dsa = 0.0;
    double dga = 0.0;
    // ends_linked 反复用的暂存
    std::vector<uint8_t> seen;
    std::vector<int64_t> stk;
};

void escalate(RouteDiag* dg, const char* why)
{
    if (!dg->escalate) {
        dg->escalate = true;
        dg->escalate_why = why;
    }
}

double focal_sum(const CellPt& sc, const CellPt& gc, double x, double y)
{
    return std::hypot(x - static_cast<double>(sc.x), y - static_cast<double>(sc.y))
           + std::hypot(x - static_cast<double>(gc.x), y - static_cast<double>(gc.y));
}

double min_focal_sum_on_edge(const CellPt& sc, const CellPt& gc, double ax, double ay, double bx, double by)
{
    double lo = 0.0;
    double hi = 1.0;
    for (int i = 0; i < 100; ++i) {
        const double m1 = lo + (hi - lo) / 3.0;
        const double m2 = hi - (hi - lo) / 3.0;
        if (focal_sum(sc, gc, ax + (bx - ax) * m1, ay + (by - ay) * m1) < focal_sum(sc, gc, ax + (bx - ax) * m2, ay + (by - ay) * m2)) {
            hi = m2;
        }
        else {
            lo = m1;
        }
    }
    return focal_sum(sc, gc, ax + (bx - ax) * lo, ay + (by - ay) * lo);
}

// 两距之和是凸函数, 区外下界取在可信区边界四条边上, 每条边三分求极小, 再让一格给取整
double ellipse_floor(const CellPt& sc, const CellPt& gc, int64_t margin, int64_t nx, int64_t ny)
{
    const double rx0 = static_cast<double>(margin - 1);
    const double ry0 = static_cast<double>(margin - 1);
    const double rx1 = static_cast<double>(nx - margin);
    const double ry1 = static_cast<double>(ny - margin);
    return std::min({ min_focal_sum_on_edge(sc, gc, rx0, ry0, rx1, ry0),
                      min_focal_sum_on_edge(sc, gc, rx1, ry0, rx1, ry1),
                      min_focal_sum_on_edge(sc, gc, rx1, ry1, rx0, ry1),
                      min_focal_sum_on_edge(sc, gc, rx0, ry1, rx0, ry0) })
           - 1.0;
}

Mask channel_mask(const RouteWindow& rw, double cc, const Mask& band)
{
    Mask w(rw.nx, rw.ny, 0);
    for (size_t i = 0; i < w.v.size(); ++i) {
        w.v[i] = static_cast<uint8_t>(static_cast<double>(rw.dist.v[i]) >= cc || band.v[i] != 0);
    }
    return w;
}

std::pair<std::optional<CellPt>, double> nearest_cell(const RouteWindow& rw, const Mask& mask, const CellPt& p)
{
    bool have = false;
    int64_t bd = 0;
    CellPt bc;
    for (int64_t y = 0; y < rw.ny; ++y) {
        for (int64_t x = 0; x < rw.nx; ++x) {
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
}

void spans_in_mask(std::vector<uint8_t>* use, Mask* c3, const RouteWindow& rw, const Mask& m2)
{
    use->assign(rw.st3->sp_h.size(), 0);
    *c3 = Mask(rw.nx, rw.ny, 0);
    for (size_t i = 0; i < use->size(); ++i) {
        const auto cell = static_cast<size_t>(rw.st3->sp_cell[i]);
        if (rw.info->reach3[i] != 0 && m2.v[cell] != 0) {
            (*use)[i] = 1;
            c3->v[cell] = 1;
        }
    }
}

std::vector<int64_t> spans_at(const RouteWindow& rw, const CellPt& c, const std::vector<uint8_t>& use)
{
    std::vector<int64_t> out;
    const int64_t j = OccupiedIndex(rw.info->st3, c.y * rw.nx + c.x);
    if (j < 0) {
        return out;
    }
    const int64_t jb = CellSpanStart(*rw.st3, j);
    for (int64_t k = 0, kn = CellSpanCount(*rw.st3, j); k < kn; ++k) {
        const int64_t v = jb + k;
        if (use[static_cast<size_t>(v)] != 0) {
            out.push_back(v);
        }
    }
    return out;
}

int64_t span_at_seed_layer(const RouteWindow& rw, const std::vector<int64_t>& vs)
{
    int64_t best = -1;
    float bd = 0.0F;
    for (const int64_t v : vs) {
        const float d = std::fabs(rw.st3->sp_h[static_cast<size_t>(v)] - static_cast<float>(rw.info->h0));
        if (best < 0 || d < bd) {
            best = v;
            bd = d;
        }
    }
    return best;
}

// 超出 kDeckBand 视为该面不在此格
int64_t span_on_deck(const RouteWindow& rw, const std::vector<int64_t>& vs, double deck)
{
    int64_t best = -1;
    double bd = 0.0;
    for (const int64_t v : vs) {
        const double d = std::fabs(static_cast<double>(rw.st3->sp_h[static_cast<size_t>(v)]) - deck);
        if (best < 0 || d < bd) {
            best = v;
            bd = d;
        }
    }
    return best >= 0 && bd <= kDeckBand ? best : -1;
}

// 终点声明是硬约束, 匹配不上返回空集让本级失败
std::vector<int64_t> goal_spans(const RouteWindow& rw, const std::vector<int64_t>& vs)
{
    if (!rw.goal_deck.has_value()) {
        return vs;
    }
    const int64_t v = span_on_deck(rw, vs, *rw.goal_deck);
    return v >= 0 ? std::vector<int64_t> { v } : std::vector<int64_t> {};
}

int64_t start_span(const RouteWindow& rw, const std::vector<int64_t>& vs)
{
    return rw.start_deck.has_value() ? span_on_deck(rw, vs, *rw.start_deck) : span_at_seed_layer(rw, vs);
}

// 最近的可走格未必带着声明的面, 所以按面吸附
std::pair<std::optional<CellPt>, double> nearest_deck_cell(
    const RouteWindow& rw,
    const std::vector<uint8_t>& use,
    const Mask& cells,
    const CellPt& at,
    std::optional<double> deck)
{
    if (!deck.has_value()) {
        return nearest_cell(rw, cells, at);
    }
    bool have = false;
    int64_t bd = 0;
    double bh = 0.0;
    CellPt bc;
    for (size_t i = 0; i < use.size(); ++i) {
        if (use[i] == 0) {
            continue;
        }
        const double dh = std::fabs(static_cast<double>(rw.st3->sp_h[i]) - *deck);
        if (dh > kDeckBand) {
            continue;
        }
        const int64_t cell = rw.st3->sp_cell[i];
        const int64_t x = cell % rw.nx;
        const int64_t y = cell / rw.nx;
        const int64_t d = (x - at.x) * (x - at.x) + (y - at.y) * (y - at.y);
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
}

bool cell_has_deck(const RouteWindow& rw, const CellPt& c, double deck)
{
    const int64_t j = OccupiedIndex(rw.info->st3, c.y * rw.nx + c.x);
    if (j < 0) {
        return false;
    }
    for (int64_t v = CellSpanStart(*rw.st3, j), end = v + CellSpanCount(*rw.st3, j); v < end; ++v) {
        if (std::fabs(static_cast<double>(rw.st3->sp_h[static_cast<size_t>(v)]) - deck) <= kDeckBand) {
            return true;
        }
    }
    return false;
}

// 端点到通道的最短接入链, 同长取瓶颈最高再按格序, 距离用整数免得浮点累加乱序
std::optional<std::vector<int64_t>> access_chain(RouteDiag* dg, const RouteWindow& rw, const Mask& lim, const std::optional<CellPt>& a)
{
    if (!a.has_value()) {
        return std::vector<int64_t> {};
    }
    const size_t n = static_cast<size_t>(rw.nx * rw.ny);
    const size_t s0 = static_cast<size_t>(a->y * rw.nx + a->x);
    if (lim.v[s0] != 0) {
        return std::vector<int64_t> {};
    }
    std::vector<int32_t> cs(n, std::numeric_limits<int32_t>::max());
    std::vector<float> bw(n, -1.0F);
    // 格号上界 kMaxCells 装得进 32 位
    std::vector<int32_t> pv(n, -1);
    std::priority_queue<std::tuple<int32_t, float, int64_t>> pq;
    cs[s0] = 0;
    bw[s0] = rw.dist.v[s0];
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
        const int64_t cx = c % rw.nx;
        const int64_t cy = c / rw.nx;
        for (int64_t dy = -1; dy <= 1; ++dy) {
            for (int64_t dx = -1; dx <= 1; ++dx) {
                const int64_t bx = cx + dx;
                const int64_t by = cy + dy;
                if ((dx == 0 && dy == 0) || bx < 0 || by < 0 || bx >= rw.nx || by >= rw.ny) {
                    continue;
                }
                const size_t k = static_cast<size_t>(by * rw.nx + bx);
                if (rw.cw3.v[k] == 0) {
                    continue;
                }
                const int32_t kc = cc + (dx != 0 && dy != 0 ? 141 : 100);
                const float kb = std::min(b, rw.dist.v[k]);
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
        if (rw.strict) {
            escalate(dg, "接入链接不通");
        }
        return std::nullopt;
    }
    const double chain_len = static_cast<double>(cs[static_cast<size_t>(hit)]) / 100.0;
    if (rw.bounded && chain_len >= rw.limit) {
        escalate(dg, "接入链碰边");
        return std::nullopt;
    }
    std::vector<int64_t> ch;
    for (int64_t c = hit; c >= 0; c = pv[static_cast<size_t>(c)]) {
        ch.push_back(c);
    }
    return ch;
}

void open_cells(Mask* lim, const std::vector<int64_t>& ch)
{
    for (const int64_t c : ch) {
        lim->v[static_cast<size_t>(c)] = 1;
    }
}

// 跳边每端最多走 kLinkChainCells 格, 走不到的留空且不升档
std::vector<std::vector<int64_t>> link_chains(const RouteWindow& rw, const Mask& lim, const std::vector<int64_t>& ends)
{
    std::vector<std::vector<int64_t>> out(ends.size());
    if (ends.empty()) {
        return out;
    }
    const size_t n = static_cast<size_t>(rw.nx * rw.ny);
    std::vector<int32_t> cs(n, std::numeric_limits<int32_t>::max());
    std::vector<float> bw(n, -1.0F);
    std::vector<int32_t> pv(n, -1);
    std::vector<int64_t> touched;
    std::priority_queue<std::tuple<int32_t, float, int64_t>> pq;
    for (size_t i = 0; i < ends.size(); ++i) {
        const size_t s0 = static_cast<size_t>(ends[i]);
        if (lim.v[s0] != 0) {
            continue;
        }
        for (const int64_t t : touched) {
            cs[static_cast<size_t>(t)] = std::numeric_limits<int32_t>::max();
            bw[static_cast<size_t>(t)] = -1.0F;
            pv[static_cast<size_t>(t)] = -1;
        }
        touched.clear();
        pq = {};
        cs[s0] = 0;
        bw[s0] = rw.dist.v[s0];
        touched.push_back(static_cast<int64_t>(s0));
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
            if (cc >= kLinkChainCells * 100) {
                break;
            }
            const int64_t cx = c % rw.nx;
            const int64_t cy = c / rw.nx;
            for (int64_t dy = -1; dy <= 1; ++dy) {
                for (int64_t dx = -1; dx <= 1; ++dx) {
                    const int64_t bx = cx + dx;
                    const int64_t by = cy + dy;
                    if ((dx == 0 && dy == 0) || bx < 0 || by < 0 || bx >= rw.nx || by >= rw.ny) {
                        continue;
                    }
                    const size_t k = static_cast<size_t>(by * rw.nx + bx);
                    if (rw.cw3.v[k] == 0) {
                        continue;
                    }
                    const int32_t kc = cc + (dx != 0 && dy != 0 ? 141 : 100);
                    const float kb = std::min(b, rw.dist.v[k]);
                    if (kc < cs[k] || (kc == cs[k] && (kb > bw[k] || (kb == bw[k] && c < pv[k])))) {
                        if (cs[k] == std::numeric_limits<int32_t>::max()) {
                            touched.push_back(static_cast<int64_t>(k));
                        }
                        cs[k] = kc;
                        bw[k] = kb;
                        pv[k] = static_cast<int32_t>(c);
                        pq.emplace(-kc, kb, -static_cast<int64_t>(k));
                    }
                }
            }
        }
        for (int64_t c = hit; c >= 0; c = pv[static_cast<size_t>(c)]) {
            out[i].push_back(c);
        }
    }
    return out;
}

bool in_core_and(const RouteWindow& rw, const Mask& lim, int64_t c)
{
    return rw.info->core.v[static_cast<size_t>(c)] != 0 && lim.v[static_cast<size_t>(c)] != 0;
}

// 两端在 core∧lim 内八连通是 solve 成功的必要条件
bool ends_linked(RouteWindow* rw, const Mask& lim)
{
    const int64_t sc = rw->as->y * rw->nx + rw->as->x;
    const int64_t gc = rw->ag->y * rw->nx + rw->ag->x;
    if (!in_core_and(*rw, lim, sc) || !in_core_and(*rw, lim, gc)) {
        return false;
    }
    std::fill(rw->seen.begin(), rw->seen.end(), static_cast<uint8_t>(0));
    rw->stk.clear();
    rw->stk.push_back(sc);
    rw->seen[static_cast<size_t>(sc)] = 1;
    for (;;) {
        while (!rw->stk.empty()) {
            const int64_t c = rw->stk.back();
            rw->stk.pop_back();
            if (c == gc) {
                return true;
            }
            const int64_t cx = c % rw->nx;
            const int64_t cy = c / rw->nx;
            for (int64_t dy = -1; dy <= 1; ++dy) {
                for (int64_t dx = -1; dx <= 1; ++dx) {
                    const int64_t bx = cx + dx;
                    const int64_t by = cy + dy;
                    if (bx < 0 || by < 0 || bx >= rw->nx || by >= rw->ny) {
                        continue;
                    }
                    const int64_t b = by * rw->nx + bx;
                    if (rw->seen[static_cast<size_t>(b)] == 0 && in_core_and(*rw, lim, b)) {
                        rw->seen[static_cast<size_t>(b)] = 1;
                        rw->stk.push_back(b);
                    }
                }
            }
        }
        for (const JumpEdge& e : rw->info->links_cell.e) {
            if (rw->seen[static_cast<size_t>(e.src)] != 0 && rw->seen[static_cast<size_t>(e.dst)] == 0 && in_core_and(*rw, lim, e.dst)) {
                rw->seen[static_cast<size_t>(e.dst)] = 1;
                rw->stk.push_back(e.dst);
            }
        }
        if (rw->stk.empty()) {
            return false;
        }
    }
}

struct WindowTopo
{
    std::vector<CellPt> q;
    std::optional<std::vector<int64_t>> qs;
    // 只有带视线判据的搜索才填, 即几何要走的折线
    std::vector<int64_t> corn;
    Mask on3;
    std::vector<std::string> warn;
};

// 拓扑搜索的计价与判据, vis 非空时判弦会改写挡线的去重戳
struct TopoSearch
{
    const PriceField* price = nullptr;
    const EdgeBits* banned = nullptr;
    const double* bnp = nullptr;
    Visibility* vis = nullptr;
};

std::optional<std::vector<int64_t>> run_span_search(
    std::vector<int64_t>* corn,
    double* cost,
    const RouteWindow& rw,
    const TopoSearch& search,
    const std::vector<uint8_t>& use,
    const Mask& m3)
{
    corn->clear();
    *cost = 0.0;
    if (m3.at(rw.as->y, rw.as->x) == 0 || m3.at(rw.ag->y, rw.ag->x) == 0) {
        return std::nullopt;
    }
    const std::vector<int64_t> gs = goal_spans(rw, spans_at(rw, *rw.ag, use));
    const int64_t sd = start_span(rw, spans_at(rw, *rw.as, use));
    if (rw.as->x == rw.ag->x && rw.as->y == rw.ag->y) {
        if (!rw.goal_deck.has_value()) {
            return sd >= 0 ? std::optional<std::vector<int64_t>> { { sd } } : std::nullopt;
        }
        return gs.empty() ? std::nullopt : std::optional<std::vector<int64_t>> { { gs.front() } };
    }
    if (sd < 0 || (rw.goal_deck.has_value() && gs.empty())) {
        return std::nullopt;
    }
    const double give_up_at = rw.bounded ? rw.limit2 - (rw.dsa + rw.dga) / kCS + 1.0 : std::numeric_limits<double>::infinity();
    const SpanSearch span_search { .st = rw.st3,
                                   .ok = &use,
                                   .ok2 = &m3,
                                   .mult = search.price,
                                   .banned = search.banned,
                                   .bnp = search.bnp,
                                   .forbidden = rw.faces,
                                   .vis = search.vis,
                                   .jumps = rw.jspan };
    return SpanAstar(search.vis != nullptr ? corn : nullptr, cost, span_search, sd, gs, give_up_at);
}

std::optional<WindowTopo> solve_spans(RouteDiag* dg, const RouteWindow& rw, const TopoSearch& search, const Mask& wl, const Mask& cr)
{
    std::vector<uint8_t> uw;
    std::vector<uint8_t> uc;
    Mask w3;
    Mask c3;
    spans_in_mask(&uw, &w3, rw, wl);
    spans_in_mask(&uc, &c3, rw, cr);
    std::vector<int64_t> corn;
    double cost = 0.0;
    WindowTopo t;
    t.on3 = w3;
    std::optional<std::vector<int64_t>> sq = run_span_search(&corn, &cost, rw, search, uw, w3);
    if (!sq.has_value()) {
        // 小窗里的退档可能是窗口切出来的假失败
        if (rw.strict) {
            escalate(dg, "walk 断开");
            return std::nullopt;
        }
        sq = run_span_search(&corn, &cost, rw, search, uc, c3);
        if (sq.has_value()) {
            t.on3 = c3;
            t.warn.push_back("walk 断开→退回 core");
        }
    }
    if (sq.has_value()) {
        // 搜索从吸附锚点出发, 焦点却取端点格, 两段吸附偏移并入代价
        if (rw.bounded && cost + (rw.dsa + rw.dga) / kCS >= rw.limit2) {
            escalate(dg, "搜索碰边");
            return std::nullopt;
        }
        t.q.reserve(sq->size());
        for (const int64_t v : *sq) {
            const int64_t c = rw.st3->sp_cell[static_cast<size_t>(v)];
            t.q.push_back({ c % rw.nx, c / rw.nx });
        }
        t.qs = std::move(sq);
        t.corn = std::move(corn);
        return t;
    }
    return std::nullopt;
}

// lim 只能收紧可达判据, 不得放宽任何一条
std::optional<WindowTopo> solve_channel(RouteDiag* dg, RouteWindow* rw, const TopoSearch& search, const Mask& lim)
{
    if (!ends_linked(rw, lim)) {
        if (rw->strict) {
            escalate(dg, "掩膜内两端不连通");
        }
        return std::nullopt;
    }
    Mask wl(rw->nx, rw->ny, 0);
    Mask cr(rw->nx, rw->ny, 0);
    for (size_t i = 0; i < wl.v.size(); ++i) {
        wl.v[i] = static_cast<uint8_t>(rw->walk.v[i] != 0 && lim.v[i] != 0);
        cr.v[i] = static_cast<uint8_t>(rw->info->core.v[i] != 0 && lim.v[i] != 0);
    }
    return solve_spans(dg, *rw, search, wl, cr);
}

// lim 只能收紧可达判据, 不得放宽任何一条
std::optional<WindowTopo> solve_baseline(RouteDiag* dg, RouteWindow* rw, const TopoSearch& search, const Mask& lim)
{
    Mask wl(rw->nx, rw->ny, 0);
    Mask cr(rw->nx, rw->ny, 0);
    for (size_t i = 0; i < wl.v.size(); ++i) {
        wl.v[i] = static_cast<uint8_t>(rw->walk.v[i] != 0 && lim.v[i] != 0);
        cr.v[i] = static_cast<uint8_t>(rw->info->core.v[i] != 0 && lim.v[i] != 0);
    }
    std::optional<WindowTopo> t = solve_spans(dg, *rw, search, wl, cr);
    if (t.has_value()) {
        return t;
    }
    // 格级搜索不看 span, 声明了端点面就不能退到这一级
    if (rw->goal_deck.has_value() || rw->start_deck.has_value()) {
        return std::nullopt;
    }
    if (rw->strict) {
        escalate(dg, "层不连通");
        return std::nullopt;
    }
    std::tie(rw->as, rw->dsa) = nearest_cell(*rw, wl, rw->sc);
    std::tie(rw->ag, rw->dga) = nearest_cell(*rw, wl, rw->gc);
    if (!rw->as.has_value() || !rw->ag.has_value()) {
        return std::nullopt;
    }
    WindowTopo t2;
    t2.on3 = wl;
    std::optional<std::vector<CellPt>> qc;
    if (rw->as->x == rw->ag->x && rw->as->y == rw->ag->y) {
        qc = std::vector<CellPt> { *rw->as };
    }
    else {
        qc = CostAstar(wl, *rw->as, *rw->ag, *search.price, search.banned, search.bnp, rw->faces, nullptr, rw->jcell);
    }
    if (!qc.has_value()) {
        t2.on3 = cr;
        qc = CostAstar(cr, *rw->as, *rw->ag, *search.price, search.banned, search.bnp, rw->faces, nullptr, rw->jcell);
        if (qc.has_value()) {
            t2.warn.push_back("walk 断开→退回 core");
        }
    }
    if (!qc.has_value()) {
        return std::nullopt;
    }
    t2.warn.push_back("层不连通→退回格级");
    t2.q = std::move(*qc);
    return t2;
}

enum class ReachDirection
{
    Forward,  // 从起点顺着可走方向扩
    Backward, // 从终点逆着可走方向扩, 禁行边与抬升判据都按 v→u 问
};

void spread_via_jumps(std::vector<uint8_t>* seen, std::vector<int64_t>* frontier, const JumpEdges& jl, const std::vector<uint8_t>& use)
{
    for (const JumpEdge& e : jl.e) {
        if ((*seen)[static_cast<size_t>(e.src)] != 0 && (*seen)[static_cast<size_t>(e.dst)] == 0 && use[static_cast<size_t>(e.dst)] != 0) {
            (*seen)[static_cast<size_t>(e.dst)] = 1;
            frontier->push_back(e.dst);
        }
    }
}

std::vector<uint8_t> reach_spans(
    const RouteWindow& rw,
    const std::vector<int64_t>& seeds,
    const std::vector<uint8_t>& use,
    const Mask& ok2,
    ReachDirection dir)
{
    const bool backward = dir == ReachDirection::Backward;
    std::vector<uint8_t> seen(rw.st3->sp_h.size(), 0);
    std::vector<int64_t> frontier;
    for (const int64_t v : seeds) {
        if (v >= 0 && use[static_cast<size_t>(v)] != 0 && seen[static_cast<size_t>(v)] == 0) {
            seen[static_cast<size_t>(v)] = 1;
            frontier.push_back(v);
        }
    }
    const JumpEdges& jl = backward ? rw.info->links_span_rev : rw.info->links_span;
    spread_via_jumps(&seen, &frontier, jl, use);
    while (!frontier.empty()) {
        std::vector<int64_t> next;
        for (const int64_t u : frontier) {
            const int64_t cu = rw.st3->sp_cell[static_cast<size_t>(u)];
            const int64_t ux = cu % rw.nx;
            const int64_t uy = cu / rw.nx;
            const float hu = rw.st3->sp_h[static_cast<size_t>(u)];
            for (int64_t dy = -1; dy <= 1; ++dy) {
                for (int64_t dx = -1; dx <= 1; ++dx) {
                    const int64_t vx = ux + dx;
                    const int64_t vy = uy + dy;
                    if ((dx == 0 && dy == 0) || vx < 0 || vy < 0 || vx >= rw.nx || vy >= rw.ny) {
                        continue;
                    }
                    const int64_t cv = vy * rw.nx + vx;
                    if (ok2.v[static_cast<size_t>(cv)] == 0) {
                        continue;
                    }
                    if (dx != 0 && dy != 0 && !(ok2.at(uy, vx) && ok2.at(vy, ux))) {
                        continue;
                    }
                    if (OccupiedIndex(rw.info->st3, cv) < 0) {
                        continue;
                    }
                    if (HasEdgeBit(*rw.faces, backward ? cv : cu, backward ? cu : cv)) {
                        continue;
                    }
                    const int64_t j = OccupiedIndex(rw.info->st3, cv);
                    const int64_t jb = CellSpanStart(*rw.st3, j);
                    for (int64_t k = 0, kn = CellSpanCount(*rw.st3, j); k < kn; ++k) {
                        const int64_t v = jb + k;
                        if (use[static_cast<size_t>(v)] == 0 || seen[static_cast<size_t>(v)] != 0) {
                            continue;
                        }
                        const float hv = rw.st3->sp_h[static_cast<size_t>(v)];
                        if (!(backward ? RiseOk(*rw.st3, rw.nx, rw.ny, cv, -dx, -dy, hv, hu)
                                       : RiseOk(*rw.st3, rw.nx, rw.ny, cu, dx, dy, hu, hv))) {
                            continue;
                        }
                        seen[static_cast<size_t>(v)] = 1;
                        next.push_back(v);
                    }
                }
            }
        }
        frontier = std::move(next);
        if (frontier.empty()) {
            spread_via_jumps(&seen, &frontier, jl, use);
        }
    }
    return seen;
}

constexpr int32_t kGapFar = std::numeric_limits<int32_t>::max() / 4;

// 逐格到终点可达集的最近距离与那个最近格, 两遍扫描的倒角距离
struct GapField
{
    int64_t nx = 0;
    int64_t ny = 0;
    std::vector<int32_t> dc;
    std::vector<int32_t> src;
};

void relax_gap(GapField* f, size_t c, int64_t bx, int64_t by, int32_t w)
{
    if (bx < 0 || by < 0 || bx >= f->nx || by >= f->ny) {
        return;
    }
    const auto b = static_cast<size_t>(by * f->nx + bx);
    if (f->dc[b] < kGapFar && f->dc[b] + w < f->dc[c]) {
        f->dc[c] = f->dc[b] + w;
        f->src[c] = f->src[b];
    }
}

void report_gap(RouteDiag* dg, const RouteWindow& rw)
{
    if (!rw.as.has_value() || !rw.ag.has_value()) {
        return;
    }
    std::vector<uint8_t> useC;
    Mask cc3;
    spans_in_mask(&useC, &cc3, rw, rw.info->core);
    const int64_t sd = start_span(rw, spans_at(rw, *rw.as, useC));
    const std::vector<int64_t> gs = goal_spans(rw, spans_at(rw, *rw.ag, useC));
    if (sd < 0 || gs.empty()) {
        return;
    }
    const std::vector<uint8_t> ra = reach_spans(rw, { sd }, useC, cc3, ReachDirection::Forward);
    const std::vector<uint8_t> rb = reach_spans(rw, gs, useC, cc3, ReachDirection::Backward);
    const size_t n = static_cast<size_t>(rw.nx * rw.ny);
    GapField field { .nx = rw.nx, .ny = rw.ny, .dc = std::vector<int32_t>(n, kGapFar), .src = std::vector<int32_t>(n, -1) };
    std::vector<uint8_t> ina(n, 0);
    for (size_t i = 0; i < ra.size(); ++i) {
        const auto c = static_cast<size_t>(rw.st3->sp_cell[i]);
        ina[c] = static_cast<uint8_t>(ina[c] | ra[i]);
        if (rb[i] != 0) {
            field.dc[c] = 0;
            field.src[c] = static_cast<int32_t>(c);
        }
    }
    for (int64_t y = 0; y < rw.ny; ++y) {
        for (int64_t x = 0; x < rw.nx; ++x) {
            const auto c = static_cast<size_t>(y * rw.nx + x);
            relax_gap(&field, c, x - 1, y - 1, 141);
            relax_gap(&field, c, x, y - 1, 100);
            relax_gap(&field, c, x + 1, y - 1, 141);
            relax_gap(&field, c, x - 1, y, 100);
        }
    }
    for (int64_t y = rw.ny - 1; y >= 0; --y) {
        for (int64_t x = rw.nx - 1; x >= 0; --x) {
            const auto c = static_cast<size_t>(y * rw.nx + x);
            relax_gap(&field, c, x + 1, y + 1, 141);
            relax_gap(&field, c, x, y + 1, 100);
            relax_gap(&field, c, x - 1, y + 1, 141);
            relax_gap(&field, c, x + 1, y, 100);
        }
    }
    int64_t best = -1;
    for (size_t c = 0; c < n; ++c) {
        if (ina[c] != 0 && field.src[c] >= 0 && (best < 0 || field.dc[c] < field.dc[static_cast<size_t>(best)])) {
            best = static_cast<int64_t>(c);
        }
    }
    if (best < 0) {
        return;
    }
    const int64_t peer = field.src[static_cast<size_t>(best)];
    const WorldPoint a { rw.x0 + (static_cast<double>(best % rw.nx) + 0.5) * kCS, rw.y0 + (static_cast<double>(best / rw.nx) + 0.5) * kCS };
    const WorldPoint b { rw.x0 + (static_cast<double>(peer % rw.nx) + 0.5) * kCS, rw.y0 + (static_cast<double>(peer / rw.nx) + 0.5) * kCS };
    dg->gap_start = a;
    dg->gap_goal = b;
    dg->gap_distance = std::hypot(a.x - b.x, a.y - b.y);
}

void open_access(
    Mask* lim,
    RouteDiag* dg,
    const RouteWindow& rw,
    const std::optional<std::vector<int64_t>>& ac_s,
    const std::optional<std::vector<int64_t>>& ac_g)
{
    const std::optional<std::vector<int64_t>> s = ac_s.has_value() ? ac_s : access_chain(dg, rw, *lim, rw.as);
    const std::optional<std::vector<int64_t>> g = ac_g.has_value() ? ac_g : access_chain(dg, rw, *lim, rw.ag);
    if (s.has_value()) {
        open_cells(lim, *s);
    }
    if (g.has_value()) {
        open_cells(lim, *g);
    }
}

std::vector<std::vector<WorldPoint>> loops_to_world(const RouteWindow& rw, const std::vector<std::vector<WorldPoint>>& loops)
{
    std::vector<std::vector<WorldPoint>> out;
    out.reserve(loops.size());
    for (const auto& L : loops) {
        std::vector<WorldPoint> w;
        w.reserve(L.size());
        for (const auto& p : L) {
            w.push_back({ rw.x0 + p.x * kCS, rw.y0 + p.y * kCS });
        }
        out.push_back(std::move(w));
    }
    return out;
}

void open_link_chains(Mask* limw, const RouteWindow& rw, const Mask& chan0, const Mask& medial, const std::vector<int64_t>& ends)
{
    const std::vector<std::vector<int64_t>> wide = link_chains(rw, chan0, ends);
    std::vector<int64_t> miss;
    for (size_t i = 0; i < ends.size(); ++i) {
        if (wide[i].empty() && chan0.v[static_cast<size_t>(ends[i])] == 0) {
            miss.push_back(ends[i]);
        }
        open_cells(limw, wide[i]);
    }
    for (const std::vector<int64_t>& ch : link_chains(rw, medial, miss)) {
        open_cells(limw, ch);
    }
}

std::vector<WorldPoint> cell_centres(const RouteWindow& rw, const std::vector<CellPt>& P)
{
    std::vector<WorldPoint> out;
    out.reserve(P.size());
    for (const auto& c : P) {
        out.push_back({ rw.x0 + (static_cast<double>(c.x) + 0.5) * kCS, rw.y0 + (static_cast<double>(c.y) + 0.5) * kCS });
    }
    return out;
}

double nearest_height(const std::vector<float>& cur, double ref)
{
    double nearest_h = static_cast<double>(cur.front());
    for (const float v : cur) {
        if (std::fabs(static_cast<double>(v) - ref) < std::fabs(nearest_h - ref)) {
            nearest_h = static_cast<double>(v);
        }
    }
    return nearest_h;
}

std::vector<double> walk_heights(const LayerOracle* lyo, const std::vector<WorldPoint>& out, float lyo_h)
{
    std::vector<double> hz;
    if (lyo == nullptr || out.empty()) {
        return hz;
    }
    std::vector<float> cur { lyo_h };
    hz.push_back(static_cast<double>(lyo_h));
    for (size_t i = 1; i < out.size(); ++i) {
        const std::optional<std::vector<float>> nxt = LayerWalk(*lyo, { out[i - 1], out[i] }, cur);
        if (!nxt.has_value() || nxt->empty()) {
            hz.clear();
            break;
        }
        cur = *nxt;
        hz.push_back(nearest_height(cur, hz.back()));
    }
    return hz;
}

std::vector<double> walk_heights_falling(const LayerOracle* lyo, const std::vector<WorldPoint>& out, float lyo_h)
{
    std::vector<double> hz;
    if (lyo == nullptr || out.empty()) {
        return hz;
    }
    std::vector<float> cur { lyo_h };
    hz.push_back(static_cast<double>(lyo_h));
    for (size_t i = 1; i < out.size(); ++i) {
        const std::vector<float> nxt = LayerWalkFalling(*lyo, { out[i - 1], out[i] }, cur);
        if (nxt.empty()) {
            hz.clear();
            break;
        }
        cur = nxt;
        hz.push_back(nearest_height(cur, hz.back()));
    }
    return hz;
}

// goal_deck 为空时终点格任一 span 都算到达, start_deck 为空时起点取离吸附面最近的 span
std::optional<std::vector<WorldPoint>> route_window(
    RouteDiag* dg,
    WindowInfo* info,
    const BaseNavPlanner& pl,
    const BaseNavPack& pack,
    const WorldPoint& s,
    const WorldPoint& g,
    std::optional<double> goal_deck,
    std::optional<double> start_deck,
    uint16_t zid)
{
    const double t_topo0 = now_ms();
    RouteWindow rw;
    rw.info = info;
    rw.nx = info->nx;
    rw.ny = info->ny;
    rw.x0 = info->x0;
    rw.y0 = info->y0;
    rw.walk = Mask(rw.nx, rw.ny, 0);
    for (size_t i = 0; i < rw.walk.v.size(); ++i) {
        rw.walk.v[i] = static_cast<uint8_t>(info->core.v[i] != 0 && info->lay.v[i] != 0);
    }
    info->lay = Mask();
    rw.dist = std::move(info->dist);
    info->dist = Grid<float>();
    Mask rdg = std::move(info->medial);
    info->medial = Mask();
    const double cpref = kClrPref;
    rw.sc = { static_cast<int64_t>((s.x - rw.x0) / kCS), static_cast<int64_t>((s.y - rw.y0) / kCS) };
    rw.gc = { static_cast<int64_t>((g.x - rw.x0) / kCS), static_cast<int64_t>((g.y - rw.y0) / kCS) };

    // 单价恒 ≥1, 代价低于 limit 或 limit2 的搜索碰不到可信区外的格
    rw.tentative = dg->margin > 0;
    rw.bounded = rw.tentative && !dg->final;
    rw.strict = rw.bounded;
    const int64_t edge_s = std::min({ rw.sc.x, rw.sc.y, rw.nx - 1 - rw.sc.x, rw.ny - 1 - rw.sc.y });
    const int64_t edge_g = std::min({ rw.gc.x, rw.gc.y, rw.nx - 1 - rw.gc.x, rw.ny - 1 - rw.gc.y });
    const int64_t edge = std::min(edge_s, edge_g);
    rw.limit = static_cast<double>(edge - dg->margin);
    rw.limit2 = rw.tentative ? ellipse_floor(rw.sc, rw.gc, dg->margin, rw.nx, rw.ny) : 0.0;
    if (rw.tentative && rw.limit <= 0.0) {
        escalate(dg, "端点离窗边不足");
        return std::nullopt;
    }

    rw.st3 = &info->st3;
    rw.lyo = { .st = rw.st3, .nx = rw.nx, .ny = rw.ny, .x0 = rw.x0, .y0 = rw.y0 };
    rw.goal_deck = goal_deck;
    rw.start_deck = start_deck;
    std::vector<uint8_t> useW;
    spans_in_mask(&useW, &rw.cw3, rw, rw.walk);

    const auto snap0 = nearest_deck_cell(rw, useW, rw.cw3, rw.sc, rw.start_deck);
    const auto snap1 = nearest_deck_cell(rw, useW, rw.cw3, rw.gc, rw.goal_deck);
    const bool goal_off_deck = rw.goal_deck.has_value() && rw.gc.x >= 0 && rw.gc.y >= 0 && rw.gc.x < rw.nx && rw.gc.y < rw.ny
                               && !cell_has_deck(rw, rw.gc, *rw.goal_deck);
    if (!snap0.first.has_value()) {
        dg->err = rw.start_deck.has_value() ? "起点附近没有未封堵的声明面" : "walk 掩膜为空";
        return std::nullopt;
    }
    if (!snap1.first.has_value()) {
        dg->err = rw.goal_deck.has_value() ? "目标附近没有未封堵的声明面" : "walk 掩膜为空";
        return std::nullopt;
    }
    rw.as = snap0.first;
    rw.ag = snap1.first;
    rw.dsa = snap0.second;
    rw.dga = snap1.second;
    if (rw.bounded && (rw.dsa / kCS >= rw.limit || rw.dga / kCS >= rw.limit)) {
        escalate(dg, "吸附距离碰边");
        return std::nullopt;
    }

    rw.faces = &info->sev.steps;
    rw.jspan = JumpEdgesEmpty(info->links_span) ? nullptr : &info->links_span;
    rw.jcell = JumpEdgesEmpty(info->links_cell) ? nullptr : &info->links_cell;
    rw.seen.assign(static_cast<size_t>(rw.nx * rw.ny), 0);
    const PriceField mult { .dist = &rw.dist, .lo = kCS, .hi = cpref };
    // 区间上端取最宽处, 单价恒不低于 1, 欧氏距离才是可采纳的下界
    const PriceField multw { .dist = &rw.dist, .lo = kClrNarrow, .hi = kClrWide };

    // 基线只按长度计价, 硬图可达就一定有解
    const Mask all(rw.nx, rw.ny, 1);
    const PriceField unit {};
    const std::optional<WindowTopo> base = solve_baseline(dg, &rw, TopoSearch { .price = &unit }, all);
    if (rw.strict && !base.has_value()) {
        escalate(dg, "基线不通");
        return std::nullopt;
    }
    if (!base.has_value()) {
        report_gap(dg, rw);
        dg->timing.topology_ms = now_ms() - t_topo0;
        if (rw.goal_deck.has_value()) {
            const std::vector<int64_t> gv = spans_at(rw, *rw.ag, useW);
            std::vector<float> hv;
            hv.reserve(gv.size());
            for (const int64_t v : gv) {
                hv.push_back(rw.st3->sp_h[static_cast<size_t>(v)]);
            }
            std::sort(hv.begin(), hv.end());
            hv.erase(std::unique(hv.begin(), hv.end()), hv.end());
            std::string list;
            char buf[32];
            for (const float h : hv) {
                std::snprintf(buf, sizeof(buf), "%.2f", static_cast<double>(h));
                list += (list.empty() ? "" : ", ") + std::string(buf);
            }
            std::snprintf(buf, sizeof(buf), "%.2f", *rw.goal_deck);
            dg->err =
                "目标面不可达 (声明 " + std::string(buf) + ", 终点格里的面 " + (list.empty() ? std::string("无") : "[" + list + "]") + ")";
            return std::nullopt;
        }
        dg->err = "不连通";
        return std::nullopt;
    }
    useW = std::vector<uint8_t>();

    const double tax = kStepTax;

    const EdgeBits* bn = tax > 0.0 ? &info->step_edges : nullptr;
    const double* bp = tax > 0.0 ? &tax : nullptr;
    // 拓扑层把路面单价抬了 kClrWide / cpref 倍, 台阶税要同比换算
    const double taxw = tax * kClrWide / cpref;
    const double* bpw = tax > 0.0 ? &taxw : nullptr;
    Mask chan0 = channel_mask(rw, cpref, Mask(rw.nx, rw.ny, 0));
    const std::optional<std::vector<int64_t>> ac_s = access_chain(dg, rw, chan0, rw.as);
    const std::optional<std::vector<int64_t>> ac_g = access_chain(dg, rw, chan0, rw.ag);
    if (dg->escalate) {
        return std::nullopt;
    }
    // 跳边端点须接到 chan0, 缝沿的中轴常是孤立碎片, 先只为基线经过的跳边开链
    std::vector<int64_t> lends;
    if (rw.jspan != nullptr && base->qs.has_value()) {
        const std::vector<int64_t>& q = *base->qs;
        for (size_t i = 1; i < q.size(); ++i) {
            if (HasJumpEdge(*rw.jspan, q[i - 1], q[i])) {
                lends.push_back(rw.st3->sp_cell[static_cast<size_t>(q[i - 1])]);
                lends.push_back(rw.st3->sp_cell[static_cast<size_t>(q[i])]);
            }
        }
    }
    const auto loops_core = loops_to_world(rw, TraceContours(info->core));

    Mask limw = channel_mask(rw, cpref, rdg);
    rdg = Mask();
    open_access(&limw, dg, rw, ac_s, ac_g);
    if (dg->escalate) {
        return std::nullopt;
    }
    // 接不到宽处的端点(窄路上的跳边)退一步接到中轴
    const Mask medial = limw;
    open_link_chains(&limw, rw, chan0, medial, lends);
    if (!JumpEdgesEmpty(info->links_cell) && !ends_linked(&rw, limw)) {
        lends.clear();
        for (const JumpEdge& e : info->links_cell.e) {
            lends.push_back(e.src);
            lends.push_back(e.dst);
        }
        open_link_chains(&limw, rw, chan0, medial, lends);
    }
    chan0 = Mask();
    std::optional<WindowTopo> vv = solve_channel(dg, &rw, TopoSearch { .price = &multw, .banned = bn, .bnp = bpw }, limw);
    if (rw.strict && !vv.has_value()) {
        escalate(dg, "通道未接通");
        return std::nullopt;
    }
    if (!vv.has_value()) {
        limw = Mask(rw.nx, rw.ny, 1);
        vv = solve_channel(dg, &rw, TopoSearch { .price = &multw, .banned = bn, .bnp = bpw }, limw);
        dg->warn.emplace_back("通道未接通, 退到全图亏欠计价");
    }
    {
        const WindowTopo& tp = vv.has_value() ? *vv : *base;
        dg->topology_cells.reserve(tp.q.size());
        for (const CellPt& c : tp.q) {
            dg->topology_cells.push_back(
                { rw.x0 + (static_cast<double>(c.x) + 0.5) * kCS, rw.y0 + (static_cast<double>(c.y) + 0.5) * kCS });
        }
        if (tp.qs.has_value()) {
            dg->topology_heights.reserve(tp.qs->size());
            for (const int64_t v : *tp.qs) {
                dg->topology_heights.push_back(static_cast<double>(rw.st3->sp_h[static_cast<size_t>(v)]));
            }
        }
    }
    dg->timing.topology_ms = now_ms() - t_topo0;
    const double t_geo0 = now_ms();
    // 弦只许落在实心区内, 对中脊取直会把线拽向墙
    const double solid_c = cpref + kGeoMargin;
    Mask band;
    Mask solidc(rw.nx, rw.ny, 0);
    {
        Mask bb(rw.nx, rw.ny, 0);
        for (const CellPt& c : (vv.has_value() ? vv->q : base->q)) {
            bb.at(c.y, c.x) = 1;
        }
        const Grid<float> cw = CorridorWidth(rw.dist, bb, &band);
        for (size_t i = 0; i < solidc.v.size(); ++i) {
            const double need = std::max(solid_c, kChordFrac * static_cast<double>(cw.v[i]));
            solidc.v[i] = static_cast<uint8_t>(static_cast<double>(rw.dist.v[i]) >= need);
        }
        // 净空场只在可信区内与整类窗口相同
        if (rw.bounded) {
            const int64_t m = dg->margin;
            for (int64_t y = 0; y < rw.ny; ++y) {
                for (int64_t x = 0; x < rw.nx; ++x) {
                    if (band.at(y, x) != 0 && (x < m || y < m || x >= rw.nx - m || y >= rw.ny - m)) {
                        escalate(dg, "走廊碰边");
                        return std::nullopt;
                    }
                }
            }
        }
    }
    const BlockerOnMask onv { &solidc, rw.x0, rw.y0, kCS };
    const BlockerSegments no_segs;
    Blockers blk_vis = MakeBlockers(no_segs, onv);
    Visibility vis { .blk = &blk_vis, .lyo = &rw.lyo, .faces = rw.faces, .steps = bn, .nx = rw.nx, .ny = rw.ny, .x0 = rw.x0, .y0 = rw.y0 };
    if (vv.has_value()) {
        // 锁在走廊里, 否则按恒一单价重解会挑回拓扑绕开的窄分支
        Mask limg = limw;
        for (size_t i = 0; i < limg.v.size(); ++i) {
            limg.v[i] = static_cast<uint8_t>(limg.v[i] != 0 && band.v[i] != 0);
        }
        band = Mask();
        std::optional<WindowTopo> tt = solve_channel(dg, &rw, TopoSearch { .price = &mult, .banned = bn, .bnp = bp, .vis = &vis }, limg);
        if (rw.strict && !tt.has_value()) {
            escalate(dg, "走廊内几何不通");
            return std::nullopt;
        }
        if (!tt.has_value()) {
            tt = solve_channel(dg, &rw, TopoSearch { .price = &mult, .banned = bn, .bnp = bp, .vis = &vis }, limw);
        }
        if (tt.has_value()) {
            vv = std::move(tt);
        }
    }
    dg->timing.geometry_ms = now_ms() - t_geo0;
    const WindowTopo& win = vv.has_value() ? *vv : *base;
    for (const std::string& w : win.warn) {
        dg->warn.push_back(w);
    }
    Mask on3 = win.on3;
    const std::optional<std::vector<CellPt>> q = win.q;
    const std::optional<std::vector<int64_t>>& qs = win.qs;
    dg->snap_start = rw.dsa;
    dg->snap_goal = rw.dga;
    // 末段锚点直连终点不查阻挡, 终点在可走面上却不在可达集里就是穿墙
    if (rw.gc.x >= 0 && rw.gc.y >= 0 && rw.gc.x < rw.nx && rw.gc.y < rw.ny) {
        // 须用基线的硬可达集, 舒适通道不含贴墙的终点
        dg->hop_barrier = !goal_off_deck && rw.walk.at(rw.gc.y, rw.gc.x) != 0 && base->on3.at(rw.gc.y, rw.gc.x) == 0;
    }
    std::vector<size_t> bad;
    for (size_t k = 1; k < q->size(); ++k) {
        const int64_t ca = (*q)[k - 1].y * rw.nx + (*q)[k - 1].x;
        const int64_t cb = (*q)[k].y * rw.nx + (*q)[k].x;
        if (HasEdgeBit(info->sev.steps, ca, cb) && !HasJumpEdge(info->links_cell, ca, cb)) {
            bad.push_back(k);
        }
    }
    if (!bad.empty()) {
        dg->warn.push_back("不可避立面 " + std::to_string(bad.size()) + " 步");
    }
    if (info->links_dropped != 0) {
        dg->warn.push_back("离网连接 " + std::to_string(info->links_dropped) + " 条落不到面");
    }
    dg->crossed_barrier = !bad.empty();

    // 取直与计价须共用一道挡线, 再把选定路径放宽一格, 免得接入链被自己的挡线判掉
    Mask tm = solidc;
    open_access(&tm, dg, rw, ac_s, ac_g);
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
                if (bx >= 0 && by >= 0 && bx < rw.nx && by < rw.ny && on3.at(by, bx) != 0) {
                    tm.at(by, bx) = 1;
                }
            }
        }
    }
    const BlockerOnMask onm { &tm, rw.x0, rw.y0, kCS };
    const BlockerSegments core_segs = MakeBlockerSegments(loops_core, &info->segA, &info->segB);
    Blockers blk_gray = MakeBlockers(core_segs, onm);
    Visibility
        vis_geo { .blk = &blk_gray, .lyo = &rw.lyo, .faces = rw.faces, .steps = bn, .nx = rw.nx, .ny = rw.ny, .x0 = rw.x0, .y0 = rw.y0 };

    const bool by_corn = win.corn.size() >= 2;
    const std::vector<int64_t>& gsrc = by_corn ? win.corn : (qs.has_value() ? *qs : win.corn);
    std::vector<float> hs;
    if (by_corn || qs.has_value()) {
        hs.reserve(gsrc.size());
        for (const int64_t v : gsrc) {
            hs.push_back(rw.st3->sp_h[static_cast<size_t>(v)]);
        }
    }
    std::vector<CellPt> gq;
    if (by_corn) {
        gq.reserve(gsrc.size());
        for (const int64_t v : gsrc) {
            const int64_t c = rw.st3->sp_cell[static_cast<size_t>(v)];
            gq.push_back({ c % rw.nx, c / rw.nx });
        }
    }
    const double t_pull0 = now_ms();
    std::vector<WorldPoint> taut = cell_centres(rw, by_corn ? gq : *q);
    dg->taut_points = taut;
    if (taut.size() >= 2) {
        taut = StringPull(&blk_gray, by_corn ? &vis_geo : nullptr, taut, hs.empty() ? nullptr : &rw.lyo, hs.empty() ? nullptr : &hs);
    }
    dg->pulled_points = taut;
    dg->timing.pull_ms = now_ms() - t_pull0;

    const double t_asm0 = now_ms();
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
    const LayerOracle* lyo_p = (by_corn || qs.has_value()) ? &rw.lyo : nullptr;
    const float lyo_h = hs.empty() ? 0.0F : hs.front();
    std::vector<WorldPoint> out;
    if (by_corn && ded.size() >= 2) {
        // 只删共线冗余点, a-b 与 b-c 都可见且共线则 a-c 必可见
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
    dg->assembled_points = out;
    dg->timing.assemble_ms = now_ms() - t_asm0;
    // 只往前对回父链, 叠层处才不会对到另一遍上
    if (!hs.empty() && out.size() >= 2) {
        size_t k = 0;
        dg->span_height.push_back(static_cast<double>(hs.front()));
        for (size_t i = 1; i + 1 < out.size(); ++i) {
            while (k < dg->taut_points.size() && (dg->taut_points[k].x != out[i].x || dg->taut_points[k].y != out[i].y)) {
                ++k;
            }
            if (k == dg->taut_points.size()) {
                dg->span_height.clear();
                break;
            }
            dg->span_height.push_back(static_cast<double>(hs[k]));
        }
        if (!dg->span_height.empty()) {
            dg->span_height.push_back(static_cast<double>(hs.back()));
        }
    }

    const double t_lift0 = now_ms();
    // 抬升须在取直之后, 判据用硬墙, 通道掩膜只比路径宽一格会锁死拐点
    if (lyo_p != nullptr && out.size() >= 3) {
        Blockers blk_hard = MakeBlockers(core_segs, std::nullopt);
        Visibility vis_hard { .blk = &blk_hard,
                              .lyo = &rw.lyo,
                              .faces = rw.faces,
                              .steps = bn,
                              .nx = rw.nx,
                              .ny = rw.ny,
                              .x0 = rw.x0,
                              .y0 = rw.y0 };
        lift_corners(&out, &vis_hard, rw.dist, rw.lyo, lyo_h, kLiftMax);
        for (size_t i = 1; i + 1 < out.size() && dg->span_height.size() == out.size(); ++i) {
            if (out[i].x == dg->assembled_points[i].x && out[i].y == dg->assembled_points[i].y) {
                continue;
            }
            const auto v = span_near(
                rw.lyo,
                static_cast<int64_t>(std::floor((out[i].x - rw.x0) / kCS)),
                static_cast<int64_t>(std::floor((out[i].y - rw.y0) / kCS)),
                dg->span_height[i]);
            if (!v.has_value()) {
                dg->span_height.clear();
                break;
            }
            dg->span_height[i] = *v;
        }
    }
    dg->timing.lift_ms = now_ms() - t_lift0;
    dg->clearance.reserve(out.size());
    for (const auto& p : out) {
        const int64_t cx = std::min(std::max(static_cast<int64_t>(std::floor((p.x - info->x0) / kCS)), int64_t { 0 }), rw.nx - 1);
        const int64_t cy = std::min(std::max(static_cast<int64_t>(std::floor((p.y - info->y0) / kCS)), int64_t { 0 }), rw.ny - 1);
        dg->clearance.push_back(static_cast<double>(rw.dist.at(cy, cx)));
    }
    dg->height = walk_heights(lyo_p, out, lyo_h);
    if (info->fall_r > 0.0) {
        // 优先用搜索挑的面高, 放行下落的走查在叠层处会漂到楼下
        const bool by_span = dg->span_height.size() == out.size();
        std::vector<double> hz = by_span ? dg->span_height : dg->height.empty() ? walk_heights_falling(lyo_p, out, lyo_h) : dg->height;
        if (hz.size() == out.size()) {
            push_drop_landings(&out, &hz, dg, rw.dist, info->fall_r, rw.lyo);
            if (by_span) {
                for (size_t i = 0; i < dg->height.size(); ++i) {
                    if (hz[i] != dg->span_height[i]) {
                        dg->height[i] = hz[i];
                    }
                }
                dg->span_height = std::move(hz);
            }
            else if (!dg->height.empty()) {
                dg->height = std::move(hz);
            }
        }
    }
    if (lyo_p != nullptr) {
        pull_waypoints(dg, &blk_gray, out, pl, pack, zid);
    }
    return out;
}

// 每个 ZoneClean 常驻几十到两百 MB
constexpr size_t kZoneCacheMax = 2;

// 一次规划沿递归往下传的约束
struct PlanScope
{
    MaaContext* stop_context = nullptr;
    const std::vector<uint32_t>* rope_off = nullptr;
    bool hopping = false; // 正在规划跨类下落的两段, 它们自己不再往下接
    bool roping = false;  // 正在规划升降索的两段, 它们自己不再接升降索
};

bool plan_stopping(MaaContext* context)
{
    return context != nullptr && MaaTaskerStopping(MaaContextGetTasker(context));
}

uint64_t zone_rank(const RecastZoneEntry& e)
{
    return ZoneCleanValid(*e.zc) ? e.used_at : 0;
}

// 返回的引用在下一次 zone_entry 后可能被淘汰, 不得跨调用持有
RecastZoneEntry& zone_entry(RecastNavEngine* engine, const BaseNavPack& pack, const BaseNavPlanner& planner, const std::string& name)
{
    auto it = engine->zones.find(name);
    if (it == engine->zones.end()) {
        // 先淘汰再建, 免得新旧两个大区同时占内存
        const BaseNavZone* zone = FindBaseNavZoneByName(pack, name);
        const bool real = zone != nullptr && zone->triangle_count > 0;
        while (real && engine->zones.size() >= kZoneCacheMax) {
            auto victim = engine->zones.begin();
            for (auto z = engine->zones.begin(); z != engine->zones.end(); ++z) {
                if (zone_rank(z->second) < zone_rank(victim->second)) {
                    victim = z;
                }
            }
            engine->zones.erase(victim);
        }
        RecastZoneEntry e;
        e.zc = std::make_unique<ZoneClean>(BuildZoneClean(pack, planner, name));
        if (const FieldsZoneDir* fzd = FindFieldsZone(*engine->fields, name); fzd != nullptr) {
            e.fz = std::make_unique<FieldsZone>();
            if (!LoadFieldsZone(e.fz.get(), &e.fields_error, *engine->fields, *fzd)) {
                e.fz.reset();
            }
        }
        else {
            e.fields_error = "旁包里没有这个区";
        }
        it = engine->zones.emplace(name, std::move(e)).first;
    }
    it->second.used_at = ++engine->zone_clock;
    return it->second;
}

RecastPlanResult plan_locked(
    RecastNavEngine* engine,
    const BaseNavPack& pack,
    const BaseNavPlanner& planner,
    const PlanScope& scope,
    const RecastPlanRequest& req);

// 走路不比坐索远的绳索省不了路, 每根只量一次
double rope_walk_px(
    RecastNavEngine* engine,
    const BaseNavPack& pack,
    const BaseNavPlanner& planner,
    const PlanScope& rope_scope,
    const std::string& zone_name,
    uint16_t zone_id,
    const FieldsRopeRec& rope)
{
    const uint64_t key = (static_cast<uint64_t>(zone_id) << 32) | rope.uid_bottom;
    if (const auto it = engine->rope_walk.find(key); it != engine->rope_walk.end()) {
        return it->second;
    }
    // 往上走不靠台沿下落, 量这段时不接下落
    PlanScope walk_scope = rope_scope;
    walk_scope.hopping = true;
    const RecastPlanRequest walk_req {
        .zone_name = zone_name,
        .start = link_side_centre(rope.bottom),
        .goal = link_side_centre(rope.top),
        .goal_deck_y = rope.top.h,
        .start_deck_y = rope.bottom.h,
    };
    const RecastPlanResult w = plan_locked(engine, pack, planner, walk_scope, walk_req);
    const double walk = w.ok ? w.length : std::numeric_limits<double>::infinity();
    engine->rope_walk.emplace(key, walk);
    return walk;
}

bool listed_uid(const std::vector<uint32_t>& v, uint32_t uid)
{
    return std::find(v.begin(), v.end(), uid) != v.end();
}

constexpr int64_t kTrustMargin = 64; // 可信余量, 格, 须盖过净空截断 kEdtCells 与中轴邻域
constexpr int64_t kLocalMaxCells = 3'000'000;
constexpr double kRatio0 = 2.0;      // 首档允许的代价与直线距离之比
constexpr double kRatioStep = 2.0;
constexpr double kCostSlack = 200.0; // 短腿的固定代价余量, 格
static_assert(kTrustMargin > kEdtCells + 8);

struct WindowRect
{
    int64_t x0 = 0, y0 = 0, nx = 0, ny = 0;
};

int64_t window_cells(const WindowRect& r)
{
    return r.nx * r.ny;
}

enum class WindowKind
{
    Tentative, // 小窗, 全套验收
    Capped,    // 面积到上限, 关掉与窗口大小有关的验收
    Full       // 整类窗口, 不做验收
};

// 以起终点为焦点的搜索椭圆, 单位是格; b* 是起点、起点吸附点与终点所在格的包围盒
struct WindowEllipse
{
    double fsx = 0.0;
    double fsy = 0.0;
    double fgx = 0.0;
    double fgy = 0.0;
    double leg = 0.0;
    double ang = 0.0;
    int64_t bx0 = 0;
    int64_t bx1 = 0;
    int64_t by0 = 0;
    int64_t by1 = 0;
};

int64_t cell_of(double v)
{
    return static_cast<int64_t>(std::floor(v / kCS));
}

// 搜索椭圆外接矩形, 半长轴 C/2, 半短轴 sqrt(C²-L²)/2
WindowRect ellipse_rect(const WindowEllipse& e, double cost)
{
    const double a = cost / 2.0;
    const double b = std::sqrt(std::max(cost * cost - e.leg * e.leg, 0.0)) / 2.0;
    const double c = std::cos(e.ang);
    const double s = std::sin(e.ang);
    const double ex = std::sqrt(a * a * c * c + b * b * s * s);
    const double ey = std::sqrt(a * a * s * s + b * b * c * c);
    const double cx = (e.fsx + e.fgx) / 2.0;
    const double cy = (e.fsy + e.fgy) / 2.0;
    constexpr int64_t halo = kTrustMargin + 8;
    const int64_t x0 = std::min(e.bx0, static_cast<int64_t>(std::floor(cx - ex))) - halo;
    const int64_t x1 = std::max(e.bx1, static_cast<int64_t>(std::ceil(cx + ex))) + halo;
    const int64_t y0 = std::min(e.by0, static_cast<int64_t>(std::floor(cy - ey))) - halo;
    const int64_t y1 = std::max(e.by1, static_cast<int64_t>(std::ceil(cy + ey))) + halo;
    return WindowRect { x0, y0, x1 - x0 + 1, y1 - y0 + 1 };
}

std::pair<WindowRect, WindowKind> tier_window(const WindowEllipse& e, const WindowRect& region_rect, int tier)
{
    const double cost = e.leg * kRatio0 * std::pow(kRatioStep, tier) + kCostSlack;
    WindowRect r = ellipse_rect(e, cost);
    const bool covers = r.x0 <= region_rect.x0 && r.y0 <= region_rect.y0 && r.x0 + r.nx >= region_rect.x0 + region_rect.nx
                        && r.y0 + r.ny >= region_rect.y0 + region_rect.ny;
    // 椭圆盖住整类或面积差不到两成时直接用整类窗口
    if (window_cells(region_rect) <= kLocalMaxCells && (covers || window_cells(r) * 5 >= window_cells(region_rect) * 4)) {
        return { region_rect, WindowKind::Full };
    }
    if (window_cells(r) <= kLocalMaxCells) {
        return { r, WindowKind::Tentative };
    }
    double lo = e.leg;
    double hi = cost;
    if (window_cells(ellipse_rect(e, lo)) > kLocalMaxCells) {
        return { ellipse_rect(e, lo), WindowKind::Capped };
    }
    for (int i = 0; i < 40; ++i) {
        const double mid = (lo + hi) / 2.0;
        (window_cells(ellipse_rect(e, mid)) <= kLocalMaxCells ? lo : hi) = mid;
    }
    return { ellipse_rect(e, lo), WindowKind::Capped };
}

// [a, b] 包围盒外扩 pad 的格图块, 不读场数据
bool load_patch(
    GridPatch* p,
    const GridPack& gp,
    const GridZoneDir& gz,
    std::span<const FieldsOpenRec> opn,
    const WorldPoint& a,
    const WorldPoint& b,
    double pad)
{
    const auto r = static_cast<int64_t>(std::ceil(pad / kCS));
    const int64_t ax = static_cast<int64_t>(std::floor(a.x / kCS));
    const int64_t ay = static_cast<int64_t>(std::floor(a.y / kCS));
    const int64_t bx = static_cast<int64_t>(std::floor(b.x / kCS));
    const int64_t by = static_cast<int64_t>(std::floor(b.y / kCS));
    const int64_t gx0 = std::min(ax, bx) - r;
    const int64_t gy0 = std::min(ay, by) - r;
    p->nx = std::max(ax, bx) + r - gx0 + 1;
    p->ny = std::max(ay, by) + r - gy0 + 1;
    p->x0 = static_cast<double>(gx0) * kCS;
    p->y0 = static_cast<double>(gy0) * kCS;
    return load_grid_window(&p->gw, gp, gz, nullptr, opn.data(), opn.size(), gx0, gy0, p->nx, p->ny);
}

std::vector<uint32_t>
    ropes_out_of_reach(const FieldsPack& fp, uint16_t zone_id, uint32_t region, const WindowInfo& info, const WindowRect& win)
{
    std::vector<uint32_t> out;
    const std::span<const FieldsRopeRec> ropes = FieldsRopesOfZone(fp, zone_id);
    for (size_t i = 0; i < ropes.size(); ++i) {
        const FieldsLinkSide& b = ropes[i].bottom;
        const int64_t cx = b.gx - win.x0;
        const int64_t cy = b.gy - win.y0;
        if (b.rid != region || cx < 0 || cy < 0 || cx >= win.nx || cy >= win.ny) {
            continue;
        }
        const int64_t j = OccupiedIndex(info.st3, cy * win.nx + cx);
        if (j < 0) {
            continue;
        }
        for (int64_t k = CellSpanStart(info.st3, j), ke = k + CellSpanCount(info.st3, j); k < ke; ++k) {
            const auto sk = static_cast<size_t>(k);
            if (info.st3.sp_h[sk] == b.h && info.vis3[sk] != 0 && info.reach3[sk] == 0) {
                out.push_back(ropes[i].uid_bottom);
            }
        }
    }
    return out;
}

void fill_debug(RecastPlanResult* res, RouteDiag* dg, const WindowRect& win, int tier, double window_ms, double t_all0)
{
    res->debug.timing = dg->timing;
    res->debug.timing.window_ms = window_ms;
    res->debug.timing.total_ms = now_ms() - t_all0;
    res->debug.x0 = static_cast<double>(win.x0) * kCS;
    res->debug.y0 = static_cast<double>(win.y0) * kCS;
    res->debug.nx = win.nx;
    res->debug.ny = win.ny;
    res->debug.cell_size = kCS;
    res->debug.tier = tier;
    res->debug.topology_cells = std::move(dg->topology_cells);
    res->debug.topology_heights = std::move(dg->topology_heights);
    res->debug.taut_points = std::move(dg->taut_points);
    res->debug.pulled_points = std::move(dg->pulled_points);
    res->debug.assembled_points = std::move(dg->assembled_points);
    res->debug.gap_start = dg->gap_start;
    res->debug.gap_goal = dg->gap_goal;
    res->debug.gap_distance = dg->gap_distance;
    res->debug.warnings = dg->warn;
}

// 依次试跨类下落, 第一个两段都接得上的合成整条
std::optional<RecastPlanResult> plan_drop_hops(
    RecastNavEngine* engine,
    const BaseNavPack& pack,
    const BaseNavPlanner& planner,
    const PlanScope& scope,
    const RecastPlanRequest& req,
    const std::vector<DropHop>& hops)
{
    PlanScope hop_scope = scope;
    hop_scope.hopping = true;
    for (const DropHop& hop : hops) {
        if (plan_stopping(scope.stop_context)) {
            break;
        }
        RecastPlanRequest to_take = req;
        to_take.goal = hop.take;
        to_take.goal_floor_y = kBaseNavFloorYNone;
        to_take.goal_deck_y = static_cast<float>(hop.take_h);
        RecastPlanResult a = plan_locked(engine, pack, planner, hop_scope, to_take);
        if (!a.ok) {
            continue;
        }
        RecastPlanRequest from_land = req;
        from_land.start = hop.land;
        from_land.start_floor_y = kBaseNavFloorYNone;
        from_land.goal_deck_y = kBaseNavFloorYNone;
        from_land.start_deck_y = static_cast<float>(hop.land_h);
        RecastPlanResult b = plan_locked(engine, pack, planner, hop_scope, from_land);
        if (!b.ok) {
            continue;
        }
        return join_drop_hop(std::move(a), b, hop.land_h);
    }
    return std::nullopt;
}

// 只在总代价低于 best_cost 时返回
std::optional<RecastPlanResult> try_ropes(
    RecastNavEngine* engine,
    const BaseNavPack& pack,
    const BaseNavPlanner& planner,
    const PlanScope& scope,
    const RecastPlanRequest& req,
    uint16_t zone_id,
    uint32_t start_region,
    uint32_t goal_region,
    double best_cost,
    size_t max_tries,
    const std::vector<uint32_t>& out_of_reach)
{
    const std::span<const FieldsRopeRec> recs = FieldsRopesOfZone(*engine->fields, zone_id);
    if (scope.roping || scope.hopping || scope.rope_off == nullptr || recs.empty()) {
        return std::nullopt;
    }
    const WorldPoint& start = req.start;
    const WorldPoint& goal = req.goal;

    // bound 是按直线距离算的总路程下界
    struct Cand
    {
        double bound = 0.0;
        const FieldsRopeRec* rope = nullptr;
    };

    std::vector<Cand> cands;
    for (size_t i = 0; i < recs.size(); ++i) {
        const FieldsRopeRec& r = recs[i];
        if (r.bottom.rid != start_region || listed_uid(*scope.rope_off, r.uid_bottom) || listed_uid(out_of_reach, r.uid_bottom)) {
            continue;
        }
        const WorldPoint b = link_side_centre(r.bottom);
        const WorldPoint t = link_side_centre(r.top);
        const double bound = std::hypot(b.x - start.x, b.y - start.y) + r.cost_px + std::hypot(goal.x - t.x, goal.y - t.y);
        if (bound < best_cost) {
            cands.push_back({ .bound = bound, .rope = &r });
        }
    }
    // 绳顶已在终点类的先试, 免得名额被还要再换层的绳索占满
    std::sort(cands.begin(), cands.end(), [goal_region](const Cand& p, const Cand& q) {
        return std::make_tuple(p.rope->top.rid != goal_region, p.bound, p.rope->uid_bottom)
               < std::make_tuple(q.rope->top.rid != goal_region, q.bound, q.rope->uid_bottom);
    });

    PlanScope rope_scope = scope;
    rope_scope.roping = true;
    std::optional<RecastPlanResult> best;
    double best_total = best_cost;
    size_t tried = 0;
    for (const Cand& c : cands) {
        if (tried >= max_tries || plan_stopping(scope.stop_context)) {
            break;
        }
        if (c.bound >= best_total) {
            continue;
        }
        const FieldsRopeRec& rope = *c.rope;
        if (rope_walk_px(engine, pack, planner, rope_scope, req.zone_name, zone_id, rope) <= rope.cost_px) {
            continue;
        }
        ++tried;
        const WorldPoint bottom = link_side_centre(rope.bottom);
        const WorldPoint top = link_side_centre(rope.top);
        RecastPlanRequest to_bottom = req;
        to_bottom.goal = bottom;
        to_bottom.goal_floor_y = kBaseNavFloorYNone;
        to_bottom.goal_deck_y = rope.bottom.h;
        RecastPlanResult a = plan_locked(engine, pack, planner, rope_scope, to_bottom);
        if (!a.ok || a.length + rope.cost_px + std::hypot(goal.x - top.x, goal.y - top.y) >= best_total) {
            continue;
        }
        RecastPlanRequest from_top = req;
        from_top.start = top;
        from_top.start_floor_y = kBaseNavFloorYNone;
        from_top.start_deck_y = rope.top.h;
        RecastPlanResult b = plan_locked(engine, pack, planner, rope_scope, from_top);
        if (!b.ok || a.length + rope.cost_px + b.length >= best_total) {
            continue;
        }
        best_total = a.length + rope.cost_px + b.length;
        best = join_rope_hop(std::move(a), b, rope);
    }
    return best;
}

RecastPlanResult plan_locked(
    RecastNavEngine* engine,
    const BaseNavPack& pack,
    const BaseNavPlanner& planner,
    const PlanScope& scope,
    const RecastPlanRequest& req)
{
    const std::string& zone_name = req.zone_name;
    const WorldPoint& start = req.start;
    const WorldPoint& goal = req.goal;
    const float start_floor_y = req.start_floor_y;
    const float goal_floor_y = req.goal_floor_y;
    const float goal_deck_y = req.goal_deck_y;
    const float start_deck_y = req.start_deck_y;
    const std::vector<BaseNavNoGoDisc>& no_go_discs = req.no_go_discs;
    const double t_all0 = now_ms();
    RecastPlanResult res;
    if (!GridPackParsed(engine->grid)) {
        res.error = engine->grid_error;
        return res;
    }
    const GridZoneDir* gz = FindGridZone(engine->grid, zone_name);
    if (gz == nullptr) {
        res.error = "区没有预烘格图 (" + zone_name + ")";
        return res;
    }
    RecastZoneEntry& ze = zone_entry(engine, pack, planner, zone_name);
    if (!ZoneCleanValid(*ze.zc)) {
        res.error = ze.zc->error;
        return res;
    }
    const FieldsZoneDir* fzd = FindFieldsZone(*engine->fields, zone_name);
    if (fzd == nullptr || ze.fz == nullptr) {
        res.error = "旁包读不出这个区 (" + zone_name + "): " + ze.fields_error;
        return res;
    }
    const FieldsZone* fz = ze.fz.get();
    const ZoneClean& zc = *ze.zc;
    const std::optional<double> sfl =
        start_floor_y > kBaseNavFloorYValidMin ? std::optional<double>(static_cast<double>(start_floor_y)) : std::nullopt;
    const std::optional<double> gfl =
        goal_floor_y > kBaseNavFloorYValidMin ? std::optional<double>(static_cast<double>(goal_floor_y)) : std::nullopt;
    const std::optional<double> gdk =
        goal_deck_y > kBaseNavFloorYValidMin ? std::optional<double>(static_cast<double>(goal_deck_y)) : std::nullopt;
    const std::optional<double> sdk =
        start_deck_y > kBaseNavFloorYValidMin ? std::optional<double>(static_cast<double>(start_deck_y)) : std::nullopt;

    const std::span<const FieldsOpenRec> zone_opens = FieldsOpensOfZone(*engine->fields, zc.zone_id);
    GridPatch ps;
    GridPatch pg;
    bool patched = false;
    // 终点声明了面时起点优先吸到含终点类面的格, 没有再照旧吸
    bool deck_region_known = false;
    uint32_t deck_region = 0;
    if (gdk.has_value()) {
        // 起点块盖住兜底吸附半径内任一吸附点再外扩一个定类半径
        patched = load_patch(&ps, engine->grid, *gz, zone_opens, start, start, kSnapFallbackRadius + kSnapRadius)
                  && load_patch(&pg, engine->grid, *gz, zone_opens, goal, goal, kSnapRadius);
        const int64_t deck_rec = patched ? pick_deck_rec(
                                     pg.gw,
                                     pg.nx,
                                     pg.ny,
                                     static_cast<int64_t>((goal.x - pg.x0) / kCS),
                                     static_cast<int64_t>((goal.y - pg.y0) / kCS),
                                     *gdk)
                                         : -1;
        if (deck_rec >= 0) {
            deck_region = pg.gw.rec[static_cast<size_t>(deck_rec)].rid;
            deck_region_known = true;
        }
    }
    std::optional<ZoneSnapHit> ss;
    if (sdk.has_value()) {
        ss = ZoneSnapOnDeck(zc, start, kSnapRadius, *sdk);
    }
    else if (deck_region_known) {
        ss = snap_seeding_region(zc, ps, deck_region, start, sfl);
    }
    if (!ss.has_value()) {
        ss = ZoneSnap(zc, start, kSnapRadius, sfl);
    }
    if (!ss.has_value()) {
        res.error = "起点不在网格附近";
        return res;
    }
    const auto gs = ZoneSnap(zc, goal, kSnapRadius, gfl);
    if (!gs.has_value()) {
        res.error = "终点不在网格附近";
        return res;
    }
    const double h0 = tri_height_of(zc.mesh, ss->tri);

    const std::vector<NoGoPoly>* nogo = FindNoGoZone(engine->nogo, zone_name);
    if (nogo != nullptr) {
        std::string hit;
        const bool in_start = NoGoContains(*nogo, start, h0, hit);
        if (in_start || NoGoContains(*nogo, goal, tri_height_of(zc.mesh, gs->tri), hit)) {
            res.no_go = true;
            res.error = std::string(in_start ? "起点" : "终点") + "在虚拟禁区内";
            if (!hit.empty()) {
                res.error += " (" + hit + ")";
            }
            return res;
        }
    }

    const BaseNavZone* pz = FindBaseNavZoneByName(pack, zone_name);
    const std::shared_ptr<const OccluderScene> solid =
        pz != nullptr ? LoadOccluderScene(OccluderSidecarPath(pack.path), zone_name) : nullptr;

    // 全区锚距是任何窗口里锚距的下界, 过不了这道闸的腿换多大窗口也接不上
    const auto [zsa, zga] = core_anchor_px(engine->grid, *gz, { start, goal });
    if (zsa > kSnapRadius || zga > kSnapRadius) {
        char buf[128];
        std::snprintf(buf, sizeof(buf), "端点接不上可走层 (起 %.1fpx / 终 %.1fpx)", zsa, zga);
        res.error = buf;
        return res;
    }

    if (!patched
        && (!load_patch(&ps, engine->grid, *gz, zone_opens, start, ss->point, kSnapRadius)
            || (gdk.has_value() && !load_patch(&pg, engine->grid, *gz, zone_opens, goal, goal, kSnapRadius)))) {
        res.error = "预烘格图解不开";
        return res;
    }
    std::string err;
    const std::optional<RegionSeed> seed = pick_region(&err, ps, pg, start, ss->point, goal, h0, gdk);
    if (!seed) {
        res.error = err;
        return res;
    }
    const uint32_t region = seed->region;
    ps = GridPatch {};
    pg = GridPatch {};

    const int64_t bx0 = std::min({ cell_of(start.x), cell_of(ss->point.x), cell_of(goal.x) });
    const int64_t bx1 = std::max({ cell_of(start.x), cell_of(ss->point.x), cell_of(goal.x) });
    const int64_t by0 = std::min({ cell_of(start.y), cell_of(ss->point.y), cell_of(goal.y) });
    const int64_t by1 = std::max({ cell_of(start.y), cell_of(ss->point.y), cell_of(goal.y) });
    // 外扩 kFieldHalo 后类边缘的场与整区图上算的相同
    auto zbit = ze.bounds.find(region);
    if (zbit == ze.bounds.end()) {
        zbit = ze.bounds.emplace(region, region_bounds(engine->grid, *gz, region)).first;
    }
    const ZoneBoundsPx zb = zbit->second;
    if (ZoneBoundsEmpty(zb)) {
        res.error = "类内没有格图";
        return res;
    }
    const int64_t rgx0 = zb.x0 - kFieldHalo;
    const int64_t rgy0 = zb.y0 - kFieldHalo;
    const int64_t rnx = zb.x1 - zb.x0 + 1 + 2 * kFieldHalo;
    const int64_t rny = zb.y1 - zb.y0 + 1 + 2 * kFieldHalo;
    const double fsx = start.x / kCS;
    const double fsy = start.y / kCS;
    const double fgx = goal.x / kCS;
    const double fgy = goal.y / kCS;
    const WindowEllipse ellipse {
        .fsx = fsx,
        .fsy = fsy,
        .fgx = fgx,
        .fgy = fgy,
        .leg = std::hypot(fgx - fsx, fgy - fsy),
        .ang = std::atan2(fgy - fsy, fgx - fsx),
        .bx0 = bx0,
        .bx1 = bx1,
        .by0 = by0,
        .by1 = by1,
    };
    const WindowRect region_rect { rgx0, rgy0, rnx, rny };
    std::optional<WindowInfo> info;
    const auto fall_cache = std::make_shared<FallCache>();
    // 窗口的 st3.fall 指着它, 须与 info 同寿
    std::optional<FallGate> fall_gate;
    int level = 0;
    bool last_resort = false;
    WindowRect cur;
    WindowRect prev { 0, 0, 0, 0 };
    WindowKind kind = WindowKind::Tentative;
    for (int tier = 0;; ++tier) {
        if (last_resort) {
            cur = region_rect;
            kind = WindowKind::Full;
        }
        else {
            std::tie(cur, kind) = tier_window(ellipse, region_rect, level);
            // 极短腿的椭圆可能一档长不出一格, 同一窗口不重跑, 跳不动就用整类窗口
            constexpr int kMaxLevel = 64;
            while (kind == WindowKind::Tentative && tier > 0 && cur.x0 == prev.x0 && cur.y0 == prev.y0 && cur.nx == prev.nx
                   && cur.ny == prev.ny) {
                if (++level > kMaxLevel) {
                    cur = region_rect;
                    kind = WindowKind::Full;
                    last_resort = true;
                    break;
                }
                std::tie(cur, kind) = tier_window(ellipse, region_rect, level);
            }
        }
        prev = cur;
        const bool local = kind != WindowKind::Full;
        const bool capped = kind == WindowKind::Capped;
        if (!local && window_cells(region_rect) > kMaxCells) {
            res.error = "类过大 (" + std::to_string(rnx) + "×" + std::to_string(rny) + " 格)";
            return res;
        }
        const int64_t gx0 = cur.x0;
        const int64_t gy0 = cur.y0;
        const int64_t nx = cur.nx;
        const int64_t ny = cur.ny;
        if (plan_stopping(scope.stop_context)) {
            res.error = "规划已取消";
            return res;
        }
        const double x0 = static_cast<double>(gx0) * kCS;
        const double y0 = static_cast<double>(gy0) * kCS;
        const double x1 = static_cast<double>(gx0 + nx) * kCS;
        const double y1 = static_cast<double>(gy0 + ny) * kCS;
        const int64_t margin = local ? kTrustMargin : 0;

        const double t_win0 = now_ms();
        info = build_window(&err, engine->grid, *gz, *engine->fields, *fzd, *fz, zc, *seed, h0, x0, y0, x1, y1, nogo, no_go_discs);
        const double window_ms = now_ms() - t_win0;
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
        if (pz != nullptr) {
            fall_gate = make_fall_gate(solid, zc, pz->transform, gx0, gy0, nx, fall_cache);
            info->st3.fall = fall_gate.has_value() ? &*fall_gate : nullptr;
            info->fall_r = kFallRadius * std::fabs(static_cast<double>(pz->transform[0]));
        }
        RouteDiag dg;
        dg.margin = margin;
        dg.final = capped;
        auto line = route_window(&dg, &*info, planner, pack, start, goal, gdk, sdk, zone_id);
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
        // 封顶档窗内不通可能是绕路出了窗, 整类窗口放得下就退整类再判
        if (local && capped && !line.has_value() && window_cells(region_rect) <= kMaxCells) {
            info.reset();
            res.debug.tier_notes.push_back((dg.err.empty() ? std::string("路线失败") : dg.err) + "→整类窗口");
            last_resort = true;
            continue;
        }
        if (!line.has_value()) {
            res.error = dg.err.empty() ? "路线失败" : dg.err;
            fill_debug(&res, &dg, cur, tier, window_ms, t_all0);
            return res;
        }
        if (std::max(dg.snap_start, dg.snap_goal) > kSnapRadius || dg.hop_barrier) {
            if (!scope.hopping && !dg.hop_barrier && dg.snap_start <= kSnapRadius && !gdk.has_value()) {
                GridPatch pq;
                const bool goal_patched = load_patch(&pq, engine->grid, *gz, zone_opens, goal, goal, kSnapRadius);
                const int64_t qrec = goal_patched ? pick_start_rec(
                                         pq.gw,
                                         pq.nx,
                                         pq.ny,
                                         static_cast<int64_t>((goal.x - pq.x0) / kCS),
                                         static_cast<int64_t>((goal.y - pq.y0) / kCS),
                                         tri_height_of(zc.mesh, gs->tri))
                                                  : -1;
                const uint32_t peer = qrec >= 0 ? pq.gw.rec[static_cast<size_t>(qrec)].rid : region;
                pq = GridPatch {};
                std::vector<DropHop> hops;
                if (peer != region) {
                    const std::vector<uint32_t> peer3 = peer_spans(engine->grid, *gz, *engine->fields, *fzd, zone_id, *info, peer);
                    if (!peer3.empty()) {
                        hops = cross_drop_hops(*info, peer3, start, goal);
                    }
                }
                const std::vector<uint32_t> far_ropes = ropes_out_of_reach(*engine->fields, zone_id, region, *info, cur);
                info.reset();

                std::optional<RecastPlanResult> dropped = plan_drop_hops(engine, pack, planner, scope, req, hops);
                if (!dropped && plan_stopping(scope.stop_context)) {
                    res.error = "规划已取消";
                    return res;
                }
                std::optional<RecastPlanResult> roped = try_ropes(
                    engine,
                    pack,
                    planner,
                    scope,
                    req,
                    zone_id,
                    region,
                    peer,
                    dropped ? dropped->length : std::numeric_limits<double>::infinity(),
                    kDropHopTries,
                    far_ropes);
                if (roped || dropped) {
                    RecastPlanResult r = std::move(roped ? *roped : *dropped);
                    r.debug.timing.total_ms = now_ms() - t_all0;
                    return r;
                }
            }
            char buf[160];
            std::snprintf(
                buf,
                sizeof(buf),
                "从起点走不到终点 (端点%s, 可达区距 起 %.1fpx / 终 %.1fpx)",
                dg.hop_barrier ? "被禁行边隔开" : "在可走面上",
                dg.snap_start,
                dg.snap_goal);
            res.error = buf;
            fill_debug(&res, &dg, cur, tier, window_ms, t_all0);
            return res;
        }
        res.ok = true;
        res.points = *line;
        for (size_t i = 1; i < line->size(); ++i) {
            res.length += std::hypot((*line)[i].x - (*line)[i - 1].x, (*line)[i].y - (*line)[i - 1].y);
        }
        res.warnings = dg.warn;
        res.clearance = dg.clearance;
        res.heights = dg.span_height;
        res.snap_start = dg.snap_start;
        res.snap_goal = dg.snap_goal;
        res.waypoints = std::move(dg.waypoints);
        res.drops = std::move(dg.drops);
        fill_debug(&res, &dg, cur, tier, window_ms, t_all0);
        res.debug.planned_points = res.points;
        const std::vector<uint32_t> far_ropes = ropes_out_of_reach(*engine->fields, zone_id, region, *info, cur);
        info.reset();
        std::optional<RecastPlanResult> roped =
            try_ropes(engine, pack, planner, scope, req, zone_id, region, region, res.length, 1, far_ropes);
        if (roped) {
            roped->debug.timing.total_ms = now_ms() - t_all0;
            return std::move(*roped);
        }
        return res;
    }
}

} // namespace

bool FallClear(FallGate* gate, int64_t a, int64_t b, float ha, float hb)
{
    const FallPointKey ka { gate->gx0 + a % gate->nx, gate->gy0 + a / gate->nx, ha };
    const FallPointKey kb { gate->gx0 + b % gate->nx, gate->gy0 + b / gate->nx, hb };
    const FallEdgeKey key { ka, kb };
    if (const auto it = gate->cache->memo.find(key); it != gate->cache->memo.end()) {
        return it->second;
    }
    const OccluderPoint pa = fall_point(gate, a, ka);
    const OccluderPoint pb = fall_point(gate, b, kb);
    const bool ok = !OccluderDropBlocked(*gate->solid, pa, pb, kFallClimb, kFallHeight);
    gate->cache->memo.emplace(key, ok);
    return ok;
}

void LoadRecastNavEngine(RecastNavEngine* engine, const BaseNavPack& pack, const std::filesystem::path& nogo_table)
{
    const BaseNavSection* sec = FindBaseNavSection(pack, kGridSectionTag);
    if (sec == nullptr || sec->bytes == nullptr) {
        engine->grid_error = "包里没有预烘格图段";
        return;
    }
    if (!ParseGridPack(&engine->grid, &engine->grid_error, sec->bytes->data(), sec->bytes->size())) {
        engine->grid = GridPack();
        return;
    }
    if (!LoadSharedFieldsPack(&engine->fields, &engine->grid_error, FieldsSidecarPath(pack.path), pack, engine->grid)) {
        engine->grid = GridPack();
        return;
    }
    if (!LoadNoGoTable(&engine->nogo, &engine->grid_error, nogo_table, pack)) {
        engine->grid = GridPack();
    }
}

RecastPlanResult PlanRecastRoute(
    RecastNavEngine* engine,
    const BaseNavPack& pack,
    const BaseNavPlanner& planner,
    const RecastPlanRequest& request,
    MaaContext* stop_context,
    const std::vector<uint32_t>* rope_off)
{
    const std::lock_guard<std::mutex> lock(engine->mutex);
    const PlanScope scope { .stop_context = stop_context, .rope_off = rope_off };
    RecastPlanResult res = plan_locked(engine, pack, planner, scope, request);
#if defined(__APPLE__)
    malloc_zone_pressure_relief(nullptr, 0);
#elif defined(_WIN32)
    _heapmin();
#endif
    return res;
}

void WarmRecastZone(RecastNavEngine* engine, const BaseNavPack& pack, const BaseNavPlanner& planner, const std::string& zone_name)
{
    const std::lock_guard<std::mutex> lock(engine->mutex);
    zone_entry(engine, pack, planner, zone_name);
}

std::vector<std::vector<uint32_t>>
    RecastRegionsNear(RecastNavEngine* engine, const std::string& zone_name, const std::vector<WorldPoint>& points)
{
    const std::lock_guard<std::mutex> lock(engine->mutex);
    std::vector<std::vector<uint32_t>> out(points.size());
    const GridZoneDir* gz = GridPackParsed(engine->grid) ? FindGridZone(engine->grid, zone_name) : nullptr;
    if (gz == nullptr || points.empty()) {
        return out;
    }
    const double cs = engine->grid.cell_size;
    const auto rad = static_cast<int64_t>(std::ceil(kSnapRadius / cs));
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
        if (!DecodeGridPackTile(&tile, engine->grid, *t)) {
            continue;
        }
        for (const GridSpanRec& r : tile.rec) {
            // 补格两个选类器都不会挑, 其余宁可多算不可漏
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

} // namespace navmesh::recast
