#pragma once

#include <array>
#include <cstddef>
#include <optional>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

struct RarityResult
{
    std::optional<int> rarity;
    double coverage = 0.0;
    std::optional<int> row_offset;
};

struct RarityRowEvidence
{
    std::array<double, 6> coverages {};

    double maximumCoverage() const;
    double maximumChromaticCoverage() const;
};

const std::array<cv::Vec3f, 6>& RarityLabPrototypes();
RarityResult ClassifyRarity(const cv::Mat& image, const cv::Rect& slot, double grid_scale = 1.0);
RarityRowEvidence MeasureRarityRow(const cv::Mat& lab_row);
double RarityRowCoverage(const cv::Mat& lab_row);

} // namespace iconrecognition::detail
