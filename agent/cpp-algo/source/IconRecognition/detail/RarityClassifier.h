#pragma once

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
    double coverage = 0.0;
    double chromatic_coverage = 0.0;
};

RarityResult ClassifyRarity(const cv::Mat& image, const cv::Rect& slot);
RarityRowEvidence MeasureRarityRow(const cv::Mat& lab_row);
double RarityRowCoverage(const cv::Mat& lab_row);

} // namespace iconrecognition::detail
