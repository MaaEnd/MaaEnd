#pragma once

#include <memory>
#include <optional>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/NoWarningCV.hpp>

namespace maplocator
{

class MapLocator;

} // namespace maplocator

namespace mapnavigator::backend::adb
{

struct AdbVirtualJoystickDriverConfig
{
    cv::Point control_origin { 198, 552 };
    cv::Rect control_roi { 89, 443, 219, 219 };
    cv::Size frame_size { 1280, 720 };
    int contact_id = 8;
    int drag_radius = 72;
    int control_edge_inset = 4;
    int touch_down_hold_ms = 16;
    int move_steps = 4;
    int move_step_delay_ms = 16;
    int release_settle_ms = 32;
};

class AdbVirtualJoystickDriver
{
public:
    AdbVirtualJoystickDriver(
        MaaController* controller,
        std::shared_ptr<maplocator::MapLocator> locator,
        AdbVirtualJoystickDriverConfig config = {});
    ~AdbVirtualJoystickDriver();

    bool SetMovementState(bool forward, bool left, bool backward, bool right, int delay_millis);
    bool PulseForward(int hold_millis);
    bool Release(int delay_millis);

private:
    enum class Direction
    {
        kNone,
        kForward,
        kForwardLeft,
        kLeft,
        kBackwardLeft,
        kBackward,
        kBackwardRight,
        kRight,
        kForwardRight,
    };

    struct ControlGeometry
    {
        cv::Point control_origin {};
        cv::Rect control_roi {};
        cv::Size frame_size {};
    };

    Direction ResolveDirection(bool forward, bool left, bool backward, bool right) const;
    std::optional<ControlGeometry> AcquireControlGeometry();
    bool BeginDirectionalDrag(Direction direction);
    bool UpdateDirectionalDrag(Direction direction);
    bool PostTouchDown(const cv::Point& point) const;
    bool PostTouchMove(const cv::Point& point) const;
    bool PostTouchUp() const;
    cv::Point ComputeTargetPoint(Direction direction) const;
    cv::Point DirectionVector(Direction direction) const;
    int ComputeDragRadius() const;
    static cv::Point ClampPoint(cv::Point point, const cv::Size& bounds);
    static void SleepIfNeeded(int delay_millis);

    MaaController* controller_ = nullptr;
    AdbVirtualJoystickDriverConfig config_;
    Direction active_direction_ = Direction::kNone;
    bool touch_active_ = false;
    std::optional<ControlGeometry> active_geometry_;
    cv::Point active_touch_point_ {};
};

} // namespace mapnavigator::backend::adb
