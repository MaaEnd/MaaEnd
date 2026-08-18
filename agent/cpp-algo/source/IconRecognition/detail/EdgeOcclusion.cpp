#include "EdgeOcclusion.h"

#include <algorithm>
#include <cmath>
#include <limits>

namespace iconrecognition::detail
{
namespace
{

// 顶部遮挡边界在模板高度 12.5%..30% 内搜索；范围来自 ADB 首行工具栏遮挡样本。
constexpr double kTopMinimumCutoffRatio = 0.125;
constexpr double kTopMaximumCutoffRatio = 0.30;
// 底部遮挡边界在模板高度 50%..65% 内搜索；覆盖 ADB 底栏渐入并保留足够图标主体。
constexpr double kBottomMinimumCutoffRatio = 0.50;
constexpr double kBottomMaximumCutoffRatio = 0.65;
// 顶部排除区平均 Lab 残差至少为保留区 3.5 倍，才视为连续遮挡；调低会把普通错位误判为遮挡。
constexpr double kTopResidualRatioThreshold = 3.5;
// 底部背景更易出现数量和稀有度纹理，因此使用更严格的 5 倍残差门槛。
constexpr double kBottomResidualRatioThreshold = 5.0;
// 动态 mask 重排后的前两名至少相差 0.20，避免靠隐藏边缘把相似模板抬过阈值。
constexpr double kMinimumRecoveryMargin = 0.20;
// 两侧都必须包含至少这些有效 mask 像素，避免透明或既有 mask 边缘用极少样本产生异常比值。
constexpr int kMinimumActivePixels = 16;
// 保留区接近完全一致时使用该下限避免除零，同时允许明确遮挡产生足够大的比值。
constexpr double kMinimumRetainedResidual = 1e-6;

cv::Mat BuildPhaseValidityMask(cv::Size size, Phase phase)
{
    cv::Mat source(size, CV_8UC1, cv::Scalar(255));
    if (phase.x == 0.0 && phase.y == 0.0) {
        return source;
    }
    cv::Mat matrix = (cv::Mat_<double>(2, 3) << 1.0, 0.0, phase.x, 0.0, 1.0, phase.y);
    cv::Mat coverage;
    cv::warpAffine(source, coverage, matrix, size, cv::INTER_LINEAR, cv::BORDER_CONSTANT, cv::Scalar(0));
    cv::Mat valid;
    // 仅保留插值核完全落在模板内的像素，避免把仿射边界填充值误判为真实画面遮挡。
    cv::compare(coverage, cv::Scalar(255), valid, cv::CMP_EQ);
    return valid;
}

std::optional<double> MeanResidual(const cv::Mat& residual, const cv::Mat& mask, int begin, int end)
{
    const int y1 = std::clamp(begin, 0, residual.rows);
    const int y2 = std::clamp(end, 0, residual.rows);
    if (y2 <= y1) {
        return std::nullopt;
    }
    double sum = 0.0;
    int active = 0;
    for (int y = y1; y < y2; ++y) {
        for (int x = 0; x < residual.cols; ++x) {
            if (mask.at<uchar>(y, x) == 0) {
                continue;
            }
            sum += residual.at<float>(y, x);
            ++active;
        }
    }
    if (active < kMinimumActivePixels) {
        return std::nullopt;
    }
    return sum / active;
}

std::optional<EdgeOcclusion>
    BestOcclusionForSide(const cv::Mat& residual, const cv::Mat& mask, EdgeOcclusionSide side, int minimum_cutoff, int maximum_cutoff)
{
    std::optional<EdgeOcclusion> best;
    for (int cutoff = minimum_cutoff; cutoff <= maximum_cutoff; ++cutoff) {
        const auto excluded =
            side == EdgeOcclusionSide::Top ? MeanResidual(residual, mask, 0, cutoff) : MeanResidual(residual, mask, cutoff, residual.rows);
        const auto retained =
            side == EdgeOcclusionSide::Top ? MeanResidual(residual, mask, cutoff, residual.rows) : MeanResidual(residual, mask, 0, cutoff);
        if (!excluded || !retained) {
            continue;
        }
        const double ratio = *excluded / std::max(*retained, kMinimumRetainedResidual);
        if (!best || ratio > best->residual_ratio) {
            best = EdgeOcclusion { .side = side, .cutoff = cutoff, .residual_ratio = ratio };
        }
    }
    return best;
}

} // namespace

bool SupportsEdgeOcclusion(GridType type)
{
    return type != GridType::Rewards && type != GridType::SingleRoi;
}

bool ShouldAttemptEdgeOcclusionRecovery(GridType type, double score, double threshold, double subpixel_threshold, bool low_texture)
{
    return SupportsEdgeOcclusion(type) && !low_texture && score >= subpixel_threshold && score < threshold;
}

bool ShouldAcceptEdgeOcclusionRecovery(
    std::size_t original_template_index,
    std::size_t recovered_template_index,
    double recovered_score,
    std::optional<double> recovered_margin,
    double threshold)
{
    return original_template_index == recovered_template_index && recovered_score >= threshold
           && (!recovered_margin || *recovered_margin >= kMinimumRecoveryMargin);
}

std::optional<EdgeOcclusion>
    DetectEdgeOcclusion(const cv::Mat& image, const cv::Rect& candidate_box, const PreparedTemplate& templ, Phase phase)
{
    if (image.empty() || image.channels() < 3 || templ.image.empty() || templ.mask.empty() || templ.image.size() != templ.mask.size()
        || candidate_box.size() != templ.image.size()) {
        return std::nullopt;
    }
    const cv::Rect bounds(0, 0, image.cols, image.rows);
    if ((candidate_box & bounds) != candidate_box) {
        return std::nullopt;
    }

    cv::Mat candidate;
    if (image.channels() == 4) {
        cv::cvtColor(image(candidate_box), candidate, cv::COLOR_BGRA2BGR);
    }
    else {
        candidate = image(candidate_box);
    }
    const PreparedTemplate transformed = ShiftTemplate(templ, phase);
    cv::Mat template_lab;
    cv::Mat candidate_lab;
    cv::cvtColor(transformed.image, template_lab, cv::COLOR_BGR2Lab);
    cv::cvtColor(candidate, candidate_lab, cv::COLOR_BGR2Lab);
    template_lab.convertTo(template_lab, CV_32FC3);
    candidate_lab.convertTo(candidate_lab, CV_32FC3);

    cv::Mat residual(templ.image.size(), CV_32FC1);
    for (int y = 0; y < residual.rows; ++y) {
        for (int x = 0; x < residual.cols; ++x) {
            residual.at<float>(y, x) = static_cast<float>(cv::norm(template_lab.at<cv::Vec3f>(y, x) - candidate_lab.at<cv::Vec3f>(y, x)));
        }
    }
    cv::Mat residual_mask;
    cv::bitwise_and(templ.mask, BuildPhaseValidityMask(templ.mask.size(), phase), residual_mask);

    const int top_minimum = std::max(1, cvRound(residual.rows * kTopMinimumCutoffRatio));
    const int top_maximum = std::max(top_minimum, cvRound(residual.rows * kTopMaximumCutoffRatio));
    const int bottom_minimum = std::max(1, cvRound(residual.rows * kBottomMinimumCutoffRatio));
    const int bottom_maximum = std::max(bottom_minimum, cvRound(residual.rows * kBottomMaximumCutoffRatio));
    const auto top = BestOcclusionForSide(residual, residual_mask, EdgeOcclusionSide::Top, top_minimum, top_maximum);
    const auto bottom = BestOcclusionForSide(residual, residual_mask, EdgeOcclusionSide::Bottom, bottom_minimum, bottom_maximum);

    const bool top_accepted = top && top->residual_ratio >= kTopResidualRatioThreshold;
    const bool bottom_accepted = bottom && bottom->residual_ratio >= kBottomResidualRatioThreshold;
    if (!top_accepted && !bottom_accepted) {
        return std::nullopt;
    }
    if (!bottom_accepted) {
        return top;
    }
    if (!top_accepted) {
        return bottom;
    }
    return top->residual_ratio / kTopResidualRatioThreshold >= bottom->residual_ratio / kBottomResidualRatioThreshold ? top : bottom;
}

void ApplyEdgeOcclusionMask(cv::Mat& mask, const EdgeOcclusion& occlusion)
{
    if (mask.empty()) {
        return;
    }
    const int cutoff = std::clamp(occlusion.cutoff, 0, mask.rows);
    if (occlusion.side == EdgeOcclusionSide::Top) {
        mask.rowRange(0, cutoff).setTo(cv::Scalar(0));
    }
    else {
        mask.rowRange(cutoff, mask.rows).setTo(cv::Scalar(0));
    }
}

} // namespace iconrecognition::detail
