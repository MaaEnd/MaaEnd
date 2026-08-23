#include "PHashFilter.h"

#include <algorithm>
#include <cmath>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>

namespace recogrid
{
namespace
{

constexpr int kDctSize = 32;
constexpr int kHashSize = 8;
constexpr int kFeatureSize = 16;
constexpr int kFeatureDistanceScale = 64;

cv::Mat ToPHashGray(const cv::Mat& image)
{
    if (image.channels() == 1) {
        return image;
    }

    cv::Mat gray;
    if (image.channels() == 4) {
        std::vector<cv::Mat> bgra;
        cv::split(image, bgra);

        cv::Mat alpha;
        bgra[3].convertTo(alpha, CV_32F, 1.0 / 255.0);
        const cv::Mat invAlpha = 1.0 - alpha;

        cv::Mat blue;
        cv::Mat green;
        cv::Mat red;
        bgra[0].convertTo(blue, CV_32F);
        bgra[1].convertTo(green, CV_32F);
        bgra[2].convertTo(red, CV_32F);

        const cv::Mat grayFloat = (0.114f * blue + 0.587f * green + 0.299f * red).mul(alpha) + 255.0f * invAlpha;
        grayFloat.convertTo(gray, CV_8U);
    }
    else if (image.channels() == 3) {
        cv::cvtColor(image, gray, cv::COLOR_BGR2GRAY);
    }
    else {
        return {};
    }

    return gray;
}

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

Hash ComputeHash(const cv::Mat& image)
{
    if (image.empty()) {
        return 0;
    }

    const cv::Mat gray = ToPHashGray(image);
    if (gray.empty()) {
        return 0;
    }
    cv::Mat resized;
    cv::resize(gray, resized, cv::Size(kDctSize, kDctSize), 0, 0, cv::INTER_AREA);
    resized.convertTo(resized, CV_32F);

    cv::Mat dctCoefficients;
    cv::dct(resized, dctCoefficients);

    std::vector<float> lowFreq;
    lowFreq.reserve(kHashSize * kHashSize - 1);
    for (int y = 0; y < kHashSize; ++y) {
        for (int x = 0; x < kHashSize; ++x) {
            if (x == 0 && y == 0) {
                continue;
            }
            lowFreq.push_back(dctCoefficients.at<float>(y, x));
        }
    }

    auto medianIt = lowFreq.begin() + static_cast<std::ptrdiff_t>(lowFreq.size() / 2);
    std::nth_element(lowFreq.begin(), medianIt, lowFreq.end());
    const float median = *medianIt;

    Hash hash = 0;
    int bit = 0;
    for (int y = 0; y < kHashSize; ++y) {
        for (int x = 0; x < kHashSize; ++x) {
            if (x == 0 && y == 0) {
                continue;
            }
            if (dctCoefficients.at<float>(y, x) > median) {
                hash |= (Hash { 1 } << bit);
            }
            ++bit;
        }
    }

    return hash;
}

int HammingDistance(Hash lhs, Hash rhs)
{
    Hash value = lhs ^ rhs;
    int distance = 0;
    while (value != 0) {
        value &= value - 1;
        ++distance;
    }
    return distance;
}

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
