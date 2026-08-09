#include "ForegroundTexture.h"

namespace iconrecognition::detail
{

namespace
{

// 纹理检测区域排除 cell 左侧边框的像素数；调大可避开边框，也会减少有效图标区域。
constexpr int kContentInsetLeft = 6;
// 纹理检测区域排除 cell 顶部边框的像素数；调大可避开顶边装饰，也会减少有效区域。
constexpr int kContentInsetTop = 6;
// 纹理检测区域排除 cell 右侧边框的像素数；调大可避开边框，也会减少有效图标区域。
constexpr int kContentInsetRight = 6;
// 纹理检测区域排除 cell 底部色带的像素数；调大可避开 rarity 色带，但可能裁掉图标下沿。
constexpr int kContentInsetBottom = 8;
// 低纹理门控只适用于 64px 双侧网格 cell，其他尺寸直接跳过该规则。
constexpr int kTextureCellSize = 64;

} // namespace

double LaplacianVariance(const cv::Mat& image, const cv::Rect& region)
{
    if (image.empty()) {
        return 0.0;
    }
    const cv::Rect clipped = region & cv::Rect(0, 0, image.cols, image.rows);
    if (clipped.width < 3 || clipped.height < 3) {
        return 0.0;
    }
    cv::Mat gray;
    if (image.channels() == 4) {
        cv::cvtColor(image(clipped), gray, cv::COLOR_BGRA2GRAY);
    }
    else if (image.channels() == 3) {
        cv::cvtColor(image(clipped), gray, cv::COLOR_BGR2GRAY);
    }
    else {
        gray = image(clipped);
    }
    cv::Mat laplacian;
    gray.convertTo(gray, CV_32F);
    cv::Laplacian(gray, laplacian, CV_32F);
    cv::Scalar mean, stddev;
    cv::meanStdDev(laplacian, mean, stddev);
    return stddev[0] * stddev[0];
}

bool IsLowTexture(const cv::Mat& image, const cv::Rect& region, GridType grid_type, double threshold)
{
    const auto score = ForegroundTextureScore(image, region, grid_type);
    return score && threshold > 0.0 && *score < threshold;
}

std::optional<double> ForegroundTextureScore(const cv::Mat& image, const cv::Rect& region, GridType grid_type)
{
    if (grid_type != GridType::Transfer && grid_type != GridType::PortStorager) {
        return std::nullopt;
    }
    if (region.width != kTextureCellSize || region.height != kTextureCellSize) {
        return std::nullopt;
    }
    const cv::Rect content(
        region.x + kContentInsetLeft,
        region.y + kContentInsetTop,
        region.width - kContentInsetLeft - kContentInsetRight,
        region.height - kContentInsetTop - kContentInsetBottom);
    return LaplacianVariance(image, content);
}

} // namespace iconrecognition::detail
