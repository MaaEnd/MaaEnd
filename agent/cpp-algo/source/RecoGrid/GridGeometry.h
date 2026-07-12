#pragma once

#include "GridDetector.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <algorithm>
#include <cmath>

namespace recogrid
{

inline cv::Rect ClampRect(const cv::Rect& rect, const cv::Size& bounds)
{
    return rect & cv::Rect(0, 0, bounds.width, bounds.height);
}

inline cv::Rect OffsetRect(const cv::Rect& rect, const cv::Point& offset)
{
    if (rect.empty()) {
        return {};
    }
    return { rect.x + offset.x, rect.y + offset.y, rect.width, rect.height };
}

inline cv::Rect ScaleRect(const cv::Rect& rect, cv::Size fromSize, cv::Size toSize)
{
    if (rect.empty() || fromSize.width <= 0 || fromSize.height <= 0 || toSize.width <= 0 || toSize.height <= 0) {
        return {};
    }

    const double scaleX = static_cast<double>(toSize.width) / static_cast<double>(fromSize.width);
    const double scaleY = static_cast<double>(toSize.height) / static_cast<double>(fromSize.height);

    const int x = static_cast<int>(std::lround(static_cast<double>(rect.x) * scaleX));
    const int y = static_cast<int>(std::lround(static_cast<double>(rect.y) * scaleY));
    const int right = static_cast<int>(std::lround(static_cast<double>(rect.x + rect.width) * scaleX));
    const int bottom = static_cast<int>(std::lround(static_cast<double>(rect.y + rect.height) * scaleY));

    return ClampRect({ x, y, std::max(1, right - x), std::max(1, bottom - y) }, toSize);
}

inline cv::Rect RoiToScreen(const cv::Rect& rect, const GridDetectOptions& options, cv::Size imageSize)
{
    return ScaleRect(OffsetRect(rect, { options.roi.x, options.roi.y }), options.normalizedSize, imageSize);
}

} // namespace recogrid
