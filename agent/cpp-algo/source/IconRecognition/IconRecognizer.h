#pragma once

#include <filesystem>
#include <memory>

#include <MaaUtils/NoWarningCV.hpp>

#include "IconRecognitionTypes.h"

namespace iconrecognition
{

class IconRecognizer
{
public:
    explicit IconRecognizer(std::filesystem::path data_root);
    ~IconRecognizer();

    IconRecognizer(const IconRecognizer&) = delete;
    IconRecognizer& operator=(const IconRecognizer&) = delete;
    IconRecognizer(IconRecognizer&&) noexcept;
    IconRecognizer& operator=(IconRecognizer&&) noexcept;

    bool initialize();
    RecognitionResult recognize(const cv::Mat& image, const RecognitionRequest& request) const;

private:
    class Impl;
    std::unique_ptr<Impl> impl_;
};

} // namespace iconrecognition
