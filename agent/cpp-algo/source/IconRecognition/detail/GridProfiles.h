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

enum class TransferGridVariant
{
    TransferLeft,
    TransferRight,
    PortStoragerLeft,
    PortStoragerRight,
};

struct TransferGridProfile
{
    int cell_size = 64;
    int pitch_min = 68;
    int pitch_max = 70;
    int preferred_pitch = 69;
    // 粗结构峰可能有 1px 量化误差，仅放宽观测间距，不扩大最终输出 pitch。
    int observed_pitch_tolerance = 1;
    // 连续稀有度色带的下边界相对 cell top 的距离。
    int rarity_anchor_offset = 64;
    int maximum_rows = 5;
    int phase_tolerance = 2;
    double minimum_rarity_coverage = 0.80;
    double strong_rarity_coverage = 0.95;
    double minimum_top_visibility = 0.85;
    double minimum_bottom_visibility = 0.70;
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
TransferGridProfile TransferProfileFor(TransferGridVariant variant);
cv::Mat BuildTransferCellScore(const cv::Mat& image, int cell_size);
std::vector<cv::Rect> PartitionTransferRegions(cv::Size crop_size, const cv::Rect& left, const cv::Rect& right);
std::vector<TransferGridHint> DiscoverTransferGridHints(const cv::Mat& crop, bool structural_rank);

} // namespace iconrecognition::detail
