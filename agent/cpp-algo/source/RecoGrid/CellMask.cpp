#include "CellMask.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <algorithm>
#include <cmath>
#include <vector>

namespace recogrid
{
namespace
{

int ScaledLength(int base, double ratio)
{
    return std::clamp(static_cast<int>(std::round(static_cast<double>(base) * ratio)), 0, base);
}

} // namespace

std::vector<cv::Rect> IgnoreRects(cv::Size cellSize, const CellMaskRatios& ratios)
{
    std::vector<cv::Rect> rects;
    if (cellSize.width <= 0 || cellSize.height <= 0) {
        return rects;
    }

    const int leftHeaderWidth = ScaledLength(cellSize.width, ratios.leftHeaderWidth);
    const int leftHeaderHeight = ScaledLength(cellSize.height, ratios.leftHeaderHeight);
    const int rightHeaderWidth = ScaledLength(cellSize.width, ratios.rightHeaderWidth);
    const int rightHeaderHeight = ScaledLength(cellSize.height, ratios.rightHeaderHeight);
    const int bottomHeight = ScaledLength(cellSize.height, ratios.bottomHeight);

    if (leftHeaderWidth > 0 && leftHeaderHeight > 0) {
        rects.emplace_back(0, 0, leftHeaderWidth, leftHeaderHeight);
    }
    if (rightHeaderWidth > 0 && rightHeaderHeight > 0) {
        rects.emplace_back(cellSize.width - rightHeaderWidth, 0, rightHeaderWidth, rightHeaderHeight);
    }
    if (bottomHeight > 0) {
        rects.emplace_back(0, cellSize.height - bottomHeight, cellSize.width, bottomHeight);
    }

    return rects;
}

cv::Mat BuildIgnoreMask(cv::Size cellSize, const CellMaskRatios& ratios)
{
    if (cellSize.width <= 0 || cellSize.height <= 0) {
        return {};
    }

    cv::Mat mask(cellSize, CV_8U, cv::Scalar(255));
    for (const auto& rect : IgnoreRects(cellSize, ratios)) {
        cv::rectangle(mask, rect, cv::Scalar(0), cv::FILLED);
    }

    return mask;
}

cv::Mat ApplyIgnoreMask(const cv::Mat& image, const CellMaskRatios& ratios)
{
    if (image.empty()) {
        return image;
    }

    const cv::Mat keepMask = BuildIgnoreMask(image.size(), ratios);
    cv::Mat output = image.clone();
    if (!keepMask.empty()) {
        output.setTo(cv::Scalar::all(255), keepMask == 0);
    }
    return output;
}

cv::Mat ApplyTemplateMask(const cv::Mat& image, const CellMaskRatios& ratios)
{
    if (image.empty()) {
        return image;
    }

    const cv::Mat keepMask = BuildIgnoreMask(image.size(), ratios);
    if (keepMask.empty()) {
        return image;
    }

    if (image.channels() != 4) {
        return ApplyIgnoreMask(image, ratios);
    }

    cv::Mat output = image.clone();
    std::vector<cv::Mat> bgra;
    cv::split(output, bgra);
    bgra[3].setTo(0, keepMask == 0);
    cv::merge(bgra, output);
    return output;
}

} // namespace recogrid
