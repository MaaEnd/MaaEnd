#include "TrustedRarity.h"

#include <algorithm>
#include <cmath>
#include <numeric>
#include <tuple>

#include "RarityClassifier.h"

namespace iconrecognition::detail
{
namespace
{

constexpr double kPrototypeDistance = 25.0;
constexpr double kMinimumWidthRatio = 0.72;
constexpr double kMaximumWidthRatio = 1.08;
constexpr int kMinimumThickness = 1;
constexpr int kMaximumThickness = 6;
constexpr double kMinimumCoverage = 0.72;
constexpr double kMinimumContinuity = 0.70;
constexpr double kMinimumBackgroundDelta = 10.0;
constexpr double kMinimumEdgeResponse = 7.0;
// 可信度同时衡量颜色覆盖、连续性、背景对比、边缘和厚度，避免单一高覆盖率接管晶格。
constexpr double kCoverageConfidenceWeight = 0.30;
constexpr double kContinuityConfidenceWeight = 0.20;
constexpr double kBackgroundConfidenceWeight = 0.20;
constexpr double kEdgeConfidenceWeight = 0.20;
constexpr double kThicknessConfidenceWeight = 0.10;
constexpr double kBackgroundDeltaScale = 25.0;
constexpr double kEdgeResponseScale = 20.0;
constexpr int kExpectedThickness = 3;
constexpr double kThicknessDeviationScale = 3.0;
constexpr double kSingleBackgroundPenalty = 0.85;

cv::Mat ToLab32(const cv::Mat& image)
{
    cv::Mat bgr;
    if (image.channels() == 4) {
        cv::cvtColor(image, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = image;
    }
    cv::Mat lab;
    cv::cvtColor(bgr, lab, cv::COLOR_BGR2Lab);
    lab.convertTo(lab, CV_32FC3);
    return lab;
}

cv::Vec3d MeanLab(const cv::Mat& lab, const cv::Rect& box, const cv::Mat& mask = {})
{
    const cv::Scalar mean = mask.empty() ? cv::mean(lab(box)) : cv::mean(lab(box), mask(box));
    return { mean[0], mean[1], mean[2] };
}

double LongestRunRatio(const cv::Mat& mask, const cv::Rect& box)
{
    int longest = 0;
    for (int y = box.y; y < box.y + box.height; ++y) {
        int current = 0;
        for (int x = box.x; x < box.x + box.width; ++x) {
            if (mask.at<unsigned char>(y, x) != 0) {
                longest = std::max(longest, ++current);
            }
            else {
                current = 0;
            }
        }
    }
    return static_cast<double>(longest) / box.width;
}

std::vector<cv::Rect> BackgroundBoxes(const cv::Rect& strip, cv::Size size)
{
    std::vector<cv::Rect> result;
    const int top_begin = std::max(0, strip.y - 4);
    const int top_end = std::max(0, strip.y - 1);
    if (top_end > top_begin) {
        result.emplace_back(strip.x, top_begin, strip.width, top_end - top_begin);
    }
    const int bottom_begin = std::min(size.height, strip.y + strip.height + 1);
    const int bottom_end = std::min(size.height, strip.y + strip.height + 4);
    if (bottom_end > bottom_begin) {
        result.emplace_back(strip.x, bottom_begin, strip.width, bottom_end - bottom_begin);
    }
    return result;
}

double Clamp01(double value)
{
    return std::clamp(value, 0.0, 1.0);
}

} // namespace

std::vector<TrustedRarityStrip> DetectTrustedRarityStrips(const cv::Mat& image, int cell_size)
{
    if (image.empty() || image.channels() < 3 || cell_size <= 0) {
        return {};
    }
    const cv::Mat lab = ToLab32(image);
    std::vector<TrustedRarityStrip> candidates;
    const int minimum_width = cvCeil(kMinimumWidthRatio * cell_size);
    const int maximum_width = cvFloor(kMaximumWidthRatio * cell_size);
    const auto& prototypes = RarityLabPrototypes();
    for (std::size_t prototype_index = 0; prototype_index < prototypes.size(); ++prototype_index) {
        cv::Mat mask(lab.size(), CV_8U, cv::Scalar(0));
        for (int y = 0; y < lab.rows; ++y) {
            for (int x = 0; x < lab.cols; ++x) {
                if (cv::norm(lab.at<cv::Vec3f>(y, x) - prototypes[prototype_index]) <= kPrototypeDistance) {
                    mask.at<unsigned char>(y, x) = 255;
                }
            }
        }
        cv::Mat connected;
        cv::morphologyEx(mask, connected, cv::MORPH_CLOSE, cv::getStructuringElement(cv::MORPH_RECT, cv::Size(3, 1)));
        cv::Mat labels;
        cv::Mat stats;
        cv::Mat centroids;
        const int components = cv::connectedComponentsWithStats(connected, labels, stats, centroids, 8, CV_32S);
        for (int component = 1; component < components; ++component) {
            const cv::Rect box(
                stats.at<int>(component, cv::CC_STAT_LEFT),
                stats.at<int>(component, cv::CC_STAT_TOP),
                stats.at<int>(component, cv::CC_STAT_WIDTH),
                stats.at<int>(component, cv::CC_STAT_HEIGHT));
            if (box.width < minimum_width || box.width > maximum_width || box.height < kMinimumThickness
                || box.height > kMaximumThickness) {
                continue;
            }
            const double coverage = static_cast<double>(cv::countNonZero(mask(box))) / box.area();
            const double continuity = LongestRunRatio(mask, box);
            const auto backgrounds = BackgroundBoxes(box, lab.size());
            if (backgrounds.empty()) {
                continue;
            }
            const cv::Vec3d strip_mean = MeanLab(lab, box, mask);
            std::vector<double> deltas;
            for (const cv::Rect& background : backgrounds) {
                deltas.push_back(cv::norm(strip_mean - MeanLab(lab, background)));
            }
            const double background_delta = std::accumulate(deltas.begin(), deltas.end(), 0.0) / static_cast<double>(deltas.size());
            const double edge_response = *std::ranges::min_element(deltas);
            const bool trusted = coverage >= kMinimumCoverage && continuity >= kMinimumContinuity
                                 && background_delta >= kMinimumBackgroundDelta && edge_response >= kMinimumEdgeResponse;
            if (!trusted) {
                continue;
            }
            double confidence = kCoverageConfidenceWeight * coverage + kContinuityConfidenceWeight * continuity
                                + kBackgroundConfidenceWeight * Clamp01(background_delta / kBackgroundDeltaScale)
                                + kEdgeConfidenceWeight * Clamp01(edge_response / kEdgeResponseScale)
                                + kThicknessConfidenceWeight
                                      * Clamp01(1.0 - std::abs(box.height - kExpectedThickness) / kThicknessDeviationScale);
            if (backgrounds.size() == 1) {
                confidence *= kSingleBackgroundPenalty;
            }
            candidates.push_back(TrustedRarityStrip {
                .box = box,
                .rarity = static_cast<int>(prototype_index + 1),
                .color_coverage = coverage,
                .continuity = continuity,
                .background_delta = background_delta,
                .edge_response = edge_response,
                .thickness = box.height,
                .confidence = confidence,
                .trusted = true,
                .can_seed_lattice = prototype_index != 0,
            });
        }
    }

    std::ranges::sort(candidates, [](const auto& left, const auto& right) {
        return std::tuple { left.confidence, -left.rarity } > std::tuple { right.confidence, -right.rarity };
    });
    std::vector<TrustedRarityStrip> kept;
    for (const auto& candidate : candidates) {
        if (std::ranges::none_of(kept, [&](const auto& item) { return !(candidate.box & item.box).empty(); })) {
            kept.push_back(candidate);
        }
    }
    std::ranges::sort(kept, [](const auto& left, const auto& right) {
        return std::tuple { left.box.y, left.box.x, left.rarity } < std::tuple { right.box.y, right.box.x, right.rarity };
    });
    return kept;
}

} // namespace iconrecognition::detail
