#pragma once

#include <optional>
#include <utility>
#include <vector>

namespace iconrecognition::detail
{

struct LatticeObservation
{
    double position = 0.0;
    double weight = 0.0;
    bool direct = false;
};

struct RegularAxisFit
{
    double origin = 0.0;
    double pitch = 69.0;
    int minimum_index = 0;
    int maximum_index = 0;
    double mean_residual = 0.0;
    double maximum_residual = 0.0;
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
