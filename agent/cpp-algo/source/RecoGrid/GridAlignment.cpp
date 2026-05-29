#include "GridAlignment.h"

#include <algorithm>
#include <limits>
#include <stdexcept>

namespace recogrid
{
namespace
{

std::size_t CellIndex(int row, int col, int cols)
{
    return static_cast<std::size_t>(row * cols + col);
}

} // namespace

Snapshot BuildSnapshot(const cv::Mat& image, const GridDetectOptions& options)
{
    if (image.empty()) {
        throw std::invalid_argument("Cannot build grid snapshot for empty image");
    }

    Snapshot snapshot;
    snapshot.grid = DetectGrid(image, options);
    snapshot.roi = snapshot.grid.roi;
    snapshot.hashes = ComputeCellHashes(snapshot.roi, snapshot.grid.cells);
    return snapshot;
}

AlignmentResult EstimateRowOffset(const Snapshot& first, const Snapshot& second, int matchDistanceThreshold)
{
    const int rows1 = static_cast<int>(first.grid.rows.size());
    const int rows2 = static_cast<int>(second.grid.rows.size());
    const int cols = std::min(static_cast<int>(first.grid.cols.size()), static_cast<int>(second.grid.cols.size()));

    if (rows1 == 0 || rows2 == 0 || cols == 0) {
        throw std::invalid_argument("Cannot align empty grids");
    }

    AlignmentResult best;
    best.score = -std::numeric_limits<double>::infinity();

    for (int offset = -rows2 + 1; offset <= rows1 - 1; ++offset) {
        int comparedCells = 0;
        int matchedCells = 0;
        int totalDistance = 0;

        for (int row2 = 0; row2 < rows2; ++row2) {
            const int row1 = row2 + offset;
            if (row1 < 0 || row1 >= rows1) {
                continue;
            }

            for (int col = 0; col < cols; ++col) {
                const std::size_t idx1 = CellIndex(row1, col, cols);
                const std::size_t idx2 = CellIndex(row2, col, cols);
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

        const double averageDistance = static_cast<double>(totalDistance) / comparedCells;
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

} // namespace recogrid
