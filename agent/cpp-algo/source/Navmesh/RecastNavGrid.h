#pragma once

#include <algorithm>
#include <array>
#include <bit>
#include <cstdint>
#include <functional>
#include <limits>
#include <optional>
#include <unordered_set>
#include <vector>

#include "BaseNavPack.h"
#include "NavmeshTypes.h"
#include "RecastNavGridIO.h"

namespace navmesh::recast
{

inline constexpr double kCS = 0.25;             // 体素边长 px
inline constexpr double kClimb = 3.0;           // 相邻格可向上连通最大高差 px
inline constexpr double kDrop = 1.0e9;          // 相邻格可向下连通最大落差 px, 角色无摔落伤害所以不设实际上限
inline constexpr int64_t kSeamCells = 4;        // 下落前探格数: 出发那层在这么多格内回来即路面上一道缝
inline constexpr double kSlope = 1.0;           // 可攀爬坡度上限 tanθ, 抬升超过水平位移的这个倍数即立面
inline constexpr double kStepUp = 0.5;          // 可直接迈上的台阶高 px
inline constexpr double kBumpUp = 1.25;         // 跨过路面窄凸起/浅坑允许的抬升 px, 仅在落差不延伸时生效
inline constexpr int64_t kBumpCells = 3;        // 前探格数: 抬升在这么多格内回到出发高度即窄凸起
inline constexpr int64_t kDipCells = 2;         // 后探格数: 目标高度在身后这么多格内出现即浅坑
inline constexpr double kWallH = 1.25;          // 禁步边同时挡住拉直视线所需的落差 px, 取路面起伏上限
inline constexpr double kMergeH = kSlope * kCS; // 同列 span 合并容差 px
inline constexpr double kQH = 1.0;              // 体素取样高差容差 px, 需装下斜面单格起伏与格心取样偏差
inline constexpr double kEdtCap = 12.0;         // 距离场截断 px
inline constexpr double kR = 1.75;              // 期望余量上限 px
inline constexpr double kMaxErr = 0.5;          // 轮廓 DP 容差 px
inline constexpr double kMcHBand = 8.0;         // 层高度带(边界边筛/盖章)px
inline constexpr double kBkt = 4.0;             // 挡线索引桶边长 px
inline constexpr double kBktPad = 1e-6;         // 入桶时给挡线包围盒的放量 px, 需盖过相交判据放给挡线两端的余量
inline constexpr double kSnapRadius = 8.0;      // 起终点吸附半径 px
inline constexpr double kDeckBand = 2.0;        // 声明面高度匹配容差 px, 需远小于相邻面间距
inline constexpr int64_t kHoleMaxCells = 32;    // 封闭小洞填充上限(格 = 2px²)
// 类窗口在类范围外多留的格数, 须盖过各场算子的依赖半径且与旁包一致
inline constexpr int64_t kFieldHalo = 32;
inline constexpr int64_t kMaxCells = 400'000'000;

inline constexpr double kClrPref = kR; // 期望净空 px
// 中轴判据的毛刺尺度 px, 小于它的边界起伏视为噪声
inline constexpr double kClrLambda = 2.0 * kCS;
// 通道层单价按净空取值的区间 px, 两端之比是最多肯绕的路长倍数
inline constexpr double kClrWide = 4.0;
inline constexpr double kClrNarrow = 1.0;
// 弦的容许净空占所在走廊半宽的比例
inline constexpr double kChordFrac = 0.8;
// 中脊上每次突变抬升的加价, 单位是格步价
inline constexpr double kStepTax = 0.0;
// 离网连接端点接入舒适通道允许的最大格数
inline constexpr int32_t kLinkChainCells = 64;
// 终线取直的拐角余量 px, 叠加在 kClrPref 上
inline constexpr double kGeoMargin = 0.0;
// 拐点朝净空更高一侧能挪的最大位移 px
inline constexpr double kLiftMax = 0.5;

// 相邻格允许的抬升 px, w 是水平位移, 单位格
inline double UpAllow(double w)
{
    return std::max(kStepUp, kSlope * w * kCS);
}

template <typename T>
struct Grid
{
    int64_t nx = 0;
    int64_t ny = 0;
    std::vector<T> v;

    Grid() = default;

    Grid(int64_t nx_in, int64_t ny_in, T fill)
        : nx(nx_in)
        , ny(ny_in)
        , v(static_cast<size_t>(nx_in * ny_in), fill)
    {
    }

    T at(int64_t y, int64_t x) const { return v[static_cast<size_t>(y * nx + x)]; }

    T& at(int64_t y, int64_t x) { return v[static_cast<size_t>(y * nx + x)]; }
};

using Mask = Grid<uint8_t>;

struct CellPt
{
    int64_t x = 0;
    int64_t y = 0;

    bool operator==(const CellPt&) const = default;
};

struct RasterCells
{
    std::vector<int64_t> cell;
    std::vector<float> h;
    std::vector<uint8_t> ins;
};

// 同一格的 span 在 sp_h 里是连续一段, 下标 [cstart(j), cstart(j)+ccnt(j))
struct SpanTable
{
    std::vector<int32_t> sp_cell;
    std::vector<float> sp_h;
    // CSR 边界, 长度为占用格数 + 1, 末位是 span 总数
    std::vector<int32_t> cs;
    // 逐占用格一位, 该列是否为被栅格化的立面
    std::vector<uint8_t> face;
    // 逐格一位的占用图与每 64 格的前缀占用数, 长度只到最大占用格, 表外视为空格
    std::vector<uint64_t> occ_bits;
    std::vector<int32_t> occ_rank;
    // 台沿下落能否跳: (出发格, 落点格, 出发高, 落点高), 空则不另判
    std::function<bool(int64_t, int64_t, float, float)> fall;

    int64_t nOcc() const { return cs.empty() ? 0 : static_cast<int64_t>(cs.size()) - 1; }

    int64_t cstart(int64_t ci) const { return cs[static_cast<size_t>(ci)]; }

    int64_t ccnt(int64_t ci) const { return cs[static_cast<size_t>(ci) + 1] - cs[static_cast<size_t>(ci)]; }

    int64_t occ(int64_t ci) const { return sp_cell[static_cast<size_t>(cs[static_cast<size_t>(ci)])]; }

    int64_t j(int64_t cid) const
    {
        const auto w = static_cast<size_t>(cid >> 6);
        if (cid < 0 || w >= occ_bits.size()) {
            return -1;
        }
        const uint64_t bits = occ_bits[w];
        const int b = static_cast<int>(cid & 63);
        if (((bits >> b) & 1U) == 0) {
            return -1;
        }
        return occ_rank[w] + std::popcount(bits & ((uint64_t { 1 } << b) - 1));
    }
};

bool RiseOk(const SpanTable& st, int64_t nx, int64_t ny, int64_t cid, int64_t dx, int64_t dy, float h0, float h1);

// walkable 非空时须与 T 同长同序, 标 0 的三角不体素化
RasterCells Rasterize(
    const BaseNavVertex* V,
    const std::vector<std::array<int32_t, 3>>& T,
    double ox,
    double oy,
    int64_t nx,
    int64_t ny,
    const std::vector<uint8_t>* walkable = nullptr);

void AppendSeamBridge(RasterCells& rc, int64_t nx, int64_t ny);

SpanTable BuildSpans(const std::vector<int64_t>& cell, const std::vector<float>& h);

// flags / aux 与 cell 同长, 按同一置换重排后写回
SpanTable
    PackSpans(std::vector<int32_t> cell, std::vector<float> h, std::vector<uint8_t>* flags = nullptr, std::vector<uint32_t>* aux = nullptr);

std::vector<uint8_t> Flood(int64_t seed, const SpanTable& st, int64_t nx);

// 邻接关系须与 Flood 一致, Flood(seed) 命中的正是种子所在那一类
std::vector<int32_t> LabelRegions(const SpanTable& st, int64_t nx);

std::vector<uint8_t> SpanReach(int64_t seed, const SpanTable& st, const std::vector<uint8_t>& ok, int64_t nx, int64_t ny);

Grid<float> Clearance(const Mask& mask);

std::vector<uint8_t> StampWalls(
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    const std::vector<double>& hh,
    double ox,
    double oy,
    int64_t nx,
    int64_t ny,
    const SpanTable& st);

// 逐墙一位, 任一采样落在层高图 lh 的高度带内即留下
std::vector<uint8_t> WallsAtLayer(
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    const std::vector<double>& hh,
    const Grid<float>& lh,
    double ox,
    double oy);

// 下标 (dy+1)*3+(dx+1), 值为位号 | (位存在终点格 ? 8 : 0), -1 表示非八邻
inline constexpr std::array<int8_t, 9> MakeEdgeSlots()
{
    std::array<int8_t, 9> t {};
    t.fill(-1);
    for (int i = 0; i < 4; ++i) {
        const int dx = static_cast<int>(kGridStepDx[i]);
        const int dy = static_cast<int>(kGridStepDy[i]);
        t[static_cast<size_t>((dy + 1) * 3 + dx + 1)] = static_cast<int8_t>(2 * i);
        t[static_cast<size_t>((1 - dy) * 3 + 1 - dx)] = static_cast<int8_t>(2 * i + 1 + 8);
    }
    return t;
}

inline constexpr std::array<int8_t, 9> kEdgeSlot = MakeEdgeSlots();

// 禁行边集, 一格一字节, 四个规范方向各占正反两位, 非八邻格对一律查不到
struct EdgeBits
{
    std::vector<uint8_t> v;
    int64_t nx = 0;
    int64_t ny = 0;
    bool any = false;

    void resize(int64_t w, int64_t h)
    {
        nx = w;
        ny = h;
        v.assign(static_cast<size_t>(w * h), 0);
        any = false;
    }

    // 返回 (存位的格 << 3) | 位号
    int64_t locate(int64_t a, int64_t b) const
    {
        const int64_t dx = b % nx - a % nx;
        const int64_t dy = b / nx - a / nx;
        if (dx < -1 || dx > 1 || dy < -1 || dy > 1) {
            return -1;
        }
        const int8_t s = kEdgeSlot[static_cast<size_t>((dy + 1) * 3 + dx + 1)];
        if (s < 0) {
            return -1;
        }
        return (((s & 8) != 0 ? b : a) << 3) | (s & 7);
    }

    void set(int64_t a, int64_t b)
    {
        const int64_t loc = locate(a, b);
        if (loc < 0) {
            return;
        }
        v[static_cast<size_t>(loc >> 3)] |= static_cast<uint8_t>(1U << (loc & 7));
        any = true;
    }

    bool has(int64_t a, int64_t b) const
    {
        const int64_t loc = locate(a, b);
        return loc >= 0 && (v[static_cast<size_t>(loc >> 3)] & (1U << (loc & 7))) != 0;
    }

    // 前提是 b = a + dy·gnx + dx
    bool hasStep(int64_t a, int64_t b, int64_t dx, int64_t dy, int64_t gnx) const
    {
        if (gnx != nx) {
            return has(a, b);
        }
        const int8_t s = kEdgeSlot[static_cast<size_t>((dy + 1) * 3 + dx + 1)];
        if (s < 0) {
            return false;
        }
        return (v[static_cast<size_t>((s & 8) != 0 ? b : a)] & (1U << (s & 7))) != 0;
    }

    bool empty() const { return !any; }
};

// 有向跳边, src 是格号或 span 下标, from 之前须先 finish 按源排序
struct JumpEdges
{
    struct Edge
    {
        int64_t src = 0;
        int64_t dst = 0;
        float cost = 0.0F; // 格单位, 须 ≥ 两端欧氏格距
    };

    std::vector<Edge> e;

    bool empty() const { return e.empty(); }

    void add(int64_t src, int64_t dst, float cost) { e.push_back({ src, dst, cost }); }

    void finish()
    {
        std::stable_sort(e.begin(), e.end(), [](const Edge& a, const Edge& b) { return a.src < b.src; });
    }

    std::pair<size_t, size_t> from(int64_t src) const
    {
        const auto lo = std::lower_bound(e.begin(), e.end(), src, [](const Edge& a, int64_t s) { return a.src < s; });
        const auto hi = std::upper_bound(e.begin(), e.end(), src, [](int64_t s, const Edge& a) { return s < a.src; });
        return { static_cast<size_t>(lo - e.begin()), static_cast<size_t>(hi - e.begin()) };
    }

    bool has(int64_t src, int64_t dst) const
    {
        auto [i, n] = from(src);
        for (; i < n; ++i) {
            if (e[i].dst == dst) {
                return true;
            }
        }
        return false;
    }
};

struct StepBarrier
{
    EdgeBits steps;
    std::vector<WorldPoint> p0;
    std::vector<WorldPoint> p1;
};

std::vector<int64_t> Comps4(const Mask& mask);

Mask FillHoles(const Mask& mask, int64_t max_cells, const Mask* protect);

Mask CloseCracks(const Mask& core, const Mask& lay, const Mask* protect);

struct PriceField
{
    const Grid<float>* dist = nullptr;
    double lo = 0.0;
    double hi = 0.0;

    float v(size_t i) const
    {
        if (dist == nullptr) {
            return 1.0F;
        }
        return static_cast<float>(hi / std::min(std::max(static_cast<double>(dist->v[i]), lo), hi));
    }

    float at(int64_t y, int64_t x) const { return dist == nullptr ? 1.0F : v(static_cast<size_t>(y * dist->nx + x)); }
};

std::optional<std::vector<CellPt>> CostAstar(
    const Mask& mask,
    CellPt s,
    CellPt g,
    const PriceField& mult,
    const EdgeBits* banned,
    const double* bnp,
    const EdgeBits* forbidden = nullptr,
    double* out_cost = nullptr,
    const JumpEdges* jumps = nullptr);

class Visibility;

// vis 非空时按 Lazy Theta* 展开, corners 收父链折线, give_up_at 只在 vis 为空时生效
std::optional<std::vector<int64_t>> SpanAstar(
    const SpanTable& st,
    const std::vector<uint8_t>& ok,
    const Mask& ok2,
    int64_t s,
    const std::vector<int64_t>& gset,
    const PriceField& mult,
    const EdgeBits* banned,
    const double* bnp,
    const EdgeBits* forbidden = nullptr,
    const Visibility* vis = nullptr,
    std::vector<int64_t>* corners = nullptr,
    double* out_cost = nullptr,
    const JumpEdges* jumps = nullptr,
    double give_up_at = std::numeric_limits<double>::infinity());

Mask MedialAxis(const Grid<float>& dist, double lam);

Grid<float> CorridorWidth(const Grid<float>& dist, const Mask& seed, Mask* band = nullptr);

std::vector<std::vector<WorldPoint>> TraceContours(const Mask& mask);

std::vector<WorldPoint> SimplifyLoop(const std::vector<WorldPoint>& P, double max_err);

class BlockerSegments
{
public:
    BlockerSegments() = default;
    BlockerSegments(
        const std::vector<std::vector<WorldPoint>>& loops,
        const std::vector<WorldPoint>* extra_a,
        const std::vector<WorldPoint>* extra_b);

private:
    friend class Blockers;

    void buildIndex() const;

    WorldPoint lo(size_t i) const { return { std::min(a_[i].x, b_[i].x), std::min(a_[i].y, b_[i].y) }; }

    WorldPoint hi(size_t i) const { return { std::max(a_[i].x, b_[i].x), std::max(a_[i].y, b_[i].y) }; }

    std::vector<WorldPoint> a_;
    std::vector<WorldPoint> b_;
    // 挡线的均匀桶 CSR 索引, 首次查询时才建
    mutable bool built_ = false;
    mutable double bx0_ = 0.0;
    mutable double by0_ = 0.0;
    mutable int64_t bnx_ = 0;
    mutable int64_t bny_ = 0;
    mutable std::vector<int32_t> bstart_;
    mutable std::vector<int32_t> bitem_;
    // 段与桶各挂一组世代戳, 单次查询内去重, 跨查询不清零
    mutable std::vector<uint32_t> seen_;
    mutable std::vector<uint32_t> bseen_;
    mutable uint32_t epoch_ = 0;
};

// on 掩膜密采样兜底, 精确 45° 弦会从轮廓顶点缝溜走
class Blockers
{
public:
    struct OnMask
    {
        const Mask* mask = nullptr;
        double x0 = 0.0;
        double y0 = 0.0;
        double cs = kCS;
    };

    Blockers(const BlockerSegments& segs, std::optional<OnMask> on)
        : segs_(&segs)
        , on_(on)
    {
    }

    bool blocked(const WorldPoint& p, const WorldPoint& q) const;

private:
    bool offMask(const WorldPoint& p, const WorldPoint& q) const;

    const BlockerSegments* segs_ = nullptr;
    std::optional<OnMask> on_;
};

// 只用来跟住弦所在的层, 抬升按体素取样容差放宽
class LayerOracle
{
public:
    LayerOracle(const SpanTable* st, int64_t nx, int64_t ny, double x0, double y0)
        : st_(st)
        , nx_(nx)
        , ny_(ny)
        , x0_(x0)
        , y0_(y0)
    {
    }

    std::optional<std::vector<float>> walk(const std::vector<WorldPoint>& pts, float h) const;

    // fall 为真时往下多深都放行, 接不上的格也不判失败
    std::optional<std::vector<float>> walk(const std::vector<WorldPoint>& pts, const std::vector<float>& h, bool fall = false) const;

    bool ok(const WorldPoint& p, const WorldPoint& q, float h, float hq) const;

private:
    const SpanTable* st_ = nullptr;
    int64_t nx_ = 0;
    int64_t ny_ = 0;
    double x0_ = 0.0;
    double y0_ = 0.0;
};

class Visibility
{
public:
    Visibility(
        const Blockers* blk,
        const LayerOracle* lyo,
        const EdgeBits* faces,
        const EdgeBits* steps,
        int64_t nx,
        int64_t ny,
        double x0,
        double y0)
        : blk_(blk)
        , lyo_(lyo)
        , faces_(faces)
        , steps_(steps)
        , nx_(nx)
        , ny_(ny)
        , x0_(x0)
        , y0_(y0)
    {
    }

    bool ok(const WorldPoint& p, const WorldPoint& q, float hp, float hq) const;

    WorldPoint at(int64_t cell) const
    {
        return { x0_ + (static_cast<double>(cell % nx_) + 0.5) * kCS, y0_ + (static_cast<double>(cell / nx_) + 0.5) * kCS };
    }

private:
    bool crossesStep(const WorldPoint& p, const WorldPoint& q) const;

    const Blockers* blk_ = nullptr;
    const LayerOracle* lyo_ = nullptr;
    const EdgeBits* faces_ = nullptr;
    const EdgeBits* steps_ = nullptr;
    int64_t nx_ = 0;
    int64_t ny_ = 0;
    double x0_ = 0.0;
    double y0_ = 0.0;
};

// vis 非空时只按 vis 判弦
std::vector<WorldPoint> StringPull(
    const std::vector<WorldPoint>& pts,
    const Blockers& blk,
    const LayerOracle* lyo = nullptr,
    const std::vector<float>* hs = nullptr,
    const Visibility* vis = nullptr);

}
