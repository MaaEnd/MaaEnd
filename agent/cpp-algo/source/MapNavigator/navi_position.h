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
    // 角色站在哪张可走面。实机定位给不出这个信息，只有预览端选了层才有值，不传就按区的主层走。
    std::optional<double> floor_y;
    // angle 是本次取位选定的朝向；camera_angle 是镜头朝向，用于起步对齐，以及停车后的精确接近。
    // 触控后端只给置信度够的读数，并在站定转向时用它代替不会跟着转的角色箭头；桌面后端照旧给原始读数。
    std::optional<double> camera_angle;
    // 本帧位置与选定朝向均可用。
    bool valid = false;
    std::string zone_id;
    std::chrono::steady_clock::time_point timestamp;
};

// 上索要走到的一个站位。坐标记录的是随朝向变化的角格锚点, 设备模型占着锚点四周哪一格未知,
// 所以候选是各个可能的中心格; 末位那个从供电桩一侧让开, 让架子重新成为离身位最近的设备。
struct ZiplineMountSpot
{
    double x = 0.0;
    double y = 0.0;
};

} // namespace mapnavigator
