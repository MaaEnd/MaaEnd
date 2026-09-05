#include "ForegroundTexture.h"

namespace iconrecognition::detail
{

namespace
{

// Transfer 在输入截图上四边各裁 4px；Win32/ADB 空格扫描用于标定，减小可保留更多图标，也更易混入边框和面板 UI。
constexpr int kTransferContentInset = 4;
// 判空必须观察到内缩内容区的至少一半面积，避免窄小残片代表整个格子；提高会让更多残格退回模板匹配。
constexpr double kTransferMinimumTextureCoverage = 0.5;
// 纹理检测区域排除 cell 左侧边框的像素数；调大可避开边框，也会减少有效图标区域。
constexpr int kContentInsetLeft = 6;
// 纹理检测区域排除 cell 顶部边框的像素数；调大可避开顶边装饰，也会减少有效区域。
constexpr int kContentInsetTop = 6;
// 纹理检测区域排除 cell 右侧边框的像素数；调大可避开边框，也会减少有效图标区域。
constexpr int kContentInsetRight = 6;
// 纹理检测区域排除 cell 底部色带的像素数；调大可避开 rarity 色带，但可能裁掉图标下沿。
constexpr int kContentInsetBottom = 8;
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

bool IsLowTexture(
    const cv::Mat& image,
    const cv::Rect& region,
    GridType grid_type,
    double threshold,
    const std::optional<cv::Rect>& texture_roi)
{
    const auto score = ForegroundTextureScore(image, region, grid_type, texture_roi);
    return score && threshold > 0.0 && *score < threshold;
}

std::optional<double>
    ForegroundTextureScore(const cv::Mat& image, const cv::Rect& region, GridType grid_type, const std::optional<cv::Rect>& texture_roi)
{
    if (grid_type != GridType::Transfer && grid_type != GridType::PortStorager) {
        return std::nullopt;
    }
    const bool transfer = grid_type == GridType::Transfer;
    const int left = transfer ? kTransferContentInset : kContentInsetLeft;
    const int top = transfer ? kTransferContentInset : kContentInsetTop;
    const int right = transfer ? kTransferContentInset : kContentInsetRight;
    const int bottom = transfer ? kTransferContentInset : kContentInsetBottom;
    const cv::Rect content(region.x + left, region.y + top, region.width - left - right, region.height - top - bottom);
    if (transfer && texture_roi) {
        const cv::Rect visible = content & *texture_roi & cv::Rect(0, 0, image.cols, image.rows);
        if (content.empty() || visible.width < 3 || visible.height < 3
            || visible.area() < content.area() * kTransferMinimumTextureCoverage) {
            return std::nullopt;
        }
        return LaplacianVariance(image, visible);
    }
    return LaplacianVariance(image, content);
}

} // namespace iconrecognition::detail
