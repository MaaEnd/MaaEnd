#include <algorithm>
#include <chrono>
#include <cmath>
#include <thread>
#include <utility>

#include <MaaUtils/Logger.h>

#include "adb_camera_swipe_driver.h"

namespace mapnavigator::backend::adb
{

namespace
{

constexpr int kAdbTouchReferenceWidth = 1280;
constexpr int kAdbTouchReferenceHeight = 720;

cv::Point ClampPoint(const cv::Point& point, const cv::Size& resolution)
{
    return {
        std::clamp(point.x, 0, std::max(0, resolution.width - 1)),
        std::clamp(point.y, 0, std::max(0, resolution.height - 1)),
    };
}

cv::Point DragLeadPoint(const cv::Point& start, const cv::Point& end, int lead, const cv::Size& resolution)
{
    const double length = std::hypot(static_cast<double>(end.x - start.x), static_cast<double>(end.y - start.y));
    if (length <= 0.0) {
        return end;
    }
    const double scale = static_cast<double>(lead) / length;
    return ClampPoint(
        { start.x + static_cast<int>(std::lround((end.x - start.x) * scale)),
          start.y + static_cast<int>(std::lround((end.y - start.y) * scale)) },
        resolution);
}

} // namespace

AdbCameraSwipeDriver::AdbCameraSwipeDriver(MaaController* controller, AdbCameraSwipeDriverConfig config)
    : controller_(controller)
    , config_(std::move(config))
{
}

bool AdbCameraSwipeDriver::SwipeByPixels(int dx, int dy) const
{
    if (controller_ == nullptr || (dx == 0 && dy == 0)) {
        return dx == 0 && dy == 0;
    }

    const std::optional<ScreenGeometry> geometry = GetScreenGeometry();
    if (!geometry.has_value()) {
        return false;
    }

    const int stroke_dx_limit = std::max(1, config_.max_stroke_dx);
    int remaining_dx = dx;
    int remaining_dy = dy;
    do {
        const int swipe_dx = std::clamp(remaining_dx, -stroke_dx_limit, stroke_dx_limit);
        const int swipe_dy = std::clamp(remaining_dy, -geometry->up_limit, geometry->down_limit);
        if (!ExecuteSwipe(*geometry, swipe_dx, swipe_dy)) {
            return false;
        }
        remaining_dx -= swipe_dx;
        remaining_dy -= swipe_dy;
    } while (remaining_dx != 0 || remaining_dy != 0);

    return true;
}

std::optional<AdbCameraSwipeDriver::ScreenGeometry> AdbCameraSwipeDriver::GetScreenGeometry() const
{
    if (controller_ == nullptr) {
        return std::nullopt;
    }

    const int safe_margin = std::max(0, config_.edge_margin);
    const int x_denominator = std::max(1, config_.origin_x_denominator);
    const int y_denominator = std::max(1, config_.origin_y_denominator);
    const int origin_x = std::clamp(kAdbTouchReferenceWidth * config_.origin_x_numerator / x_denominator, 0, kAdbTouchReferenceWidth - 1);
    const int origin_y = std::clamp(
        kAdbTouchReferenceHeight * config_.origin_y_numerator / y_denominator + config_.origin_y_offset,
        0,
        kAdbTouchReferenceHeight - 1);
    const cv::Point center(origin_x, origin_y);

    ScreenGeometry geometry;
    geometry.resolution = { kAdbTouchReferenceWidth, kAdbTouchReferenceHeight };
    geometry.center = center;
    geometry.up_limit = std::max(1, center.y - safe_margin);
    geometry.down_limit = std::max(1, (kAdbTouchReferenceHeight - 1 - safe_margin) - center.y);
    return geometry;
}

bool AdbCameraSwipeDriver::ExecuteSwipe(const ScreenGeometry& geometry, int swipe_dx, int swipe_dy) const
{
    return ExecuteStableDrag(geometry, swipe_dx, swipe_dy);
}

bool AdbCameraSwipeDriver::ExecuteStableDrag(const ScreenGeometry& geometry, int swipe_dx, int swipe_dy) const
{
    // 横向以中心对称起落: 被当成点击时落点离屏幕中心最近, 碰不到两侧的任务追踪和按钮
    const cv::Point start = ClampPoint({ geometry.center.x - swipe_dx / 2, geometry.center.y }, geometry.resolution);
    const cv::Point end = ClampPoint({ start.x + swipe_dx, geometry.center.y + swipe_dy }, geometry.resolution);
    // 第一步越过起拖阈值, 之后的位移全额计入; 比阈值短的笔越过去再拉回终点
    const cv::Point lead = DragLeadPoint(start, end, config_.drag_start_lead, geometry.resolution);

    if (!PostTouchDown(start)) {
        return false;
    }

    SleepIfNeeded(config_.touch_down_hold_ms);

    const int move_steps = std::max(2, config_.move_steps);
    for (int step = 1; step <= move_steps; ++step) {
        const double ratio = static_cast<double>(step - 1) / static_cast<double>(move_steps - 1);
        const int next_x = static_cast<int>(std::lround(lead.x + static_cast<double>(end.x - lead.x) * ratio));
        const int next_y = static_cast<int>(std::lround(lead.y + static_cast<double>(end.y - lead.y) * ratio));

        if (!PostTouchMove({ next_x, next_y })) {
            const bool ignored = PostTouchUp();
            (void)ignored;
            return false;
        }

        if (step < move_steps) {
            SleepIfNeeded(config_.move_step_delay_ms);
        }
    }

    SleepIfNeeded(config_.end_hold_ms);

    if (!PostTouchUp()) {
        return false;
    }

    SleepIfNeeded(config_.post_swipe_settle_ms);
    return true;
}

bool AdbCameraSwipeDriver::PostTouchDown(const cv::Point& point) const
{
    if (controller_ == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostTouchDown(controller_, config_.contact_id, point.x, point.y, config_.pressure);
    return WaitForControllerAction(ctrl_id, "touch_down");
}

bool AdbCameraSwipeDriver::PostTouchMove(const cv::Point& point) const
{
    if (controller_ == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostTouchMove(controller_, config_.contact_id, point.x, point.y, config_.pressure);
    return WaitForControllerAction(ctrl_id, "touch_move");
}

bool AdbCameraSwipeDriver::PostTouchUp() const
{
    if (controller_ == nullptr) {
        return false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostTouchUp(controller_, config_.contact_id);
    return WaitForControllerAction(ctrl_id, "touch_up");
}

bool AdbCameraSwipeDriver::EnsureControllerActionPosted(MaaCtrlId ctrl_id, const char* action_name) const
{
    if (ctrl_id != MaaInvalidId) {
        return true;
    }

    LogWarn << "AdbCameraSwipeDriver: failed to post controller action." << VAR(action_name);
    return false;
}

bool AdbCameraSwipeDriver::WaitForControllerAction(MaaCtrlId ctrl_id, const char* action_name) const
{
    if (!EnsureControllerActionPosted(ctrl_id, action_name)) {
        return false;
    }

    const MaaStatus status = MaaControllerWait(controller_, ctrl_id);
    if (status == MaaStatus_Succeeded) {
        return true;
    }

    LogWarn << "AdbCameraSwipeDriver: controller action did not succeed." << VAR(action_name) << VAR(ctrl_id) << VAR(status);
    return false;
}

void AdbCameraSwipeDriver::SleepIfNeeded(int delay_millis)
{
    if (delay_millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(delay_millis));
}

} // namespace mapnavigator::backend::adb
