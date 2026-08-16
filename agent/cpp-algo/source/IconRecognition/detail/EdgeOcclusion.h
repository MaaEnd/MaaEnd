#pragma once

#include <optional>

#include <MaaUtils/NoWarningCV.hpp>

#include "../IconRecognitionTypes.h"
#include "SubpixelMatcher.h"
#include "TemplateTypes.h"

namespace iconrecognition::detail
{

enum class EdgeOcclusionSide
{
    Top,
    Bottom,
};

struct EdgeOcclusion
{
    EdgeOcclusionSide side = EdgeOcclusionSide::Top;
    // Top 排除 [0, cutoff)，Bottom 排除 [cutoff, height)。
    int cutoff = 0;
    double residual_ratio = 0.0;
};

bool SupportsEdgeOcclusion(GridType type);
bool ShouldAttemptEdgeOcclusionRecovery(GridType type, double score, double threshold, double subpixel_threshold, bool low_texture);
bool ShouldAcceptEdgeOcclusionRecovery(
    std::size_t original_template_index,
    std::size_t recovered_template_index,
    double recovered_score,
    std::optional<double> recovered_margin,
    double threshold);
std::optional<EdgeOcclusion>
    DetectEdgeOcclusion(const cv::Mat& image, const cv::Rect& candidate_box, const PreparedTemplate& templ, Phase phase);
void ApplyEdgeOcclusionMask(cv::Mat& mask, const EdgeOcclusion& occlusion);

} // namespace iconrecognition::detail
