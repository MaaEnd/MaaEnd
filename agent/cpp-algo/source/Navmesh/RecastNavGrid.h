#pragma once

#include <array>
#include <cstdint>
#include <optional>
#include <unordered_set>
#include <vector>

#include "NavmeshTypes.h"

namespace navmesh::recast
{

inline constexpr double kCS = 0.25;                // 体素边长 px
inline constexpr double kClimb = 3.0;              // 相邻格可连通最大高差 px
inline constexpr double kSlope = 1.0;              // 可攀爬坡度上限 tanθ, 抬升超过水平位移的这个倍数即立面
inline constexpr double kUp = kSlope * kCS;        // 正交相邻格允许的抬升 px, 斜向按实际水平位移等比放大
inline constexpr double kMergeH = kUp;             // 同列 span 合并容差 px, 取 kUp 使同层内处处可一步跨到
inline constexpr double kQH = 1.0;                 // 体素取样高差容差 px, 需装下斜面单格起伏与格心取样偏差
inline constexpr double kEdtCap = 12.0;            // 距离场截断 px
inline constexpr double kR = 1.75;                 // 期望余量上限 px
inline constexpr double kGeoR = 3.5;               // 几何口径舒适余量上限 px
inline constexpr double kRel = 0.6;                // 期望余量 = min(R, REL×局部净空)
inline constexpr double kLam = 4.0;                // 按局部通道目标计亏欠的满亏欠一步加价倍数
inline constexpr double kLamR = 28.0;              // 按固定余量目标 kR 计亏欠的满亏欠一步加价倍数
inline constexpr double kRidgeFloor = 0.5;         // 脊线保底余量地板 px
inline constexpr double kMaxErr = 0.5;             // 轮廓 DP 容差 px
inline constexpr double kSlimEps = 0.5;            // 终线共线剔除容差 px
inline constexpr double kClrTol = 0.125;           // 拉直允许的净空退让 px, 取半格即采样步长
inline constexpr double kCornerR = 1.75;           // 过角期望余量 px
inline constexpr double kCornerTurn = 5.0;         // 需要留过角余量的最小转角 度
inline constexpr double kCornerSeg = 2.0;          // 认定为拐点的最小相邻段长 px
inline constexpr double kCornerMax = 4.0;          // 拐点外挪上限 px
inline constexpr double kCornerStep = 0.5;         // 拐点外挪步长 px
inline constexpr int64_t kCornerDirs = 32;         // 拐点外挪候选方向数
inline constexpr int64_t kCornerRounds = 3;        // 拐点外挪迭代轮数
inline constexpr double kCostTol = 1e-9;           // 代价判据相对容差, 容纳共线子路径的浮点求和差
inline constexpr double kMcHBand = 8.0;            // 层高度带(边界边筛/盖章)px
inline constexpr double kSnapRadius = 8.0;         // 起终点吸附半径 px
inline constexpr double kDeckBand = 2.0;           // 声明面高度匹配容差 px, 需远小于相邻面间距
inline constexpr double kMargin = 25.0;            // 窗口外扩 px
inline constexpr double kBlockedPointRadius = 1.0; // 封堵点盖章半径 px
inline constexpr int64_t kHoleMaxCells = 32;       // 封闭小洞填充上限(格 = 2px²)
inline constexpr int64_t kMaxCells = 30'000'000;

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

// HK/IK 行主序 [n_occ][K],空槽 inf/-1
struct SpanTable
{
    std::vector<int64_t> sp_cell;
    std::vector<float> sp_h;
    std::vector<int64_t> occ;
    std::vector<int64_t> cstart;
    std::vector<int64_t> ccnt;
    int64_t K = 0;
    std::vector<float> HK;
    std::vector<int64_t> IK;
    std::vector<int64_t> sp_ci;
};

struct WallCsr
{
    std::vector<int64_t> wid;
    std::vector<int64_t> start;
};

RasterCells Rasterize(
    const std::vector<WorldPoint>& V,
    const std::vector<double>& H,
    const std::vector<std::array<int32_t, 3>>& T,
    double ox,
    double oy,
    int64_t nx,
    int64_t ny);

void AppendSeamBridge(RasterCells& rc, int64_t nx, int64_t ny);

SpanTable BuildSpans(const std::vector<int64_t>& cell, const std::vector<float>& h);

SpanTable PackSpans(std::vector<int64_t> cell, std::vector<float> h, std::vector<uint8_t>* flags = nullptr);

std::vector<uint8_t> Flood(int64_t seed, const SpanTable& st, int64_t nx);

// Flood 的无种子版本:逐 span 给出所在的连通类号。邻接关系与 Flood 逐条相同,
// 所以 Flood(seed) 命中的正是种子那一类,类号可以离线算好。
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

std::vector<uint8_t> WallsAtLayer(
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    const std::vector<double>& hh,
    const Grid<float>& lh,
    double ox,
    double oy);

WallCsr BuildWallIndex(const std::vector<WorldPoint>& p0, const std::vector<WorldPoint>& p1, double ox, double oy, int64_t nx, int64_t ny);

struct StepBarrier
{
    std::unordered_set<int64_t> steps;
    std::vector<WorldPoint> p0;
    std::vector<WorldPoint> p1;
    std::vector<float> t0;
};

StepBarrier StepBreaks(const SpanTable& st, const std::vector<uint8_t>& vis, const Mask& lay, double ox, double oy);

std::vector<int64_t> Comps4(const Mask& mask);

Mask FillHoles(const Mask& mask, int64_t max_cells, const Mask* protect);

Mask CloseCracks(const Mask& core, const Mask& lay, const Mask* protect);

std::optional<std::vector<CellPt>> CostAstar(
    const Mask& mask,
    CellPt s,
    CellPt g,
    const Grid<float>& mult,
    const std::unordered_set<int64_t>* banned,
    const double* bnp,
    const std::unordered_set<int64_t>* forbidden = nullptr);

std::optional<std::vector<int64_t>> SpanAstar(
    const SpanTable& st,
    const std::vector<uint8_t>& ok,
    const std::vector<int64_t>& cidx,
    const Mask& ok2,
    int64_t s,
    const std::vector<int64_t>& gset,
    const Grid<float>& mult,
    const std::unordered_set<int64_t>* banned,
    const double* bnp,
    const std::unordered_set<int64_t>* forbidden = nullptr);

Grid<float> PrefField(const Grid<float>& dist, bool ridge);

Grid<float> TargetField(const Grid<float>& dist);

std::vector<std::vector<WorldPoint>> TraceContours(const Mask& mask);

std::vector<WorldPoint> SimplifyLoop(const std::vector<WorldPoint>& P, double max_err);

// on 掩膜密采样兜底:精确 45° 弦会从轮廓顶点缝溜走
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

    Blockers(
        const std::vector<std::vector<WorldPoint>>& loops,
        const std::vector<WorldPoint>* extra_a,
        const std::vector<WorldPoint>* extra_b,
        std::optional<OnMask> on);

    bool blocked(const WorldPoint& p, const WorldPoint& q) const;

private:
    bool offMask(const WorldPoint& p, const WorldPoint& q) const;

    std::vector<WorldPoint> a_;
    std::vector<WorldPoint> b_;
    std::vector<WorldPoint> lo_;
    std::vector<WorldPoint> hi_;
    std::optional<OnMask> on_;
};

// 沿弦按半格步长采样: seg 取 min(净空, 目标余量) 的下确界, cost 取代价泛函积分
class ClearanceFloor
{
public:
    ClearanceFloor(const Grid<float>* cf, const Grid<float>* mg, double x0, double y0, double cs)
        : cf_(cf)
        , mg_(mg)
        , x0_(x0)
        , y0_(y0)
        , cs_(cs)
    {
    }

    float seg(const WorldPoint& p, const WorldPoint& q) const;

    double cost(const WorldPoint& p, const WorldPoint& q) const;

private:
    const Grid<float>* cf_ = nullptr;
    const Grid<float>* mg_ = nullptr;
    double x0_ = 0.0;
    double y0_ = 0.0;
    double cs_ = kCS;
};

// 走查只用来跟住弦所在的层, 立面本身由挡线集与拓扑禁步管住, 故抬升按体素取样
// 容差放宽: 高度取自格心, 斜面与接缝上相邻格的取样差本就能超出一步抬升上限,
// 照拓扑口径卡会把大量直弦判死, 拉直退化成网格锯齿。
class LayerOracle
{
public:
    LayerOracle(const SpanTable* st, const std::vector<int64_t>* cidx, int64_t nx, int64_t ny, double x0, double y0)
        : st_(st)
        , cidx_(cidx)
        , nx_(nx)
        , ny_(ny)
        , x0_(x0)
        , y0_(y0)
    {
    }

    // h 取起点高度或一组可达高度
    std::optional<std::vector<float>> walk(const std::vector<WorldPoint>& pts, float h) const;

    std::optional<std::vector<float>> walk(const std::vector<WorldPoint>& pts, const std::vector<float>& h) const;

    bool ok(const WorldPoint& p, const WorldPoint& q, float h, float hq) const;

private:
    const SpanTable* st_ = nullptr;
    const std::vector<int64_t>* cidx_ = nullptr;
    int64_t nx_ = 0;
    int64_t ny_ = 0;
    double x0_ = 0.0;
    double y0_ = 0.0;
};

std::vector<WorldPoint> StringPull(
    const std::vector<WorldPoint>& pts,
    const Blockers& blk,
    const ClearanceFloor* cfl,
    const LayerOracle* lyo = nullptr,
    const std::vector<float>* hs = nullptr);

std::vector<WorldPoint> Slim(
    const std::vector<WorldPoint>& pts,
    const Blockers& blk,
    double eps,
    const ClearanceFloor* cfl,
    const LayerOracle* lyo = nullptr,
    float h = 0.0F);

std::vector<WorldPoint> WidenCorners(
    const std::vector<WorldPoint>& pts,
    const Blockers& blk,
    const Grid<float>& dist,
    double x0,
    double y0,
    double cs,
    const ClearanceFloor* cfl,
    const LayerOracle* lyo,
    float h);

std::vector<WorldPoint> DropLoops(const std::vector<WorldPoint>& pts);

}
