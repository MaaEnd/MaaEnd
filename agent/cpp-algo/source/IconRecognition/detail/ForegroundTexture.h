#pragma once

#include <optional>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

double LaplacianVariance(const cv::Mat& image, const cv::Rect& region);
std::optional<double> ForegroundTextureScore(const cv::Mat& image, const cv::Rect& region, GridType grid_type);
bool IsLowTexture(const cv::Mat& image, const cv::Rect& region, GridType grid_type, double threshold = 10.0);

} // namespace iconrecognition::detail
