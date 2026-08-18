#pragma once

#include <cstdint>
#include <filesystem>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

bool SaveDebugCapture(
    const std::filesystem::path& root,
    const cv::Mat& image,
    const RecognitionResult& result,
    std::uint64_t reco_id) noexcept;

} // namespace iconrecognition::detail
