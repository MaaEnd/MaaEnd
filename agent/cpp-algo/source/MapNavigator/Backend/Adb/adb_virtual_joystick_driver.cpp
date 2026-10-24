#include <algorithm>
#include <cmath>
#include <numbers>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../../navi_time.h"
#include "adb_virtual_joystick_driver.h"

namespace mapnavigator::backend::adb
{

namespace
{

constexpr int32_t kAdbDefaultTouchPressure = 0;

cv::Point ResolveDirection(const MovementState& state)
{
    return { (state.right ? 1 : 0) - (state.left ? 1 : 0), (state.backward ? 1 : 0) - (state.forward ? 1 : 0) };
}

} // namespace

AdbVirtualJoystickDriver::AdbVirtualJoystickDriver(MaaController* controller, AdbVirtualJoystickDriverConfig config)
    : controller_(controller)
    , config_(std::move(config))
{
}

AdbVirtualJoystickDriver::~AdbVirtualJoystickDriver()
{
    const bool released = Release(0);
    (void)released;
}

bool AdbVirtualJoystickDriver::SetMovementState(MovementState state, int delay_millis)
{
    const cv::Point direction = ResolveDirection(state);
    if (direction == cv::Point()) {
        return Release(delay_millis);
    }

    if (!touch_active_) {
        const bool started = BeginDirectionalDrag(direction);
        if (!started) {
            return false;
        }
    }
    else if (active_direction_ != direction) {
        const bool updated = UpdateDirectionalDrag(direction);
        if (!updated) {
            Release(0);
            return false;
        }
    }

    SleepFor(delay_millis);
    return true;
}

bool AdbVirtualJoystickDriver::PulseForward(int hold_millis)
{
    if (!SetMovementState({ .forward = true }, 0)) {
        return false;
    }
    SleepFor(hold_millis);
    return Release(0);
}

bool AdbVirtualJoystickDriver::Release(int delay_millis)
{
    if (!touch_active_) {
        ClearContact();
        SleepFor(delay_millis);
        return true;
    }

    if (!PostTouchUp()) {
        LogWarn << "AdbVirtualJoystickDriver: failed to release joystick touch contact.";
        ClearContact();
        SleepFor(delay_millis);
        return false;
    }

    ClearContact();
    SleepFor(std::max(delay_millis, config_.release_settle_ms));
    return true;
}

bool AdbVirtualJoystickDriver::SetWalking(bool walking)
{
    walking_ = walking;
    return !touch_active_ || UpdateDirectionalDrag(active_direction_);
}

std::optional<AdbVirtualJoystickDriver::ControlGeometry> AdbVirtualJoystickDriver::AcquireControlGeometry()
{
    if (config_.frame_size.width <= 0 || config_.frame_size.height <= 0 || config_.control_roi.width <= 0
        || config_.control_roi.height <= 0) {
        LogWarn << "AdbVirtualJoystickDriver: invalid fixed joystick geometry.";
        return std::nullopt;
    }

    ControlGeometry geometry;
    geometry.control_roi = config_.control_roi & cv::Rect(0, 0, config_.frame_size.width, config_.frame_size.height);
    if (geometry.control_roi.width <= 0 || geometry.control_roi.height <= 0) {
        LogWarn << "AdbVirtualJoystickDriver: fixed joystick control roi is out of bounds.";
        return std::nullopt;
    }

    geometry.control_origin = ClampPoint(config_.control_origin, config_.frame_size);
    if (!geometry.control_roi.contains(geometry.control_origin)) {
        geometry.control_origin = {
            geometry.control_roi.x + geometry.control_roi.width / 2,
            geometry.control_roi.y + geometry.control_roi.height / 2,
        };
    }
    geometry.frame_size = config_.frame_size;
    return geometry;
}

bool AdbVirtualJoystickDriver::BeginDirectionalDrag(cv::Point direction)
{
    active_geometry_ = AcquireControlGeometry();
    if (!active_geometry_.has_value()) {
        active_direction_ = {};
        return false;
    }

    const cv::Point start = ClampPoint(active_geometry_->control_origin, active_geometry_->frame_size);
    if (!PostTouchDown(start)) {
        active_geometry_.reset();
        active_direction_ = {};
        return false;
    }

    touch_active_ = true;
    active_touch_point_ = start;
    SleepFor(config_.touch_down_hold_ms);

    if (!UpdateDirectionalDrag(direction)) {
        Release(0);
        return false;
    }
    return true;
}

bool AdbVirtualJoystickDriver::UpdateDirectionalDrag(cv::Point direction)
{
    if (!touch_active_ || !active_geometry_.has_value()) {
        return false;
    }

    const cv::Point target = ComputeTargetPoint(direction);
    if (target == active_touch_point_) {
        active_direction_ = direction;
        return true;
    }

    const int move_steps = std::max(1, config_.move_steps);
    const cv::Point start = active_touch_point_;
    for (int step = 1; step <= move_steps; ++step) {
        const double ratio = static_cast<double>(step) / static_cast<double>(move_steps);
        const int next_x = static_cast<int>(std::lround(start.x + static_cast<double>(target.x - start.x) * ratio));
        const int next_y = static_cast<int>(std::lround(start.y + static_cast<double>(target.y - start.y) * ratio));
        if (!PostTouchMove({ next_x, next_y })) {
            return false;
        }
        SleepFor(config_.move_step_delay_ms);
    }

    active_touch_point_ = target;
    active_direction_ = direction;
    return true;
}

void AdbVirtualJoystickDriver::ClearContact()
{
    touch_active_ = false;
    active_direction_ = {};
    active_geometry_.reset();
    active_touch_point_ = {};
}

bool AdbVirtualJoystickDriver::PostTouchDown(const cv::Point& point) const
{
    if (controller_ == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostTouchDown(controller_, config_.contact_id, point.x, point.y, kAdbDefaultTouchPressure);
    MaaControllerWait(controller_, ctrl_id);
    return true;
}

bool AdbVirtualJoystickDriver::PostTouchMove(const cv::Point& point) const
{
    if (controller_ == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostTouchMove(controller_, config_.contact_id, point.x, point.y, kAdbDefaultTouchPressure);
    MaaControllerWait(controller_, ctrl_id);
    return true;
}

bool AdbVirtualJoystickDriver::PostTouchUp() const
{
    if (controller_ == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostTouchUp(controller_, config_.contact_id);
    MaaControllerWait(controller_, ctrl_id);
    return true;
}

cv::Point AdbVirtualJoystickDriver::ComputeTargetPoint(cv::Point direction) const
{
    if (!active_geometry_.has_value()) {
        return {};
    }

    const int radius = ComputeDragRadius();
    const bool diagonal = direction.x != 0 && direction.y != 0;
    const double unit_scale = (diagonal ? 1.0 / std::numbers::sqrt2_v<double> : 1.0) * (walking_ ? config_.walk_drag_ratio : 1.0);

    const cv::Point image_target(
        active_geometry_->control_origin.x
            + static_cast<int>(std::lround(static_cast<double>(direction.x) * static_cast<double>(radius) * unit_scale)),
        active_geometry_->control_origin.y
            + static_cast<int>(std::lround(static_cast<double>(direction.y) * static_cast<double>(radius) * unit_scale)));
    return ClampPoint(image_target, active_geometry_->frame_size);
}

int AdbVirtualJoystickDriver::ComputeDragRadius() const
{
    if (!active_geometry_.has_value()) {
        return std::max(1, config_.drag_radius);
    }

    const int control_extent = std::min(active_geometry_->control_roi.width, active_geometry_->control_roi.height);
    const int available_radius = std::max(1, control_extent / 2 - config_.control_edge_inset);
    return std::clamp(config_.drag_radius, 1, available_radius);
}

cv::Point AdbVirtualJoystickDriver::ClampPoint(cv::Point point, const cv::Size& bounds)
{
    point.x = std::clamp(point.x, 0, std::max(0, bounds.width - 1));
    point.y = std::clamp(point.y, 0, std::max(0, bounds.height - 1));
    return point;
}

} // namespace mapnavigator::backend::adb
