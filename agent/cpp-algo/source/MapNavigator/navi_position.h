#pragma once

#include <chrono>
#include <optional>
#include <string>

namespace mapnavigator
{

struct NaviPosition
{
    double x = 0.0;
    double y = 0.0;
    double angle = 0.0;
    double score = 0.0;
    // 角色站在哪张可走面。实机定位给不出这个信息，只有预览端选了层才有值，不传就按区的主层走。
    std::optional<double> floor_y;
    // 镜头朝向，与 angle（角色箭头朝向）是两个独立识别结果。判不出来就是空，消费方按没有这项信息处理。
    std::optional<double> camera_angle;
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
