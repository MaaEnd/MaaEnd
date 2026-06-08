#pragma once

#include "RecoGridScanCells.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <limits>
#include <vector>

namespace recogrid
{

struct PlacementCandidate
{
    int viewportStartRow = 0;
    int rowOffset = 0;
    int newVisibleKeys = 0;
    double matchRatio = 0.0;
    double score = -std::numeric_limits<double>::infinity();
    std::vector<GridScanCell> cells;
};

std::vector<int> BuildCandidateOffsets(const GridDeltaResult& delta, int currentRows, int lastPositiveRowOffset);
std::vector<PlacementCandidate> BuildPlacementCandidates(
    const SessionCells& sessionCells,
    const GridHashSnapshot& previousSnapshot,
    const GridHashSnapshot& currentSnapshot,
    const GridRecognitionResult& recognition,
    const GridScanOptions& options,
    cv::Size imageSize,
    int baseViewportStartRow,
    int expectedViewportStartRow,
    const std::vector<int>& rowOffsets,
    int maxCandidates);

} // namespace recogrid
