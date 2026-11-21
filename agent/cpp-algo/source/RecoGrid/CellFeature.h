#pragma once

#include <MaaUtils/NoWarningCV.hpp>

#include <cstdint>
#include <vector>

namespace recogrid
{

struct CellFeature
{
    std::vector<std::uint8_t> data;
    int width = 0;
    int height = 0;
    int channels = 0;
};

CellFeature ComputeCellFeature(const cv::Mat& image);
int FeatureDistance(const CellFeature& lhs, const CellFeature& rhs);

} // namespace recogrid
