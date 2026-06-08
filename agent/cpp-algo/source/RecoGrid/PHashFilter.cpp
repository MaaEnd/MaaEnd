#include "PHashFilter.h"

#include "GridGeometry.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <algorithm>
#include <stdexcept>
#include <vector>

namespace recogrid
{
namespace
{

constexpr int kDctSize = 32;
constexpr int kHashSize = 8;

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

        const cv::Mat grayFloat =
            (0.114f * blue + 0.587f * green + 0.299f * red).mul(alpha) + 255.0f * invAlpha;
        grayFloat.convertTo(gray, CV_8U);
    }
    else if (image.channels() == 3) {
        cv::cvtColor(image, gray, cv::COLOR_BGR2GRAY);
    }
    else {
        throw std::invalid_argument("Unsupported image channel count for pHash");
    }

    return gray;
}

} // namespace

Hash ComputeHash(const cv::Mat& image)
{
    if (image.empty()) {
        throw std::invalid_argument("Cannot compute pHash for an empty image");
    }

    cv::Mat resized;
    cv::resize(ToPHashGray(image), resized, cv::Size(kDctSize, kDctSize), 0, 0, cv::INTER_AREA);
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

std::vector<Hash> ComputeCellHashes(
    const cv::Mat& roi,
    const std::vector<cv::Rect>& cells,
    const CellMaskRatios& maskRatios)
{
    std::vector<Hash> hashes;
    hashes.reserve(cells.size());

    for (const auto& cell : cells) {
        const cv::Rect clipped = ClampRect(cell, roi.size());
        if (clipped.empty()) {
            hashes.push_back(0);
            continue;
        }
        hashes.push_back(ComputeHash(ApplyIgnoreMask(roi(clipped), maskRatios)));
    }

    return hashes;
}

Hash ComputeHashResizedTo(const cv::Mat& image, cv::Size size, const CellMaskRatios& maskRatios)
{
    if (image.empty()) {
        throw std::invalid_argument("Cannot resize an empty image for pHash");
    }
    if (size.width <= 0 || size.height <= 0) {
        throw std::invalid_argument("Cannot resize pHash input to empty size");
    }

    cv::Mat resized;
    const int interpolation = image.cols > size.width || image.rows > size.height ? cv::INTER_AREA : cv::INTER_CUBIC;
    cv::resize(image, resized, size, 0, 0, interpolation);
    return ComputeHash(ApplyIgnoreMask(resized, maskRatios));
}

std::vector<Candidate> FilterCandidates(
    const cv::Mat& roi,
    const std::vector<cv::Rect>& cells,
    Hash targetHash,
    int maxDistance,
    const CellMaskRatios& maskRatios)
{
    std::vector<Candidate> candidates;
    candidates.reserve(cells.size());

    for (std::size_t i = 0; i < cells.size(); ++i) {
        const cv::Rect clipped = ClampRect(cells[i], roi.size());
        if (clipped.empty()) {
            continue;
        }

        const Hash cellHash = ComputeHash(ApplyIgnoreMask(roi(clipped), maskRatios));
        const int distance = HammingDistance(cellHash, targetHash);
        if (distance <= maxDistance) {
            candidates.push_back({ i, cells[i], cellHash, distance });
        }
    }

    std::sort(candidates.begin(), candidates.end(), [](const Candidate& lhs, const Candidate& rhs) {
        if (lhs.distance != rhs.distance) {
            return lhs.distance < rhs.distance;
        }
        return lhs.cellIndex < rhs.cellIndex;
    });

    return candidates;
}

std::vector<Candidate> FilterCandidates(
    const cv::Mat& roi,
    const std::vector<cv::Rect>& cells,
    const cv::Mat& target,
    int maxDistance,
    const CellMaskRatios& maskRatios)
{
    std::vector<Candidate> candidates;
    candidates.reserve(cells.size());

    for (std::size_t i = 0; i < cells.size(); ++i) {
        const cv::Rect clipped = ClampRect(cells[i], roi.size());
        if (clipped.empty()) {
            continue;
        }

        const Hash targetHash = ComputeHashResizedTo(target, clipped.size(), maskRatios);
        const Hash cellHash = ComputeHash(ApplyIgnoreMask(roi(clipped), maskRatios));
        const int distance = HammingDistance(cellHash, targetHash);
        if (distance <= maxDistance) {
            candidates.push_back({ i, cells[i], cellHash, distance });
        }
    }

    std::sort(candidates.begin(), candidates.end(), [](const Candidate& lhs, const Candidate& rhs) {
        if (lhs.distance != rhs.distance) {
            return lhs.distance < rhs.distance;
        }
        return lhs.cellIndex < rhs.cellIndex;
    });

    return candidates;
}

} // namespace recogrid
