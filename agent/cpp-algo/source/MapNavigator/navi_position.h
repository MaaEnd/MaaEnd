#pragma once

#include <chrono>
#include <optional>
#include <string>

namespace mapnavigator
{

enum class HeadingSource
{
    Character,
    Camera,
};

struct NaviPosition
{
    double x = 0.0;
    double y = 0.0;
    double angle = 0.0;
    double score = 0.0;
    // 所在可走面高度, 只有预览端选了层才有值, 缺省按区的主层
    std::optional<double> floor_y;
    // angle 是本次选定的朝向, camera_angle 是镜头朝向, 后者可能因置信度不足而缺省
    std::optional<double> camera_angle;
    // 位置与选定朝向都可用
    bool valid = false;
    std::string zone_id;
    std::chrono::steady_clock::time_point timestamp;
};

// 上索站位, 候选为锚点四周可能的中心格, 末位从供电桩一侧让开
struct ZiplineMountSpot
{
    double x = 0.0;
    double y = 0.0;
};

} // namespace mapnavigator
