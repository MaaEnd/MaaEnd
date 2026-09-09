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
    // 相机朝向（北为零、顺时针）与识别置信度。模型不可用、本帧被遮挡或非成功帧时没有值。
    std::optional<double> cam_angle;
    double cam_conf = 0.0;
    double score = 0.0;
    // 角色站在哪张可走面。实机定位给不出这个信息，只有预览端选了层才有值，不传就按区的主层走。
    std::optional<double> floor_y;
    bool valid = false;
    std::string zone_id;
    std::chrono::steady_clock::time_point timestamp;
};

// 上索认不出提示时的备用站位。面板给的是离身位最近的那台设备, 架子边上贴着供电桩时会被它抢走,
// 这个点从供电桩那侧让开一点点, 让架子重新成为最近的那个。
struct ZiplineRestand
{
    double x = 0.0;
    double y = 0.0;
};

} // namespace mapnavigator
