#include "collectible_scanner.h"

#include <cmath>
#include <utility>

#include <opencv2/imgproc.hpp>

#include <MaaUtils/Logger.h>

#include "navi_config.h"

namespace mapnavigator
{

namespace
{

// Scale the base-resolution collect ROI (1280x720, from the pipeline) to the real frame size, clamped.
cv::Rect ScaledRoi(const cv::Size& frame_size, const cv::Rect& base_roi)
{
    const double sx = static_cast<double>(frame_size.width) / static_cast<double>(kCollectRoiBaseWidth);
    const double sy = static_cast<double>(frame_size.height) / static_cast<double>(kCollectRoiBaseHeight);
    const cv::Rect roi(
        static_cast<int>(std::lround(base_roi.x * sx)),
        static_cast<int>(std::lround(base_roi.y * sy)),
        static_cast<int>(std::lround(base_roi.width * sx)),
        static_cast<int>(std::lround(base_roi.height * sy)));
    return roi & cv::Rect(0, 0, frame_size.width, frame_size.height);
}

// Collapse a BGRA/BGR/gray ROI to one channel, shared by both detectors. Lives here (not utils.h) so this
// framework-free worker TU doesn't pull in the framework headers utils.h includes.
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

// Fallback detector: threshold bright pixels, close horizontally so a word's glyphs merge into one blob, then
// look for a connected component shaped like a short, wide, sparse text run. Input is already grayscale.
bool HasLabelLikeText(const cv::Mat& gray)
{
    if (gray.empty()) {
        return false;
    }

    cv::Mat bright;
    cv::threshold(gray, bright, kCollectLabelBrightThreshold, 255, cv::THRESH_BINARY);

    cv::Mat joined;
    const cv::Mat kernel = cv::getStructuringElement(cv::MORPH_RECT, cv::Size(kCollectLabelMorphWidth, 1));
    cv::morphologyEx(bright, joined, cv::MORPH_CLOSE, kernel);

    cv::Mat labels;
    cv::Mat stats;
    cv::Mat centroids;
    const int n = cv::connectedComponentsWithStats(joined, labels, stats, centroids, 8, CV_32S);

    int best_w = 0;
    for (int i = 1; i < n; ++i) { // 0 is the background component
        const int w = stats.at<int>(i, cv::CC_STAT_WIDTH);
        const int h = stats.at<int>(i, cv::CC_STAT_HEIGHT);
        const int area = stats.at<int>(i, cv::CC_STAT_AREA);
        if (h < kCollectLabelMinHeight || h > kCollectLabelMaxHeight) {
            continue; // too thin to be a glyph row, or too tall to be one
        }
        const double fill = static_cast<double>(area) / (static_cast<double>(w) * static_cast<double>(h));
        if (fill > kCollectLabelMaxFill) {
            continue; // a near-solid block is a panel/icon, not sparse text
        }
        if (w > best_w) {
            best_w = w;
        }
    }

    if (best_w > 0) {
        LogDebug << "CollectibleScanner candidate." << VAR(best_w);
    }
    return best_w >= kCollectLabelMinWidth;
}

// Primary detector: template-match the interact icon (white ⟳). Template and ROI share base scale, so it
// matches at native size (no rescale); a ⟳ is far harder for terrain to fake than a bright blob. Already gray.
bool MatchesIcon(const cv::Mat& gray, const cv::Mat& templ)
{
    if (gray.empty() || templ.empty() || gray.rows < templ.rows || gray.cols < templ.cols) {
        return false;
    }

    cv::Mat result;
    cv::matchTemplate(gray, templ, result, cv::TM_CCOEFF_NORMED);
    double max_val = 0.0;
    cv::minMaxLoc(result, nullptr, &max_val, nullptr, nullptr);
    if (max_val >= 0.5) { // log near matches to calibrate the threshold without flooding
        LogDebug << "CollectibleScanner icon match." << VAR(max_val);
    }
    return max_val >= kCollectIconMatchThreshold;
}

} // namespace

CollectibleScanner::CollectibleScanner(const cv::Rect& base_roi, const cv::Mat& icon_template)
    : base_roi_(base_roi)
    , icon_template_(icon_template)
{
    worker_ = std::thread(&CollectibleScanner::WorkerLoop, this);
}

CollectibleScanner::~CollectibleScanner()
{
    stop_.store(true);
    cv_.notify_all();
    if (worker_.joinable()) {
        worker_.join();
    }
}

void CollectibleScanner::SubmitFrame(const cv::Mat& frame)
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

bool CollectibleScanner::ConsumeDetection()
{
    return detected_.exchange(false);
}

void CollectibleScanner::WorkerLoop()
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

        const cv::Mat gray = ToGray(normalized);
        const bool hit = icon_template_.empty() ? HasLabelLikeText(gray) : MatchesIcon(gray, icon_template_);
        if (hit) {
            detected_.store(true);
        }
    }
}

} // namespace mapnavigator
