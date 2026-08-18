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
    // 规划器给的驱动航点下标(不含起点,末位恒为 points.size()-1); 为空则由航点侧自行拉直
    std::vector<size_t> waypoints;
    std::vector<size_t> segment_breaks;
};

}
