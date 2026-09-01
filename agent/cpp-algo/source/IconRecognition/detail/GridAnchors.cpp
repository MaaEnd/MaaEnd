#include "GridAnchors.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <map>
#include <numeric>
#include <tuple>
#include <vector>

#include "RarityClassifier.h"
#include "RegularLattice.h"
#include "TrustedRarity.h"

namespace iconrecognition::detail
{
namespace
{

// 可信色带轴拟合的 pitch 搜索步长（像素）；调小提高精度但增加搜索次数，调大则相反。
constexpr double kTrustedAxisPitchStep = 0.25;
// 浮点 pitch 循环的闭区间容差，仅确保搜索包含 profile 上界。
constexpr double kPitchLoopEpsilon = 1e-9;

struct RarityLine
{
    int y = 0;
    int supported_columns = 0;
    int strong_columns = 0;
    int chromatic_columns = 0;
    double mean_coverage = 0.0;
};

struct RarityBand
{
    int top = 0;
    int bottom = 0;
    int supported_columns = 0;
    int strong_columns = 0;
    int chromatic_columns = 0;
    double mean_coverage = 0.0;
};

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

bool HasFormalVerticalExtent(int cell_top, int image_height, const TransferGridProfile& profile)
{
    const int visible_top = std::max(0, cell_top);
    const int visible_bottom = std::min(image_height, cell_top + profile.cell_size);
    const double visibility = static_cast<double>(std::max(0, visible_bottom - visible_top)) / profile.cell_size;
    const bool top_ok = cell_top >= 0 || visibility >= profile.minimum_top_visibility;
    const bool bottom_ok = cell_top + profile.cell_size <= image_height || visibility >= profile.minimum_bottom_visibility;
    return top_ok && bottom_ok;
}

std::vector<RarityBand> DetectRarityBands(const cv::Mat& lab, const std::vector<int>& x_starts, const TransferGridProfile& profile)
{
    if (lab.empty() || x_starts.empty()) {
        return {};
    }
    const int minimum_supported_columns = x_starts.size() == 1 ? 1 : 2;
    std::vector<RarityLine> candidates;
    for (int y = 0; y < lab.rows; ++y) {
        std::vector<double> coverages;
        int strong_columns = 0;
        int chromatic_columns = 0;
        for (int x : x_starts) {
            const int left = std::max(0, x);
            const int right = std::min(lab.cols, x + profile.cell_size);
            if (right > left) {
                const auto evidence = MeasureRarityRow(lab(cv::Rect(left, y, right - left, 1)));
                coverages.push_back(evidence.maximumCoverage());
                if (evidence.maximumChromaticCoverage() >= profile.strong_rarity_coverage) {
                    ++strong_columns;
                }
                if (evidence.maximumChromaticCoverage() >= profile.minimum_rarity_coverage) {
                    ++chromatic_columns;
                }
            }
        }
        const int supported = static_cast<int>(
            std::ranges::count_if(coverages, [&](double coverage) { return coverage >= profile.minimum_rarity_coverage; }));
        if (supported < minimum_supported_columns) {
            continue;
        }
        std::ranges::sort(coverages, std::greater {});
        const std::size_t count = std::min<std::size_t>(3, coverages.size());
        const double mean = std::accumulate(coverages.begin(), coverages.begin() + count, 0.0) / count;
        candidates.push_back({ y, supported, strong_columns, chromatic_columns, mean });
    }

    std::vector<RarityBand> bands;
    for (std::size_t begin = 0; begin < candidates.size();) {
        std::size_t end = begin + 1;
        while (end < candidates.size() && candidates[end].y == candidates[end - 1].y + 1) {
            ++end;
        }
        const auto maximum =
            std::max_element(candidates.begin() + begin, candidates.begin() + end, [](const auto& left, const auto& right) {
                return std::tie(left.chromatic_columns, left.strong_columns, left.supported_columns)
                       < std::tie(right.chromatic_columns, right.strong_columns, right.supported_columns);
            });
        const int maximum_chromatic = maximum->chromatic_columns;
        const int maximum_strong = maximum->strong_columns;
        int maximum_support = 0;
        for (auto iterator = candidates.begin() + begin; iterator != candidates.begin() + end; ++iterator) {
            if (iterator->chromatic_columns == maximum_chromatic && iterator->strong_columns == maximum_strong) {
                maximum_support = std::max(maximum_support, iterator->supported_columns);
            }
        }
        // 大面积同色背景可能把真实色带和后续噪声连在一起；优先保留彩色列峰的平台。
        for (std::size_t peak_begin = begin; peak_begin < end;) {
            while (peak_begin < end
                   && (candidates[peak_begin].chromatic_columns != maximum_chromatic
                       || candidates[peak_begin].strong_columns != maximum_strong
                       || candidates[peak_begin].supported_columns != maximum_support)) {
                ++peak_begin;
            }
            if (peak_begin == end) {
                break;
            }
            std::size_t peak_end = peak_begin + 1;
            while (peak_end < end && candidates[peak_end].y == candidates[peak_end - 1].y + 1
                   && candidates[peak_end].chromatic_columns == maximum_chromatic && candidates[peak_end].strong_columns == maximum_strong
                   && candidates[peak_end].supported_columns == maximum_support) {
                ++peak_end;
            }
            const auto best =
                std::max_element(candidates.begin() + peak_begin, candidates.begin() + peak_end, [](const auto& left, const auto& right) {
                    return std::tie(left.mean_coverage, left.y) < std::tie(right.mean_coverage, right.y);
                });
            const int bottom = candidates[peak_end - 1].y + 1;
            if (HasFormalVerticalExtent(bottom - profile.rarity_anchor_offset, lab.rows, profile)) {
                bands.push_back({
                    candidates[peak_begin].y,
                    bottom,
                    maximum_support,
                    maximum_strong,
                    maximum_chromatic,
                    best->mean_coverage,
                });
            }
            peak_begin = peak_end;
        }
        begin = end;
    }
    return bands;
}

int RoundedMedian(const std::vector<int>& values)
{
    std::vector<int> ordered = values;
    std::ranges::sort(ordered);
    const std::size_t middle = ordered.size() / 2;
    if (ordered.size() % 2 == 1) {
        return ordered[middle];
    }
    return static_cast<int>(std::floor(0.5 * (ordered[middle - 1] + ordered[middle]) + 0.5));
}

struct VerticalBandFit
{
    int origin = 0;
    int pitch = 0;
    int supporting_rows = 0;
    int supporting_cells = 0;
    int supporting_strong_cells = 0;
    int supporting_chromatic_cells = 0;
    int residual_sum = 0;
    int origin_residual = 0;
    double mean_coverage = 0.0;
};

std::optional<VerticalBandFit> FitVerticalBands(
    const std::vector<RarityBand>& bands,
    const std::vector<int>& coarse_y_starts,
    const TransferGridProfile& profile,
    int minimum_rows)
{
    using Rank = std::tuple<int, int, int, int, double, int, int, int>;
    Rank best_rank {
        -1, -1, -1, -1, -1.0, std::numeric_limits<int>::min(), std::numeric_limits<int>::min(), std::numeric_limits<int>::min(),
    };
    std::optional<VerticalBandFit> best;
    for (int pitch = profile.pitch_min; pitch <= profile.pitch_max; ++pitch) {
        for (const auto& seed : bands) {
            const int seed_start = seed.bottom - profile.rarity_anchor_offset;
            std::map<int, std::pair<RarityBand, int>> slots;
            for (const auto& band : bands) {
                const int start = band.bottom - profile.rarity_anchor_offset;
                const int index = cvRound(static_cast<double>(start - seed_start) / pitch);
                const int residual = std::abs(start - (seed_start + index * pitch));
                if (residual > profile.phase_tolerance) {
                    continue;
                }
                const auto found = slots.find(index);
                const auto rank = std::tuple {
                    band.strong_columns, band.supported_columns, band.chromatic_columns, band.mean_coverage, -residual,
                };
                if (found == slots.end()
                    || rank > std::tuple {
                           found->second.first.strong_columns,
                           found->second.first.supported_columns,
                           found->second.first.chromatic_columns,
                           found->second.first.mean_coverage,
                           -found->second.second,
                       }) {
                    slots[index] = { band, residual };
                }
            }
            if (static_cast<int>(slots.size()) < minimum_rows) {
                continue;
            }
            const int span = slots.rbegin()->first - slots.begin()->first + 1;
            if (span > profile.maximum_rows + 1) {
                continue;
            }
            std::vector<int> origins;
            int supporting_cells = 0;
            int supporting_strong_cells = 0;
            int supporting_chromatic_cells = 0;
            double coverage_sum = 0.0;
            for (const auto& [index, item] : slots) {
                const auto& band = item.first;
                origins.push_back(band.bottom - profile.rarity_anchor_offset - index * pitch);
                supporting_cells += band.supported_columns;
                supporting_strong_cells += band.strong_columns;
                supporting_chromatic_cells += band.chromatic_columns;
                coverage_sum += band.mean_coverage;
            }
            const int phase_origin = RoundedMedian(origins);
            int residual_sum = 0;
            for (const auto& [index, item] : slots) {
                const int observed = item.first.bottom - profile.rarity_anchor_offset;
                residual_sum += std::abs(phase_origin + index * pitch - observed);
            }
            const int origin = phase_origin + cvRound(static_cast<double>(coarse_y_starts.front() - phase_origin) / pitch) * pitch;
            const int origin_residual = std::abs(origin - coarse_y_starts.front());
            const double mean_coverage = coverage_sum / slots.size();
            const Rank rank {
                supporting_strong_cells, supporting_cells, supporting_chromatic_cells, static_cast<int>(slots.size()),
                mean_coverage,           -residual_sum,    -origin_residual,           -std::abs(pitch - profile.preferred_pitch),
            };
            if (rank > best_rank) {
                best_rank = rank;
                best = VerticalBandFit {
                    .origin = origin,
                    .pitch = pitch,
                    .supporting_rows = static_cast<int>(slots.size()),
                    .supporting_cells = supporting_cells,
                    .supporting_strong_cells = supporting_strong_cells,
                    .supporting_chromatic_cells = supporting_chromatic_cells,
                    .residual_sum = residual_sum,
                    .origin_residual = origin_residual,
                    .mean_coverage = mean_coverage,
                };
            }
        }
    }
    return best;
}

std::optional<RegularAxisFit>
    BestTrustedAxis(const std::vector<LatticeObservation>& observations, int maximum_count, const TransferGridProfile& profile)
{
    if (observations.empty()) {
        return std::nullopt;
    }
    std::optional<RegularAxisFit> best;
    std::tuple<std::size_t, double, double> best_rank { 0, -1.0, -1.0 };
    for (const auto& seed : observations) {
        for (double pitch = profile.pitch_min; pitch <= profile.pitch_max + kPitchLoopEpsilon; pitch += kTrustedAxisPitchStep) {
            std::vector<LatticeObservation> consistent;
            double total_weight = 0.0;
            for (const auto& observation : observations) {
                const int index = cvRound((observation.position - seed.position) / pitch);
                const double residual = std::abs(observation.position - (seed.position + index * pitch));
                if (residual <= profile.phase_tolerance) {
                    consistent.push_back(observation);
                    total_weight += observation.weight;
                }
            }
            const auto fit = FitRegularAxis(
                consistent,
                maximum_count,
                { static_cast<double>(profile.pitch_min), static_cast<double>(profile.pitch_max) },
                profile.preferred_pitch,
                profile.observed_pitch_tolerance);
            if (!fit) {
                continue;
            }
            const auto rank = std::tuple { fit->direct_indices.size(), total_weight, fit->confidence };
            if (rank > best_rank) {
                best_rank = rank;
                best = fit;
            }
        }
    }
    return best;
}

double NearestResidual(int position, const std::vector<int>& starts)
{
    double residual = std::numeric_limits<double>::infinity();
    for (int start : starts) {
        residual = std::min(residual, static_cast<double>(std::abs(position - start)));
    }
    return residual;
}

} // namespace

std::optional<TrustedRarityGridFit> FitTrustedRarityGrid(const cv::Mat& image, const cv::Rect& region, const TransferGridProfile& profile)
{
    if (image.empty() || (region & cv::Rect(0, 0, image.cols, image.rows)) != region || region.width <= 0 || region.height <= 0) {
        return std::nullopt;
    }
    std::vector<TrustedRarityStrip> strips;
    for (const auto& strip : DetectTrustedRarityStrips(image, profile.cell_size)) {
        const cv::Point center = strip.box.tl() + cv::Point(strip.box.width / 2, strip.box.height / 2);
        if (strip.can_seed_lattice && region.contains(center)) {
            strips.push_back(strip);
        }
    }
    if (strips.empty()) {
        return std::nullopt;
    }

    std::vector<LatticeObservation> x_observations;
    std::vector<LatticeObservation> y_observations;
    for (const auto& strip : strips) {
        x_observations.push_back({ static_cast<double>(strip.box.x), strip.confidence, true });
        y_observations.push_back(
            { static_cast<double>(strip.box.y + strip.box.height - profile.rarity_anchor_offset), strip.confidence, true });
    }
    const int maximum_columns = std::max(1, (region.width - profile.cell_size) / profile.pitch_min + 1);
    const auto x_axis = BestTrustedAxis(x_observations, maximum_columns, profile);
    const auto y_axis = BestTrustedAxis(y_observations, profile.maximum_rows, profile);
    if (!x_axis || !y_axis) {
        return std::nullopt;
    }
    const auto x_starts = ProjectRegularAxis(*x_axis);
    const auto y_starts = ProjectRegularAxis(*y_axis);
    std::vector<TrustedRarityStrip> aligned;
    std::array<int, 6> rarity_counts {};
    double confidence_sum = 0.0;
    for (const auto& strip : strips) {
        const int cell_top = strip.box.y + strip.box.height - profile.rarity_anchor_offset;
        if (NearestResidual(strip.box.x, x_starts) <= profile.phase_tolerance
            && NearestResidual(cell_top, y_starts) <= profile.phase_tolerance) {
            aligned.push_back(strip);
            ++rarity_counts[static_cast<std::size_t>(strip.rarity - 1)];
            confidence_sum += strip.confidence;
        }
    }
    if (aligned.empty()) {
        return std::nullopt;
    }
    const int supporting_cells = static_cast<int>(aligned.size());
    return TrustedRarityGridFit {
        .x_axis = *x_axis,
        .y_axis = *y_axis,
        .x_starts = x_starts,
        .y_starts = y_starts,
        .strips = std::move(aligned),
        .rarity_counts = rarity_counts,
        .supporting_cells = supporting_cells,
        .mean_confidence = confidence_sum / supporting_cells,
    };
}

std::optional<RarityGridFit> FitRarityGrid(
    const cv::Mat& image,
    const std::vector<int>& x_starts,
    const std::vector<int>& coarse_y_starts,
    const TransferGridProfile& profile)
{
    if (image.empty() || x_starts.empty() || coarse_y_starts.empty()) {
        return std::nullopt;
    }
    const cv::Mat lab = ToLab32(image);
    const int column_count = static_cast<int>(x_starts.size());
    const int coarse_phase = x_starts.front();
    std::vector<int> coarse_spacings;
    for (std::size_t index = 1; index < x_starts.size(); ++index) {
        coarse_spacings.push_back(x_starts[index] - x_starts[index - 1]);
    }
    const int coarse_pitch = coarse_spacings.empty() ? profile.preferred_pitch : RoundedMedian(coarse_spacings);
    using Rank = std::tuple<int, int, double, int, int, int, int, int, int>;
    Rank best_rank {
        -1,
        std::numeric_limits<int>::min(),
        -1.0,
        -1,
        -1,
        std::numeric_limits<int>::min(),
        std::numeric_limits<int>::min(),
        std::numeric_limits<int>::min(),
        std::numeric_limits<int>::min(),
    };
    Rank reference_rank = best_rank;
    RarityGridFit best;
    std::optional<RarityGridFit> reference;
    const int reference_pitch = std::clamp(coarse_pitch, profile.pitch_min, profile.pitch_max);
    for (int pitch_x = profile.pitch_min; pitch_x <= profile.pitch_max; ++pitch_x) {
        const int phase_radius = pitch_x / 2;
        for (int shift = -phase_radius; shift <= phase_radius; ++shift) {
            std::vector<int> candidate_x;
            for (int column = 0; column < column_count; ++column) {
                candidate_x.push_back(coarse_phase + shift + column * pitch_x);
            }
            if (candidate_x.front() < 0 || candidate_x.back() + profile.cell_size > image.cols) {
                continue;
            }
            const auto bands = DetectRarityBands(lab, candidate_x, profile);
            const int minimum_rows = column_count == 1 ? 1 : 2;
            const auto vertical = FitVerticalBands(bands, coarse_y_starts, profile, minimum_rows);
            if (!vertical) {
                continue;
            }
            const Rank rank {
                vertical->supporting_strong_cells, -std::abs(pitch_x - coarse_pitch),    vertical->mean_coverage,
                vertical->supporting_cells,        vertical->supporting_chromatic_cells, vertical->supporting_rows,
                -vertical->residual_sum,           -vertical->origin_residual,           -std::abs(shift),
            };
            const RarityGridFit current {
                .x_starts = candidate_x,
                .origin = vertical->origin,
                .pitch_x = pitch_x,
                .pitch = vertical->pitch,
                .supporting_rows = vertical->supporting_rows,
                .supporting_cells = vertical->supporting_cells,
                .supporting_strong_cells = vertical->supporting_strong_cells,
                .supporting_chromatic_cells = vertical->supporting_chromatic_cells,
                .mean_coverage = vertical->mean_coverage,
            };
            if (rank > best_rank) {
                best_rank = rank;
                best = current;
            }
            if (pitch_x == reference_pitch && shift == 0 && rank > reference_rank) {
                reference_rank = rank;
                reference = current;
            }
        }
    }
    if (best.supporting_rows == 0) {
        return std::nullopt;
    }
    if (reference && (best.x_starts != reference->x_starts || best.pitch_x != reference->pitch_x)) {
        const int minimum_cell_gain = std::max(1, (column_count + 1) / 2);
        // 完整色带和弱色带任一证据显著增强时才接管结构相位，避免跨格弱覆盖形成整张假网格。
        const bool strong_evidence_improved = best.supporting_strong_cells >= reference->supporting_strong_cells + minimum_cell_gain;
        const bool supported_evidence_improved = best.supporting_cells >= reference->supporting_cells + minimum_cell_gain;
        if (!strong_evidence_improved && !supported_evidence_improved) {
            best = *reference;
        }
    }
    return best;
}

} // namespace iconrecognition::detail
