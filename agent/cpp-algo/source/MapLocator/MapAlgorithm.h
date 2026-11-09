#pragma once

#include "MapTypes.h"
#include <MaaUtils/NoWarningCV.hpp>

namespace maplocator
{

cv::Mat GenerateMinimapMask(const cv::Mat& minimap, const ImageProcessingConfig& cfg, bool withUiMask = true, bool withCenterMask = true);

cv::Mat MinimapRingMask(const cv::Size& size, const ImageProcessingConfig& cfg);

cv::Mat LocalContrastNormalize(const cv::Mat& gray);

double InferYellowArrowRotation(const cv::Mat& minimap);

} // namespace maplocator
