#include "RegularLattice.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <numeric>
#include <set>
#include <tuple>

namespace iconrecognition::detail
{
namespace
{

// 相距不超过该像素值的轴观测会合并；调大抑制重复峰，调小保留更近的独立证据。
constexpr double kObservationClusterRadius = 0.75;
// 全局 pitch 搜索步长（像素）；调小提高拟合精度但增加候选数量，调大则相反。
constexpr double kPitchStep = 0.05;
// 浮点 pitch 循环的闭区间容差，只用于包含上界，不参与识别评分。
constexpr double kPitchLoopEpsilon = 1e-9;
// 直接观测覆盖率在轴拟合置信度中的权重；调高更偏好证据密集的晶格。
constexpr double kSupportConfidenceWeight = 0.45;
// 平均残差质量在轴拟合置信度中的权重；调高更严格惩罚几何偏差。
constexpr double kResidualConfidenceWeight = 0.30;
// 接近 profile 首选 pitch 的程度在置信度中的权重；调高会增强先验偏好。
constexpr double kPitchConfidenceWeight = 0.25;
// 只有一个直接色带观测时使用的保守置信度；调高会让单点晶格更容易胜出。
constexpr double kDirectSingletonConfidence = 0.55;
// 只有一个间接结构观测时使用的低置信度；调高会增加弱证据生成晶格的风险。
constexpr double kIndirectSingletonConfidence = 0.25;

std::vector<LatticeObservation> NormalizeObservations(const std::vector<LatticeObservation>& source)
{
    std::vector<LatticeObservation> ordered;
    for (const auto& observation : source) {
        if (std::isfinite(observation.position) && std::isfinite(observation.weight) && observation.weight > 0.0) {
            ordered.push_back(observation);
        }
    }
    std::ranges::sort(ordered, {}, &LatticeObservation::position);
    std::vector<LatticeObservation> clustered;
    for (const auto& observation : ordered) {
        if (clustered.empty() || observation.position - clustered.back().position > kObservationClusterRadius) {
            clustered.push_back(observation);
            continue;
        }
        auto& current = clustered.back();
        const double weight = current.weight + observation.weight;
        current.position = (current.position * current.weight + observation.position * observation.weight) / weight;
        current.weight = weight;
        current.direct = current.direct || observation.direct;
    }
    return clustered;
}

std::optional<RegularAxisFit> FitCandidate(
    const std::vector<LatticeObservation>& observations,
    double seed,
    double coarse_pitch,
    int maximum_count,
    std::pair<double, double> pitch_range,
    double preferred_pitch)
{
    std::vector<int> indices;
    indices.reserve(observations.size());
    for (const auto& observation : observations) {
        indices.push_back(static_cast<int>(std::nearbyint((observation.position - seed) / coarse_pitch)));
    }
    const int minimum = *std::ranges::min_element(indices);
    for (int& index : indices) {
        index -= minimum;
    }
    if (std::set<int>(indices.begin(), indices.end()).size() != indices.size()) {
        return std::nullopt;
    }
    const int maximum = *std::ranges::max_element(indices);
    if (maximum + 1 > maximum_count) {
        return std::nullopt;
    }
    for (std::size_t index = 1; index < indices.size(); ++index) {
        if (indices[index] - indices[index - 1] > 2) {
            return std::nullopt;
        }
    }

    double weight_sum = 0.0;
    double weighted_index = 0.0;
    double weighted_position = 0.0;
    for (std::size_t index = 0; index < observations.size(); ++index) {
        weight_sum += observations[index].weight;
        weighted_index += observations[index].weight * indices[index];
        weighted_position += observations[index].weight * observations[index].position;
    }
    const double mean_index = weighted_index / weight_sum;
    const double mean_position = weighted_position / weight_sum;
    double numerator = 0.0;
    double denominator = 0.0;
    for (std::size_t index = 0; index < observations.size(); ++index) {
        const double centered_index = indices[index] - mean_index;
        numerator += observations[index].weight * centered_index * (observations[index].position - mean_position);
        denominator += observations[index].weight * centered_index * centered_index;
    }
    if (denominator <= std::numeric_limits<double>::epsilon()) {
        return std::nullopt;
    }
    const double pitch = numerator / denominator;
    if (pitch < pitch_range.first || pitch > pitch_range.second) {
        return std::nullopt;
    }
    const double origin = mean_position - mean_index * pitch;

    double weighted_residual = 0.0;
    double maximum_residual = 0.0;
    double trend_numerator = 0.0;
    for (std::size_t index = 0; index < observations.size(); ++index) {
        const double signed_residual = observations[index].position - (origin + indices[index] * pitch);
        const double residual = std::abs(signed_residual);
        weighted_residual += observations[index].weight * residual;
        maximum_residual = std::max(maximum_residual, residual);
        trend_numerator += observations[index].weight * (indices[index] - mean_index) * signed_residual;
    }
    if (maximum_residual > kMaximumRegularAxisResidual) {
        return std::nullopt;
    }
    const double mean_residual = weighted_residual / weight_sum;
    const double endpoint_drift =
        std::abs((observations.back().position - observations.front().position) - (indices.back() - indices.front()) * pitch);
    const double residual_trend = trend_numerator / denominator;
    std::vector<int> direct_indices;
    for (std::size_t index = 0; index < observations.size(); ++index) {
        if (observations[index].direct) {
            direct_indices.push_back(indices[index]);
        }
    }
    std::ranges::sort(direct_indices);
    direct_indices.erase(std::unique(direct_indices.begin(), direct_indices.end()), direct_indices.end());
    const int span = maximum + 1;
    const double support_ratio = static_cast<double>(direct_indices.size()) / span;
    const double pitch_span = std::max(pitch_range.second - pitch_range.first, 1.0);
    const double pitch_confidence = std::clamp(1.0 - std::abs(pitch - preferred_pitch) / pitch_span, 0.0, 1.0);
    const double confidence = std::clamp(
        kSupportConfidenceWeight * support_ratio + kResidualConfidenceWeight * (1.0 - mean_residual / kMaximumRegularAxisResidual)
            + kPitchConfidenceWeight * pitch_confidence,
        0.0,
        1.0);
    return RegularAxisFit {
        .origin = origin,
        .pitch = pitch,
        .minimum_index = 0,
        .maximum_index = maximum,
        .mean_residual = mean_residual,
        .maximum_residual = maximum_residual,
        .endpoint_drift = endpoint_drift,
        .residual_trend = residual_trend,
        .support_ratio = support_ratio,
        .confidence = confidence,
        .low_geometry_confidence = direct_indices.size() < 2,
        .direct_indices = std::move(direct_indices),
    };
}

} // namespace

std::optional<RegularAxisFit> FitRegularAxis(
    const std::vector<LatticeObservation>& source,
    int maximum_count,
    std::pair<double, double> pitch_range,
    double preferred_pitch)
{
    const auto observations = NormalizeObservations(source);
    if (observations.empty() || maximum_count <= 0 || pitch_range.first <= 0.0 || pitch_range.second < pitch_range.first) {
        return std::nullopt;
    }
    if (observations.size() == 1) {
        return RegularAxisFit {
            .origin = observations.front().position,
            .pitch = preferred_pitch,
            .minimum_index = 0,
            .maximum_index = 0,
            .support_ratio = observations.front().direct ? 1.0 : 0.0,
            .confidence = observations.front().direct ? kDirectSingletonConfidence : kIndirectSingletonConfidence,
            .low_geometry_confidence = true,
            .direct_indices = observations.front().direct ? std::vector<int> { 0 } : std::vector<int> {},
        };
    }

    std::optional<RegularAxisFit> best;
    std::tuple<double, double, double, double, double, double> best_rank {
        -1.0,
        -1.0,
        -std::numeric_limits<double>::infinity(),
        -std::numeric_limits<double>::infinity(),
        -std::numeric_limits<double>::infinity(),
        -std::numeric_limits<double>::infinity(),
    };
    for (double pitch = pitch_range.first; pitch <= pitch_range.second + kPitchLoopEpsilon; pitch += kPitchStep) {
        for (const auto& seed : observations) {
            const auto candidate = FitCandidate(observations, seed.position, pitch, maximum_count, pitch_range, preferred_pitch);
            if (!candidate) {
                continue;
            }
            const auto rank = std::tuple {
                candidate->support_ratio,   candidate->confidence,     -candidate->maximum_residual,
                -candidate->endpoint_drift, -candidate->mean_residual, -std::abs(candidate->pitch - preferred_pitch),
            };
            if (rank > best_rank) {
                best_rank = rank;
                best = candidate;
            }
        }
    }
    return best;
}

std::vector<int> ProjectRegularAxis(const RegularAxisFit& fit)
{
    std::vector<int> starts;
    if (fit.maximum_index < fit.minimum_index) {
        return starts;
    }
    starts.reserve(static_cast<std::size_t>(fit.maximum_index - fit.minimum_index + 1));
    for (int index = fit.minimum_index; index <= fit.maximum_index; ++index) {
        starts.push_back(static_cast<int>(std::floor(fit.origin + index * fit.pitch + 0.5)));
    }
    return starts;
}

} // namespace iconrecognition::detail
