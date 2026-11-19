#pragma once

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/NoWarningCV.hpp>

#include "../../navi_config.h"
#include "../../navi_time.h"

namespace mapnavigator::backend::adb
{

struct AdbCameraSwipeDriverConfig
{
    int contact_id = 1;
    int pressure = 0;
    int edge_margin = 32;
    int origin_x_numerator = 1;
    int origin_x_denominator = 2;
    int origin_y_numerator = 1;
    int origin_y_denominator = 2;
    int origin_y_offset = -96;
    // 过完加载画面按着的手指不再转镜头, 闲置这么久就抬起重按
    int held_idle_repress_ms = 2000;

    // 与按下或抬起同帧的位移会整步丢失, 两端停留须盖过一帧
    int touch_down_hold_ms = 100;
    // 离按下点不超过起拖阈值的位移游戏不认, 第一步直接移出这么远
    int drag_start_lead = 32;
    int move_steps = 6;
    int move_step_delay_ms = 10;
    int end_hold_ms = 100;

    int post_swipe_settle_ms = kAdbTouchTurnProfile.post_swipe_settle_ms;
};

struct AdbCameraSwipeDriver
{
    MaaController* controller = nullptr;
    AdbCameraSwipeDriverConfig config;
    bool touch_active = false;
    cv::Point touch_point {};
    TimePoint last_touch_at {};
};

bool SwipeCameraByPixels(AdbCameraSwipeDriver* driver, int dx, int dy);
bool ReleaseCameraTouch(AdbCameraSwipeDriver* driver);

} // namespace mapnavigator::backend::adb
