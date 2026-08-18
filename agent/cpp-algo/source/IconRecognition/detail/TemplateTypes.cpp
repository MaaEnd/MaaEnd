#include "TemplateTypes.h"

#include <algorithm>
#include <cmath>
#include <stdexcept>

#include <MaaUtils/NoWarningCV.hpp>

#include "MaskPolicy.h"

namespace iconrecognition::detail
{
namespace
{

// 复合图标内容层占基础图标边长的比例；调大提高内容可见性，调小减少对底图轮廓的覆盖。
constexpr double kCompositeContentSizeRatio = 7.0 / 16.0;
// 8 位 alpha 通道的最大值，用于把透明度归一化到 0..1。
constexpr double kAlphaChannelMaximum = 255.0;

cv::Mat ToBgr(const cv::Mat& source, cv::Mat& alpha)
{
    if (source.empty()) {
        throw std::invalid_argument("icon template is empty");
    }
    cv::Mat bgr;
    if (source.channels() == 4) {
        cv::cvtColor(source, bgr, cv::COLOR_BGRA2BGR);
        std::vector<cv::Mat> channels;
        cv::split(source, channels);
        alpha = channels[3];
    }
    else if (source.channels() == 3) {
        bgr = source.clone();
        alpha = cv::Mat(source.rows, source.cols, CV_8UC1, cv::Scalar(255));
    }
    else if (source.channels() == 1) {
        cv::cvtColor(source, bgr, cv::COLOR_GRAY2BGR);
        alpha = cv::Mat(source.rows, source.cols, CV_8UC1, cv::Scalar(255));
    }
    else {
        throw std::invalid_argument("icon template has unsupported channel count");
    }
    return bgr;
}

cv::Mat ResizeAndCenterBgr(const cv::Mat& source, int target_size, cv::Mat& alpha)
{
    cv::Mat bgr = ToBgr(source, alpha);
    const double scale = static_cast<double>(target_size) / std::max(bgr.cols, bgr.rows);
    const cv::Size size(std::max(1, cvRound(bgr.cols * scale)), std::max(1, cvRound(bgr.rows * scale)));
    cv::Mat resized_bgr;
    cv::Mat resized_alpha;
    cv::resize(bgr, resized_bgr, size, 0, 0, cv::INTER_AREA);
    cv::resize(alpha, resized_alpha, size, 0, 0, cv::INTER_AREA);
    cv::Mat output = cv::Mat::zeros(target_size, target_size, CV_8UC3);
    cv::Mat output_alpha = cv::Mat::zeros(target_size, target_size, CV_8UC1);
    const int x = (target_size - size.width) / 2;
    const int y = (target_size - size.height) / 2;
    resized_bgr.copyTo(output(cv::Rect(x, y, size.width, size.height)));
    resized_alpha.copyTo(output_alpha(cv::Rect(x, y, size.width, size.height)));
    alpha = output_alpha;
    return output;
}

cv::Mat AlphaMask(const cv::Mat& alpha, int threshold)
{
    cv::Mat mask;
    cv::threshold(alpha, mask, threshold, 255, cv::THRESH_BINARY);
    return mask;
}

} // namespace

cv::Mat DecodeBgra(const std::filesystem::path& path)
{
    const cv::Mat image = cv::imread(path.string(), cv::IMREAD_UNCHANGED);
    if (image.empty()) {
        throw std::runtime_error("unable to decode icon: " + path.string());
    }
    const int edge = image.cols;
    // 发布素材必须保持正方形二次幂尺寸，避免缩放时引入不可控的非等比基准。
    const bool is_power_of_two = edge > 0 && (edge & (edge - 1)) == 0;
    if (image.rows != edge || !is_power_of_two) {
        throw std::runtime_error(
            "icon source must be a square power-of-two image: " + path.string() + " (" + std::to_string(image.cols) + "x"
            + std::to_string(image.rows) + ")");
    }
    return image;
}

cv::Mat ResizeAndCenter(const cv::Mat& source, int target_size)
{
    cv::Mat alpha;
    return ResizeAndCenterBgr(source, target_size, alpha);
}

PreparedTemplate PrepareStandardTemplate(const TemplateRecord& record, const cv::Mat& source, int target_size, int alpha_threshold)
{
    cv::Mat alpha;
    cv::Mat image = ResizeAndCenterBgr(source, target_size, alpha);
    cv::Mat mask;
    cv::bitwise_and(AlphaMask(alpha, alpha_threshold), BuildLowerExtendedMask(target_size), mask);
    if (cv::countNonZero(mask) == 0) {
        throw std::runtime_error("icon template mask is empty: " + record.item_id);
    }
    return PreparedTemplate { record, std::move(image), std::move(mask), false };
}

PreparedTemplate PrepareCompositeTemplate(
    const TemplateRecord& record,
    const cv::Mat& base,
    const cv::Mat& content,
    int target_size,
    int alpha_threshold)
{
    cv::Mat base_alpha;
    cv::Mat output = ResizeAndCenterBgr(base, target_size, base_alpha);
    cv::Mat base_mask;
    cv::bitwise_and(AlphaMask(base_alpha, alpha_threshold), BuildLowerExtendedMask(target_size), base_mask);

    const int content_size = std::max(1, cvRound(target_size * kCompositeContentSizeRatio));
    cv::Mat content_alpha;
    cv::Mat content_bgr = ResizeAndCenterBgr(content, content_size, content_alpha);
    if (cv::countNonZero(AlphaMask(content_alpha, alpha_threshold)) == 0) {
        throw std::runtime_error("composite content mask is empty: " + record.item_id);
    }
    const int offset = (target_size - content_size) / 2;
    cv::Mat destination = output(cv::Rect(offset, offset, content_size, content_size));
    for (int y = 0; y < content_size; ++y) {
        for (int x = 0; x < content_size; ++x) {
            const float alpha = static_cast<float>(content_alpha.at<uchar>(y, x) / kAlphaChannelMaximum);
            if (alpha <= 0.0F) {
                continue;
            }
            const cv::Vec3b source_pixel = content_bgr.at<cv::Vec3b>(y, x);
            cv::Vec3b& target_pixel = destination.at<cv::Vec3b>(y, x);
            for (int channel = 0; channel < 3; ++channel) {
                target_pixel[channel] =
                    static_cast<uchar>(std::clamp(source_pixel[channel] * alpha + target_pixel[channel] * (1.0F - alpha), 0.0F, 255.0F));
            }
        }
    }
    cv::Mat mask = base_mask.clone();
    cv::Mat content_mask = AlphaMask(content_alpha, alpha_threshold);
    cv::bitwise_or(
        mask(cv::Rect(offset, offset, content_size, content_size)),
        content_mask,
        mask(cv::Rect(offset, offset, content_size, content_size)));
    if (cv::countNonZero(mask) == 0) {
        throw std::runtime_error("composite mask is empty: " + record.item_id);
    }
    return PreparedTemplate { record, std::move(output), std::move(mask), true };
}

} // namespace iconrecognition::detail
