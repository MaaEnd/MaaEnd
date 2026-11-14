#include <algorithm>
#include <cmath>
#include <numbers>

#include <MaaUtils/Logger.h>

#include "../../navi_time.h"
#include "adb_virtual_joystick_driver.h"

namespace mapnavigator::backend::adb
{

namespace
{

constexpr int32_t kAdbDefaultTouchPressure = 0;

cv::Point resolve_direction(const MovementState& state)
{
    return { (state.right ? 1 : 0) - (state.left ? 1 : 0), (state.backward ? 1 : 0) - (state.forward ? 1 : 0) };
}

cv::Point clamp_point(cv::Point point, const cv::Size& bounds)
{
    point.x = std::clamp(point.x, 0, std::max(0, bounds.width - 1));
    point.y = std::clamp(point.y, 0, std::max(0, bounds.height - 1));
    return point;
}

std::optional<JoystickControlGeometry> acquire_control_geometry(const AdbVirtualJoystickDriverConfig& config)
{
    if (config.frame_size.width <= 0 || config.frame_size.height <= 0 || config.control_roi.width <= 0 || config.control_roi.height <= 0) {
        LogWarn << "AdbVirtualJoystickDriver: invalid fixed joystick geometry.";
        return std::nullopt;
    }

    JoystickControlGeometry geometry;
    geometry.control_roi = config.control_roi & cv::Rect(0, 0, config.frame_size.width, config.frame_size.height);
    if (geometry.control_roi.width <= 0 || geometry.control_roi.height <= 0) {
        LogWarn << "AdbVirtualJoystickDriver: fixed joystick control roi is out of bounds.";
        return std::nullopt;
    }

    geometry.control_origin = clamp_point(config.control_origin, config.frame_size);
    if (!geometry.control_roi.contains(geometry.control_origin)) {
        geometry.control_origin = {
            geometry.control_roi.x + geometry.control_roi.width / 2,
            geometry.control_roi.y + geometry.control_roi.height / 2,
        };
    }
    geometry.frame_size = config.frame_size;
    return geometry;
}

bool post_touch_down(const AdbVirtualJoystickDriver& joystick, const cv::Point& point)
{
    if (joystick.controller == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id =
        MaaControllerPostTouchDown(joystick.controller, joystick.config.contact_id, point.x, point.y, kAdbDefaultTouchPressure);
    MaaControllerWait(joystick.controller, ctrl_id);
    return true;
}

bool post_touch_move(const AdbVirtualJoystickDriver& joystick, const cv::Point& point)
{
    if (joystick.controller == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id =
        MaaControllerPostTouchMove(joystick.controller, joystick.config.contact_id, point.x, point.y, kAdbDefaultTouchPressure);
    MaaControllerWait(joystick.controller, ctrl_id);
    return true;
}

bool post_touch_up(const AdbVirtualJoystickDriver& joystick)
{
    if (joystick.controller == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostTouchUp(joystick.controller, joystick.config.contact_id);
    MaaControllerWait(joystick.controller, ctrl_id);
    return true;
}

int compute_drag_radius(const AdbVirtualJoystickDriver& joystick)
{
    if (!joystick.active_geometry.has_value()) {
        return std::max(1, joystick.config.drag_radius);
    }

    const int control_extent = std::min(joystick.active_geometry->control_roi.width, joystick.active_geometry->control_roi.height);
    const int available_radius = std::max(1, control_extent / 2 - joystick.config.control_edge_inset);
    return std::clamp(joystick.config.drag_radius, 1, available_radius);
}

cv::Point compute_target_point(const AdbVirtualJoystickDriver& joystick, cv::Point direction)
{
    if (!joystick.active_geometry.has_value()) {
        return {};
    }

    const int radius = compute_drag_radius(joystick);
    const bool diagonal = direction.x != 0 && direction.y != 0;
    const double unit_scale =
        (diagonal ? 1.0 / std::numbers::sqrt2_v<double> : 1.0) * (joystick.walking ? joystick.config.walk_drag_ratio : 1.0);

    const cv::Point image_target(
        joystick.active_geometry->control_origin.x
            + static_cast<int>(std::lround(static_cast<double>(direction.x) * static_cast<double>(radius) * unit_scale)),
        joystick.active_geometry->control_origin.y
            + static_cast<int>(std::lround(static_cast<double>(direction.y) * static_cast<double>(radius) * unit_scale)));
    return clamp_point(image_target, joystick.active_geometry->frame_size);
}

void clear_contact(AdbVirtualJoystickDriver* joystick)
{
    joystick->touch_active = false;
    joystick->active_direction = {};
    joystick->active_geometry.reset();
    joystick->active_touch_point = {};
}

bool update_directional_drag(AdbVirtualJoystickDriver* joystick, cv::Point direction)
{
    if (!joystick->touch_active || !joystick->active_geometry.has_value()) {
        return false;
    }

    const cv::Point target = compute_target_point(*joystick, direction);
    if (target == joystick->active_touch_point) {
        joystick->active_direction = direction;
        return true;
    }

    const int move_steps = std::max(1, joystick->config.move_steps);
    const cv::Point start = joystick->active_touch_point;
    for (int step = 1; step <= move_steps; ++step) {
        const double ratio = static_cast<double>(step) / static_cast<double>(move_steps);
        const int next_x = static_cast<int>(std::lround(start.x + static_cast<double>(target.x - start.x) * ratio));
        const int next_y = static_cast<int>(std::lround(start.y + static_cast<double>(target.y - start.y) * ratio));
        if (!post_touch_move(*joystick, { next_x, next_y })) {
            return false;
        }
        SleepFor(joystick->config.move_step_delay_ms);
    }

    joystick->active_touch_point = target;
    joystick->active_direction = direction;
    return true;
}

bool begin_directional_drag(AdbVirtualJoystickDriver* joystick, cv::Point direction)
{
    joystick->active_geometry = acquire_control_geometry(joystick->config);
    if (!joystick->active_geometry.has_value()) {
        joystick->active_direction = {};
        return false;
    }

    const cv::Point start = clamp_point(joystick->active_geometry->control_origin, joystick->active_geometry->frame_size);
    if (!post_touch_down(*joystick, start)) {
        joystick->active_geometry.reset();
        joystick->active_direction = {};
        return false;
    }

    joystick->touch_active = true;
    joystick->active_touch_point = start;
    SleepFor(joystick->config.touch_down_hold_ms);

    if (!update_directional_drag(joystick, direction)) {
        ReleaseJoystick(joystick, 0);
        return false;
    }
    return true;
}

} // namespace

bool SetJoystickMovementState(AdbVirtualJoystickDriver* joystick, MovementState state, int delay_millis)
{
    const cv::Point direction = resolve_direction(state);
    if (direction == cv::Point()) {
        return ReleaseJoystick(joystick, delay_millis);
    }

    if (!joystick->touch_active) {
        const bool started = begin_directional_drag(joystick, direction);
        if (!started) {
            return false;
        }
    }
    else if (joystick->active_direction != direction) {
        const bool updated = update_directional_drag(joystick, direction);
        if (!updated) {
            ReleaseJoystick(joystick, 0);
            return false;
        }
    }

    SleepFor(delay_millis);
    return true;
}

bool PulseJoystickForward(AdbVirtualJoystickDriver* joystick, int hold_millis)
{
    if (!SetJoystickMovementState(joystick, { .forward = true }, 0)) {
        return false;
    }
    SleepFor(hold_millis);
    return ReleaseJoystick(joystick, 0);
}

bool ReleaseJoystick(AdbVirtualJoystickDriver* joystick, int delay_millis)
{
    if (!joystick->touch_active) {
        clear_contact(joystick);
        SleepFor(delay_millis);
        return true;
    }

    if (!post_touch_up(*joystick)) {
        LogWarn << "AdbVirtualJoystickDriver: failed to release joystick touch contact.";
        clear_contact(joystick);
        SleepFor(delay_millis);
        return false;
    }

    clear_contact(joystick);
    SleepFor(std::max(delay_millis, joystick->config.release_settle_ms));
    return true;
}

bool SetJoystickWalking(AdbVirtualJoystickDriver* joystick, bool walking)
{
    joystick->walking = walking;
    return !joystick->touch_active || update_directional_drag(joystick, joystick->active_direction);
}

} // namespace mapnavigator::backend::adb
