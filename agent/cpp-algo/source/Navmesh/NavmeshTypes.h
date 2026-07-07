#pragma once

#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace navmesh
{

struct WorldPoint
{
    double x = 0.0;
    double y = 0.0;
};

struct WorldPath
{
    uint16_t zone_id = 0;
    std::string zone_name;
    std::vector<WorldPoint> points;
    std::vector<size_t> segment_breaks;
};

}
