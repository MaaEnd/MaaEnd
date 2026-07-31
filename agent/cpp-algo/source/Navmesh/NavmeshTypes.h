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
    // 与 points 等长的逐点通道半宽 px; 为空表示该路径不是 navmesh 规划出来的
    std::vector<double> clearance;
    std::vector<size_t> segment_breaks;
};

}
