#include "GridAlignment.h"

#include <algorithm>
#include <stdexcept>
#include <utility>

namespace recogrid
{
namespace
{

constexpr double kDoubleEpsilon = 1e-9;

std::size_t CellIndex(int row, int col, int cols)
{
    return static_cast<std::size_t>(row * cols + col);
}

bool IsBetterAlignment(const AlignmentResult& candidate, const AlignmentResult& best)
{
    if (best.comparedCells == 0) {
        return true;
    }

    const double candidateMatchRatio =
        static_cast<double>(candidate.matchedCells) / static_cast<double>(candidate.comparedCells);
    const double bestMatchRatio = static_cast<double>(best.matchedCells) / static_cast<double>(best.comparedCells);
    if (candidateMatchRatio > bestMatchRatio + kDoubleEpsilon) {
        return true;
    }
    if (candidateMatchRatio + kDoubleEpsilon < bestMatchRatio) {
        return false;
    }

    if (candidate.averageDistance + kDoubleEpsilon < best.averageDistance) {
        return true;
    }
    if (candidate.averageDistance > best.averageDistance + kDoubleEpsilon) {
        return false;
    }

    return candidate.comparedCells > best.comparedCells;
}

int CellDistance(const GridHashSnapshot& first, std::size_t firstIndex, const GridHashSnapshot& second, std::size_t secondIndex)
{
    if (firstIndex < first.features.size() && secondIndex < second.features.size()) {
        const CellFeature& firstFeature = first.features[firstIndex];
        const CellFeature& secondFeature = second.features[secondIndex];
        if (!firstFeature.data.empty() && !secondFeature.data.empty()) {
            return FeatureDistance(firstFeature, secondFeature);
        }
    }

    if (firstIndex >= first.hashes.size() || secondIndex >= second.hashes.size()) {
        return 64;
    }
    return HammingDistance(first.hashes[firstIndex], second.hashes[secondIndex]);
}

AlignmentResult EstimateRowOffsetCore(
    const GridHashSnapshot& first,
    const GridHashSnapshot& second,
    int matchDistanceThreshold)
{
    if (first.rows == 0 || second.rows == 0 || first.cols == 0 || second.cols == 0) {
        throw std::invalid_argument("Cannot align empty grids");
    }

    const int comparedCols = std::min(first.cols, second.cols);
    AlignmentResult best;

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
                const bool hasFeaturePair = idx1 < first.features.size() && idx2 < second.features.size();
                const bool hasHashPair = idx1 < first.hashes.size() && idx2 < second.hashes.size();
                if (!hasFeaturePair && !hasHashPair) {
                    continue;
                }

                const int distance = CellDistance(first, idx1, second, idx2);
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
        const double matchRatio = static_cast<double>(matchedCells) / static_cast<double>(comparedCells);
        const double score = matchRatio * 1000.0 - averageDistance;

        AlignmentResult candidate;
        candidate.rowOffset = offset;
        candidate.comparedCells = comparedCells;
        candidate.matchedCells = matchedCells;
        candidate.totalDistance = totalDistance;
        candidate.averageDistance = averageDistance;
        candidate.score = score;

        if (IsBetterAlignment(candidate, best)) {
            best = candidate;
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
    snapshot.features = ComputeCellFeatures(snapshot.roi, snapshot.grid.cells, maskRatios);
    return snapshot;
}

AlignmentResult EstimateRowOffset(const Snapshot& first, const Snapshot& second, int matchDistanceThreshold)
{
    return EstimateRowOffsetCore(
        MakeGridHashSnapshot(
            static_cast<int>(first.grid.rows.size()),
            static_cast<int>(first.grid.cols.size()),
            first.hashes,
            first.features),
        MakeGridHashSnapshot(
            static_cast<int>(second.grid.rows.size()),
            static_cast<int>(second.grid.cols.size()),
            second.hashes,
            second.features),
        matchDistanceThreshold);
}

AlignmentResult EstimateRowOffset(const GridHashSnapshot& first, const GridHashSnapshot& second, int matchDistanceThreshold)
{
    return EstimateRowOffsetCore(first, second, matchDistanceThreshold);
}

GridHashSnapshot MakeGridHashSnapshot(int rows, int cols, std::vector<Hash> hashes, std::vector<CellFeature> features)
{
    return { rows, cols, std::move(hashes), std::move(features) };
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
