#pragma once

#include "GridTypes.h"

namespace iconrecognition::detail
{

GridDetection DetectGrid(const cv::Mat& image, GridType type, const cv::Rect& roi);

} // namespace iconrecognition::detail
