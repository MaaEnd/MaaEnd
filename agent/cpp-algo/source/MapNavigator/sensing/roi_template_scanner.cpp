#include "roi_template_scanner.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <utility>

#include <opencv2/imgproc.hpp>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"

namespace mapnavigator
{

namespace
{

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

// 本文件不能引入 utils.h 带来的框架头文件, 所以自带一份
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

// 模板与 ROI 同为基础尺度, gray 须已是灰度图
bool MatchesTemplate(const cv::Mat& gray, const cv::Mat& templ, const cv::Mat& mask, double threshold, const std::string& tag)
{
    if (gray.empty() || templ.empty() || gray.rows < templ.rows || gray.cols < templ.cols) {
        return false;
    }

    cv::Mat result;
    cv::matchTemplate(gray, templ, result, cv::TM_CCOEFF_NORMED, mask);

    // 带掩码的归一化相关在平坦窗口上会出 inf/nan, 须先剔除非有限值
    cv::Mat finite;
    if (!mask.empty()) {
        constexpr double kFloatLimit = static_cast<double>(std::numeric_limits<float>::max());
        cv::inRange(result, -kFloatLimit, kFloatLimit, finite);
    }
    double max_val = 0.0;
    cv::minMaxLoc(result, nullptr, &max_val, nullptr, nullptr, finite);
    if (max_val >= 0.5) {
        LogDebug << "RoiTemplateScanner template match." << VAR(tag) << VAR(max_val);
    }
    return max_val >= threshold;
}

bool MatchNormalized(const cv::Mat& crop, const cv::Size& base_size, const std::vector<ScanIcon>& icons, const std::string& tag)
{
    cv::Mat normalized;
    if (crop.size() == base_size) {
        normalized = crop;
    }
    else {
        cv::resize(crop, normalized, base_size, 0, 0, cv::INTER_AREA);
    }
    const cv::Mat gray = ToGray(normalized);
    return std::ranges::any_of(icons, [&](const ScanIcon& icon) {
        return MatchesTemplate(gray, icon.templ, icon.mask, icon.threshold, tag);
    });
}

} // namespace

bool MatchTemplateOnFrame(const cv::Mat& frame, const cv::Rect& base_roi, const std::vector<ScanIcon>& icons, std::string_view tag)
{
    if (frame.empty() || icons.empty()) {
        return false;
    }
    const cv::Rect roi = ScaledRoi(frame.size(), base_roi);
    if (roi.width <= 0 || roi.height <= 0) {
        return false;
    }
    return MatchNormalized(frame(roi), base_roi.size(), icons, std::string(tag));
}

RoiTemplateScanner::RoiTemplateScanner(std::string tag, const cv::Rect& base_roi, std::vector<ScanIcon> icons)
    : tag_(std::move(tag))
    , base_roi_(base_roi)
    , icons_(std::move(icons))
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
        frame(roi).copyTo(pending_roi_); // 须深拷贝, 返回后控制器会复用这块帧缓冲
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

        if (MatchNormalized(roi, base_roi_.size(), icons_, tag_)) {
            detected_.store(true);
        }
    }
}

} // namespace mapnavigator
