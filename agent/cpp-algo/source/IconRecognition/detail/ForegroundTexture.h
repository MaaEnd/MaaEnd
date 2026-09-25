#pragma once

#include <optional>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

// 沿用低纹理判定的灰度拉普拉斯方差阈值；调高会排除更多空格，也增加低纹理物品被误排的风险。
constexpr double kDefaultLowTextureThreshold = 10.0;

enum class TextureBoundaryMode
{
    IsolatedRegion,
    SourceContext,
};

double LaplacianVariance(
    const cv::Mat& image,
    const cv::Rect& region,
    TextureBoundaryMode boundary_mode = TextureBoundaryMode::IsolatedRegion);
// texture_roi 仅约束 Transfer；先内缩完整格框再求交，证据不足时不提供判空分数。
std::optional<double> ForegroundTextureScore(
    const cv::Mat& image,
    const cv::Rect& region,
    GridType grid_type,
    const std::optional<cv::Rect>& texture_roi = std::nullopt,
    TextureBoundaryMode boundary_mode = TextureBoundaryMode::IsolatedRegion);
// threshold 为拉普拉斯方差下限；调高会拒绝更多低纹理 cell，调低可保留暗淡物品但增加空格误检。
bool IsLowTexture(
    const cv::Mat& image,
    const cv::Rect& region,
    GridType grid_type,
    double threshold = kDefaultLowTextureThreshold,
    const std::optional<cv::Rect>& texture_roi = std::nullopt);

} // namespace iconrecognition::detail
