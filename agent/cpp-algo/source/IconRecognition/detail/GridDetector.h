#pragma once

#include "GridTypes.h"

namespace iconrecognition::detail
{

GridDetection DetectGrid(const cv::Mat& image, GridType type, const cv::Rect& roi, double source_grid_scale = 1.0);
std::optional<double> EstimateGridScale(const cv::Mat& image, GridType type, const cv::Rect& roi);
bool HasFormalCardExtent(const cv::Rect& cell, const cv::Rect& roi, GridType type, double source_grid_scale = 1.0);
bool ShouldDropPortFirstRow(
    int column_count,
    double first_row_support,
    double second_row_support,
    int first_row_y,
    int cell_size);
std::vector<int> CompleteRewardsRowStarts(const std::vector<int>& observed_starts, double pitch);
std::vector<int> RefineShipmentRowStarts(
    const cv::Mat& image,
    const cv::Rect& roi,
    const std::vector<int>& x_starts,
    const std::vector<int>& initial_y_starts,
    int cell_size);

} // namespace iconrecognition::detail
