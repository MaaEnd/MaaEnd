#pragma once

#include <optional>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/NoWarningCV.hpp>

#include "../../navi_config.h"
#include "../backend.h"

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

class AdbVirtualJoystickDriver
{
public:
    explicit AdbVirtualJoystickDriver(MaaController* controller, AdbVirtualJoystickDriverConfig config = {});
    ~AdbVirtualJoystickDriver();

    bool setMovementState(MovementState state, int delay_millis);
    bool pulseForward(int hold_millis);
    bool release(int delay_millis);
    bool setWalking(bool walking);

    bool walking() const { return walking_; }

private:
    struct ControlGeometry
    {
        cv::Point control_origin {};
        cv::Rect control_roi {};
        cv::Size frame_size {};
    };

    std::optional<ControlGeometry> acquireControlGeometry();
    bool beginDirectionalDrag(cv::Point direction);
    bool updateDirectionalDrag(cv::Point direction);
    void clearContact();
    bool postTouchDown(const cv::Point& point) const;
    bool postTouchMove(const cv::Point& point) const;
    bool postTouchUp() const;
    cv::Point computeTargetPoint(cv::Point direction) const;
    int computeDragRadius() const;
    static cv::Point clampPoint(cv::Point point, const cv::Size& bounds);

    MaaController* controller_ = nullptr;
    AdbVirtualJoystickDriverConfig config_;
    // 推杆方向, x 向右为正, y 向后为正, 分量只取 -1/0/1, 零向量表示没推
    cv::Point active_direction_ {};
    bool touch_active_ = false;
    bool walking_ = false;
    std::optional<ControlGeometry> active_geometry_;
    cv::Point active_touch_point_ {};
};

} // namespace mapnavigator::backend::adb
