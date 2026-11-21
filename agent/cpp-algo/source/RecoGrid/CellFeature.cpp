#include "CellFeature.h"

#include <cmath>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>

namespace recogrid
{
namespace
{

constexpr int kFeatureSize = 16;
constexpr int kFeatureDistanceScale = 64;

cv::Mat ToFeatureBgr(const cv::Mat& image)
{
    if (image.channels() == 3) {
        return image;
    }

    cv::Mat bgr;
    if (image.channels() == 4) {
        cv::cvtColor(image, bgr, cv::COLOR_BGRA2BGR);
    }
    else if (image.channels() == 1) {
        cv::cvtColor(image, bgr, cv::COLOR_GRAY2BGR);
    }
    else {
        return {};
    }
    return bgr;
}

} // namespace

CellFeature ComputeCellFeature(const cv::Mat& image)
{
    if (image.empty()) {
        return {};
    }

    const cv::Mat bgr = ToFeatureBgr(image);
    if (bgr.empty()) {
        return {};
    }
    cv::Mat resized;
    cv::resize(bgr, resized, cv::Size(kFeatureSize, kFeatureSize), 0, 0, cv::INTER_AREA);
    if (!resized.isContinuous()) {
        resized = resized.clone();
    }

    CellFeature feature;
    feature.width = resized.cols;
    feature.height = resized.rows;
    feature.channels = resized.channels();
    feature.data.assign(resized.datastart, resized.dataend);
    return feature;
}

int FeatureDistance(const CellFeature& lhs, const CellFeature& rhs)
{
    if (lhs.data.empty() || rhs.data.empty() || lhs.data.size() != rhs.data.size() || lhs.width != rhs.width || lhs.height != rhs.height
        || lhs.channels != rhs.channels) {
        return kFeatureDistanceScale;
    }

    int total = 0;
    for (std::size_t i = 0; i < lhs.data.size(); ++i) {
        total += std::abs(static_cast<int>(lhs.data[i]) - static_cast<int>(rhs.data[i]));
    }

    const double average = static_cast<double>(total) / static_cast<double>(lhs.data.size());
    return static_cast<int>(std::round(average * static_cast<double>(kFeatureDistanceScale) / 255.0));
}

} // namespace recogrid
