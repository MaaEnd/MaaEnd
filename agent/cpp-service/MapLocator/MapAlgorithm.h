#pragma once

#include <opencv2/opencv.hpp>
#include <vector>
#include "MapTypes.h"

namespace maplocator {

cv::Mat GenerateMinimapMask(const cv::Mat &minimap, const ImageProcessingConfig &cfg);

} // namespace maplocator
