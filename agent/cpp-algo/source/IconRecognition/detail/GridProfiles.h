#pragma once

#include "GridTypes.h"

namespace iconrecognition::detail
{

struct GridProfile
{
    int cell_size = 64;
    double pitch_x = 64.0;
    double pitch_y = 64.0;
    int min_columns = 1;
    int min_rows = 1;
};

struct TransferGridHint
{
    cv::Rect region;
    cv::Rect rect;
    double score = 0.0;
    double occupancy = 0.0;
    std::vector<int> x_starts;
    std::vector<int> y_starts;
};

GridProfile ProfileFor(GridType type);
cv::Mat BuildTransferCellScore(const cv::Mat& image, int cell_size);
std::vector<cv::Rect> PartitionTransferRegions(cv::Size crop_size, const cv::Rect& left, const cv::Rect& right);
std::vector<TransferGridHint> DiscoverTransferGridHints(const cv::Mat& crop, bool structural_rank);

} // namespace iconrecognition::detail
