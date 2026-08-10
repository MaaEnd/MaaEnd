#pragma once

#include <optional>
#include <utility>
#include <vector>

namespace iconrecognition::detail
{

// 全局规则轴允许的最大单点残差（像素）；调大提高畸变容忍度，调小可更早拒绝错位晶格。
inline constexpr double kMaximumRegularAxisResidual = 2.25;

struct LatticeObservation
{
    double position = 0.0;
    double weight = 0.0;
    bool direct = false;
};

struct RegularAxisFit
{
    double origin = 0.0;
    double pitch = 0.0;
    int minimum_index = 0;
    int maximum_index = 0;
    double mean_residual = 0.0;
    double maximum_residual = 0.0;
    // 观测首尾间距与拟合轴首尾间距的差值，衡量 pitch 误差的累计幅度。
    double endpoint_drift = 0.0;
    double residual_trend = 0.0;
    double support_ratio = 0.0;
    double confidence = 0.0;
    bool low_geometry_confidence = true;
    std::vector<int> direct_indices;
};

std::optional<RegularAxisFit> FitRegularAxis(
    const std::vector<LatticeObservation>& observations,
    int maximum_count,
    std::pair<double, double> pitch_range,
    double preferred_pitch);
std::vector<int> ProjectRegularAxis(const RegularAxisFit& fit);

} // namespace iconrecognition::detail
