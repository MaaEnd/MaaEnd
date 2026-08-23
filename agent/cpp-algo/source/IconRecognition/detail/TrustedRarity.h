#pragma once

#include <vector>

#include <MaaUtils/NoWarningCV.hpp>

namespace iconrecognition::detail
{

struct TrustedRarityStrip
{
    cv::Rect box;
    int rarity = 0;
    double color_coverage = 0.0;
    double continuity = 0.0;
    double background_delta = 0.0;
    double edge_response = 0.0;
    int thickness = 0;
    double confidence = 0.0;
    bool trusted = false;
    bool can_seed_lattice = false;
};

std::vector<TrustedRarityStrip> DetectTrustedRarityStrips(const cv::Mat& image, int cell_size);

} // namespace iconrecognition::detail
