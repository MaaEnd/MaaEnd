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

RarityResult ClassifyRarity(const cv::Mat& image, const cv::Rect& slot);

} // namespace iconrecognition::detail
