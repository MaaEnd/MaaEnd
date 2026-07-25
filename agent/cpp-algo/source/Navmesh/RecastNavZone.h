#pragma once

#include <array>
#include <cstdint>
#include <optional>
#include <string>
#include <unordered_map>
#include <vector>

#include "BaseNavPack.h"
#include "BaseNavPlanner.h"
#include "RecastNavGrid.h"

namespace navmesh::recast
{

inline constexpr double kWeldDh = 3.0;              // 顶点焊接同柱高差容差 px
inline constexpr double kSnapFallbackRadius = 16.0; // 吸附兜底半径 px
inline constexpr double kSrcadjMaxGap = 8.0;
inline constexpr double kSrcadjLocalR = 12.0;

struct PolyMesh
{
    std::vector<WorldPoint> V;
    std::vector<double> H;
    std::vector<std::array<int32_t, 3>> T;
    std::vector<std::array<int32_t, 3>> NB;

    PolyMesh() = default;
    PolyMesh(std::vector<WorldPoint> v, std::vector<std::array<int32_t, 3>> t, std::vector<double> h);

    void buildNb();
    std::vector<int32_t> trisNear(const WorldPoint& p, double r) const; // 升序去重

    static constexpr double kGridCell = 24.0;
    static constexpr int64_t kGridStride = int64_t(1) << 24;
    std::vector<int64_t> gkeys;
    std::vector<int32_t> gtris;

private:
    void buildGrid();
};

struct HopPt
{
    WorldPoint exit_pt;
    WorldPoint entry_pt;
    int32_t to_tri = -1;
};

class ZoneClean
{
public:
    ZoneClean(const BaseNavPack& pack, const BaseNavPlanner& planner, const std::string& zone_name);

    bool valid() const { return error_.empty(); }

    const std::string& error() const { return error_; }

    struct SnapHit
    {
        int32_t tri = -1;
        WorldPoint point;
        double dist = 0.0;
    };

    std::optional<SnapHit> snap(const WorldPoint& p, double radius, std::optional<double> floor_y) const;

    std::vector<int32_t> batchLocate(const std::vector<WorldPoint>& pts, const std::vector<double>& hints) const;

    std::string name;
    uint16_t zone_id = 0;
    int64_t lo = 0;
    int64_t hi = 0;
    PolyMesh mesh;
    std::vector<int32_t> comp;        // 三角 → 分量代表(区内最小三角号)
    std::vector<uint8_t> comp_island; // 按分量代表值索引
    std::vector<HopPt> hops;
    std::string stats;

private:
    std::string error_;
};

class WallOracle
{
public:
    explicit WallOracle(const ZoneClean& zc);

    std::vector<int64_t> wallsInBbox(double x0, double y0, double x1, double y1);

    std::vector<WorldPoint> P0;
    std::vector<WorldPoint> P1;
    std::vector<double> HH;

private:
    bool hopNear(int64_t ei, double tol = 1.5) const;
    void classify(const std::vector<int64_t>& idx);

    const ZoneClean& zc_;
    std::vector<WorldPoint> M_;
    std::vector<WorldPoint> NOBS_;
    std::vector<WorldPoint> lo_;
    std::vector<WorldPoint> hi_;
    std::vector<int8_t> cls_;
    static constexpr double kHopCell = 4.0;
    std::unordered_map<int64_t, std::vector<std::array<double, 3>>> hop_grid_;
};

}
