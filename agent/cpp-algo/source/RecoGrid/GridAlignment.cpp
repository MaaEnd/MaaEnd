#include "GridAlignment.h"

#include <algorithm>
#include <limits>
#include <stdexcept>
#include <utility>

namespace recogrid
{
namespace
{

std::size_t CellIndex(int row, int col, int cols)
{
    return static_cast<std::size_t>(row * cols + col);
}

AlignmentResult EstimateRowOffsetCore(const GridHashSnapshot& first, const GridHashSnapshot& second, int matchDistanceThreshold)
{
    if (first.rows == 0 || second.rows == 0 || first.cols == 0 || second.cols == 0) {
        throw std::invalid_argument("Cannot align empty grids");
    }

    const int comparedCols = std::min(first.cols, second.cols);

    AlignmentResult best;
    best.score = -std::numeric_limits<double>::infinity();

    for (int offset = -second.rows + 1; offset <= first.rows - 1; ++offset) {
        int comparedCells = 0;
        int matchedCells = 0;
        int totalDistance = 0;

        for (int currentRow = 0; currentRow < second.rows; ++currentRow) {
            const int previousRow = currentRow + offset;
            if (previousRow < 0 || previousRow >= first.rows) {
                continue;
            }

            for (int col = 0; col < comparedCols; ++col) {
                const std::size_t idx1 = CellIndex(previousRow, col, first.cols);
                const std::size_t idx2 = CellIndex(currentRow, col, second.cols);
                if (idx1 >= first.hashes.size() || idx2 >= second.hashes.size()) {
                    continue;
                }

                const int distance = HammingDistance(first.hashes[idx1], second.hashes[idx2]);
                totalDistance += distance;
                ++comparedCells;
                if (distance <= matchDistanceThreshold) {
                    ++matchedCells;
                }
            }
        }

        if (comparedCells == 0) {
            continue;
        }

        const double averageDistance = static_cast<double>(totalDistance) / static_cast<double>(comparedCells);
        const double score = static_cast<double>(matchedCells) * 100.0 - averageDistance;

        if (score > best.score || (score == best.score && comparedCells > best.comparedCells)) {
            best.rowOffset = offset;
            best.comparedCells = comparedCells;
            best.matchedCells = matchedCells;
            best.totalDistance = totalDistance;
            best.averageDistance = averageDistance;
            best.score = score;
        }
    }

    return best;
}

} // namespace

Snapshot BuildSnapshot(const cv::Mat& image, const GridDetectOptions& options, const CellMaskRatios& maskRatios)
{
    if (image.empty()) {
        throw std::invalid_argument("Cannot build grid snapshot for empty image");
    }

    Snapshot snapshot;
    snapshot.grid = DetectGrid(image, options);
    snapshot.roi = snapshot.grid.roi;
    snapshot.hashes = ComputeCellHashes(snapshot.roi, snapshot.grid.cells, maskRatios);
    return snapshot;
}

AlignmentResult EstimateRowOffset(const Snapshot& first, const Snapshot& second, int matchDistanceThreshold)
{
    return EstimateRowOffsetCore(
        MakeGridHashSnapshot(static_cast<int>(first.grid.rows.size()), static_cast<int>(first.grid.cols.size()), first.hashes),
        MakeGridHashSnapshot(static_cast<int>(second.grid.rows.size()), static_cast<int>(second.grid.cols.size()), second.hashes),
        matchDistanceThreshold);
}

AlignmentResult EstimateRowOffset(const GridHashSnapshot& first, const GridHashSnapshot& second, int matchDistanceThreshold)
{
    return EstimateRowOffsetCore(first, second, matchDistanceThreshold);
}

GridHashSnapshot MakeGridHashSnapshot(int rows, int cols, std::vector<Hash> hashes)
{
    return { rows, cols, std::move(hashes) };
}

GridDeltaResult ComputeGridDelta(const GridHashSnapshot& previous, const GridHashSnapshot& current, const GridDeltaOptions& options)
{
    GridDeltaResult result;
    const AlignmentResult alignment = EstimateRowOffset(previous, current, std::max(0, options.matchDistanceThreshold));
    result.rowOffset = alignment.rowOffset;
    result.comparedCells = alignment.comparedCells;
    result.matchedCells = alignment.matchedCells;
    result.totalDistance = alignment.totalDistance;
    result.averageDistance = alignment.averageDistance;
    result.score = alignment.score;
    if (result.comparedCells > 0) {
        result.matchRatio = static_cast<double>(result.matchedCells) / static_cast<double>(result.comparedCells);
    }

    result.reliable = result.comparedCells > 0 && result.matchRatio >= std::clamp(options.minMatchRatio, 0.0, 1.0);
    if (!result.reliable || result.rowOffset <= 0 || current.rows <= 0 || current.cols <= 0) {
        return result;
    }

    const int newRows = std::min(result.rowOffset, current.rows);
    const int startRow = std::max(0, current.rows - newRows);
    for (int row = startRow; row < current.rows; ++row) {
        for (int col = 0; col < current.cols; ++col) {
            const std::size_t index = CellIndex(row, col, current.cols);
            if (index < current.hashes.size()) {
                result.newCellIndices.push_back(index);
            }
        }
    }
    result.hasProgress = !result.newCellIndices.empty();
    return result;
}

} // namespace recogrid
