#pragma once

#include "CellMask.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <cstddef>
#include <cstdint>
#include <vector>

namespace recogrid
{

using Hash = std::uint64_t;

struct CellFeature
{
    std::vector<std::uint8_t> data;
    int width = 0;
    int height = 0;
    int channels = 0;
};

struct Candidate
{
    std::size_t cellIndex = 0;
    cv::Rect cell;
    Hash hash = 0;
    int distance = 0;
};

Hash ComputeHash(const cv::Mat& image);
int HammingDistance(Hash lhs, Hash rhs);
std::vector<Hash> ComputeCellHashes(
    const cv::Mat& roi,
    const std::vector<cv::Rect>& cells,
    const CellMaskRatios& maskRatios = {});
CellFeature ComputeCellFeature(const cv::Mat& image);
int FeatureDistance(const CellFeature& lhs, const CellFeature& rhs);
std::vector<CellFeature> ComputeCellFeatures(
    const cv::Mat& roi,
    const std::vector<cv::Rect>& cells,
    const CellMaskRatios& maskRatios = {});
Hash ComputeHashResizedTo(const cv::Mat& image, cv::Size size, const CellMaskRatios& maskRatios = {});
std::vector<Candidate> FilterCandidates(
    const cv::Mat& roi,
    const std::vector<cv::Rect>& cells,
    Hash targetHash,
    int maxDistance,
    const CellMaskRatios& maskRatios = {});
std::vector<Candidate> FilterCandidates(
    const cv::Mat& roi,
    const std::vector<cv::Rect>& cells,
    const cv::Mat& target,
    int maxDistance,
    const CellMaskRatios& maskRatios = {});

} // namespace recogrid
