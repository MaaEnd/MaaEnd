#pragma once

#include "MapTypes.h"
#include <MaaUtils/NoWarningCV.hpp>

namespace maplocator
{

cv::Mat GenerateMinimapMask(const cv::Mat& minimap, const ImageProcessingConfig& cfg, bool withUiMask = true, bool withCenterMask = true);

double InferYellowArrowRotation(const cv::Mat& minimap);

} // namespace maplocator
