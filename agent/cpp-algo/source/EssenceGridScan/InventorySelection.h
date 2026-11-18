#pragma once

#include "../GridTracker/GridTracker.h"

#include <optional>
#include <span>

namespace essencegridscan
{

bool ConfirmInventorySelection(
    const cv::Mat& before,
    const cv::Mat& after,
    std::span<const gridtracker::Cell> visible,
    const cv::Rect& target,
    const cv::Rect& preview,
    std::optional<cv::Rect> previous_selection,
    bool single_item);

} // namespace essencegridscan
