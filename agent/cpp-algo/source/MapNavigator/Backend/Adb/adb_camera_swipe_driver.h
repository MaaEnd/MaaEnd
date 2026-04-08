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

    int turn_swipe_duration_ms = kAdbTouchTurnProfile.swipe_duration_ms;

    int touch_down_hold_ms = 8;
    int move_steps = 6;
    int move_step_delay_ms = 10;
    int end_hold_ms = 30;

    int post_swipe_settle_ms = kAdbTouchTurnProfile.post_swipe_settle_ms;
};

class AdbCameraSwipeDriver
{
public:
    AdbCameraSwipeDriver(MaaController* controller, AdbCameraSwipeDriverConfig config = {});

    bool SwipeByPixels(int dx, int dy) const;

private:
    struct ScreenGeometry
    {
        cv::Size resolution {};
        cv::Point center {};
        int left_limit = 0;
        int right_limit = 0;
        int up_limit = 0;
        int down_limit = 0;
    };

    std::optional<ScreenGeometry> GetScreenGeometry() const;
    bool ExecuteSwipe(const ScreenGeometry& geometry, int swipe_dx) const;
    bool ExecuteStableDrag(const ScreenGeometry& geometry, int swipe_dx) const;
    bool PostTouchDown(const cv::Point& point) const;
    bool PostTouchMove(const cv::Point& point) const;
    bool PostTouchUp() const;
    bool EnsureControllerActionPosted(MaaCtrlId ctrl_id, const char* action_name) const;
    bool WaitForControllerAction(MaaCtrlId ctrl_id, const char* action_name) const;
    static void SleepIfNeeded(int delay_millis);

    MaaController* controller_ = nullptr;
    AdbCameraSwipeDriverConfig config_;
};

} // namespace mapnavigator::backend::adb
