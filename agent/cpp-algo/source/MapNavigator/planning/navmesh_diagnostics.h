#pragma once

#include <cstdint>
#include <string>
#include <vector>

#include "../../Navmesh/NavmeshTypes.h"

namespace mapnavigator
{

struct NavmeshRouteDiagnostic
{
    struct Timing
    {
        double window_ms = 0.0;
        double topology_ms = 0.0;
        double geometry_ms = 0.0;
        double pull_ms = 0.0;
        double assemble_ms = 0.0;
        double lift_ms = 0.0;
        double total_ms = 0.0;
    } timing;

    navmesh::WorldPoint start;
    navmesh::WorldPoint goal;
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
    double cell_size = 0.0;
    std::vector<navmesh::WorldPoint> topology_cells;
    std::vector<double> topology_heights;
    std::vector<navmesh::WorldPoint> taut_points;
    std::vector<navmesh::WorldPoint> pulled_points;
    std::vector<navmesh::WorldPoint> assembled_points;
    std::vector<navmesh::WorldPoint> planned_points;
    std::vector<std::string> warnings;
};

} // namespace mapnavigator
