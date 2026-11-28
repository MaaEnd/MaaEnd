#include <algorithm>
#include <cmath>
#include <optional>

#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

#include "../../navi_time.h"
#include "adb_camera_swipe_driver.h"

namespace mapnavigator::backend::adb
{

namespace
{

struct ScreenGeometry
{
    cv::Size resolution {};
    cv::Point center {};
    int left_x = 0;
    int right_x = 0;
    int up_limit = 0;
    int down_limit = 0;
};

cv::Point clamp_point(const cv::Point& point, const cv::Size& resolution)
{
    return {
        std::clamp(point.x, 0, std::max(0, resolution.width - 1)),
        std::clamp(point.y, 0, std::max(0, resolution.height - 1)),
    };
}

cv::Point drag_lead_point(const cv::Point& start, const cv::Point& end, int lead, const cv::Size& resolution)
{
    const double length = std::hypot(static_cast<double>(end.x - start.x), static_cast<double>(end.y - start.y));
    if (length <= 0.0) {
        return end;
    }
    const double scale = static_cast<double>(lead) / length;
    return clamp_point(
        { start.x + static_cast<int>(std::lround((end.x - start.x) * scale)),
          start.y + static_cast<int>(std::lround((end.y - start.y) * scale)) },
        resolution);
}

std::optional<ScreenGeometry> get_screen_geometry(const AdbCameraSwipeDriver& driver)
{
    if (driver.controller == nullptr) {
        return std::nullopt;
    }

    const int safe_margin = std::max(0, driver.config.edge_margin);
    const int x_denominator = std::max(1, driver.config.origin_x_denominator);
    const int y_denominator = std::max(1, driver.config.origin_y_denominator);
    const int origin_x = std::clamp(kWorkWidth * driver.config.origin_x_numerator / x_denominator, 0, kWorkWidth - 1);
    const int origin_y =
        std::clamp(kWorkHeight * driver.config.origin_y_numerator / y_denominator + driver.config.origin_y_offset, 0, kWorkHeight - 1);
    const cv::Point center(origin_x, origin_y);

    ScreenGeometry geometry;
    geometry.resolution = { kWorkWidth, kWorkHeight };
    geometry.center = center;
    geometry.left_x = safe_margin;
    geometry.right_x = kWorkWidth - 1 - safe_margin;
    geometry.up_limit = std::max(1, center.y - safe_margin);
    geometry.down_limit = std::max(1, (kWorkHeight - 1 - safe_margin) - center.y);
    return geometry;
}

bool ensure_controller_action_posted(MaaCtrlId ctrl_id, const char* action_name)
{
    if (ctrl_id != MaaInvalidId) {
        return true;
    }

    LogWarn << "AdbCameraSwipeDriver: failed to post controller action." << VAR(action_name);
    return false;
}

bool wait_for_controller_action(const AdbCameraSwipeDriver& driver, MaaCtrlId ctrl_id, const char* action_name)
{
    if (!ensure_controller_action_posted(ctrl_id, action_name)) {
        return false;
    }

    const MaaStatus status = MaaControllerWait(driver.controller, ctrl_id);
    if (status == MaaStatus_Succeeded) {
        return true;
    }

    LogWarn << "AdbCameraSwipeDriver: controller action did not succeed." << VAR(action_name) << VAR(ctrl_id) << VAR(status);
    return false;
}

bool post_touch_down(AdbCameraSwipeDriver* driver, const cv::Point& point)
{
    if (driver->controller == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id =
        MaaControllerPostTouchDown(driver->controller, driver->config.contact_id, point.x, point.y, driver->config.pressure);
    const bool done = wait_for_controller_action(*driver, ctrl_id, "touch_down");
    driver->last_touch_at = std::chrono::steady_clock::now();
    return done;
}

bool post_touch_move(AdbCameraSwipeDriver* driver, const cv::Point& point)
{
    if (driver->controller == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id =
        MaaControllerPostTouchMove(driver->controller, driver->config.contact_id, point.x, point.y, driver->config.pressure);
    const bool done = wait_for_controller_action(*driver, ctrl_id, "touch_move");
    driver->last_touch_at = std::chrono::steady_clock::now();
    return done;
}

bool post_touch_up(AdbCameraSwipeDriver* driver)
{
    if (driver->controller == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostTouchUp(driver->controller, driver->config.contact_id);
    const bool done = wait_for_controller_action(*driver, ctrl_id, "touch_up");
    driver->last_touch_at = std::chrono::steady_clock::now();
    return done;
}

void wait_after_last_touch(const AdbCameraSwipeDriver& driver, int hold_ms)
{
    const int64_t elapsed_ms = ElapsedMs(driver.last_touch_at);
    if (elapsed_ms < hold_ms) {
        SleepFor(hold_ms - static_cast<int>(elapsed_ms));
    }
}

bool move_camera_touch(AdbCameraSwipeDriver* driver, const cv::Point& point)
{
    if (!post_touch_move(driver, point)) {
        const bool released = ReleaseCameraTouch(driver);
        (void)released;
        return false;
    }
    driver->touch_point = point;
    return true;
}

bool press_camera_touch(AdbCameraSwipeDriver* driver, const ScreenGeometry& geometry, int dx)
{
    wait_after_last_touch(*driver, driver->config.touch_down_hold_ms);
    if (!post_touch_down(driver, geometry.center)) {
        return false;
    }
    driver->touch_active = true;
    driver->touch_point = geometry.center;
    SleepFor(driver->config.touch_down_hold_ms);

    const int lead = driver->config.drag_start_lead;
    if (std::abs(dx) >= lead) {
        return true;
    }
    // 比起拖阈值短的转量先越过阈值, 下一帧再拉回
    if (!move_camera_touch(driver, { geometry.center.x + (dx > 0 ? lead : -lead), geometry.center.y })) {
        return false;
    }
    SleepFor(driver->config.touch_down_hold_ms);
    return true;
}

bool turn_with_held_touch(AdbCameraSwipeDriver* driver, const ScreenGeometry& geometry, int dx)
{
    if (driver->touch_active && ElapsedMs(driver->last_touch_at) >= driver->config.held_idle_repress_ms) {
        if (!ReleaseCameraTouch(driver)) {
            return false;
        }
    }

    int remaining_dx = dx;
    while (remaining_dx != 0) {
        const int edge_x = remaining_dx > 0 ? geometry.right_x : geometry.left_x;
        if (driver->touch_active && driver->touch_point.x == edge_x) {
            if (!ReleaseCameraTouch(driver)) {
                return false;
            }
        }
        if (!driver->touch_active) {
            if (!press_camera_touch(driver, geometry, remaining_dx)) {
                return false;
            }
            remaining_dx -= driver->touch_point.x - geometry.center.x;
            continue;
        }
        const int from_x = driver->touch_point.x;
        const int to_x = std::clamp(from_x + remaining_dx, geometry.left_x, geometry.right_x);
        if (!move_camera_touch(driver, { to_x, geometry.center.y })) {
            return false;
        }
        remaining_dx -= to_x - from_x;
    }
    return true;
}

bool execute_stable_drag(AdbCameraSwipeDriver* driver, const ScreenGeometry& geometry, int swipe_dy)
{
    const cv::Point start = geometry.center;
    const cv::Point end = clamp_point({ start.x, start.y + swipe_dy }, geometry.resolution);
    // 比起拖阈值短的笔先越过阈值再拉回终点
    const cv::Point lead = drag_lead_point(start, end, driver->config.drag_start_lead, geometry.resolution);

    if (!post_touch_down(driver, start)) {
        return false;
    }

    SleepFor(driver->config.touch_down_hold_ms);

    const int move_steps = std::max(2, driver->config.move_steps);
    for (int step = 1; step <= move_steps; ++step) {
        const double ratio = static_cast<double>(step - 1) / static_cast<double>(move_steps - 1);
        const int next_x = static_cast<int>(std::lround(lead.x + static_cast<double>(end.x - lead.x) * ratio));
        const int next_y = static_cast<int>(std::lround(lead.y + static_cast<double>(end.y - lead.y) * ratio));

        if (!post_touch_move(driver, { next_x, next_y })) {
            const bool ignored = post_touch_up(driver);
            (void)ignored;
            return false;
        }

        if (step < move_steps) {
            SleepFor(driver->config.move_step_delay_ms);
        }
    }

    SleepFor(driver->config.end_hold_ms);

    if (!post_touch_up(driver)) {
        return false;
    }

    SleepFor(driver->config.post_swipe_settle_ms);
    return true;
}

bool swipe_pitch(AdbCameraSwipeDriver* driver, const ScreenGeometry& geometry, int dy)
{
    if (!ReleaseCameraTouch(driver)) {
        return false;
    }
    wait_after_last_touch(*driver, driver->config.touch_down_hold_ms);

    int remaining_dy = dy;
    do {
        const int swipe_dy = std::clamp(remaining_dy, -geometry.up_limit, geometry.down_limit);
        if (!execute_stable_drag(driver, geometry, swipe_dy)) {
            return false;
        }
        remaining_dy -= swipe_dy;
    } while (remaining_dy != 0);

    return true;
}

} // namespace

bool SwipeCameraByPixels(AdbCameraSwipeDriver* driver, int dx, int dy)
{
    if (driver->controller == nullptr || (dx == 0 && dy == 0)) {
        return dx == 0 && dy == 0;
    }

    const std::optional<ScreenGeometry> geometry = get_screen_geometry(*driver);
    if (!geometry.has_value()) {
        return false;
    }

    if (dx != 0) {
        if (!turn_with_held_touch(driver, *geometry, dx)) {
            return false;
        }
    }
    if (dy == 0) {
        return true;
    }
    return swipe_pitch(driver, *geometry, dy);
}

bool ReleaseCameraTouch(AdbCameraSwipeDriver* driver)
{
    if (!driver->touch_active) {
        return true;
    }

    wait_after_last_touch(*driver, driver->config.end_hold_ms);
    if (!post_touch_up(driver)) {
        return false;
    }
    driver->touch_active = false;
    return true;
}

} // namespace mapnavigator::backend::adb
