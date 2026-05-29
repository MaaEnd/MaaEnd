#pragma once

#include "GridDetector.h"
#include "PHashFilter.h"

#include <opencv2/core.hpp>

#include <vector>

namespace recogrid
{

struct Snapshot
{
    cv::Mat roi;
    GridResult grid;
    std::vector<Hash> hashes;
};

struct AlignmentResult
{
    int rowOffset = 0;
    int comparedCells = 0;
    int matchedCells = 0;
    int totalDistance = 0;
    double averageDistance = 0.0;
    double score = 0.0;
};

Snapshot BuildSnapshot(const cv::Mat& image, const GridDetectOptions& options = {});
AlignmentResult EstimateRowOffset(const Snapshot& first, const Snapshot& second, int matchDistanceThreshold = 12);

} // namespace recogrid
