#pragma once

#include <array>
#include <optional>
#include <vector>

#include "GridProfiles.h"
#include "RegularLattice.h"
#include "TrustedRarity.h"

namespace iconrecognition::detail
{

struct RarityGridFit
{
    std::vector<int> x_starts;
    int origin = 0;
    int pitch_x = 0;
    int pitch = 0;
    int supporting_rows = 0;
    int supporting_cells = 0;
    int supporting_strong_cells = 0;
    int supporting_chromatic_cells = 0;
    double mean_coverage = 0.0;
};

struct TrustedRarityGridFit
{
    RegularAxisFit x_axis;
    RegularAxisFit y_axis;
    std::vector<int> x_starts;
    std::vector<int> y_starts;
    std::vector<TrustedRarityStrip> strips;
    std::array<int, 6> rarity_counts {};
    int supporting_cells = 0;
    double mean_confidence = 0.0;
};

std::optional<RarityGridFit> FitRarityGrid(
    const cv::Mat& image,
    const std::vector<int>& x_starts,
    const std::vector<int>& coarse_y_starts,
    const TransferGridProfile& profile);
std::optional<TrustedRarityGridFit> FitTrustedRarityGrid(const cv::Mat& image, const cv::Rect& region, const TransferGridProfile& profile);

} // namespace iconrecognition::detail
