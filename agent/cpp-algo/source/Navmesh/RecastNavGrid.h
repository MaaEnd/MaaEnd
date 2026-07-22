#pragma once

#include <array>
#include <cstdint>
#include <optional>
#include <unordered_set>
#include <vector>

#include "NavmeshTypes.h"

namespace navmesh::recast
{

inline constexpr double kCS = 0.25;         // 体素边长 px
inline constexpr double kClimb = 3.0;       // 相邻格可连通最大高差 px
inline constexpr double kMergeH = 1.0;      // 同列 span 合并容差 px
inline constexpr double kEdtCap = 12.0;     // 距离场截断 px
inline constexpr double kR = 1.75;          // 期望余量上限 px
inline constexpr double kRel = 0.6;         // 期望余量 = min(R, REL×局部净空)
inline constexpr double kLam = 1.5;         // 满亏欠一步加价倍数
inline constexpr double kRidgeFloor = 0.5;  // 脊线保底余量地板 px
inline constexpr double kMaxErr = 0.5;      // 轮廓 DP 容差 px
inline constexpr double kSlimEps = 0.5;     // 终线共线剔除容差 px
inline constexpr double kMcHBand = 8.0;     // 层高度带(墙筛/盖章)px
inline constexpr double kHBand = 6.0;       // 真墙探针高度带 px
inline constexpr double kEpsProbe = 0.75;   // 真墙探针距离 px
inline constexpr double kSnapRadius = 8.0;  // 起终点吸附半径 px
inline constexpr double kMargin = 25.0;     // 窗口外扩 px
inline constexpr double kBlockedPointRadius = 1.0;  // 封堵点盖章半径 px
inline constexpr int64_t kHoleMaxCells = 32;        // 封闭小洞填充上限(格 = 2px²)
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

std::vector<uint8_t> Flood(int64_t seed, const SpanTable& st, int64_t nx);

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

WallCsr BuildWallIndex(
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    double ox,
    double oy,
    int64_t nx,
    int64_t ny);

std::unordered_set<int64_t> BannedSteps(
    const Mask& free,
    const WallCsr& csr,
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    double ox,
    double oy);

std::vector<int64_t> Comps4(const Mask& mask);

Mask FillHoles(const Mask& mask, int64_t max_cells, const Mask* protect);

Mask CloseCracks(const Mask& core, const Mask& lay, const Mask* protect);

std::optional<std::vector<CellPt>> CostAstar(
    const Mask& mask,
    CellPt s,
    CellPt g,
    const Grid<float>& mult,
    const std::unordered_set<int64_t>* banned,
    const double* bnp);

Grid<float> PrefField(const Grid<float>& dist, bool ridge);

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

std::vector<WorldPoint> StringPull(const std::vector<WorldPoint>& pts, const Blockers& blk);

std::vector<WorldPoint> Slim(const std::vector<WorldPoint>& pts, const Blockers& blk, double eps);

std::vector<WorldPoint> DropLoops(const std::vector<WorldPoint>& pts);

}
