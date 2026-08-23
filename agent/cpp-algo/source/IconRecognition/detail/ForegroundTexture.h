#pragma once

#include <optional>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

double LaplacianVariance(const cv::Mat& image, const cv::Rect& region);
std::optional<double> ForegroundTextureScore(const cv::Mat& image, const cv::Rect& region, GridType grid_type);
// threshold 为拉普拉斯方差下限；调高会拒绝更多低纹理 cell，调低可保留暗淡物品但增加空格误检。
bool IsLowTexture(const cv::Mat& image, const cv::Rect& region, GridType grid_type, double threshold = 10.0);

} // namespace iconrecognition::detail
