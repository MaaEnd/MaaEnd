#include <chrono>
#include <thread>

#include "../utils.h"
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

class ScopedImageBuffer
{
public:
    ScopedImageBuffer()
        : buffer_(MaaImageBufferCreate())
    {
    }

    ~ScopedImageBuffer() { MaaImageBufferDestroy(buffer_); }

    ScopedImageBuffer(const ScopedImageBuffer&) = delete;
    ScopedImageBuffer& operator=(const ScopedImageBuffer&) = delete;

    MaaImageBuffer* Get() const { return buffer_; }

private:
    MaaImageBuffer* buffer_;
};

} // namespace

PositionProvider::PositionProvider(MaaController* controller, std::shared_ptr<maplocator::MapLocator> locator)
    : controller_(controller)
    , locator_(std::move(locator))
{
}

bool PositionProvider::Capture(NaviPosition* out_pos, bool force_global_search, const std::string& expected_zone_id)
{
    if (out_pos == nullptr) {
        return false;
    }

    last_capture_was_black_screen_ = false;

    const MaaCtrlId screencap_id = MaaControllerPostScreencap(controller_);
    MaaControllerWait(controller_, screencap_id);
    ScopedImageBuffer buffer;

    if (!MaaControllerCachedImage(controller_, buffer.Get()) || MaaImageBufferIsEmpty(buffer.Get())) {
        return false;
    }

    cv::Mat image = to_mat(buffer.Get());
    last_capture_was_black_screen_ = IsBlackScreen(image);
    cv::Rect roi(maplocator::MinimapROIOriginX, maplocator::MinimapROIOriginY, maplocator::MinimapROIWidth, maplocator::MinimapROIHeight);
    roi = roi & cv::Rect(0, 0, image.cols, image.rows);
    if (roi.empty()) {
        return false;
    }

    maplocator::LocateOptions options;
    options.force_global_search = force_global_search;
    options.expected_zone_id = expected_zone_id;

    const auto locate_result = locator_->locate(image(roi), options);
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
    out_pos->timestamp = std::chrono::steady_clock::now();
    last_capture_was_held_ = locate_result.position->isHeld;
    held_fix_streak_ = last_capture_was_held_ ? (held_fix_streak_ + 1) : 0;
    return true;
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
        std::this_thread::sleep_for(std::chrono::milliseconds(retry_interval_ms));
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
