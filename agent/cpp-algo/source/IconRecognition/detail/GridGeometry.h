#pragma once

#include <utility>

#include "GridTypes.h"

namespace iconrecognition::detail
{

struct AxisSequence
{
    std::vector<double> continuous_starts;
    std::vector<int> integer_starts;
    std::vector<double> local_scores;
    std::vector<double> spacings;
    double confidence = 0.0;
    double ambiguity_width = 4.0;
    double best_vs_second_margin = 0.0;
    bool low_confidence = true;
};

std::vector<float> NormalizeSignal(const std::vector<float>& source);
double Median(std::vector<double> values);
int EstimatePeriod(const std::vector<float>& signal, int minimum_period, int maximum_period);
AxisSequence FitSubpixelAxis(
    const std::vector<float>& boundary_signal,
    const std::vector<float>& signed_signal,
    const std::vector<float>& support_signal,
    const std::vector<float>& diagonal_penalty,
    int cell_size,
    int expected_pitch,
    std::pair<int, int> pitch_range,
    int minimum_count);
GridLayout BuildLattice(int grid_index, cv::Point origin, int rows, int columns, int cell_size, double pitch_x, double pitch_y);

} // namespace iconrecognition::detail
