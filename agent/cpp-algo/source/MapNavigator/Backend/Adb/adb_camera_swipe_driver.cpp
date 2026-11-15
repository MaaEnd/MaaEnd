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

bool post_touch_down(const AdbCameraSwipeDriver& driver, const cv::Point& point)
{
    if (driver.controller == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id =
        MaaControllerPostTouchDown(driver.controller, driver.config.contact_id, point.x, point.y, driver.config.pressure);
    return wait_for_controller_action(driver, ctrl_id, "touch_down");
}

bool post_touch_move(const AdbCameraSwipeDriver& driver, const cv::Point& point)
{
    if (driver.controller == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id =
        MaaControllerPostTouchMove(driver.controller, driver.config.contact_id, point.x, point.y, driver.config.pressure);
    return wait_for_controller_action(driver, ctrl_id, "touch_move");
}

bool post_touch_up(const AdbCameraSwipeDriver& driver)
{
    if (driver.controller == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostTouchUp(driver.controller, driver.config.contact_id);
    return wait_for_controller_action(driver, ctrl_id, "touch_up");
}

bool execute_stable_drag(const AdbCameraSwipeDriver& driver, const ScreenGeometry& geometry, int swipe_dx, int swipe_dy)
{
    // 以中心对称起落, 被当成点击时也碰不到两侧的任务追踪和按钮
    const cv::Point start = clamp_point({ geometry.center.x - swipe_dx / 2, geometry.center.y }, geometry.resolution);
    const cv::Point end = clamp_point({ start.x + swipe_dx, geometry.center.y + swipe_dy }, geometry.resolution);
    // 比起拖阈值短的笔先越过阈值再拉回终点
    const cv::Point lead = drag_lead_point(start, end, driver.config.drag_start_lead, geometry.resolution);

    if (!post_touch_down(driver, start)) {
        return false;
    }

    SleepFor(driver.config.touch_down_hold_ms);

    const int move_steps = std::max(2, driver.config.move_steps);
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
            SleepFor(driver.config.move_step_delay_ms);
        }
    }

    SleepFor(driver.config.end_hold_ms);

    if (!post_touch_up(driver)) {
        return false;
    }

    SleepFor(driver.config.post_swipe_settle_ms);
    return true;
}

} // namespace

bool SwipeCameraByPixels(const AdbCameraSwipeDriver& driver, int dx, int dy)
{
    if (driver.controller == nullptr || (dx == 0 && dy == 0)) {
        return dx == 0 && dy == 0;
    }

    const std::optional<ScreenGeometry> geometry = get_screen_geometry(driver);
    if (!geometry.has_value()) {
        return false;
    }

    const int stroke_dx_limit = std::max(1, driver.config.max_stroke_dx);
    int remaining_dx = dx;
    int remaining_dy = dy;
    do {
        const int swipe_dx = std::clamp(remaining_dx, -stroke_dx_limit, stroke_dx_limit);
        const int swipe_dy = std::clamp(remaining_dy, -geometry->up_limit, geometry->down_limit);
        if (!execute_stable_drag(driver, *geometry, swipe_dx, swipe_dy)) {
            return false;
        }
        remaining_dx -= swipe_dx;
        remaining_dy -= swipe_dy;
    } while (remaining_dx != 0 || remaining_dy != 0);

    return true;
}

} // namespace mapnavigator::backend::adb
