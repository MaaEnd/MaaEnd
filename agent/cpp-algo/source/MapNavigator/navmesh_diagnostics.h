#pragma once

#include <cstdint>
#include <string>
#include <vector>

#include "../Navmesh/NavmeshTypes.h"

namespace mapnavigator
{

// One successful planner leg selected by a complete authored-route expansion. The WebUI opts into
// collecting these records; normal navigation leaves the sink unset and does not retain the arrays.
struct NavmeshRouteDiagnostic
{
    navmesh::WorldPoint start;
    navmesh::WorldPoint goal;
    double x0 = 0.0;
    double y0 = 0.0;
    int64_t nx = 0;
    int64_t ny = 0;
    double cell_size = 0.0;
    std::vector<navmesh::WorldPoint> astar_cells;
    std::vector<navmesh::WorldPoint> rerouted_points;
    std::vector<navmesh::WorldPoint> string_pull_points;
    std::vector<navmesh::WorldPoint> assembled_points;
    std::vector<navmesh::WorldPoint> loop_fixed_points;
    std::vector<navmesh::WorldPoint> slim_points;
    std::vector<navmesh::WorldPoint> widened_points;
    std::vector<navmesh::WorldPoint> planned_points;
    std::vector<std::string> warnings;
};

} // namespace mapnavigator
