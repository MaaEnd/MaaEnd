#include "GridProfiles.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <map>
#include <optional>
#include <set>
#include <stdexcept>
#include <tuple>

#include "GridFeatures.h"
#include "GridGeometry.h"

namespace iconrecognition::detail
{
namespace
{

constexpr double kEpsilon = 1e-8;

struct Peak
{
    int x = 0;
    int y = 0;
    float score = 0.0F;
};

struct TransferHypothesis
{
    cv::Rect rect;
    double score = 0.0;
    double occupancy = 0.0;
    int columns = 0;
    int rows = 0;
    std::vector<int> x_starts;
    std::vector<int> y_starts;
};

std::vector<Peak> local_peaks(const cv::Mat& score, int maximum)
{
    if (score.empty()) {
        return {};
    }
    double maximum_score = 0.0;
    cv::minMaxLoc(score, nullptr, &maximum_score);
    if (maximum_score <= kEpsilon) {
        return {};
    }
    std::vector<float> positive;
    for (int y = 0; y < score.rows; ++y) {
        for (int x = 0; x < score.cols; ++x) {
            const float value = score.at<float>(y, x);
            if (value > 0.0F) {
                positive.push_back(value);
            }
        }
    }
    const double threshold = std::max(0.22 * maximum_score, Percentile(std::move(positive), 92.0));
    cv::Mat dilated;
    cv::dilate(score, dilated, cv::Mat::ones(5, 5, CV_8U));
    std::vector<Peak> peaks;
    for (int y = 0; y < score.rows; ++y) {
        for (int x = 0; x < score.cols; ++x) {
            const float value = score.at<float>(y, x);
            if (value >= threshold && value >= dilated.at<float>(y, x) - 1e-7F) {
                peaks.push_back({ x, y, value });
            }
        }
    }
    std::ranges::sort(peaks, [](const Peak& left, const Peak& right) { return left.score > right.score; });
    if (static_cast<int>(peaks.size()) > maximum) {
        peaks.resize(maximum);
    }
    return peaks;
}

std::vector<Peak> suppress_close_peaks(const std::vector<Peak>& peaks, int radius = 6)
{
    std::vector<Peak> kept;
    for (const Peak& peak : peaks) {
        if (std::ranges::all_of(kept, [&](const Peak& other) {
                return std::abs(peak.x - other.x) > radius || std::abs(peak.y - other.y) > radius;
            })) {
            kept.push_back(peak);
        }
    }
    return kept;
}

std::vector<std::vector<std::pair<int, int>>> lattice_components(const std::map<std::pair<int, int>, Peak>& slots)
{
    std::set<std::pair<int, int>> remaining;
    for (const auto& [key, value] : slots) {
        remaining.insert(key);
    }
    std::vector<std::vector<std::pair<int, int>>> components;
    while (!remaining.empty()) {
        const auto start = *remaining.begin();
        remaining.erase(remaining.begin());
        std::vector<std::pair<int, int>> component { start };
        std::vector<std::pair<int, int>> pending { start };
        while (!pending.empty()) {
            const auto current = pending.back();
            pending.pop_back();
            std::vector<std::pair<int, int>> linked;
            for (const auto& point : remaining) {
                if ((point.first == current.first && std::abs(point.second - current.second) <= 2)
                    || (point.second == current.second && std::abs(point.first - current.first) <= 2)) {
                    linked.push_back(point);
                }
            }
            for (const auto& point : linked) {
                remaining.erase(point);
                component.push_back(point);
                pending.push_back(point);
            }
        }
        components.push_back(std::move(component));
    }
    return components;
}

int rounded_median(const std::vector<int>& values)
{
    std::vector<double> converted(values.begin(), values.end());
    return cvRound(Median(std::move(converted)));
}

std::vector<TransferHypothesis>
    phase_hypotheses(const cv::Mat& crop, std::pair<int, int> pitch_range, int minimum_columns, int minimum_rows)
{
    const auto peaks = suppress_close_peaks(local_peaks(BuildTransferCellScore(crop, 64), 1000));
    if (peaks.empty()) {
        return {};
    }
    std::map<std::array<int, 4>, TransferHypothesis> hypotheses;
    const int seed_count = std::min(static_cast<int>(peaks.size()), 160);
    for (int seed_index = 0; seed_index < seed_count; ++seed_index) {
        const Peak& seed = peaks[seed_index];
        for (int pitch_x = pitch_range.first; pitch_x <= pitch_range.second; ++pitch_x) {
            for (int pitch_y = pitch_range.first; pitch_y <= pitch_range.second; ++pitch_y) {
                std::map<std::pair<int, int>, Peak> slots;
                for (const Peak& peak : peaks) {
                    const int column = static_cast<int>(std::nearbyint(static_cast<double>(peak.x - seed.x) / pitch_x));
                    const int row = static_cast<int>(std::nearbyint(static_cast<double>(peak.y - seed.y) / pitch_y));
                    if (std::abs(peak.x - (seed.x + column * pitch_x)) > 4 || std::abs(peak.y - (seed.y + row * pitch_y)) > 4) {
                        continue;
                    }
                    const auto key = std::pair { column, row };
                    const auto found = slots.find(key);
                    if (found == slots.end() || peak.score > found->second.score) {
                        slots[key] = peak;
                    }
                }
                for (const auto& component : lattice_components(slots)) {
                    std::vector<int> columns;
                    std::vector<int> rows;
                    for (const auto& [column, row] : component) {
                        columns.push_back(column), rows.push_back(row);
                    }
                    std::ranges::sort(columns);
                    columns.erase(std::unique(columns.begin(), columns.end()), columns.end());
                    std::ranges::sort(rows);
                    rows.erase(std::unique(rows.begin(), rows.end()), rows.end());
                    if (static_cast<int>(columns.size()) < minimum_columns || static_cast<int>(rows.size()) < minimum_rows) {
                        continue;
                    }
                    const auto maximum_gap = [](const std::vector<int>& values) {
                        int maximum = 1;
                        for (std::size_t index = 1; index < values.size(); ++index) {
                            maximum = std::max(maximum, values[index] - values[index - 1]);
                        }
                        return maximum;
                    };
                    if (maximum_gap(columns) > 2 || maximum_gap(rows) > 2) {
                        continue;
                    }
                    const double occupancy = static_cast<double>(component.size()) / (columns.size() * rows.size());
                    if (occupancy < 0.42) {
                        continue;
                    }
                    std::vector<int> x_starts;
                    std::vector<int> y_starts;
                    for (int column : columns) {
                        std::vector<int> values;
                        for (const auto& key : component) {
                            if (key.first == column) {
                                values.push_back(slots.at(key).x);
                            }
                        }
                        x_starts.push_back(rounded_median(values));
                    }
                    for (int row : rows) {
                        std::vector<int> values;
                        for (const auto& key : component) {
                            if (key.second == row) {
                                values.push_back(slots.at(key).y);
                            }
                        }
                        y_starts.push_back(rounded_median(values));
                    }
                    double mean_score = 0.0;
                    for (const auto& key : component) {
                        mean_score += slots.at(key).score;
                    }
                    mean_score /= component.size();
                    const double score = occupancy * mean_score * std::sqrt(static_cast<double>(columns.size() * rows.size()));
                    TransferHypothesis hypothesis {
                        .rect = cv::Rect(
                            x_starts.front(),
                            y_starts.front(),
                            x_starts.back() + 65 - x_starts.front(),
                            y_starts.back() + 65 - y_starts.front()),
                        .score = score,
                        .occupancy = occupancy,
                        .columns = static_cast<int>(columns.size()),
                        .rows = static_cast<int>(rows.size()),
                        .x_starts = std::move(x_starts),
                        .y_starts = std::move(y_starts),
                    };
                    const std::array key { hypothesis.rect.x / 4,
                                           hypothesis.rect.y / 4,
                                           (hypothesis.rect.x + hypothesis.rect.width) / 4,
                                           (hypothesis.rect.y + hypothesis.rect.height) / 4 };
                    const auto found = hypotheses.find(key);
                    if (found == hypotheses.end() || hypothesis.score > found->second.score) {
                        hypotheses[key] = std::move(hypothesis);
                    }
                }
            }
        }
    }
    std::vector<TransferHypothesis> result;
    for (auto& [key, hypothesis] : hypotheses) {
        result.push_back(std::move(hypothesis));
    }
    std::ranges::sort(result, [](const auto& left, const auto& right) { return left.score > right.score; });
    return result;
}

std::vector<cv::Rect> discover_transfer_regions(const cv::Mat& crop)
{
    if (crop.cols <= 700) {
        return { cv::Rect(0, 0, crop.cols, crop.rows) };
    }
    const auto hypotheses = phase_hypotheses(crop, { 66, 74 }, 3, 3);
    std::vector<TransferHypothesis> localized;
    for (const auto& item : hypotheses) {
        if (item.columns <= 8 && item.rows <= 5 && item.rect.width <= crop.cols * 0.70) {
            localized.push_back(item);
        }
    }
    if (localized.empty()) {
        throw std::runtime_error("transfer ROI contains no local grid candidate");
    }
    std::vector<TransferHypothesis> independent;
    const double threshold = localized.front().score * 0.15;
    for (const auto& hypothesis : localized) {
        if (hypothesis.score < threshold) {
            break;
        }
        if (std::ranges::any_of(independent, [&](const auto& accepted) { return !(hypothesis.rect & accepted.rect).empty(); })) {
            continue;
        }
        independent.push_back(hypothesis);
    }
    if (independent.size() > 2) {
        throw std::runtime_error("transfer ROI contains more than two strong grids");
    }

    std::optional<std::pair<TransferHypothesis, TransferHypothesis>> best_pair;
    double best_pair_score = -1.0;
    for (const auto& left : localized) {
        for (const auto& right : localized) {
            if (left.rect.x >= right.rect.x || right.rect.x - (left.rect.x + left.rect.width) < 32) {
                continue;
            }
            const double weaker = std::min(left.score, right.score);
            const double stronger = std::max(left.score, right.score);
            if (weaker < stronger * 0.15) {
                continue;
            }
            const bool spanned = std::ranges::any_of(localized, [&](const auto& item) {
                return &item != &left && &item != &right && item.score >= stronger && item.rect.x <= left.rect.x + 16
                       && item.rect.x + item.rect.width >= right.rect.x + right.rect.width - 16;
            });
            if (spanned) {
                continue;
            }
            if (left.score + right.score > best_pair_score) {
                best_pair_score = left.score + right.score, best_pair = std::pair { left, right };
            }
        }
    }
    if (best_pair) {
        const auto& [left, right] = *best_pair;
        const int left_x2 = std::clamp(left.rect.x + left.rect.width + 12, 1, crop.cols - 1);
        const int right_x1 = std::clamp(right.rect.x - 12, left_x2 + 1, crop.cols - 1);
        return { cv::Rect(0, 0, left_x2, crop.rows), cv::Rect(right_x1, 0, crop.cols - right_x1, crop.rows) };
    }
    const auto& dominant = localized.front();
    const double center = dominant.rect.x + dominant.rect.width / 2.0;
    if (center >= crop.cols / 2.0) {
        const int left_x2 = std::max(1, dominant.rect.x - 64);
        const int right_x1 = std::clamp(dominant.rect.x - 12, left_x2 + 1, crop.cols - 1);
        return { cv::Rect(0, 0, left_x2, crop.rows), cv::Rect(right_x1, 0, crop.cols - right_x1, crop.rows) };
    }
    const int left_x2 = std::min(crop.cols - 1, dominant.rect.x + dominant.rect.width + 12);
    const int right_x1 = std::clamp(dominant.rect.x + dominant.rect.width + 52, left_x2 + 1, crop.cols - 1);
    return { cv::Rect(0, 0, left_x2, crop.rows), cv::Rect(right_x1, 0, crop.cols - right_x1, crop.rows) };
}

std::optional<TransferHypothesis> select_grid_hypothesis(const cv::Mat& crop)
{
    const int maximum_columns = std::max(1, (crop.cols - 64) / 68 + 1);
    const int maximum_rows = std::max(1, (crop.rows - 64) / 68 + 1);
    const auto candidates = [&](int minimum) {
        std::vector<TransferHypothesis> filtered;
        for (const auto& item : phase_hypotheses(crop, { 68, 70 }, minimum, minimum)) {
            if (item.columns <= std::min(8, maximum_columns) && item.rows <= std::min(5, maximum_rows)
                && item.rect.x + item.rect.width <= crop.cols && item.rect.y + item.rect.height <= crop.rows) {
                filtered.push_back(item);
            }
        }
        return filtered;
    };
    auto hypotheses = candidates(3);
    if (hypotheses.empty()) {
        hypotheses = candidates(1);
    }
    if (hypotheses.empty()) {
        return std::nullopt;
    }
    const auto rank = [](const TransferHypothesis& item) {
        return std::tuple {
            item.columns, -item.rect.x, item.columns == 7 ? 0 : item.rows, item.occupancy, item.score, item.rows, -item.rect.y,
        };
    };
    return *std::ranges::max_element(hypotheses, {}, rank);
}

TransferGridHint to_hint(const TransferHypothesis& hypothesis, const cv::Rect& region, int offset_x)
{
    TransferGridHint hint {
        .region = region,
        .rect = cv::Rect(offset_x + hypothesis.rect.x, hypothesis.rect.y, hypothesis.rect.width, hypothesis.rect.height),
        .score = hypothesis.score,
        .occupancy = hypothesis.occupancy,
        .x_starts = hypothesis.x_starts,
        .y_starts = hypothesis.y_starts,
    };
    for (int& value : hint.x_starts) {
        value += offset_x;
    }
    return hint;
}

} // namespace

GridProfile ProfileFor(GridType type)
{
    switch (type) {
    case GridType::Trade:
        return { 96, 310.0, 109.0, 3, 3 };
    case GridType::Transfer:
        return { 64, 69.0, 69.0, 3, 3 };
    case GridType::PortStorager:
        return { 64, 69.0, 69.0, 3, 3 };
    case GridType::Valuables:
        return { 96, 103.5, 103.5, 7, 4 };
    case GridType::Shipment:
        return { 64, 73.6, 112.0, 4, 3 };
    case GridType::CreditTrade:
        return { 128, 161.0, 205.0, 7, 1 };
    case GridType::SingleRoi:
        throw std::invalid_argument("single_roi does not use a grid profile");
    }
    throw std::invalid_argument("unknown grid type");
}

cv::Mat BuildTransferCellScore(const cv::Mat& image, int cell_size)
{
    const StructureMaps maps = BuildStructureMaps(image, cell_size);
    const int extent = cell_size + 1;
    cv::Mat vertical_support;
    cv::Mat horizontal_support;
    cv::boxFilter(maps.vertical, vertical_support, CV_32F, cv::Size(1, extent), cv::Point(0, 0), true, cv::BORDER_CONSTANT);
    cv::boxFilter(maps.horizontal, horizontal_support, CV_32F, cv::Size(extent, 1), cv::Point(0, 0), true, cv::BORDER_CONSTANT);
    const int valid_height = maps.vertical.rows - cell_size;
    const int valid_width = maps.vertical.cols - cell_size;
    if (valid_height <= 0 || valid_width <= 0) {
        return {};
    }
    cv::Mat result(valid_height, valid_width, CV_32F);
    for (int y = 0; y < valid_height; ++y) {
        for (int x = 0; x < valid_width; ++x) {
            const double product = std::max(
                static_cast<double>(vertical_support.at<float>(y, x)) * vertical_support.at<float>(y, x + cell_size)
                    * horizontal_support.at<float>(y, x) * horizontal_support.at<float>(y + cell_size, x),
                0.0);
            result.at<float>(y, x) = static_cast<float>(std::pow(product, 0.25));
        }
    }
    cv::GaussianBlur(result, result, cv::Size(), 0.8);
    return result;
}

std::vector<TransferGridHint> DiscoverTransferGridHints(const cv::Mat& crop, bool structural_rank)
{
    const auto regions = discover_transfer_regions(crop);
    const auto broad = phase_hypotheses(crop, { 66, 74 }, 3, 3);
    std::vector<TransferGridHint> hints;
    for (const auto& raw_region : regions) {
        const cv::Rect region(raw_region.x, 0, raw_region.width, crop.rows);
        std::vector<TransferGridHint> candidates;
        if (const auto local = select_grid_hypothesis(crop(region))) {
            candidates.push_back(to_hint(*local, region, region.x));
        }
        for (const auto& hypothesis : broad) {
            if (hypothesis.rect.x >= region.x && hypothesis.rect.x + hypothesis.rect.width <= region.x + region.width) {
                candidates.push_back(to_hint(hypothesis, region, 0));
            }
        }
        if (candidates.empty()) {
            throw std::runtime_error("transfer subregion contains no coarse lattice");
        }
        const auto rank = [&](const TransferGridHint& hint) {
            const int columns = static_cast<int>(hint.x_starts.size());
            std::vector<double> spacings;
            for (std::size_t index = 1; index < hint.x_starts.size(); ++index) {
                spacings.push_back(hint.x_starts[index] - hint.x_starts[index - 1]);
            }
            const double pitch = spacings.empty() ? 69.0 : Median(spacings);
            if (structural_rank) {
                return std::tuple<double, double, double, double, double, double, double> {
                    static_cast<double>(columns),
                    static_cast<double>(columns == 7 ? 0 : hint.y_starts.size()),
                    hint.occupancy,
                    hint.score,
                    -std::abs(pitch - 69.0),
                    static_cast<double>(-(hint.rect.x - hint.region.x)),
                    static_cast<double>(hint.y_starts.size()),
                };
            }
            return std::tuple<double, double, double, double, double, double, double> {
                static_cast<double>(columns),
                static_cast<double>(-(hint.rect.x - hint.region.x)),
                static_cast<double>(columns == 7 ? 0 : hint.y_starts.size()),
                hint.occupancy,
                hint.score,
                static_cast<double>(hint.y_starts.size()),
                static_cast<double>(-hint.rect.y),
            };
        };
        hints.push_back(*std::ranges::max_element(candidates, {}, rank));
    }
    return hints;
}

} // namespace iconrecognition::detail
