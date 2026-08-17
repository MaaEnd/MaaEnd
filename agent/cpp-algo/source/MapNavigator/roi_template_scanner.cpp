#include "roi_template_scanner.h"

#include <cmath>
#include <limits>
#include <utility>

#include <opencv2/imgproc.hpp>

#include <MaaUtils/Logger.h>

#include "navi_config.h"

namespace mapnavigator
{

namespace
{

// Scale a base-resolution ROI (1280x720, from the pipeline) to the real frame size, clamped.
cv::Rect ScaledRoi(const cv::Size& frame_size, const cv::Rect& base_roi)
{
    const double sx = static_cast<double>(frame_size.width) / static_cast<double>(kPipelineRoiBaseWidth);
    const double sy = static_cast<double>(frame_size.height) / static_cast<double>(kPipelineRoiBaseHeight);
    const cv::Rect roi(
        static_cast<int>(std::lround(base_roi.x * sx)),
        static_cast<int>(std::lround(base_roi.y * sy)),
        static_cast<int>(std::lround(base_roi.width * sx)),
        static_cast<int>(std::lround(base_roi.height * sy)));
    return roi & cv::Rect(0, 0, frame_size.width, frame_size.height);
}

// Collapse a BGRA/BGR/gray ROI to one channel. Lives here (not utils.h) so this framework-free worker TU
// doesn't pull in the framework headers utils.h includes.
cv::Mat ToGray(const cv::Mat& roi)
{
    cv::Mat gray;
    switch (roi.channels()) {
    case 4:
        cv::cvtColor(roi, gray, cv::COLOR_BGRA2GRAY);
        break;
    case 3:
        cv::cvtColor(roi, gray, cv::COLOR_BGR2GRAY);
        break;
    default:
        gray = roi;
        break;
    }
    return gray;
}

// Template and ROI share base scale, so it matches at native size (no rescale). Already gray.
bool MatchesTemplate(const cv::Mat& gray, const cv::Mat& templ, const cv::Mat& mask, double threshold, const std::string& tag)
{
    if (gray.empty() || templ.empty() || gray.rows < templ.rows || gray.cols < templ.cols) {
        return false;
    }

    cv::Mat result;
    cv::matchTemplate(gray, templ, result, cv::TM_CCOEFF_NORMED, mask);

    // Masked normalized correlation has no divide-by-zero guard: a flat window yields inf/nan, and inf would top the
    // score. With every cell rejected minMaxLoc hands back 0, which is below any threshold.
    cv::Mat finite;
    if (!mask.empty()) {
        constexpr double kFloatLimit = static_cast<double>(std::numeric_limits<float>::max());
        cv::inRange(result, -kFloatLimit, kFloatLimit, finite);
    }
    double max_val = 0.0;
    cv::minMaxLoc(result, nullptr, &max_val, nullptr, nullptr, finite);
    if (max_val >= 0.5) { // log near matches to calibrate the threshold without flooding
        LogDebug << "RoiTemplateScanner template match." << VAR(tag) << VAR(max_val);
    }
    return max_val >= threshold;
}

} // namespace

RoiTemplateScanner::RoiTemplateScanner(
    std::string tag,
    const cv::Rect& base_roi,
    const cv::Mat& templ,
    const cv::Mat& mask,
    double match_threshold)
    : tag_(std::move(tag))
    , base_roi_(base_roi)
    , template_(templ)
    , mask_(mask)
    , match_threshold_(match_threshold)
{
    worker_ = std::thread(&RoiTemplateScanner::WorkerLoop, this);
}

RoiTemplateScanner::~RoiTemplateScanner()
{
    stop_.store(true);
    cv_.notify_all();
    if (worker_.joinable()) {
        worker_.join();
    }
}

void RoiTemplateScanner::SubmitFrame(const cv::Mat& frame)
{
    if (frame.empty()) {
        return;
    }
    const cv::Rect roi = ScaledRoi(frame.size(), base_roi_);
    if (roi.width <= 0 || roi.height <= 0) {
        return;
    }

    {
        std::lock_guard<std::mutex> lock(mutex_);
        frame(roi).copyTo(pending_roi_); // deep copy: the controller reuses the frame buffer after we return
        has_pending_ = true;
    }
    cv_.notify_one();
}

bool RoiTemplateScanner::ConsumeDetection()
{
    return detected_.exchange(false);
}

void RoiTemplateScanner::WorkerLoop()
{
    for (;;) {
        cv::Mat roi;
        {
            std::unique_lock<std::mutex> lock(mutex_);
            cv_.wait(lock, [this] { return has_pending_ || stop_.load(); });
            if (stop_.load()) {
                return;
            }
            roi = std::move(pending_roi_);
            has_pending_ = false;
        }

        // Normalize the crop back to the authored ROI size so detection runs in one fixed pixel space.
        // INTER_AREA for the downscale.
        cv::Mat normalized;
        if (roi.size() == base_roi_.size()) {
            normalized = roi;
        }
        else {
            cv::resize(roi, normalized, base_roi_.size(), 0, 0, cv::INTER_AREA);
        }

        if (MatchesTemplate(ToGray(normalized), template_, mask_, match_threshold_, tag_)) {
            detected_.store(true);
        }
    }
}

} // namespace mapnavigator
