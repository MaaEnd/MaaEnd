#pragma once

#include <MaaUtils/NoWarningCV.hpp>

namespace recogrid
{

struct CellMaskRatios
{
    double leftHeaderWidth = 0.0;
    double leftHeaderHeight = 0.0;
    double rightHeaderWidth = 0.0;
    double rightHeaderHeight = 0.0;
    double bottomHeight = 0.0;
};

cv::Mat BuildIgnoreMask(cv::Size cellSize, const CellMaskRatios& ratios = {});
cv::Mat ApplyIgnoreMask(const cv::Mat& image, const CellMaskRatios& ratios = {});

} // namespace recogrid
