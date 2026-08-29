#include "DisabledIcon.h"

#include <algorithm>
#include <stdexcept>

namespace iconrecognition::detail
{
namespace
{

// 游戏禁用态素材对应 128px 原始图标；缩放到目标 cell 时保留素材两侧的透明边缘。
constexpr int kDisabledOverlayReferenceSize = 128;
// OpenCV 与游戏 UI 都以像素中心采样；居中叠加层时，垂直方向需要半像素向上校正。
constexpr double kDisabledOverlayOffsetX = 0.0;
constexpr double kDisabledOverlayOffsetY = -0.5;
// 8 位 alpha 通道最大值，用于将素材透明度归一化到 0..1。
constexpr float kAlphaChannelMaximum = 255.0F;

struct CenteredOverlay
{
    cv::Mat bgr;
    cv::Mat alpha;
    cv::Rect box;
};

CenteredOverlay ResizeCentered(const cv::Mat& source, int target_size, double offset_x, double offset_y)
{
    if (source.empty() || source.type() != CV_8UC4) {
        throw std::invalid_argument("disabled overlay must be a non-empty BGRA image");
    }
    const double scale = static_cast<double>(target_size) / kDisabledOverlayReferenceSize;
    const cv::Size size {
        std::max(1, cvRound(source.cols * scale)),
        std::max(1, cvRound(source.rows * scale)),
    };
    cv::Mat resized;
    cv::resize(source, resized, size, 0.0, 0.0, cv::INTER_AREA);
    if (offset_x != 0.0 || offset_y != 0.0) {
        std::vector<cv::Mat> source_channels;
        cv::split(resized, source_channels);
        cv::Mat bgr;
        cv::merge(std::vector<cv::Mat> { source_channels[0], source_channels[1], source_channels[2] }, bgr);
        cv::Mat shifted_bgr = cv::Mat::zeros(target_size, target_size, CV_8UC3);
        cv::Mat shifted_alpha = cv::Mat::zeros(target_size, target_size, CV_8UC1);
        const cv::Mat transform =
            (cv::Mat_<double>(2, 3) << 1.0,
             0.0,
             (target_size - size.width) / 2.0 + offset_x,
             0.0,
             1.0,
             (target_size - size.height) / 2.0 + offset_y);
        cv::warpAffine(bgr, shifted_bgr, transform, shifted_bgr.size(), cv::INTER_LINEAR, cv::BORDER_CONSTANT);
        cv::warpAffine(source_channels[3], shifted_alpha, transform, shifted_alpha.size(), cv::INTER_LINEAR, cv::BORDER_CONSTANT);
        return { std::move(shifted_bgr), std::move(shifted_alpha), cv::Rect(0, 0, target_size, target_size) };
    }
    std::vector<cv::Mat> channels;
    cv::split(resized, channels);
    cv::Mat bgr;
    cv::merge(std::vector<cv::Mat> { channels[0], channels[1], channels[2] }, bgr);
    return {
        std::move(bgr),
        std::move(channels[3]),
        cv::Rect((target_size - size.width) / 2, (target_size - size.height) / 2, size.width, size.height),
    };
}

void BlendOverlay(cv::Mat& destination, const CenteredOverlay& overlay, const cv::Mat* limit_mask)
{
    cv::Mat target = destination(overlay.box);
    const cv::Mat limited = limit_mask == nullptr ? cv::Mat {} : (*limit_mask)(overlay.box);
    for (int y = 0; y < overlay.box.height; ++y) {
        for (int x = 0; x < overlay.box.width; ++x) {
            if (!limited.empty() && limited.at<unsigned char>(y, x) == 0) {
                continue;
            }
            const float alpha = overlay.alpha.at<unsigned char>(y, x) / kAlphaChannelMaximum;
            if (alpha <= 0.0F) {
                continue;
            }
            const cv::Vec3b source_pixel = overlay.bgr.at<cv::Vec3b>(y, x);
            cv::Vec3b& target_pixel = target.at<cv::Vec3b>(y, x);
            for (int channel = 0; channel < 3; ++channel) {
                target_pixel[channel] = static_cast<unsigned char>(
                    std::clamp(source_pixel[channel] * alpha + target_pixel[channel] * (1.0F - alpha), 0.0F, 255.0F));
            }
        }
    }
}

cv::Mat BuildOverlayAlphaMask(const CenteredOverlay& overlay, cv::Size target_size, int alpha_threshold)
{
    cv::Mat alpha_mask = cv::Mat::zeros(target_size, CV_8UC1);
    cv::threshold(overlay.alpha, alpha_mask(overlay.box), alpha_threshold, 255, cv::THRESH_BINARY);
    return alpha_mask;
}

void ApplyRegionUnavailableBandRowsMask(cv::Mat& mask, const CenteredOverlay& dark_band, int alpha_threshold)
{
    const cv::Mat band_mask = BuildOverlayAlphaMask(dark_band, mask.size(), alpha_threshold);
    if (cv::countNonZero(band_mask) == 0) {
        throw std::invalid_argument("disabled band alpha mask must not be empty");
    }
    const cv::Rect band_box = cv::boundingRect(band_mask);
    // 禁用横条覆盖的底图已不可见；整行排除可同时忽略截图中白标与模板叠加层的细微差异。
    mask.rowRange(band_box.y, band_box.y + band_box.height).setTo(cv::Scalar(0));
}

} // namespace

PreparedTemplate
    BuildRegionUnavailableTemplate(const PreparedTemplate& base, const cv::Mat& dark_band, const cv::Mat& white_mark, int alpha_threshold)
{
    if (base.image.empty() || base.image.type() != CV_8UC3 || base.image.rows != base.image.cols) {
        throw std::invalid_argument("disabled base template must be a non-empty square BGR image");
    }
    if (base.mask.empty() || base.mask.type() != CV_8UC1 || base.mask.size() != base.image.size()) {
        throw std::invalid_argument("disabled base template mask must match the image size");
    }
    if (alpha_threshold < 0 || alpha_threshold > 255) {
        throw std::invalid_argument("disabled overlay alpha threshold must be in 0..255");
    }

    PreparedTemplate result = base;
    result.image = base.image.clone();
    result.mask = base.mask.clone();
    result.region_unavailable = true;

    const CenteredOverlay resized_band = ResizeCentered(dark_band, base.image.cols, kDisabledOverlayOffsetX, kDisabledOverlayOffsetY);
    BlendOverlay(result.image, resized_band, &base.mask);
    ApplyRegionUnavailableBandRowsMask(result.mask, resized_band, alpha_threshold);

    const CenteredOverlay resized_mark = ResizeCentered(white_mark, base.image.cols, kDisabledOverlayOffsetX, kDisabledOverlayOffsetY);
    BlendOverlay(result.image, resized_mark, nullptr);
    return result;
}

} // namespace iconrecognition::detail
