#include <chrono>

#include <MaaUtils/Logger.h>

#include "../utils.h"
#include "MapNavigator/controller_info_utils.h"
#include "controller_type_utils.h"
#include "navi_math.h"
#include "position_provider.h"

namespace mapnavigator
{

namespace
{

bool IsBlackScreen(const cv::Mat& image)
{
    if (image.empty()) {
        return false;
    }

    cv::Mat gray;
    switch (image.channels()) {
    case 4:
        cv::cvtColor(image, gray, cv::COLOR_BGRA2GRAY);
        break;
    case 3:
        cv::cvtColor(image, gray, cv::COLOR_BGR2GRAY);
        break;
    default:
        gray = image;
        break;
    }

    cv::Scalar mean_luma;
    cv::Scalar stddev_luma;
    cv::meanStdDev(gray, mean_luma, stddev_luma);

    cv::Mat dark_mask;
    cv::threshold(gray, dark_mask, 24, 255, cv::THRESH_BINARY_INV);
    const double dark_ratio = static_cast<double>(cv::countNonZero(dark_mask)) / static_cast<double>(gray.total());

    return mean_luma[0] <= 12.0 && stddev_luma[0] <= 10.0 && dark_ratio >= 0.98;
}

} // namespace

PositionProvider::PositionProvider(MaaController* controller, std::shared_ptr<maplocator::MapLocator> locator)
    : controller_(controller)
    , locator_(std::move(locator))
    , uses_adb_minimap_roi_(IsAdbLikeControllerType(DetectControllerType(controller_)))
{
}

void PositionProvider::SetFrameObserver(std::function<void(const cv::Mat&)> observer)
{
    frame_observer_ = std::move(observer);
}

bool PositionProvider::Capture(NaviPosition* out_pos, bool force_global_search, const std::string& expected_zone_id)
{
    if (out_pos == nullptr) {
        return false;
    }

    last_capture_was_black_screen_ = false;
    const auto capture_started_at = std::chrono::steady_clock::now();

    const MaaCtrlId screencap_id = MaaControllerPostScreencap(controller_);
    MaaControllerWait(controller_, screencap_id);
    ScopedImageBuffer buffer;

    if (!MaaControllerCachedImage(controller_, buffer.Get()) || MaaImageBufferIsEmpty(buffer.Get())) {
        return false;
    }

    cv::Mat image = to_mat(buffer.Get());
    last_capture_was_black_screen_ = IsBlackScreen(image);
    if (frame_observer_) {
        frame_observer_(image);
    }
    cv::Mat minimap;
    if (!maplocator::TryExtractMinimap(image, uses_adb_minimap_roi_, &minimap)) {
        return false;
    }

    maplocator::LocateOptions options;
    options.force_global_search = force_global_search;
    options.expected_zone_id = expected_zone_id;

    const auto locate_result = locator_->locate(minimap, options);
    const int status = static_cast<int>(locate_result.status);
    if (locate_result.position) {
        const auto& position = *locate_result.position;
        LogInfo << "MapLocator" << VAR(status) << VAR(locate_result.debugMessage) << VAR(position.zoneId) << VAR(position.x)
                << VAR(position.y) << VAR(position.score) << VAR(position.sliceIndex) << VAR(position.scale) << VAR(position.angle)
                << VAR(position.latencyMs) << VAR(position.isHeld);
    }
    else {
        LogInfo << "MapLocator" << VAR(status) << VAR(locate_result.debugMessage) << "position=null";
    }
    if (locate_result.status != maplocator::LocateStatus::Success || !locate_result.position) {
        last_capture_was_held_ = false;
        held_fix_streak_ = 0;
        return false;
    }

    out_pos->x = locate_result.position->x;
    out_pos->y = locate_result.position->y;
    out_pos->angle = locate_result.position->angle;
    out_pos->zone_id = locate_result.position->zoneId;
    out_pos->valid = true;
    out_pos->timestamp = capture_started_at;
    last_capture_was_held_ = locate_result.position->isHeld;
    held_fix_streak_ = last_capture_was_held_ ? (held_fix_streak_ + 1) : 0;

    // Single chokepoint: every capture path (semantic nodes, the state machine, WaitForFix) funnels
    // through here, and out_pos is always repopulated from the fresh locate result above before this
    // runs, so the normalizer is applied exactly once per fix — double-transform is impossible.
    if (position_normalizer_) {
        position_normalizer_(*out_pos);
    }
    return true;
}

void PositionProvider::SetPositionNormalizer(std::function<void(NaviPosition&)> normalizer)
{
    position_normalizer_ = std::move(normalizer);
}

bool PositionProvider::WaitForFix(
    NaviPosition* out_pos,
    const std::string& expected_zone_id,
    int max_retries,
    int retry_interval_ms,
    const std::function<bool()>& should_stop)
{
    for (int retry = 0; retry < max_retries; ++retry) {
        if (should_stop()) {
            return false;
        }
        if (Capture(out_pos, !expected_zone_id.empty(), expected_zone_id)) {
            return true;
        }
        utils::SleepFor(retry_interval_ms);
    }
    return false;
}

void PositionProvider::ResetTracking()
{
    locator_->resetTrackingState();
    last_capture_was_held_ = false;
    last_capture_was_black_screen_ = false;
    held_fix_streak_ = 0;
}

bool PositionProvider::LastCaptureWasHeld() const
{
    return last_capture_was_held_;
}

bool PositionProvider::LastCaptureWasBlackScreen() const
{
    return last_capture_was_black_screen_;
}

int PositionProvider::HeldFixStreak() const
{
    return held_fix_streak_;
}

} // namespace mapnavigator
