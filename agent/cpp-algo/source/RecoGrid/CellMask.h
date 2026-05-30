#pragma once

#include <opencv2/core.hpp>

#include <vector>

namespace recogrid
{

struct CellMaskRatios
{
    double leftHeaderWidth = 20.0 / 96.0;
    double leftHeaderHeight = 20.0 / 96.0;
    double rightHeaderWidth = 30.0 / 96.0;
    double rightHeaderHeight = 30.0 / 96.0;
    double bottomHeight = 20.0 / 96.0;
};

std::vector<cv::Rect> IgnoreRects(cv::Size cellSize, const CellMaskRatios& ratios = {});
cv::Mat BuildIgnoreMask(cv::Size cellSize, const CellMaskRatios& ratios = {});
cv::Mat ApplyIgnoreMask(const cv::Mat& image, const CellMaskRatios& ratios = {});
cv::Mat ApplyTemplateMask(const cv::Mat& image, const CellMaskRatios& ratios = {});

} // namespace recogrid
