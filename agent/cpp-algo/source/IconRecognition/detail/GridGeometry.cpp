#include "GridGeometry.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <numeric>

namespace iconrecognition::detail
{
namespace
{

constexpr double kEpsilon = 1e-8;
// 格框响应由离线样本标定的四类证据组合：对边与单向边是主体，亮度对比补强，斜纹理用于抑制误检。
constexpr double kPairedBorderWeight = 0.45;
constexpr double kDirectionalBorderWeight = 0.35;
constexpr double kContrastWeight = 0.20;
constexpr double kDiagonalPenaltyWeight = 0.20;

double huber(double value)
{
    const double absolute = std::abs(value);
    return absolute <= 1.0 ? 0.5 * absolute * absolute : absolute - 0.5;
}

AxisSequence fallback_axis(int length, int cell_size, int expected_pitch, int minimum_count)
{
    const int limit = std::max(length - cell_size, 0);
    std::vector<int> starts;
    for (int value = 0; value <= limit; value += std::max(expected_pitch, 1)) {
        starts.push_back(value);
    }
    if (static_cast<int>(starts.size()) < minimum_count) {
        starts.clear();
        if (minimum_count == 1) {
            starts.push_back(0);
        }
        else {
            for (int index = 0; index < minimum_count; ++index) {
                starts.push_back(static_cast<int>(std::floor(index * static_cast<double>(limit) / (minimum_count - 1) + 0.5)));
            }
        }
    }
    AxisSequence sequence;
    sequence.integer_starts = starts;
    for (int value : starts) {
        sequence.continuous_starts.push_back(value);
    }
    sequence.local_scores.assign(starts.size(), 0.0);
    for (std::size_t index = 1; index < starts.size(); ++index) {
        sequence.spacings.push_back(starts[index] - starts[index - 1]);
    }
    return sequence;
}

} // namespace

std::vector<float> NormalizeSignal(const std::vector<float>& source)
{
    if (source.empty()) {
        return {};
    }
    const float minimum = std::min(0.0F, *std::ranges::min_element(source));
    const float maximum = std::max(0.0F, *std::ranges::max_element(source));
    if (maximum - minimum <= kEpsilon) {
        return std::vector<float>(source.size(), 0.0F);
    }
    std::vector<float> result(source.size());
    std::ranges::transform(source, result.begin(), [&](float value) { return (value - minimum) / (maximum - minimum); });
    return result;
}

double Median(std::vector<double> values)
{
    if (values.empty()) {
        return 0.0;
    }
    std::ranges::sort(values);
    return values.size() % 2 == 0 ? 0.5 * (values[values.size() / 2 - 1] + values[values.size() / 2]) : values[values.size() / 2];
}

int EstimatePeriod(const std::vector<float>& signal, int minimum_period, int maximum_period)
{
    const double mean = std::accumulate(signal.begin(), signal.end(), 0.0) / signal.size();
    std::vector<double> centered(signal.size());
    std::ranges::transform(signal, centered.begin(), [&](float value) { return value - mean; });
    const double energy = std::inner_product(centered.begin(), centered.end(), centered.begin(), 0.0);
    if (energy <= kEpsilon) {
        return minimum_period;
    }
    int best_period = minimum_period;
    double best_score = -std::numeric_limits<double>::infinity();
    for (int period = minimum_period; period <= maximum_period; ++period) {
        double correlation = 0.0;
        for (int index = 0; index + period < static_cast<int>(centered.size()); ++index) {
            correlation += centered[index] * centered[index + period];
        }
        const int overlap = std::max(static_cast<int>(centered.size()) - period, 1);
        const double score = correlation / energy * centered.size() / overlap;
        if (score > best_score) {
            best_score = score;
            best_period = period;
        }
    }
    return best_period;
}

AxisSequence FitSubpixelAxis(
    const std::vector<float>& boundary_signal,
    const std::vector<float>& signed_signal,
    const std::vector<float>& support_signal,
    const std::vector<float>& diagonal_penalty,
    int cell_size,
    int expected_pitch,
    std::pair<int, int> pitch_range,
    int minimum_count)
{
    const int length = static_cast<int>(boundary_signal.size());
    if (length <= cell_size) {
        return fallback_axis(length, cell_size, expected_pitch, minimum_count);
    }
    const auto boundary = NormalizeSignal(boundary_signal);
    float signed_scale = 0.0F;
    for (float value : signed_signal) {
        signed_scale = std::max(signed_scale, std::abs(value));
    }
    std::vector<float> signed_values(signed_signal.size(), 0.0F);
    if (signed_scale > kEpsilon) {
        std::ranges::transform(signed_signal, signed_values.begin(), [&](float value) { return value / signed_scale; });
    }
    const auto support = NormalizeSignal(support_signal);
    const int limit = length - cell_size - 1;
    std::vector<float> local(limit + 1, 0.0F);
    for (int start = 0; start <= limit; ++start) {
        const int end = start + cell_size;
        const double pair = std::sqrt(std::max(boundary[start], 0.0F) * std::max(boundary[end], 0.0F));
        const double forward = std::sqrt(std::max(signed_values[start], 0.0F) * std::max(-signed_values[end], 0.0F));
        const double reverse = std::sqrt(std::max(-signed_values[start], 0.0F) * std::max(signed_values[end], 0.0F));
        double inside = 0.0;
        const int inside_begin = start + 2;
        const int inside_end = std::max(start + 3, end - 2);
        for (int index = inside_begin; index < inside_end; ++index) {
            inside += support[index];
        }
        inside /= std::max(inside_end - inside_begin, 1);
        double outside = 0.0;
        int outside_count = 0;
        for (int index = std::max(0, start - 3); index < start; ++index) {
            outside += support[index], ++outside_count;
        }
        for (int index = end + 1; index < std::min(length, end + 4); ++index) {
            outside += support[index], ++outside_count;
        }
        outside = outside_count ? outside / outside_count : 0.0;
        const double contrast = outside_count ? std::abs(inside - outside) : 0.0;
        const double diagonal = 0.5 * (diagonal_penalty[start] + diagonal_penalty[end]);
        local[start] = static_cast<float>(std::max(
            kPairedBorderWeight * pair + kDirectionalBorderWeight * std::max(forward, reverse) + kContrastWeight * contrast
                - kDiagonalPenaltyWeight * diagonal,
            0.0));
    }
    if (*std::ranges::max_element(local) <= kEpsilon) {
        return fallback_axis(length, cell_size, expected_pitch, minimum_count);
    }

    const int maximum_count = limit / pitch_range.first + 1;
    const int stride = maximum_count + 1;
    const double negative_infinity = -std::numeric_limits<double>::infinity();
    std::vector<double> quality((limit + 1) * stride, negative_infinity);
    std::vector<int> previous((limit + 1) * stride, -1);
    const auto offset = [&](int start, int count) {
        return start * stride + count;
    };
    for (int start = 0; start <= limit; ++start) {
        quality[offset(start, 1)] = local[start];
    }
    for (int count = 2; count <= maximum_count; ++count) {
        for (int start = 0; start <= limit; ++start) {
            double best_quality = negative_infinity;
            int best_prior = -1;
            for (int spacing = pitch_range.first; spacing <= pitch_range.second; ++spacing) {
                const int prior = start - spacing;
                if (prior < 0 || !std::isfinite(quality[offset(prior, count - 1)])) {
                    continue;
                }
                double gap = 0.0;
                int gap_count = 0;
                for (int index = prior + cell_size + 1; index < start; ++index) {
                    gap += std::abs(boundary_signal[index]), ++gap_count;
                }
                const double candidate = quality[offset(prior, count - 1)] + local[start] - (gap_count ? 0.30 * gap / gap_count : 0.0)
                                         - 0.05 * huber(spacing - expected_pitch);
                if (candidate > best_quality) {
                    best_quality = candidate, best_prior = prior;
                }
            }
            if (best_prior >= 0) {
                quality[offset(start, count)] = best_quality, previous[offset(start, count)] = best_prior;
            }
        }
    }
    int count = 0;
    for (int candidate = minimum_count; candidate <= maximum_count; ++candidate) {
        bool available = false;
        for (int start = 0; start <= limit; ++start) {
            if (std::isfinite(quality[offset(start, candidate)])) {
                available = true;
                break;
            }
        }
        if (available) {
            count = candidate;
        }
    }
    if (count == 0) {
        return fallback_axis(length, cell_size, expected_pitch, minimum_count);
    }
    std::vector<int> endpoints;
    for (int start = 0; start <= limit; ++start) {
        if (std::isfinite(quality[offset(start, count)])) {
            endpoints.push_back(start);
        }
    }
    std::ranges::sort(endpoints, [&](int left, int right) { return quality[offset(left, count)] > quality[offset(right, count)]; });
    int endpoint = endpoints.front();
    const double best_quality = quality[offset(endpoint, count)];
    const double second_quality = endpoints.size() > 1 ? quality[offset(endpoints[1], count)] : best_quality;
    const double margin = std::max((best_quality - second_quality) / std::max(count, 1), 0.0);
    std::vector<int> integer_starts;
    for (int remaining = count; remaining > 0 && endpoint >= 0; --remaining) {
        integer_starts.push_back(endpoint);
        endpoint = previous[offset(endpoint, remaining)];
    }
    std::ranges::reverse(integer_starts);

    AxisSequence sequence;
    std::vector<double> ambiguities;
    for (int start : integer_starts) {
        double position = start;
        double ambiguity = 2.0;
        if (start > 0 && start + 1 < static_cast<int>(local.size())) {
            const double left = local[start - 1];
            const double center = local[start];
            const double right = local[start + 1];
            const double denominator = left - 2.0 * center + right;
            if (denominator < -kEpsilon) {
                position += std::clamp(0.5 * (left - right) / denominator, -0.5, 0.5);
                const double curvature = std::max(-denominator, 0.0);
                ambiguity = std::clamp(1.0 / std::sqrt(curvature + 1e-6), 0.25, 4.0);
            }
        }
        position += 0.5;
        sequence.continuous_starts.push_back(position);
        sequence.integer_starts.push_back(static_cast<int>(std::floor(position + 0.5)));
        sequence.local_scores.push_back(local[start]);
        ambiguities.push_back(ambiguity);
    }
    for (std::size_t index = 1; index < sequence.continuous_starts.size(); ++index) {
        sequence.spacings.push_back(sequence.continuous_starts[index] - sequence.continuous_starts[index - 1]);
    }
    const double evidence = std::accumulate(sequence.local_scores.begin(), sequence.local_scores.end(), 0.0) / sequence.local_scores.size();
    sequence.ambiguity_width = std::accumulate(ambiguities.begin(), ambiguities.end(), 0.0) / ambiguities.size();
    sequence.best_vs_second_margin = margin;
    sequence.confidence = std::clamp(0.85 * evidence + 0.15 * std::min(margin, 1.0), 0.0, 1.0);
    sequence.low_confidence = sequence.confidence < 0.12;
    return sequence;
}

GridLayout BuildLattice(int grid_index, cv::Point origin, int rows, int columns, int cell_size, double pitch_x, double pitch_y)
{
    GridLayout layout;
    layout.grid_index = grid_index;
    layout.cell_size = cell_size;
    layout.pitch_x = pitch_x;
    layout.pitch_y = pitch_y;
    layout.rows = rows;
    layout.columns = columns;
    layout.bounds = cv::Rect(
        origin.x,
        origin.y,
        std::max(0, cvRound((columns - 1) * pitch_x) + cell_size),
        std::max(0, cvRound((rows - 1) * pitch_y) + cell_size));
    layout.cells.reserve(rows * columns);
    for (int row = 0; row < rows; ++row) {
        for (int column = 0; column < columns; ++column) {
            const cv::Rect box(origin.x + cvRound(column * pitch_x), origin.y + cvRound(row * pitch_y), cell_size, cell_size);
            layout.cells.push_back({ grid_index, row, column, box });
        }
    }
    return layout;
}

} // namespace iconrecognition::detail
