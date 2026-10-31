#pragma once

#include <optional>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/NoWarningCV.hpp>

#include "../../navi_config.h"

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
    // 单笔过长时按下点会落进左侧摇杆区, 那一笔不转镜头
    int max_stroke_dx = 270;

    // 与按下或抬起同帧的位移会整步丢失, 两端停留须盖过一帧
    int touch_down_hold_ms = 100;
    // 离按下点不超过起拖阈值的位移游戏不认, 第一步直接移出这么远
    int drag_start_lead = 32;
    int move_steps = 6;
    int move_step_delay_ms = 10;
    int end_hold_ms = 100;

    int post_swipe_settle_ms = kAdbTouchTurnProfile.post_swipe_settle_ms;
};

class AdbCameraSwipeDriver
{
public:
    AdbCameraSwipeDriver(MaaController* controller, AdbCameraSwipeDriverConfig config = {});

    bool swipeByPixels(int dx, int dy) const;

private:
    struct ScreenGeometry
    {
        cv::Size resolution {};
        cv::Point center {};
        int up_limit = 0;
        int down_limit = 0;
    };

    std::optional<ScreenGeometry> getScreenGeometry() const;
    bool executeStableDrag(const ScreenGeometry& geometry, int swipe_dx, int swipe_dy) const;
    bool postTouchDown(const cv::Point& point) const;
    bool postTouchMove(const cv::Point& point) const;
    bool postTouchUp() const;
    bool ensureControllerActionPosted(MaaCtrlId ctrl_id, const char* action_name) const;
    bool waitForControllerAction(MaaCtrlId ctrl_id, const char* action_name) const;

    MaaController* controller_ = nullptr;
    AdbCameraSwipeDriverConfig config_;
};

} // namespace mapnavigator::backend::adb
