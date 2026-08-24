#pragma once

#include <MaaUtils/NoWarningCV.hpp>

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

Hash ComputeHash(const cv::Mat& image);
int HammingDistance(Hash lhs, Hash rhs);
CellFeature ComputeCellFeature(const cv::Mat& image);
int FeatureDistance(const CellFeature& lhs, const CellFeature& rhs);

} // namespace recogrid
