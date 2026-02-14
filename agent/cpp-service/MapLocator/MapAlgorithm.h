#pragma once

#include <opencv2/opencv.hpp>
#include <vector>

namespace maplocator {

cv::Mat GenerateMinimapMask(const cv::Mat &minimap);

} // namespace maplocator
