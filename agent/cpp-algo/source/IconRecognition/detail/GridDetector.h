#pragma once

#include "GridTypes.h"

namespace iconrecognition::detail
{

GridDetection DetectGrid(
    const cv::Mat& image,
    GridType type,
    const cv::Rect& roi,
    std::optional<double> grid_scale_hint = std::nullopt);
std::optional<double> EstimateGridScale(const cv::Mat& image, GridType type, const cv::Rect& roi);
bool HasFormalCardExtent(const cv::Rect& cell, const cv::Rect& roi, GridType type, double source_grid_scale = 1.0);
bool ShouldDropPortFirstRow(int column_count, double first_row_support, double second_row_support, int first_row_y, int cell_size);
std::vector<int> CompleteRewardsRowStarts(const std::vector<int>& observed_starts, double pitch);

} // namespace iconrecognition::detail
