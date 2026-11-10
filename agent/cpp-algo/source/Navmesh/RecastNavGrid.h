#pragma once

#include <algorithm>
#include <array>
#include <bit>
#include <cstdint>
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

// 定义在规划层
struct FallGate;

// 台沿下落能否跳, 格号是窗口内格号; 会写 gate 的记忆表
bool FallClear(FallGate* gate, int64_t from, int64_t to, float h_from, float h_to);

// 同一格的 span 在 sp_h 里是连续一段, 下标 [CellSpanStart(st, j), CellSpanStart(st, j)+CellSpanCount(st, j))
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
    // 台沿下落的另一道判据, 空则不另判
    FallGate* fall = nullptr;
};

inline int64_t OccupiedCount(const SpanTable& st)
{
    return st.cs.empty() ? 0 : static_cast<int64_t>(st.cs.size()) - 1;
}

inline int64_t CellSpanStart(const SpanTable& st, int64_t ci)
{
    return st.cs[static_cast<size_t>(ci)];
}

inline int64_t CellSpanCount(const SpanTable& st, int64_t ci)
{
    return st.cs[static_cast<size_t>(ci) + 1] - st.cs[static_cast<size_t>(ci)];
}

inline int64_t OccupiedCell(const SpanTable& st, int64_t ci)
{
    return st.sp_cell[static_cast<size_t>(st.cs[static_cast<size_t>(ci)])];
}

inline int64_t OccupiedIndex(const SpanTable& st, int64_t cid)
{
    const auto w = static_cast<size_t>(cid >> 6);
    if (cid < 0 || w >= st.occ_bits.size()) {
        return -1;
    }
    const uint64_t bits = st.occ_bits[w];
    const int b = static_cast<int>(cid & 63);
    if (((bits >> b) & 1U) == 0) {
        return -1;
    }
    return st.occ_rank[w] + std::popcount(bits & ((uint64_t { 1 } << b) - 1));
}

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
};

inline void ResetEdgeBits(EdgeBits* bits, int64_t w, int64_t h)
{
    bits->nx = w;
    bits->ny = h;
    bits->v.assign(static_cast<size_t>(w * h), 0);
    bits->any = false;
}

// 返回 (存位的格 << 3) | 位号
inline int64_t EdgeBitLocation(const EdgeBits& bits, int64_t a, int64_t b)
{
    const int64_t dx = b % bits.nx - a % bits.nx;
    const int64_t dy = b / bits.nx - a / bits.nx;
    if (dx < -1 || dx > 1 || dy < -1 || dy > 1) {
        return -1;
    }
    const int8_t s = kEdgeSlot[static_cast<size_t>((dy + 1) * 3 + dx + 1)];
    if (s < 0) {
        return -1;
    }
    return (((s & 8) != 0 ? b : a) << 3) | (s & 7);
}

inline void SetEdgeBit(EdgeBits* bits, int64_t a, int64_t b)
{
    const int64_t loc = EdgeBitLocation(*bits, a, b);
    if (loc < 0) {
        return;
    }
    bits->v[static_cast<size_t>(loc >> 3)] |= static_cast<uint8_t>(1U << (loc & 7));
    bits->any = true;
}

inline bool HasEdgeBit(const EdgeBits& bits, int64_t a, int64_t b)
{
    const int64_t loc = EdgeBitLocation(bits, a, b);
    return loc >= 0 && (bits.v[static_cast<size_t>(loc >> 3)] & (1U << (loc & 7))) != 0;
}

// 前提是 b = a + dy·gnx + dx
inline bool HasEdgeBitStep(const EdgeBits& bits, int64_t a, int64_t b, int64_t dx, int64_t dy, int64_t gnx)
{
    if (gnx != bits.nx) {
        return HasEdgeBit(bits, a, b);
    }
    const int8_t s = kEdgeSlot[static_cast<size_t>((dy + 1) * 3 + dx + 1)];
    if (s < 0) {
        return false;
    }
    return (bits.v[static_cast<size_t>((s & 8) != 0 ? b : a)] & (1U << (s & 7))) != 0;
}

inline bool EdgeBitsEmpty(const EdgeBits& bits)
{
    return !bits.any;
}

struct JumpEdge
{
    int64_t src = 0;
    int64_t dst = 0;
    float cost = 0.0F; // 格单位, 须 ≥ 两端欧氏格距
};

// 有向跳边, src 是格号或 span 下标, 查询前须先 SortJumpEdges 按源排序
struct JumpEdges
{
    std::vector<JumpEdge> e;
};

inline bool JumpEdgesEmpty(const JumpEdges& jumps)
{
    return jumps.e.empty();
}

inline void AddJumpEdge(JumpEdges* jumps, int64_t src, int64_t dst, float cost)
{
    jumps->e.push_back({ src, dst, cost });
}

inline void SortJumpEdges(JumpEdges* jumps)
{
    std::stable_sort(jumps->e.begin(), jumps->e.end(), [](const JumpEdge& a, const JumpEdge& b) { return a.src < b.src; });
}

inline std::pair<size_t, size_t> JumpEdgesFrom(const JumpEdges& jumps, int64_t src)
{
    const auto lo = std::lower_bound(jumps.e.begin(), jumps.e.end(), src, [](const JumpEdge& a, int64_t s) { return a.src < s; });
    const auto hi = std::upper_bound(jumps.e.begin(), jumps.e.end(), src, [](int64_t s, const JumpEdge& a) { return s < a.src; });
    return { static_cast<size_t>(lo - jumps.e.begin()), static_cast<size_t>(hi - jumps.e.begin()) };
}

inline bool HasJumpEdge(const JumpEdges& jumps, int64_t src, int64_t dst)
{
    auto [i, n] = JumpEdgesFrom(jumps, src);
    for (; i < n; ++i) {
        if (jumps.e[i].dst == dst) {
            return true;
        }
    }
    return false;
}

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
};

inline float PriceAt(const PriceField& price, size_t i)
{
    if (price.dist == nullptr) {
        return 1.0F;
    }
    return static_cast<float>(price.hi / std::min(std::max(static_cast<double>(price.dist->v[i]), price.lo), price.hi));
}

inline float PriceAtCell(const PriceField& price, int64_t y, int64_t x)
{
    return price.dist == nullptr ? 1.0F : PriceAt(price, static_cast<size_t>(y * price.dist->nx + x));
}

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

struct Visibility;

struct SpanSearch
{
    const SpanTable* st = nullptr;
    const std::vector<uint8_t>* ok = nullptr;
    const Mask* ok2 = nullptr;
    const PriceField* mult = nullptr;
    const EdgeBits* banned = nullptr;
    const double* bnp = nullptr;
    const EdgeBits* forbidden = nullptr;
    // 非空时按 Lazy Theta* 展开, 判弦时改写挡线的去重戳
    Visibility* vis = nullptr;
    const JumpEdges* jumps = nullptr;
};

// corners 收父链折线, give_up_at 只在 search.vis 为空时生效
std::optional<std::vector<int64_t>> SpanAstar(
    std::vector<int64_t>* corners,
    double* out_cost,
    const SpanSearch& search,
    int64_t s,
    const std::vector<int64_t>& gset,
    double give_up_at);

Mask MedialAxis(const Grid<float>& dist, double lam);

Grid<float> CorridorWidth(const Grid<float>& dist, const Mask& seed, Mask* band = nullptr);

std::vector<std::vector<WorldPoint>> TraceContours(const Mask& mask);

std::vector<WorldPoint> SimplifyLoop(const std::vector<WorldPoint>& P, double max_err);

struct BlockerOnMask
{
    const Mask* mask = nullptr;
    double x0 = 0.0;
    double y0 = 0.0;
    double cs = kCS;
};

// 挡线与均匀桶 CSR 索引, MakeBlockerSegments 一次建好后只读
struct BlockerSegments
{
    std::vector<WorldPoint> a;
    std::vector<WorldPoint> b;
    double bx0 = 0.0;
    double by0 = 0.0;
    int64_t bnx = 0;
    int64_t bny = 0;
    std::vector<int32_t> bstart;
    std::vector<int32_t> bitem;
};

BlockerSegments MakeBlockerSegments(
    const std::vector<std::vector<WorldPoint>>& loops,
    const std::vector<WorldPoint>* extra_a,
    const std::vector<WorldPoint>* extra_b);

// on 掩膜密采样兜底, 精确 45° 弦会从轮廓顶点缝溜走
struct Blockers
{
    const BlockerSegments* segs = nullptr;
    std::optional<BlockerOnMask> on;
    // 段与桶各一组世代戳, 单次查询内去重, 跨查询不清零
    std::vector<uint32_t> seen;
    std::vector<uint32_t> bseen;
    uint32_t epoch = 0;
};

Blockers MakeBlockers(const BlockerSegments& segs, std::optional<BlockerOnMask> on);

// 只改 blk 的去重戳
bool SegmentBlocked(Blockers* blk, const WorldPoint& p, const WorldPoint& q);

// 只用来跟住弦所在的层, 抬升按体素取样容差放宽
struct LayerOracle
{
    const SpanTable* st = nullptr;
    int64_t nx = 0;
    int64_t ny = 0;
    double x0 = 0.0;
    double y0 = 0.0;
};

std::optional<std::vector<float>> LayerWalk(const LayerOracle& lyo, const std::vector<WorldPoint>& pts, float h);

std::optional<std::vector<float>> LayerWalk(const LayerOracle& lyo, const std::vector<WorldPoint>& pts, const std::vector<float>& h);

// 往下多深都放行, 接不上的格也不判失败
std::vector<float> LayerWalkFalling(const LayerOracle& lyo, const std::vector<WorldPoint>& pts, const std::vector<float>& h);

bool LayerStepOk(const LayerOracle& lyo, const WorldPoint& p, const WorldPoint& q, float h, float hq);

struct Visibility
{
    // 判弦时改写它的去重戳
    Blockers* blk = nullptr;
    const LayerOracle* lyo = nullptr;
    const EdgeBits* faces = nullptr;
    const EdgeBits* steps = nullptr;
    int64_t nx = 0;
    int64_t ny = 0;
    double x0 = 0.0;
    double y0 = 0.0;
};

// 只改 vis->blk 的去重戳
bool ChordVisible(Visibility* vis, const WorldPoint& p, const WorldPoint& q, float hp, float hq);

inline WorldPoint VisibilityCellCenter(const Visibility& vis, int64_t cell)
{
    return { vis.x0 + (static_cast<double>(cell % vis.nx) + 0.5) * kCS, vis.y0 + (static_cast<double>(cell / vis.nx) + 0.5) * kCS };
}

// vis 非空时只按 vis 判弦
std::vector<WorldPoint>
    StringPull(Blockers* blk, Visibility* vis, const std::vector<WorldPoint>& pts, const LayerOracle* lyo, const std::vector<float>* hs);

}
