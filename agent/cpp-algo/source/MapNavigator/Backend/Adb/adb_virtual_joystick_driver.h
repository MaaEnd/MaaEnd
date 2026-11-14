#pragma once

#include <optional>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/NoWarningCV.hpp>

#include "../../navi_config.h"
#include "../backend_types.h"

namespace mapnavigator::backend::adb
{

struct AdbVirtualJoystickDriverConfig
{
    cv::Point control_origin { 198, 552 };
    cv::Rect control_roi { 89, 443, 219, 219 };
    cv::Size frame_size { kWorkWidth, kWorkHeight };
    int contact_id = 8;
    int drag_radius = 72;
    double walk_drag_ratio = 0.25;
    int control_edge_inset = 4;
    int touch_down_hold_ms = 16;
    int move_steps = 4;
    int move_step_delay_ms = 16;
    int release_settle_ms = 32;
};

struct JoystickControlGeometry
{
    cv::Point control_origin {};
    cv::Rect control_roi {};
    cv::Size frame_size {};
};

struct AdbVirtualJoystickDriver
{
    MaaController* controller = nullptr;
    AdbVirtualJoystickDriverConfig config;
    // 推杆方向, x 向右为正, y 向后为正, 分量只取 -1/0/1, 零向量表示没推
    cv::Point active_direction {};
    bool touch_active = false;
    bool walking = false;
    std::optional<JoystickControlGeometry> active_geometry;
    cv::Point active_touch_point {};
};

bool SetJoystickMovementState(AdbVirtualJoystickDriver* joystick, MovementState state, int delay_millis);
bool PulseJoystickForward(AdbVirtualJoystickDriver* joystick, int hold_millis);
bool ReleaseJoystick(AdbVirtualJoystickDriver* joystick, int delay_millis);
bool SetJoystickWalking(AdbVirtualJoystickDriver* joystick, bool walking);

} // namespace mapnavigator::backend::adb
