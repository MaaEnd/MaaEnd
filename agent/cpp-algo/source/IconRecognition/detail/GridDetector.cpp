#include "GridDetector.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <limits>
#include <map>
#include <numeric>
#include <optional>
#include <set>
#include <stdexcept>
#include <tuple>
#include <vector>

#include "GridAnchors.h"
#include "GridFeatures.h"
#include "GridGeometry.h"
#include "GridProfiles.h"

namespace iconrecognition::detail
{
namespace
{

constexpr double kEpsilon = 1e-8;
constexpr int kCreditTradeColumns = 7;
constexpr int kCreditTradeCellOffsetX = 10;
constexpr int kCreditTradeCellOffsetY = 6;
// 单网格和卡片网格的结构搜索参数按 720p 截图标定。
constexpr double kDefaultMinimumTopVisibility = 0.90;
constexpr double kDefaultMinimumBottomVisibility = 0.70;
constexpr double kMinimumHorizontalVisibility = 0.70;
constexpr int kSingleLatticePitchSearchRadius = 8;
constexpr int kSingleLatticePitchTolerance = 1;
constexpr int kCardMinimumInset = 6;
constexpr double kCardInsetRatio = 0.08;
constexpr int kCardMinimumBand = 4;
constexpr double kCardBandRatio = 0.05;
constexpr double kCardProfilePitchRadius = 8.0;
constexpr double kCardCurrentPitchRadius = 1.5;
constexpr double kPitchRefinementStep = 0.25;
constexpr double kPitchLoopEpsilon = 0.001;
constexpr double kCardMinimumAbsoluteGain = 30.0;
constexpr double kCardMinimumRelativeGain = 1.20;
// 双侧网格的相位选择和候选评分只使用这些集中门槛。
constexpr int kDefaultStructuralPhaseMaximumShift = 20;
constexpr double kDefaultStructuralPhaseMinimumGain = 0.08;
constexpr int kIgnoredStructuralPhaseShift = 4;
constexpr double kMinimumStructuralPhaseResponse = 0.15;
constexpr double kTransferAxisResidualPenalty = 0.05;
constexpr int kBorderProjectionMinimumMargin = 2;
constexpr double kBorderProjectionMarginRatio = 0.06;
constexpr int kWideTransferPhaseMaximumShift = 12;
constexpr double kWideTransferPhaseMinimumGain = 0.25;
constexpr double kTrustedStructureWeight = 0.40;
constexpr double kTrustedConfidenceWeight = 0.35;
constexpr double kTrustedConsistencyWeight = 0.25;
constexpr int kMinimumTrustedCellsWithoutLegacySupport = 2;
constexpr double kMinimumTrustedStructureWithoutLegacySupport = 0.10;
constexpr double kLegacyStructureWeight = 0.65;
constexpr double kLegacyRarityWeight = 0.35;
constexpr double kRowCompletionSupportRatio = 0.04;
constexpr std::size_t kSingleObservationCompletionLimit = 2;
constexpr std::size_t kStableRarityMinimumRows = 3;

bool IsFormal(
    const cv::Rect& cell,
    const cv::Rect& roi,
    double minimum_top_visibility = kDefaultMinimumTopVisibility,
    double minimum_bottom_visibility = kDefaultMinimumBottomVisibility)
{
    const cv::Rect intersection = cell & roi;
    if (intersection.empty()) {
        return false;
    }
    const double visible_x = static_cast<double>(intersection.width) / cell.width;
    const double visible_y = static_cast<double>(intersection.height) / cell.height;
    const bool top_ok = cell.y >= roi.y || visible_y >= minimum_top_visibility;
    const bool bottom_ok = cell.y + cell.height <= roi.y + roi.height || visible_y >= minimum_bottom_visibility;
    return visible_x >= kMinimumHorizontalVisibility && top_ok && bottom_ok;
}

GridLayout DetectSingleLattice(const cv::Mat& image, GridType type, const cv::Rect& roi)
{
    const GridProfile profile = ProfileFor(type);
    const cv::Mat crop = image(roi);
    const StructureMaps maps = BuildStructureMaps(crop, profile.cell_size);
    const auto x_signal = RobustProjection(maps.vertical, true);
    const auto y_signal = RobustProjection(maps.horizontal, false);
    const auto diagonal_x = RobustProjection(maps.diagonal_penalty, true);
    const auto diagonal_y = RobustProjection(maps.diagonal_penalty, false);
    const auto signed_x = AggregateSigned(maps.signed_x, true);
    const auto signed_y = AggregateSigned(maps.signed_y, false);
    cv::Mat bgr;
    if (crop.channels() == 4) {
        cv::cvtColor(crop, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = crop;
    }
    cv::Mat gray;
    cv::cvtColor(bgr, gray, cv::COLOR_BGR2GRAY);
    gray.convertTo(gray, CV_32F, 1.0 / 255.0);
    const auto support_x = MedianProjection(gray, true);
    const auto support_y = MedianProjection(gray, false);
    const int pitch_x =
        EstimatePeriod(
            x_signal,
            static_cast<int>(std::floor(profile.pitch_x)) - kSingleLatticePitchSearchRadius,
            static_cast<int>(std::ceil(profile.pitch_x)) + kSingleLatticePitchSearchRadius);
    const int pitch_y =
        EstimatePeriod(
            y_signal,
            static_cast<int>(std::floor(profile.pitch_y)) - kSingleLatticePitchSearchRadius,
            static_cast<int>(std::ceil(profile.pitch_y)) + kSingleLatticePitchSearchRadius);
    const auto pitch_range_x =
        std::pair { pitch_x - kSingleLatticePitchTolerance, pitch_x + kSingleLatticePitchTolerance };
    const auto pitch_range_y =
        std::pair { pitch_y - kSingleLatticePitchTolerance, pitch_y + kSingleLatticePitchTolerance };
    const int expected_columns = std::max(profile.min_columns, (roi.width - profile.cell_size) / std::max(pitch_x, 1) + 1);
    const int expected_rows = std::max(profile.min_rows, (roi.height - profile.cell_size) / std::max(pitch_y, 1) + 1);
    const AxisSequence x_axis =
        FitSubpixelAxis(x_signal, signed_x, support_x, diagonal_x, profile.cell_size, pitch_x, pitch_range_x, expected_columns);
    const AxisSequence y_axis =
        FitSubpixelAxis(y_signal, signed_y, support_y, diagonal_y, profile.cell_size, pitch_y, pitch_range_y, expected_rows);

    GridLayout layout;
    layout.grid_index = 0;
    layout.cell_size = profile.cell_size;
    layout.pitch_x = x_axis.spacings.empty() ? pitch_x : Median(x_axis.spacings);
    layout.pitch_y = y_axis.spacings.empty() ? pitch_y : Median(y_axis.spacings);
    std::vector<int> kept_x;
    std::vector<int> kept_y;
    for (int local_x : x_axis.integer_starts) {
        const int absolute_x = roi.x + local_x;
        if (IsFormal(cv::Rect(absolute_x, roi.y, profile.cell_size, profile.cell_size), roi)) {
            kept_x.push_back(absolute_x);
        }
    }
    for (int local_y : y_axis.integer_starts) {
        const int absolute_y = roi.y + local_y;
        if (IsFormal(cv::Rect(roi.x, absolute_y, profile.cell_size, profile.cell_size), roi)) {
            kept_y.push_back(absolute_y);
        }
    }
    for (int row = 0; row < static_cast<int>(kept_y.size()); ++row) {
        for (int column = 0; column < static_cast<int>(kept_x.size()); ++column) {
            layout.cells.push_back({ 0, row, column, cv::Rect(kept_x[column], kept_y[row], profile.cell_size, profile.cell_size) });
        }
    }
    if (layout.cells.empty()) {
        throw std::runtime_error("grid ROI contains no formal cells");
    }
    layout.rows = static_cast<int>(kept_y.size());
    layout.columns = static_cast<int>(kept_x.size());
    layout.bounds = cv::Rect(
        kept_x.front(),
        kept_y.front(),
        kept_x.back() + profile.cell_size - kept_x.front(),
        kept_y.back() + profile.cell_size - kept_y.front());
    return layout;
}

double CardVerticalPhaseScore(const cv::Mat& gray, int phase_y, double pitch_y, int cell_size, const std::vector<int>& x_starts)
{
    const int inset = std::max(kCardMinimumInset, cvRound(cell_size * kCardInsetRatio));
    const int band = std::max(kCardMinimumBand, cvRound(cell_size * kCardBandRatio));
    std::vector<double> scores;
    for (int row = 0; row < 8; ++row) {
        const int y = cvRound(phase_y + row * pitch_y);
        if (y - band < 0 || y + cell_size + band > gray.rows) {
            continue;
        }
        for (int x : x_starts) {
            const int x1 = std::max(0, x + inset);
            const int x2 = std::min(gray.cols, x + cell_size - inset);
            if (x2 <= x1) {
                continue;
            }
            const auto mean = [&](int top, int bottom) {
                return cv::mean(gray(cv::Rect(x1, top, x2 - x1, bottom - top)))[0];
            };
            const double inside_top = mean(y + 2, y + 2 + band);
            const double outside_top = mean(y - band, y);
            const double inside_bottom = mean(y + cell_size - band - 2, y + cell_size - 2);
            const double outside_bottom = mean(y + cell_size, y + cell_size + band);
            scores.push_back(std::max(inside_top - outside_top, 0.0) + std::max(inside_bottom - outside_bottom, 0.0));
        }
    }
    return Median(std::move(scores));
}

void RefineCardVerticalPhase(const cv::Mat& image, const cv::Rect& roi, GridType type, GridLayout& layout)
{
    cv::Mat bgr;
    if (image.channels() == 4) {
        cv::cvtColor(image, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = image;
    }
    cv::Mat gray;
    cv::cvtColor(bgr, gray, cv::COLOR_BGR2GRAY);
    std::vector<int> x_starts;
    for (const auto& cell : layout.cells) {
        if (cell.row == 0) {
            x_starts.push_back(cell.cell_box.x);
        }
    }
    const int current_y = layout.cells.front().cell_box.y;
    const double current_pitch = layout.pitch_y;
    const double current_score = CardVerticalPhaseScore(gray, current_y, current_pitch, layout.cell_size, x_starts);
    const GridProfile profile = ProfileFor(type);
    const double pitch_min =
        std::max(profile.pitch_y - kCardProfilePitchRadius, current_pitch - kCardCurrentPitchRadius);
    const double pitch_max =
        std::min(profile.pitch_y + kCardProfilePitchRadius, current_pitch + kCardCurrentPitchRadius);
    const int phase_stop = std::min(roi.y + roi.height, roi.y + cvRound(current_pitch));
    std::tuple<double, double, double, int, double> best { -1.0, 0.0, 0.0, current_y, current_pitch };
    for (int phase_y = roi.y; phase_y < phase_stop; ++phase_y) {
        for (double pitch = pitch_min; pitch <= pitch_max + kPitchLoopEpsilon; pitch += kPitchRefinementStep) {
            const auto candidate = std::tuple {
                CardVerticalPhaseScore(gray, phase_y, pitch, layout.cell_size, x_starts),
                -std::abs(pitch - current_pitch),
                -std::abs(phase_y - current_y),
                phase_y,
                pitch,
            };
            if (candidate > best) {
                best = candidate;
            }
        }
    }
    const auto [best_score, ignored_pitch, ignored_phase, best_y, best_pitch] = best;
    if (best_score < current_score + kCardMinimumAbsoluteGain || best_score < current_score * kCardMinimumRelativeGain) {
        return;
    }
    std::vector<int> y_starts;
    for (int row = 0; row < 16; ++row) {
        const int y = cvRound(best_y + row * best_pitch);
        if (y >= roi.y + roi.height) {
            break;
        }
        if (IsFormal(cv::Rect(x_starts.front(), y, layout.cell_size, layout.cell_size), roi)) {
            y_starts.push_back(y);
        }
    }
    if (static_cast<int>(y_starts.size()) < profile.min_rows) {
        return;
    }
    layout.cells.clear();
    for (int row = 0; row < static_cast<int>(y_starts.size()); ++row) {
        for (int column = 0; column < static_cast<int>(x_starts.size()); ++column) {
            layout.cells.push_back({ 0, row, column, cv::Rect(x_starts[column], y_starts[row], layout.cell_size, layout.cell_size) });
        }
    }
    layout.pitch_y = best_pitch;
    layout.rows = static_cast<int>(y_starts.size());
    layout.bounds = cv::Rect(
        x_starts.front(),
        y_starts.front(),
        x_starts.back() + layout.cell_size - x_starts.front(),
        y_starts.back() + layout.cell_size - y_starts.front());
}

GridLayout BuildCreditTradeLattice(const cv::Rect& roi, int x_phase, int y_phase, const GridProfile& profile)
{
    const int pitch_x = cvRound(profile.pitch_x);
    const int pitch_y = cvRound(profile.pitch_y);

    GridLayout layout;
    layout.grid_index = 0;
    layout.cell_size = profile.cell_size;
    layout.pitch_x = profile.pitch_x;
    layout.pitch_y = profile.pitch_y;
    layout.columns = kCreditTradeColumns;
    for (int row = 0; row < 8; ++row) {
        const int y = y_phase + row * pitch_y + kCreditTradeCellOffsetY;
        if (y >= roi.y + roi.height) {
            break;
        }
        bool kept_row = false;
        for (int column = 0; column < kCreditTradeColumns; ++column) {
            const cv::Rect cell(x_phase + column * pitch_x + kCreditTradeCellOffsetX, y, profile.cell_size, profile.cell_size);
            if (IsFormal(cell, roi)) {
                layout.cells.push_back({ 0, layout.rows, column, cell });
                kept_row = true;
            }
        }
        layout.rows += kept_row ? 1 : 0;
    }
    if (layout.cells.empty()) {
        throw std::runtime_error("credit_trade ROI contains no formal cells");
    }

    int x1 = std::numeric_limits<int>::max();
    int y1 = std::numeric_limits<int>::max();
    int x2 = std::numeric_limits<int>::min();
    int y2 = std::numeric_limits<int>::min();
    for (const auto& cell : layout.cells) {
        x1 = std::min(x1, cell.cell_box.x), y1 = std::min(y1, cell.cell_box.y), x2 = std::max(x2, cell.cell_box.x + profile.cell_size),
        y2 = std::max(y2, cell.cell_box.y + profile.cell_size);
    }
    layout.bounds = cv::Rect(x1, y1, x2 - x1, y2 - y1);
    return layout;
}

GridLayout DetectCreditTrade(const cv::Mat& image, const cv::Rect& roi)
{
    const GridProfile profile = ProfileFor(GridType::CreditTrade);
    const int pitch_x = cvRound(profile.pitch_x);
    const int pitch_y = cvRound(profile.pitch_y);
    cv::Mat crop = image(roi);
    cv::Mat bgr;
    if (crop.channels() == 4) {
        cv::cvtColor(crop, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = crop;
    }
    cv::Mat hsv;
    cv::cvtColor(bgr, hsv, cv::COLOR_BGR2HSV);
    cv::Mat bright;
    cv::inRange(hsv, cv::Scalar(0, 0, 226), cv::Scalar(179, 34, 255), bright);
    cv::Mat labels;
    cv::Mat stats;
    cv::Mat centroids;
    const int count = cv::connectedComponentsWithStats(bright, labels, stats, centroids, 8);
    std::vector<cv::Point> cards;
    for (int index = 1; index < count; ++index) {
        const int width = stats.at<int>(index, cv::CC_STAT_WIDTH);
        const int height = stats.at<int>(index, cv::CC_STAT_HEIGHT);
        const int area = stats.at<int>(index, cv::CC_STAT_AREA);
        if (width >= 140 && width <= 155 && height >= 150 && height <= 180 && area >= 5000) {
            cards.emplace_back(stats.at<int>(index, cv::CC_STAT_LEFT) + roi.x, stats.at<int>(index, cv::CC_STAT_TOP) + roi.y);
        }
    }
    if (cards.size() < 2) {
        return DetectSingleLattice(image, GridType::CreditTrade, roi);
    }
    std::vector<double> x_phases;
    std::vector<double> y_phases;
    for (const auto& card : cards) {
        x_phases.push_back(card.x - std::nearbyint(static_cast<double>(card.x - roi.x) / pitch_x) * pitch_x);
        y_phases.push_back(card.y - std::nearbyint(static_cast<double>(card.y - roi.y) / pitch_y) * pitch_y);
    }
    const int x_phase = cvRound(Median(std::move(x_phases)));
    const int y_phase = cvRound(Median(std::move(y_phases)));
    if (cards.size() < 5) {
        const double maximum_residual = 0.04 * std::min(pitch_x, pitch_y);
        const bool coherent = std::ranges::all_of(cards, [&](const auto& card) {
            const int column = cvRound(static_cast<double>(card.x - x_phase) / pitch_x);
            const int row = cvRound(static_cast<double>(card.y - y_phase) / pitch_y);
            return std::abs(card.x - (x_phase + column * pitch_x)) <= maximum_residual
                   && std::abs(card.y - (y_phase + row * pitch_y)) <= maximum_residual;
        });
        if (coherent) {
            return BuildCreditTradeLattice(roi, x_phase, y_phase, profile);
        }
        return DetectSingleLattice(image, GridType::CreditTrade, roi);
    }
    std::vector<std::pair<int, int>> observed;
    for (const auto& card : cards) {
        const int row = cvRound(static_cast<double>(card.y - y_phase) / pitch_y);
        const int column = cvRound(static_cast<double>(card.x - x_phase) / pitch_x);
        if (row >= 0 && column >= 0 && column < kCreditTradeColumns) {
            observed.emplace_back(row, column);
        }
    }
    std::ranges::sort(observed);
    observed.erase(std::unique(observed.begin(), observed.end()), observed.end());
    if (observed.empty()) {
        throw std::runtime_error("credit_trade cards do not form a lattice");
    }
    const int first_row = observed.front().first;
    const int first_row_count =
        static_cast<int>(std::ranges::count_if(observed, [&](const auto& item) { return item.first == first_row; }));
    if (first_row_count >= 5) {
        for (int column = 0; column < kCreditTradeColumns; ++column) {
            observed.emplace_back(first_row, column);
        }
    }
    std::ranges::sort(observed);
    observed.erase(std::unique(observed.begin(), observed.end()), observed.end());
    std::vector<int> rows;
    for (const auto& [row, column] : observed) {
        rows.push_back(row);
    }
    std::ranges::sort(rows);
    rows.erase(std::unique(rows.begin(), rows.end()), rows.end());

    GridLayout layout;
    layout.grid_index = 0;
    layout.cell_size = profile.cell_size;
    layout.pitch_x = profile.pitch_x;
    layout.pitch_y = profile.pitch_y;
    layout.columns = kCreditTradeColumns;
    layout.rows = static_cast<int>(rows.size());
    for (const auto& [raw_row, column] : observed) {
        const int row = static_cast<int>(std::ranges::lower_bound(rows, raw_row) - rows.begin());
        const cv::Rect cell(
            x_phase + column * pitch_x + kCreditTradeCellOffsetX,
            y_phase + raw_row * pitch_y + kCreditTradeCellOffsetY,
            profile.cell_size,
            profile.cell_size);
        if (IsFormal(cell, roi)) {
            layout.cells.push_back({ 0, row, column, cell });
        }
    }
    if (layout.cells.empty()) {
        throw std::runtime_error("credit_trade ROI contains no formal cells");
    }
    int x1 = std::numeric_limits<int>::max();
    int y1 = std::numeric_limits<int>::max();
    int x2 = std::numeric_limits<int>::min();
    int y2 = std::numeric_limits<int>::min();
    for (const auto& cell : layout.cells) {
        x1 = std::min(x1, cell.cell_box.x), y1 = std::min(y1, cell.cell_box.y), x2 = std::max(x2, cell.cell_box.x + profile.cell_size),
        y2 = std::max(y2, cell.cell_box.y + profile.cell_size);
    }
    layout.bounds = cv::Rect(x1, y1, x2 - x1, y2 - y1);
    return layout;
}

double SampleSignal(const std::vector<float>& signal, double position)
{
    if (position < 0.0 || position > signal.size() - 1) {
        return 0.0;
    }
    const int left = static_cast<int>(std::floor(position));
    const int right = std::min(left + 1, static_cast<int>(signal.size()) - 1);
    const double fraction = position - left;
    return (1.0 - fraction) * signal[left] + fraction * signal[right];
}

int BoundaryCenter(const std::vector<float>& boundary, int position)
{
    const int left = std::max(0, position - 4);
    const int right = std::min(static_cast<int>(boundary.size()), position + 5);
    if (right <= left) {
        return position;
    }
    float maximum = 0.0F;
    for (int index = left; index < right; ++index) {
        maximum = std::max(maximum, boundary[index]);
    }
    if (maximum <= kEpsilon) {
        return position;
    }
    double sum = 0.0;
    int count = 0;
    for (int index = left; index < right; ++index) {
        if (boundary[index] >= maximum * 0.90F) {
            sum += index, ++count;
        }
    }
    return count ? static_cast<int>(std::floor(sum / count + 0.5)) : position;
}

std::vector<int> RefineFirstBoundary(const std::vector<int>& starts, const std::vector<float>& boundary, int offset, int cell_size)
{
    if (starts.empty()) {
        throw std::runtime_error("transfer coarse lattice has no axis start");
    }
    const int first = starts.front() - offset;
    std::vector<double> deltas {
        static_cast<double>(BoundaryCenter(boundary, first) - first),
        static_cast<double>(BoundaryCenter(boundary, first + cell_size) - (first + cell_size)),
    };
    const int shift = std::clamp(static_cast<int>(std::floor(Median(std::move(deltas)) + 0.5)), -4, 4);
    std::vector<int> result = starts;
    for (int& value : result) {
        value += shift;
    }
    return result;
}

std::vector<int> RefineStructuralPhase(
    const std::vector<int>& starts,
    const std::vector<float>& boundary,
    int offset,
    int cell_size,
    int maximum_shift = kDefaultStructuralPhaseMaximumShift,
    double minimum_gain = kDefaultStructuralPhaseMinimumGain,
    bool allow_small_shift = false)
{
    if (starts.empty()) {
        throw std::runtime_error("transfer coarse lattice has no vertical start");
    }
    const auto normalized = NormalizeSignal(boundary);
    if (*std::ranges::max_element(normalized) <= kEpsilon) {
        return starts;
    }
    std::vector<int> local_starts;
    for (int value : starts) {
        local_starts.push_back(value - offset);
    }
    const auto score = [&](int shift) {
        std::vector<double> pairs;
        for (int start : local_starts) {
            const int shifted = start + shift;
            const int end = shifted + cell_size;
            if (shifted < 0 || end >= static_cast<int>(normalized.size())) {
                continue;
            }
            pairs.push_back(std::sqrt(std::max(SampleSignal(normalized, shifted), 0.0) * std::max(SampleSignal(normalized, end), 0.0)));
        }
        return pairs.empty() ? 0.0 : std::accumulate(pairs.begin(), pairs.end(), 0.0) / pairs.size();
    };
    const double current = score(0);
    std::tuple<double, int, int> best { -1.0, std::numeric_limits<int>::min(), 0 };
    for (int shift = -maximum_shift; shift <= maximum_shift; ++shift) {
        best = std::max(best, std::tuple { score(shift), -std::abs(shift), shift });
    }
    const auto [best_score, ignored, best_shift] = best;
    if ((!allow_small_shift && std::abs(best_shift) <= kIgnoredStructuralPhaseShift)
        || best_score < kMinimumStructuralPhaseResponse || best_score < current + minimum_gain) {
        return starts;
    }
    std::vector<int> result = starts;
    for (int& value : result) {
        value += best_shift;
    }
    return result;
}

struct TransferAxisFit
{
    std::vector<int> starts;
    double phase = 0.0;
    double pitch = 0.0;
    double score = 0.0;
    double mean_residual = 0.0;
};

TransferAxisFit FitTransferAxis(
    const std::vector<int>& starts,
    const std::vector<float>& boundary,
    int offset,
    int cell_size,
    std::pair<double, double> pitch_range,
    int observed_pitch_tolerance,
    int maximum_count,
    bool refine_phase)
{
    std::vector<int> ordered = starts;
    std::ranges::sort(ordered);
    ordered.erase(std::unique(ordered.begin(), ordered.end()), ordered.end());
    if (ordered.empty() || boundary.empty()) {
        throw std::runtime_error("transfer axis fit has no evidence");
    }
    std::vector<double> observed;
    for (int value : ordered) {
        observed.push_back(value - offset);
    }
    std::vector<double> valid_spacings;
    for (std::size_t index = 1; index < observed.size(); ++index) {
        const double spacing = observed[index] - observed[index - 1];
        if (spacing >= pitch_range.first - observed_pitch_tolerance && spacing <= pitch_range.second + observed_pitch_tolerance) {
            valid_spacings.push_back(spacing);
        }
    }
    double coarse_pitch = valid_spacings.empty() ? 0.5 * (pitch_range.first + pitch_range.second) : Median(valid_spacings);
    coarse_pitch = std::clamp(coarse_pitch, pitch_range.first, pitch_range.second);
    std::vector<int> indices;
    for (double value : observed) {
        indices.push_back(static_cast<int>(std::nearbyint((value - observed.front()) / coarse_pitch)));
    }
    for (std::size_t index = 1; index < indices.size(); ++index) {
        indices[index] = std::max(indices[index], indices[index - 1]);
    }
    if (std::set<int>(indices.begin(), indices.end()).size() != indices.size()) {
        std::iota(indices.begin(), indices.end(), 0);
    }
    const int count = std::min(indices.back() + 1, maximum_count);
    const double mean_index = std::accumulate(indices.begin(), indices.end(), 0.0) / indices.size();
    const double mean_observed = std::accumulate(observed.begin(), observed.end(), 0.0) / observed.size();
    double denominator = 0.0;
    double numerator = 0.0;
    for (std::size_t index = 0; index < indices.size(); ++index) {
        denominator += (indices[index] - mean_index) * (indices[index] - mean_index);
        numerator += (indices[index] - mean_index) * (observed[index] - mean_observed);
    }
    double fitted_pitch = ordered.size() >= 6 || denominator <= kEpsilon ? coarse_pitch : numerator / denominator;
    fitted_pitch = std::clamp(fitted_pitch, pitch_range.first, pitch_range.second);
    double phase_center = 0.0;
    for (std::size_t index = 0; index < observed.size(); ++index) {
        phase_center += observed[index] - indices[index] * fitted_pitch;
    }
    phase_center /= observed.size();
    const auto normalized = NormalizeSignal(boundary);
    std::tuple<double, double, double> best { -std::numeric_limits<double>::infinity(), 0.0, 0.0 };
    double best_phase = phase_center;
    double best_score = 0.0;
    double best_residual = 0.0;
    const double phase_begin = refine_phase ? phase_center - 2.0 : phase_center;
    const double phase_end = refine_phase ? phase_center + 2.0001 : phase_center + 0.0001;
    for (double phase = phase_begin; phase <= phase_end; phase += refine_phase ? 0.25 : 1.0) {
        std::vector<double> pairs;
        for (int index = 0; index < count; ++index) {
            const double position = phase + index * fitted_pitch;
            pairs.push_back(std::sqrt(
                std::max(SampleSignal(normalized, position), 0.0) * std::max(SampleSignal(normalized, position + cell_size), 0.0)));
        }
        std::vector<double> residuals;
        for (std::size_t index = 0; index < observed.size(); ++index) {
            if (indices[index] < count) {
                residuals.push_back(std::abs(phase + indices[index] * fitted_pitch - observed[index]));
            }
        }
        const double residual = residuals.empty() ? 0.0 : std::accumulate(residuals.begin(), residuals.end(), 0.0) / residuals.size();
        const double evidence = pairs.empty() ? 0.0 : std::accumulate(pairs.begin(), pairs.end(), 0.0) / pairs.size();
        const double score = evidence - kTransferAxisResidualPenalty * residual;
        const auto candidate = std::tuple { score, -residual, -std::abs(phase - phase_center) };
        if (candidate > best) {
            best = candidate, best_phase = phase, best_score = score, best_residual = residual;
        }
    }
    TransferAxisFit fit { .phase = best_phase + offset, .pitch = fitted_pitch, .score = best_score, .mean_residual = best_residual };
    for (int index = 0; index < count; ++index) {
        fit.starts.push_back(static_cast<int>(std::floor(best_phase + index * fitted_pitch + 0.5)) + offset);
    }
    return fit;
}

std::vector<float> ProjectCellBorders(
    const cv::Mat& values,
    const std::vector<int>& orthogonal_starts,
    int offset,
    int cell_size,
    bool x_axis,
    bool preserve_sign = false)
{
    const int margin = std::max(kBorderProjectionMinimumMargin, cvRound(cell_size * kBorderProjectionMarginRatio));
    std::vector<std::vector<float>> samples;
    for (int start : orthogonal_starts) {
        const int local = start - offset;
        const int begin = std::max(0, local + margin);
        const int end = std::min(x_axis ? values.rows : values.cols, local + cell_size - margin);
        if (end <= begin) {
            continue;
        }
        std::vector<float> projected(x_axis ? values.cols : values.rows, 0.0F);
        if (x_axis) {
            for (int x = 0; x < values.cols; ++x) {
                for (int y = begin; y < end; ++y) {
                    projected[x] += values.at<float>(y, x) / (end - begin);
                }
            }
        }
        else {
            for (int y = 0; y < values.rows; ++y) {
                for (int x = begin; x < end; ++x) {
                    projected[y] += values.at<float>(y, x) / (end - begin);
                }
            }
        }
        samples.push_back(std::move(projected));
    }
    const int length = x_axis ? values.cols : values.rows;
    if (samples.empty()) {
        return std::vector<float>(length, 0.0F);
    }
    std::vector<float> result(length);
    std::vector<float> values_at(samples.size());
    for (int index = 0; index < length; ++index) {
        for (std::size_t sample = 0; sample < samples.size(); ++sample) {
            values_at[sample] = samples[sample][index];
        }
        std::ranges::sort(values_at);
        result[index] = values_at.size() % 2 == 0 ? 0.5F * (values_at[values_at.size() / 2 - 1] + values_at[values_at.size() / 2])
                                                  : values_at[values_at.size() / 2];
    }
    if (preserve_sign) {
        float maximum = 0.0F;
        for (float value : result) {
            maximum = std::max(maximum, std::abs(value));
        }
        if (maximum <= kEpsilon) {
            return std::vector<float>(length, 0.0F);
        }
        for (float& value : result) {
            value /= maximum;
        }
        return result;
    }

    const float minimum = std::min(0.0F, *std::ranges::min_element(result));
    for (float& value : result) {
        value -= minimum;
    }
    const float maximum = std::max(0.0F, *std::ranges::max_element(result));
    if (maximum <= kEpsilon) {
        return std::vector<float>(length, 0.0F);
    }
    for (float& value : result) {
        value = std::clamp(value / maximum, 0.0F, 1.0F);
    }
    return result;
}

std::vector<int> CompleteAxis(
    const std::vector<int>& starts,
    int maximum_count,
    std::optional<int> fixed_pitch,
    int preferred_pitch,
    int pitch_min,
    int pitch_max,
    int observed_pitch_tolerance,
    bool fit_phase)
{
    std::vector<int> ordered = starts;
    std::ranges::sort(ordered);
    ordered.erase(std::unique(ordered.begin(), ordered.end()), ordered.end());
    if (ordered.empty()) {
        throw std::runtime_error("transfer coarse lattice has no axis start");
    }
    std::vector<double> valid;
    for (std::size_t index = 1; index < ordered.size(); ++index) {
        const int spacing = ordered[index] - ordered[index - 1];
        if (spacing >= pitch_min - observed_pitch_tolerance && spacing <= pitch_max) {
            valid.push_back(spacing);
        }
    }
    const int pitch = std::clamp(
        fixed_pitch.value_or(valid.empty() ? preferred_pitch : static_cast<int>(std::floor(Median(valid) + 0.5))),
        pitch_min,
        pitch_max);
    if (fit_phase && ordered.size() > 1) {
        std::vector<int> indices;
        std::vector<double> phases;
        for (int value : ordered) {
            const int index = cvRound(static_cast<double>(value - ordered.front()) / pitch);
            indices.push_back(index);
            phases.push_back(value - index * pitch);
        }
        const int phase = static_cast<int>(std::floor(Median(std::move(phases)) + 0.5));
        std::vector<int> completed;
        for (int index = 0; index < std::min(indices.back() + 1, maximum_count); ++index) {
            completed.push_back(phase + index * pitch);
        }
        return completed;
    }
    std::vector<int> completed { ordered.front() };
    for (std::size_t index = 1; index < ordered.size(); ++index) {
        const int expected = completed.back() + pitch;
        completed.push_back(std::abs(ordered[index] - expected) <= 2 ? ordered[index] : expected);
    }
    return completed;
}

std::vector<int>
    RefinePortY(const std::vector<int>& starts, const std::vector<float>& boundary, int offset, int column_count, int cell_size)
{
    if (column_count != 4) {
        return RefineFirstBoundary(starts, boundary, offset, cell_size);
    }
    std::vector<std::pair<int, float>> evidence;
    for (int value : starts) {
        const int position = value - offset + cell_size;
        const int center = BoundaryCenter(boundary, position);
        const int left = std::max(0, position - 4);
        const int right = std::min(static_cast<int>(boundary.size()), position + 5);
        float score = 0.0F;
        for (int index = left; index < right; ++index) {
            score = std::max(score, boundary[index]);
        }
        if (score > 0.0F) {
            evidence.emplace_back(center - position, score);
        }
    }
    if (!evidence.empty()) {
        const float maximum = std::ranges::max_element(evidence, {}, &std::pair<int, float>::second)->second;
        std::vector<double> reliable;
        for (const auto& [delta, score] : evidence) {
            if (score >= maximum * 0.15F) {
                reliable.push_back(delta);
            }
        }
        if (reliable.size() >= 2 && *std::ranges::max_element(reliable) - *std::ranges::min_element(reliable) <= 1.0) {
            const int shift = std::clamp(static_cast<int>(std::floor(Median(std::move(reliable)) + 0.5)), -4, 4);
            std::vector<int> result = starts;
            for (int& value : result) {
                value += shift;
            }
            return result;
        }
    }
    return RefineFirstBoundary(starts, boundary, offset, cell_size);
}

double CellSupport(const cv::Mat& score, int x, int y)
{
    constexpr int kSupportRadius = 2;
    const int x1 = std::max(0, x - kSupportRadius);
    const int x2 = std::min(score.cols, x + kSupportRadius + 1);
    const int y1 = std::max(0, y - kSupportRadius);
    const int y2 = std::min(score.rows, y + kSupportRadius + 1);
    if (x2 <= x1 || y2 <= y1) {
        return 0.0;
    }
    double maximum = 0.0;
    cv::minMaxLoc(score(cv::Rect(x1, y1, x2 - x1, y2 - y1)), nullptr, &maximum);
    return maximum;
}

int AlignedTrustedStrips(
    const TrustedRarityGridFit& fit,
    const std::vector<int>& x_starts,
    const std::vector<int>& y_starts,
    const TransferGridProfile& profile)
{
    const auto nearest = [](int position, const std::vector<int>& starts) {
        int residual = std::numeric_limits<int>::max();
        for (int start : starts) {
            residual = std::min(residual, std::abs(position - start));
        }
        return residual;
    };
    int aligned = 0;
    for (const auto& strip : fit.strips) {
        const int cell_top = strip.box.y + strip.box.height - profile.rarity_anchor_offset;
        if (nearest(strip.box.x, x_starts) <= profile.phase_tolerance && nearest(cell_top, y_starts) <= profile.phase_tolerance) {
            ++aligned;
        }
    }
    return aligned;
}

double NormalizedStructureSupport(const cv::Mat& score, const std::vector<int>& x_starts, const std::vector<int>& y_starts)
{
    if (score.empty() || x_starts.empty() || y_starts.empty()) {
        return 0.0;
    }
    double maximum = 0.0;
    cv::minMaxLoc(score, nullptr, &maximum);
    if (maximum <= kEpsilon) {
        return 0.0;
    }
    double total = 0.0;
    for (int y : y_starts) {
        for (int x : x_starts) {
            total += CellSupport(score, x, y) / maximum;
        }
    }
    return total / static_cast<double>(x_starts.size() * y_starts.size());
}

std::vector<int> DropPortRows(
    const cv::Mat& image,
    const cv::Rect& roi,
    const std::vector<int>& x_starts,
    std::vector<int> y_starts,
    int column_count,
    int cell_size)
{
    // 端口面板的空白首/末行由格框支持与格内纹理共同判定，阈值按现有 720p 样本标定。
    constexpr double kSecondRowMinimumSupport = 0.20;
    constexpr double kFirstToSecondSupportRatio = 0.50;
    constexpr double kLastRowMaximumSupport = 0.08;
    constexpr double kTextureSplitRatio = 45.0 / 64.0;
    constexpr double kMinimumTextureDrop = 5.0;
    if (y_starts.size() < 2 || x_starts.empty()) {
        return y_starts;
    }
    const cv::Mat crop = image(roi);
    const cv::Mat score = BuildTransferCellScore(crop, cell_size);
    const auto row_support = [&](int y) {
        double total = 0.0;
        for (int x : x_starts) {
            total += CellSupport(score, x, y);
        }
        return total / x_starts.size();
    };
    if (column_count == 7 && row_support(y_starts[1]) >= kSecondRowMinimumSupport
        && row_support(y_starts[0]) < row_support(y_starts[1]) * kFirstToSecondSupportRatio) {
        y_starts.erase(y_starts.begin());
    }
    if (column_count != 4 || y_starts.size() < 2) {
        return y_starts;
    }
    const int y = y_starts.back();
    const double support = row_support(y);
    const int x1 = *std::ranges::min_element(x_starts);
    const int x2 = std::min(crop.cols, *std::ranges::max_element(x_starts) + cell_size);
    cv::Mat bgr;
    if (crop.channels() == 4) {
        cv::cvtColor(crop, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = crop;
    }
    cv::Mat gray;
    cv::cvtColor(bgr, gray, cv::COLOR_BGR2GRAY);
    const auto standard_deviation = [&](int top, int bottom) {
        const int clipped_top = std::clamp(top, 0, gray.rows);
        const int clipped_bottom = std::clamp(bottom, 0, gray.rows);
        if (clipped_bottom <= clipped_top || x2 <= x1) {
            return 0.0;
        }
        cv::Scalar mean;
        cv::Scalar deviation;
        cv::meanStdDev(gray(cv::Rect(x1, clipped_top, x2 - x1, clipped_bottom - clipped_top)), mean, deviation);
        return deviation[0];
    };
    const int texture_split = cvRound(cell_size * kTextureSplitRatio);
    const double texture_drop = standard_deviation(y, y + texture_split) - standard_deviation(y + texture_split, y + cell_size);
    if (support < kLastRowMaximumSupport && texture_drop > kMinimumTextureDrop) {
        y_starts.pop_back();
    }
    return y_starts;
}

GridLayout BuildTransferLayout(const cv::Mat& image, const cv::Rect& roi, const TransferGridHint& hint, int grid_index, GridType type)
{
    const bool transfer = type == GridType::Transfer;
    const int absolute_center = roi.x + hint.rect.x + hint.rect.width / 2;
    const bool left_side = absolute_center < image.cols / 2;
    const TransferGridVariant variant = transfer
                                            ? (left_side ? TransferGridVariant::TransferLeft : TransferGridVariant::TransferRight)
                                            : (left_side ? TransferGridVariant::PortStoragerLeft : TransferGridVariant::PortStoragerRight);
    const TransferGridProfile profile = TransferProfileFor(variant);
    const cv::Rect absolute_region(roi.x + hint.region.x, roi.y + hint.region.y, hint.region.width, hint.region.height);
    const StructureMaps maps = BuildStructureMaps(image(absolute_region), profile.cell_size);
    const auto boundary_x = RobustProjection(maps.vertical, true);
    const auto boundary_y = RobustProjection(maps.horizontal, false);
    const int column_count = static_cast<int>(hint.x_starts.size());
    const auto trusted_fit = FitTrustedRarityGrid(image(roi), hint.region, profile);
    const auto refined_x = RefineFirstBoundary(hint.x_starts, boundary_x, hint.region.x, profile.cell_size);
    const TransferAxisFit x_fit = FitTransferAxis(
        refined_x,
        boundary_x,
        hint.region.x,
        profile.cell_size,
        { static_cast<double>(profile.pitch_min), static_cast<double>(profile.pitch_max) },
        profile.observed_pitch_tolerance,
        static_cast<int>(refined_x.size()),
        !transfer);
    std::vector<int> local_x = x_fit.starts;
    std::vector<int> local_y;
    const auto rarity_fit = FitRarityGrid(image(roi), local_x, hint.y_starts, profile);
    if (rarity_fit) {
        local_x = rarity_fit->x_starts;
        const int count = std::min(profile.maximum_rows, std::max(static_cast<int>(hint.y_starts.size()), rarity_fit->supporting_rows));
        for (int row = 0; row < count; ++row) {
            local_y.push_back(rarity_fit->origin + row * rarity_fit->pitch);
        }
    }
    else {
        auto structural_y = transfer ? RefineStructuralPhase(hint.y_starts, boundary_y, hint.region.y, profile.cell_size) : hint.y_starts;
        auto refined_y = structural_y != hint.y_starts
                             ? structural_y
                             : RefinePortY(hint.y_starts, boundary_y, hint.region.y, column_count, profile.cell_size);
        local_y = CompleteAxis(
            refined_y,
            profile.maximum_rows,
            transfer ? std::optional<int>(static_cast<int>(std::floor(x_fit.pitch + 0.5))) : std::nullopt,
            profile.preferred_pitch,
            profile.pitch_min,
            profile.pitch_max,
            profile.observed_pitch_tolerance,
            column_count == 4 || column_count == 7);
        if (transfer && column_count >= 7) {
            local_y = RefineStructuralPhase(
                local_y,
                boundary_y,
                hint.region.y,
                profile.cell_size,
                kWideTransferPhaseMaximumShift,
                kWideTransferPhaseMinimumGain,
                true);
        }
    }
    const cv::Mat cell_score = BuildTransferCellScore(image(roi), profile.cell_size);
    bool trusted_selected = false;
    double trusted_candidate_score = 0.0;
    const double legacy_structure = NormalizedStructureSupport(cell_score, local_x, local_y);
    double legacy_rarity = 0.0;
    std::vector<std::string> rejected_reasons;
    if (trusted_fit) {
        legacy_rarity = static_cast<double>(AlignedTrustedStrips(*trusted_fit, local_x, local_y, profile)) / trusted_fit->supporting_cells;
        const double trusted_structure = NormalizedStructureSupport(cell_score, trusted_fit->x_starts, trusted_fit->y_starts);
        const double trusted_consistency = 0.5 * (trusted_fit->x_axis.confidence + trusted_fit->y_axis.confidence);
        trusted_candidate_score = kTrustedStructureWeight * trusted_structure
                                  + kTrustedConfidenceWeight * trusted_fit->mean_confidence
                                  + kTrustedConsistencyWeight * trusted_consistency;
        if (legacy_rarity == 0.0
            && (trusted_fit->supporting_cells >= kMinimumTrustedCellsWithoutLegacySupport
                || trusted_structure >= kMinimumTrustedStructureWithoutLegacySupport)) {
            local_x = trusted_fit->x_starts;
            local_y = trusted_fit->y_starts;
            trusted_selected = true;
        }
        else if (legacy_rarity > 0.0) {
            rejected_reasons.emplace_back("trusted-evidence-already-explained");
        }
        else {
            rejected_reasons.emplace_back("trusted-candidate-lacks-structure");
        }
    }
    else {
        rejected_reasons.emplace_back("no-trusted-chromatic-strip");
    }
    const double legacy_candidate_score = kLegacyStructureWeight * legacy_structure + kLegacyRarityWeight * legacy_rarity;
    if (local_y.size() < static_cast<std::size_t>(profile.maximum_rows)) {
        const auto row_support = [&](int y) {
            double total = 0.0;
            for (int x : local_x) {
                total += CellSupport(cell_score, x, y);
            }
            return local_x.empty() ? 0.0 : total / local_x.size();
        };
        double existing_support = 0.0;
        for (int y : local_y) {
            existing_support = std::max(existing_support, row_support(y));
        }
        const double minimum_support = existing_support * kRowCompletionSupportRatio;
        std::vector<double> spacings;
        for (std::size_t index = 1; index < local_y.size(); ++index) {
            spacings.push_back(local_y[index] - local_y[index - 1]);
        }
        const int pitch_y = spacings.empty() ? profile.preferred_pitch : static_cast<int>(std::floor(Median(spacings) + 0.5));
        const std::size_t completion_limit = trusted_selected && trusted_fit->y_axis.direct_indices.size() == 1
                                                  ? std::min<std::size_t>(kSingleObservationCompletionLimit, profile.maximum_rows)
                                                 : static_cast<std::size_t>(profile.maximum_rows);
        while (local_y.size() < completion_limit) {
            const int following = local_y.back() + std::clamp(pitch_y, profile.pitch_min, profile.pitch_max);
            if (hint.region.y + hint.region.height - following < profile.minimum_bottom_visibility * profile.cell_size) {
                break;
            }
            const bool stable_rarity_lattice =
                (rarity_fit && rarity_fit->supporting_rows >= static_cast<int>(kStableRarityMinimumRows))
                || (trusted_selected && trusted_fit->y_axis.direct_indices.size() >= kStableRarityMinimumRows);
            if (!stable_rarity_lattice && (minimum_support <= 0.0 || row_support(following) < minimum_support)) {
                break;
            }
            local_y.push_back(following);
        }
    }
    if (!transfer && !rarity_fit && !trusted_selected) {
        local_y = DropPortRows(image, roi, local_x, local_y, column_count, profile.cell_size);
    }

    const auto fit_final_axis = [&](const std::vector<int>& starts, int maximum_count) {
        std::vector<LatticeObservation> observations;
        observations.reserve(starts.size());
        for (int start : starts) {
            observations.push_back({ static_cast<double>(start), 1.0, true });
        }
        return FitRegularAxis(
            observations,
            maximum_count,
            { static_cast<double>(profile.pitch_min), static_cast<double>(profile.pitch_max) },
            profile.preferred_pitch);
    };
    const auto final_x_axis = fit_final_axis(local_x, std::max(1, static_cast<int>(local_x.size())));
    const auto final_y_axis = fit_final_axis(local_y, profile.maximum_rows);
    if (!final_x_axis || !final_y_axis) {
        throw std::runtime_error("transfer final lattice does not fit one global origin and pitch");
    }
    local_x = ProjectRegularAxis(*final_x_axis);
    local_y = ProjectRegularAxis(*final_y_axis);

    GridLayout layout;
    layout.grid_index = grid_index;
    layout.cell_size = profile.cell_size;
    for (int row = 0; row < static_cast<int>(local_y.size()); ++row) {
        for (int column = 0; column < static_cast<int>(local_x.size()); ++column) {
            const cv::Rect cell(roi.x + local_x[column], roi.y + local_y[row], profile.cell_size, profile.cell_size);
            if (IsFormal(cell, roi, profile.minimum_top_visibility, profile.minimum_bottom_visibility)) {
                layout.cells.push_back({ grid_index, row, column, cell });
            }
        }
    }
    if (layout.cells.empty()) {
        throw std::runtime_error("transfer hint contains no formal cells");
    }
    layout.pitch_x = final_x_axis->pitch;
    layout.pitch_y = final_y_axis->pitch;
    layout.columns = static_cast<int>(local_x.size());
    layout.rows = static_cast<int>(local_y.size());
    int x1 = std::numeric_limits<int>::max();
    int y1 = std::numeric_limits<int>::max();
    int x2 = std::numeric_limits<int>::min();
    int y2 = std::numeric_limits<int>::min();
    for (const auto& cell : layout.cells) {
        x1 = std::min(x1, cell.cell_box.x), y1 = std::min(y1, cell.cell_box.y), x2 = std::max(x2, cell.cell_box.x + layout.cell_size),
        y2 = std::max(y2, cell.cell_box.y + layout.cell_size);
    }
    layout.bounds = cv::Rect(x1, y1, x2 - x1, y2 - y1);
    const double final_structure = NormalizedStructureSupport(cell_score, local_x, local_y);
    const double final_rarity = trusted_fit ? static_cast<double>(AlignedTrustedStrips(*trusted_fit, local_x, local_y, profile))
                                                  / trusted_fit->supporting_cells * trusted_fit->mean_confidence
                                            : 0.0;
    const double maximum_residual = std::max(final_x_axis->maximum_residual, final_y_axis->maximum_residual);
    const double consistency = std::clamp(1.0 - maximum_residual / kMaximumRegularAxisResidual, 0.0, 1.0);
    const double selected_score = trusted_selected ? trusted_candidate_score : legacy_candidate_score;
    const double other_score = trusted_selected ? legacy_candidate_score : trusted_candidate_score;
    layout.selection_diagnostics = GridSelectionDiagnostics {
        .origin = cv::Point2d(roi.x + final_x_axis->origin, roi.y + final_y_axis->origin),
        .pitch = cv::Point2d(final_x_axis->pitch, final_y_axis->pitch),
        .rows = layout.rows,
        .columns = layout.columns,
        .best_score = selected_score,
        .second_score = other_score,
        .score_margin = selected_score - other_score,
        .structure_score = final_structure,
        .rarity_score = final_rarity,
        .consistency_score = consistency,
        .maximum_residual = maximum_residual,
        .residual_trend = std::max(std::abs(final_x_axis->residual_trend), std::abs(final_y_axis->residual_trend)),
        .trusted_rarity_cells = trusted_fit ? trusted_fit->rarity_counts : std::array<int, 6> {},
        .fallback_used = !trusted_selected,
        .fallback_reason = trusted_selected ? "" : "legacy-structure-without-conflicting-trusted-rarity",
        .rejected_reasons = std::move(rejected_reasons),
    };
    return layout;
}

void Append(GridDetection& result, GridLayout layout)
{
    result.cells.insert(result.cells.end(), layout.cells.begin(), layout.cells.end());
    result.grids.push_back(std::move(layout));
}

} // namespace

GridDetection DetectGrid(const cv::Mat& image, GridType type, const cv::Rect& roi)
{
    if (image.empty()) {
        throw std::invalid_argument("cannot detect grid in empty image");
    }
    const cv::Rect bounds(0, 0, image.cols, image.rows);
    if ((roi & bounds) != roi || roi.width <= 0 || roi.height <= 0) {
        throw std::invalid_argument("grid ROI is outside image");
    }
    GridDetection result { type, roi, {}, {} };
    if (type == GridType::CreditTrade) {
        Append(result, DetectCreditTrade(image, roi));
    }
    else if (type == GridType::Transfer || type == GridType::PortStorager) {
        const auto hints = DiscoverTransferGridHints(image(roi), type == GridType::Transfer);
        for (int index = 0; index < static_cast<int>(hints.size()); ++index) {
            Append(result, BuildTransferLayout(image, roi, hints[index], index, type));
        }
    }
    else {
        GridLayout layout = DetectSingleLattice(image, type, roi);
        if (type == GridType::Trade || type == GridType::Valuables || type == GridType::Shipment) {
            RefineCardVerticalPhase(image, roi, type, layout);
        }
        Append(result, std::move(layout));
    }
    return result;
}

} // namespace iconrecognition::detail
