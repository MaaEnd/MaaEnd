#pragma once

#include <MaaUtils/NoWarningCV.hpp>

#include <vector>

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

std::vector<cv::Rect> IgnoreRects(cv::Size cellSize, const CellMaskRatios& ratios = {});
cv::Mat BuildIgnoreMask(cv::Size cellSize, const CellMaskRatios& ratios = {});
cv::Mat ApplyIgnoreMask(const cv::Mat& image, const CellMaskRatios& ratios = {});
cv::Mat ApplyTemplateMask(const cv::Mat& image, const CellMaskRatios& ratios = {});

} // namespace recogrid
