#include "TrustedRarity.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <numeric>
#include <optional>
#include <tuple>

#include "RarityClassifier.h"

namespace iconrecognition::detail
{
namespace
{

// 像素与 rarity 原型色的最大 Lab 距离；调大提高色带召回，调小减少相近背景进入 mask。
constexpr double kPrototypeDistance = 25.0;
// 连通色带宽度相对 cell 的下限；调低可接受残缺色带，也会增加短横线误检。
constexpr double kMinimumWidthRatio = 0.72;
// 连通色带宽度相对 cell 的上限；调高容忍跨格粘连，调低更严格限制为单格色带。
constexpr double kMaximumWidthRatio = 1.08;
// 色带连通域允许的最小厚度（像素）；调低没有意义，调高会漏掉细弱色带。
constexpr int kMinimumThickness = 1;
// 色带连通域允许的最大厚度（像素）；调高会容忍模糊，也更易接受宽背景条。
constexpr int kMaximumThickness = 6;
// 连通框内原型色像素的最低覆盖率；调高提高纯度，调低可适应压缩噪声。
constexpr double kMinimumCoverage = 0.72;
// 色带最长连续段占宽度的最低比例；调高拒绝断裂噪声，调低可接受遮挡色带。
constexpr double kMinimumContinuity = 0.70;
// 色带均色与上下背景的最低平均 Lab 差；调高减少同色背景误判，调低提高低对比召回。
constexpr double kMinimumBackgroundDelta = 10.0;
// 色带与较相近一侧背景仍需达到的最低 Lab 差；调高要求双侧边界更清晰。
constexpr double kMinimumEdgeResponse = 7.0;
// 颜色覆盖率在可信度中的权重；调高更看重色带像素纯度。
constexpr double kCoverageConfidenceWeight = 0.30;
// 横向连续性在可信度中的权重；调高更偏好完整长条。
constexpr double kContinuityConfidenceWeight = 0.20;
// 上下背景平均对比度在可信度中的权重；调高更看重整体背景分离。
constexpr double kBackgroundConfidenceWeight = 0.20;
// 较弱一侧边缘响应在可信度中的权重；调高更看重双侧都存在边界。
constexpr double kEdgeConfidenceWeight = 0.20;
// 色带厚度接近期望值的权重；调高会更严格拒绝过厚或过薄的条带。
constexpr double kThicknessConfidenceWeight = 0.10;
// 背景 Lab 差映射到满置信度的尺度；调大使背景对比贡献增长更慢。
constexpr double kBackgroundDeltaScale = 25.0;
// 单侧边缘 Lab 差映射到满置信度的尺度；调大使边缘证据贡献增长更慢。
constexpr double kEdgeResponseScale = 20.0;
// 720p 双侧网格 rarity 色带的期望厚度（像素）。
constexpr int kExpectedThickness = 3;
// 厚度偏差归一化尺度；调大减轻厚度偏差惩罚，调小则更严格。
constexpr double kThicknessDeviationScale = 3.0;
// 只有一侧背景可采样时施加的置信度折扣；调低更保守，调高更信任边缘样本。
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

struct HorizontalRun
{
    int x = 0;
    int width = 0;
};

std::optional<HorizontalRun> LongestHorizontalRun(const cv::Mat& mask, int y, int x_begin, int x_end)
{
    std::optional<HorizontalRun> best;
    int run_begin = x_begin;
    for (int x = x_begin; x <= x_end; ++x) {
        const bool covered = x < x_end && mask.at<unsigned char>(y, x) != 0;
        if (covered) {
            continue;
        }
        if (x > run_begin && (!best || x - run_begin > best->width)) {
            best = HorizontalRun { .x = run_begin, .width = x - run_begin };
        }
        run_begin = x + 1;
    }
    return best;
}

double LongestRunRatio(const cv::Mat& mask, const cv::Rect& box)
{
    int longest = 0;
    for (int y = box.y; y < box.y + box.height; ++y) {
        if (const auto run = LongestHorizontalRun(mask, y, box.x, box.x + box.width)) {
            longest = std::max(longest, run->width);
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
            const cv::Rect component_box(
                stats.at<int>(component, cv::CC_STAT_LEFT),
                stats.at<int>(component, cv::CC_STAT_TOP),
                stats.at<int>(component, cv::CC_STAT_WIDTH),
                stats.at<int>(component, cv::CC_STAT_HEIGHT));
            // 组件可能被同色数字向上或向下粘连，先只按宽度排除不可能的短横线；
            // 高度和上限在提取水平核心后再判断，避免杂点把真实色带提前过滤掉。
            if (component_box.width < minimum_width) {
                continue;
            }

            // 数字等同色杂点可能与色带连成一个组件；只保留达到单格宽度的水平核心行，
            // 再用核心行重建色带框，避免杂点扩大高度并稀释颜色覆盖率。
            struct CoreRun
            {
                int y = 0;
                HorizontalRun run;
            };
            std::vector<CoreRun> core_runs;
            for (int y = component_box.y; y < component_box.y + component_box.height; ++y) {
                if (const auto run = LongestHorizontalRun(mask, y, component_box.x, component_box.x + component_box.width)) {
                    if (run->width >= minimum_width) {
                        core_runs.push_back({ y, *run });
                    }
                }
            }
            for (std::size_t begin = 0; begin < core_runs.size();) {
                std::size_t end = begin + 1;
                const int first_y = core_runs[begin].y;
                while (end < core_runs.size() && core_runs[end].y == core_runs[end - 1].y + 1) {
                    ++end;
                }
                const int top = first_y;
                const int bottom = core_runs[end - 1].y + 1;
                int left = std::numeric_limits<int>::max();
                int right = std::numeric_limits<int>::min();
                for (std::size_t index = begin; index < end; ++index) {
                    left = std::min(left, core_runs[index].run.x);
                    right = std::max(right, core_runs[index].run.x + core_runs[index].run.width);
                }
                const cv::Rect box(left, top, right - left, bottom - top);
                if (box.width > maximum_width || box.height < kMinimumThickness || box.height > kMaximumThickness) {
                    begin = end;
                    continue;
                }
                const double coverage = static_cast<double>(cv::countNonZero(mask(box))) / box.area();
                const double continuity = LongestRunRatio(mask, box);
                const auto backgrounds = BackgroundBoxes(box, lab.size());
                if (backgrounds.empty()) {
                    begin = end;
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
                if (trusted) {
                    double confidence =
                        kCoverageConfidenceWeight * coverage + kContinuityConfidenceWeight * continuity
                        + kBackgroundConfidenceWeight * Clamp01(background_delta / kBackgroundDeltaScale)
                        + kEdgeConfidenceWeight * Clamp01(edge_response / kEdgeResponseScale)
                        + kThicknessConfidenceWeight * Clamp01(1.0 - std::abs(box.height - kExpectedThickness) / kThicknessDeviationScale);
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
                begin = end;
            }
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
