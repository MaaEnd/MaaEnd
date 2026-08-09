#include "IconMatcher.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>

namespace iconrecognition::detail
{
namespace
{

using PerformanceClock = std::chrono::steady_clock;

// 兼容旧的单模板 API 时允许的像素平移半径；调大提高错位容忍度，但增加搜索面积。
constexpr int kDefaultSearchRadius = 2;
// 模板相关性在最终分数中的权重；调高更重视轮廓结构，调低会让颜色差异占比上升。
constexpr double kTemplateScoreWeight = 0.85;
// Lab 颜色相似度在最终分数中的权重；调高更能区分同轮廓物品，但更易受光照影响。
constexpr double kColorScoreWeight = 0.15;
// OpenCV Lab 通道距离的归一化尺度，把平均颜色距离映射到 0..1 分数区间。
constexpr double kColorDistanceNormalization = 255.0;

double ElapsedMilliseconds(PerformanceClock::time_point started_at)
{
    return std::chrono::duration<double, std::milli>(PerformanceClock::now() - started_at).count();
}

cv::Mat ToBgr(const cv::Mat& image)
{
    if (image.channels() == 4) {
        cv::Mat bgr;
        cv::cvtColor(image, bgr, cv::COLOR_BGRA2BGR);
        return bgr;
    }
    if (image.channels() == 1) {
        cv::Mat bgr;
        cv::cvtColor(image, bgr, cv::COLOR_GRAY2BGR);
        return bgr;
    }
    return image;
}

cv::Mat ToLab32(const cv::Mat& image)
{
    cv::Mat lab;
    cv::cvtColor(image, lab, cv::COLOR_BGR2Lab);
    lab.convertTo(lab, CV_32FC3);
    return lab;
}

} // namespace

MatchDiagnostics ScoreTemplateAt(const cv::Mat& image, const cv::Rect& slot, const PreparedTemplate& templ, int search_radius, Phase phase)
{
    return ScoreTemplateAt(image, slot, templ, search_radius, phase, nullptr);
}

MatchDiagnostics ScoreTemplateAt(
    const cv::Mat& image,
    const cv::Rect& slot,
    const PreparedTemplate& templ,
    int search_radius,
    Phase phase,
    MatcherPerformanceDiagnostics* performance)
{
    MatchDiagnostics result;
    if (image.empty() || image.channels() < 3 || templ.image.empty() || templ.mask.empty() || search_radius < 0) {
        return result;
    }
    if (performance) {
        ++performance->score_calls;
    }

    const auto canvas_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    const cv::Mat source = ToBgr(image);
    const cv::Rect search(
        slot.x - search_radius,
        slot.y - search_radius,
        templ.image.cols + search_radius * 2,
        templ.image.rows + search_radius * 2);
    cv::Mat canvas = cv::Mat::zeros(search.height, search.width, CV_8UC3);
    const cv::Rect bounds(0, 0, source.cols, source.rows);
    const cv::Rect clipped = search & bounds;
    if (clipped.width > 0 && clipped.height > 0) {
        source(clipped).copyTo(canvas(cv::Rect(clipped.x - search.x, clipped.y - search.y, clipped.width, clipped.height)));
    }
    if (performance) {
        performance->canvas_prepare_ms += ElapsedMilliseconds(canvas_started);
    }

    const auto shift_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    const PreparedTemplate transformed = ShiftTemplate(templ, phase);
    if (performance) {
        performance->template_shift_ms += ElapsedMilliseconds(shift_started);
    }
    if (canvas.cols < transformed.image.cols || canvas.rows < transformed.image.rows) {
        return result;
    }

    const auto match_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    cv::Mat response;
    cv::matchTemplate(canvas, transformed.image, response, cv::TM_CCOEFF_NORMED, transformed.mask);
    if (performance) {
        performance->template_match_ms += ElapsedMilliseconds(match_started);
    }

    const auto reduce_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    cv::patchNaNs(response, -1.0);
    double maximum = -1.0;
    cv::Point location;
    cv::minMaxLoc(response, nullptr, &maximum, nullptr, &location);
    if (!std::isfinite(maximum)) {
        maximum = -1.0;
    }
    const cv::Mat matched = canvas(cv::Rect(location, transformed.image.size()));
    if (performance) {
        performance->response_reduce_ms += ElapsedMilliseconds(reduce_started);
    }

    const auto lab_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    const cv::Mat templ_lab = ToLab32(transformed.image);
    const cv::Mat matched_lab = ToLab32(matched);
    if (performance) {
        performance->lab_conversion_ms += ElapsedMilliseconds(lab_started);
    }

    const auto color_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    double distance = 0.0;
    int active = 0;
    for (int y = 0; y < transformed.mask.rows; ++y) {
        for (int x = 0; x < transformed.mask.cols; ++x) {
            if (transformed.mask.at<uchar>(y, x) == 0) {
                continue;
            }
            const cv::Vec3f delta = templ_lab.at<cv::Vec3f>(y, x) - matched_lab.at<cv::Vec3f>(y, x);
            distance += cv::norm(delta);
            ++active;
        }
    }
    const double color_score =
        active == 0 ? 0.0 : std::clamp(1.0 - distance / active / kColorDistanceNormalization, 0.0, 1.0);
    if (performance) {
        performance->color_distance_ms += ElapsedMilliseconds(color_started);
    }
    result.tm_score = maximum;
    result.color_score = color_score;
    result.score = kTemplateScoreWeight * maximum + kColorScoreWeight * color_score;
    result.position = cv::Point(location.x + search.x, location.y + search.y);
    result.phase = phase;
    return result;
}

MatchDiagnostics
    MatchTemplateAt(const cv::Mat& image, const cv::Rect& slot, const PreparedTemplate& templ, double threshold, double subpixel_threshold)
{
    MatchDiagnostics best = ScoreTemplateAt(image, slot, templ, kDefaultSearchRadius, { 0.0, 0.0 });
    if (best.score >= subpixel_threshold && best.score < threshold) {
        best.fallback_used = true;
        for (const Phase phase : PhaseGrid()) {
            if (phase.x == 0.0 && phase.y == 0.0) {
                continue;
            }
            const MatchDiagnostics candidate = ScoreTemplateAt(image, slot, templ, kDefaultSearchRadius, phase);
            if (candidate.score > best.score) {
                best = candidate, best.fallback_used = true;
            }
        }
        const auto extensions = BoundaryExtensionPhases(best.phase);
        for (const Phase phase : extensions) {
            const MatchDiagnostics candidate = ScoreTemplateAt(image, slot, templ, kDefaultSearchRadius, phase);
            if (candidate.score > best.score) {
                best = candidate, best.fallback_used = true;
            }
        }
    }
    return best;
}

} // namespace iconrecognition::detail
