#pragma once

#include "CellMask.h"
#include "GridDetector.h"
#include "PHashFilter.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <cstddef>
#include <vector>

namespace recogrid
{

struct Snapshot
{
    cv::Mat roi;
    GridResult grid;
    std::vector<Hash> hashes;
    std::vector<CellFeature> features;
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

struct GridHashSnapshot
{
    int rows = 0;
    int cols = 0;
    std::vector<Hash> hashes;
    std::vector<CellFeature> features;
};

struct GridDeltaOptions
{
    int matchDistanceThreshold = 12;
    double minMatchRatio = 0.6;
};

struct GridDeltaResult
{
    int rowOffset = 0;
    int comparedCells = 0;
    int matchedCells = 0;
    int totalDistance = 0;
    double averageDistance = 0.0;
    double score = 0.0;
    double matchRatio = 0.0;
    bool reliable = false;
    bool hasProgress = false;
    std::vector<std::size_t> newCellIndices;
};

Snapshot BuildSnapshot(
    const cv::Mat& image,
    const GridDetectOptions& options = {},
    const CellMaskRatios& maskRatios = {});
AlignmentResult EstimateRowOffset(const Snapshot& first, const Snapshot& second, int matchDistanceThreshold = 12);
AlignmentResult EstimateRowOffset(
    const GridHashSnapshot& first,
    const GridHashSnapshot& second,
    int matchDistanceThreshold = 12);
GridHashSnapshot MakeGridHashSnapshot(
    int rows,
    int cols,
    std::vector<Hash> hashes,
    std::vector<CellFeature> features = {});
GridDeltaResult ComputeGridDelta(
    const GridHashSnapshot& previous,
    const GridHashSnapshot& current,
    const GridDeltaOptions& options = {});

} // namespace recogrid
