#include "RecoGridPlacement.h"

#include <algorithm>
#include <cmath>

namespace recogrid
{
namespace
{

struct FixedDeltaStats
{
    int rowOffset = 0;
    int comparedCells = 0;
    int matchedCells = 0;
    int totalDistance = 0;
    double averageDistance = 0.0;
    double matchRatio = 0.0;
};

FixedDeltaStats ComputeFixedGridDelta(
    const GridHashSnapshot& previous,
    const GridHashSnapshot& current,
    int rowOffset,
    int matchDistanceThreshold)
{
    FixedDeltaStats stats;
    stats.rowOffset = rowOffset;
    if (previous.rows <= 0 || current.rows <= 0 || previous.cols <= 0 || current.cols <= 0) {
        return stats;
    }

    const int comparedCols = std::min(previous.cols, current.cols);
    for (int currentRow = 0; currentRow < current.rows; ++currentRow) {
        const int previousRow = currentRow + rowOffset;
        if (previousRow < 0 || previousRow >= previous.rows) {
            continue;
        }

        for (int col = 0; col < comparedCols; ++col) {
            const std::size_t previousIndex = CellIndex(previousRow, col, previous.cols);
            const std::size_t currentIndex = CellIndex(currentRow, col, current.cols);
            if (previousIndex >= previous.hashes.size() || currentIndex >= current.hashes.size()) {
                continue;
            }

            const int distance = HammingDistance(previous.hashes[previousIndex], current.hashes[currentIndex]);
            stats.totalDistance += distance;
            ++stats.comparedCells;
            if (distance <= matchDistanceThreshold) {
                ++stats.matchedCells;
            }
        }
    }

    if (stats.comparedCells > 0) {
        stats.averageDistance = static_cast<double>(stats.totalDistance) / static_cast<double>(stats.comparedCells);
        stats.matchRatio = static_cast<double>(stats.matchedCells) / static_cast<double>(stats.comparedCells);
    }
    return stats;
}

void AddCandidateOffset(std::vector<int>& offsets, int offset, int maxRows)
{
    if (offset <= 0 || maxRows <= 0 || offset > maxRows) {
        return;
    }
    if (std::find(offsets.begin(), offsets.end(), offset) == offsets.end()) {
        offsets.push_back(offset);
    }
}

PlacementCandidate ScorePlacementCandidate(
    const SessionCells& sessionCells,
    const GridHashSnapshot& previousSnapshot,
    const GridHashSnapshot& currentSnapshot,
    const GridRecognitionResult& recognition,
    const GridScanOptions& options,
    cv::Size imageSize,
    int baseViewportStartRow,
    int expectedViewportStartRow,
    int candidateRowOffset)
{
    PlacementCandidate candidate;
    candidate.rowOffset = candidateRowOffset;
    candidate.viewportStartRow = baseViewportStartRow + candidateRowOffset;
    candidate.cells = MakeUnknownCells(
        candidate.viewportStartRow,
        static_cast<int>(recognition.grid.rows.size()),
        static_cast<int>(recognition.grid.cols.size()),
        recognition.grid.roi,
        recognition.grid.cells,
        options,
        options.recognition,
        imageSize,
        options.unknownTemplateId);

    const FixedDeltaStats frameDelta = ComputeFixedGridDelta(
        previousSnapshot,
        currentSnapshot,
        candidateRowOffset,
        std::max(0, options.matchDistanceThreshold));
    candidate.matchRatio = frameDelta.matchRatio;

    int overlapCompared = 0;
    for (const GridScanCell& cell : candidate.cells) {
        const auto sessionIter = sessionCells.find(std::make_pair(cell.row, cell.col));
        if (sessionIter == sessionCells.end()) {
            ++candidate.newVisibleKeys;
            continue;
        }

        ++overlapCompared;
    }

    const int continuityDistance = std::abs(candidate.viewportStartRow - expectedViewportStartRow);
    const double frameScore =
        static_cast<double>(frameDelta.matchedCells) * 12.0 + candidate.matchRatio * 60.0 -
        frameDelta.averageDistance * 0.15;
    const double overlapScore = static_cast<double>(overlapCompared) * 2.0;
    const double newCellScore = static_cast<double>(candidate.newVisibleKeys) * 8.0;
    const double continuityScore = -static_cast<double>(continuityDistance) * 28.0;
    candidate.score = frameScore + overlapScore + newCellScore + continuityScore;
    return candidate;
}

} // namespace

std::vector<int> BuildCandidateOffsets(const GridDeltaResult& delta, int currentRows, int lastPositiveRowOffset)
{
    std::vector<int> offsets;
    AddCandidateOffset(offsets, delta.rowOffset, currentRows);
    AddCandidateOffset(offsets, delta.rowOffset - 1, currentRows);
    AddCandidateOffset(offsets, delta.rowOffset + 1, currentRows);
    AddCandidateOffset(offsets, lastPositiveRowOffset, currentRows);
    AddCandidateOffset(offsets, lastPositiveRowOffset - 1, currentRows);
    AddCandidateOffset(offsets, lastPositiveRowOffset + 1, currentRows);
    AddCandidateOffset(offsets, 1, currentRows);
    return offsets;
}

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
    int maxCandidates)
{
    std::vector<PlacementCandidate> candidates;
    candidates.reserve(rowOffsets.size());
    for (const int rowOffset : rowOffsets) {
        PlacementCandidate candidate = ScorePlacementCandidate(
            sessionCells,
            previousSnapshot,
            currentSnapshot,
            recognition,
            options,
            imageSize,
            baseViewportStartRow,
            expectedViewportStartRow,
            rowOffset);
        if (std::isfinite(candidate.score)) {
            candidates.push_back(std::move(candidate));
        }
    }

    std::sort(candidates.begin(), candidates.end(), [](const PlacementCandidate& lhs, const PlacementCandidate& rhs) {
        if (lhs.score != rhs.score) {
            return lhs.score > rhs.score;
        }
        if (lhs.newVisibleKeys != rhs.newVisibleKeys) {
            return lhs.newVisibleKeys > rhs.newVisibleKeys;
        }
        if (lhs.matchRatio != rhs.matchRatio) {
            return lhs.matchRatio > rhs.matchRatio;
        }
        return lhs.rowOffset > rhs.rowOffset;
    });
    if (maxCandidates > 0 && static_cast<int>(candidates.size()) > maxCandidates) {
        candidates.resize(static_cast<std::size_t>(maxCandidates));
    }
    return candidates;
}

} // namespace recogrid
