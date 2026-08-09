#pragma once

#include "GridTypes.h"

namespace iconrecognition::detail
{

struct GridProfile
{
    // 单个物品 cell 的模板边长，单位为 720p 像素。
    int cell_size = 64;
    // 相邻列起点的水平间距，允许使用浮点值描述稳定的平均 pitch。
    double pitch_x = 64.0;
    // 相邻行起点的垂直间距，允许使用浮点值描述稳定的平均 pitch。
    double pitch_y = 64.0;
    // 接受网格所需的最少列数，防止单个结构峰误建网格。
    int min_columns = 1;
    // 接受网格所需的最少行数，防止单个结构峰误建网格。
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
    // 双侧网格的标准 cell 边长，单位为 720p 像素。
    int cell_size = 64;
    // 正式规则晶格允许的最小 pitch；调低会放宽密集网格候选。
    int pitch_min = 68;
    // 正式规则晶格允许的最大 pitch；调高会放宽稀疏网格候选。
    int pitch_max = 70;
    // pitch 排名的先验中心；偏离该值会降低几何置信度。
    int preferred_pitch = 69;
    // 粗结构峰可能有 1px 量化误差，仅放宽观测间距，不扩大最终输出 pitch。
    int observed_pitch_tolerance = 1;
    // 连续稀有度色带的下边界相对 cell top 的距离。
    int rarity_anchor_offset = 64;
    // 单侧网格允许生成的最大行数，限制远端结构误扩展。
    int maximum_rows = 5;
    // 粗相位与色带锚点允许的像素偏差；调大提高容忍度，也增加错位候选。
    int phase_tolerance = 2;
    // rarity 色带被视为有效证据的最低覆盖率；调高更可靠，调低提高弱色带召回。
    double minimum_rarity_coverage = 0.80;
    // rarity 色带被视为强证据的覆盖率；调高减少强锚点，调低会更信任不完整色带。
    double strong_rarity_coverage = 0.95;
    // 顶部被 ROI 裁切时保留 cell 所需的最小可见比例。
    double minimum_top_visibility = 0.85;
    // 底部被 ROI 裁切时保留 cell 所需的最小可见比例。
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
