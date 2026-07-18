#include "RecoGridScanCells.h"

#include "GridGeometry.h"

#include <algorithm>
#include <stdexcept>
#include <unordered_map>

namespace recogrid
{

std::size_t CellIndex(int row, int col, int cols)
{
    return static_cast<std::size_t>(row * cols + col);
}

std::vector<std::size_t> NewCellIndicesForOffset(const GridHashSnapshot& current, int rowOffset)
{
    std::vector<std::size_t> indices;
    if (rowOffset <= 0 || current.rows <= 0 || current.cols <= 0) {
        return indices;
    }

    const int newRows = std::min(rowOffset, current.rows);
    const int startRow = std::max(0, current.rows - newRows);
    indices.reserve(static_cast<std::size_t>(newRows * current.cols));
    for (int row = startRow; row < current.rows; ++row) {
        for (int col = 0; col < current.cols; ++col) {
            const std::size_t index = CellIndex(row, col, current.cols);
            if (index < current.hashes.size()) {
                indices.push_back(index);
            }
        }
    }
    return indices;
}

int CountLeadingPartialRows(const GridResult& grid)
{
    int count = 0;
    for (const Segment& row : grid.rows) {
        if (grid.minRowHeight <= 0 || SegmentLength(row) >= grid.minRowHeight) {
            break;
        }
        ++count;
    }
    return count;
}

cv::Mat ToGray(const cv::Mat& image)
{
    if (image.channels() == 1) {
        return image;
    }

    cv::Mat gray;
    if (image.channels() == 4) {
        cv::cvtColor(image, gray, cv::COLOR_BGRA2GRAY);
    }
    else if (image.channels() == 3) {
        cv::cvtColor(image, gray, cv::COLOR_BGR2GRAY);
    }
    else {
        throw std::invalid_argument("Unsupported image channel count for grid cell occupancy");
    }
    return gray;
}

bool IsOccupiedCell(const cv::Mat& roi, const cv::Rect& rect, const GridScanOptions& options)
{
    const cv::Rect clipped = ClampRect(rect, roi.size());
    if (clipped.empty()) {
        return false;
    }

    const cv::Mat gray = ToGray(roi(clipped));
    const cv::Mat keepMask = BuildIgnoreMask(gray.size(), options.recognition.mask);
    const int keptPixels = keepMask.empty() ? gray.rows * gray.cols : cv::countNonZero(keepMask);
    if (keptPixels <= 0) {
        return false;
    }

    cv::Mat bright;
    cv::threshold(gray, bright, std::clamp(options.occupiedBrightThreshold, 0, 255), 255, cv::THRESH_BINARY);
    if (!keepMask.empty()) {
        cv::bitwise_and(bright, keepMask, bright);
    }

    const double mean = keepMask.empty() ? cv::mean(gray)[0] : cv::mean(gray, keepMask)[0];
    const double brightRatio = static_cast<double>(cv::countNonZero(bright)) / static_cast<double>(keptPixels);
    return mean >= options.minOccupiedMean && brightRatio >= options.minOccupiedBrightRatio;
}

std::vector<std::size_t> CellIndices(const std::vector<GridScanCell>& cells)
{
    std::vector<std::size_t> indices;
    indices.reserve(cells.size());
    for (const GridScanCell& cell : cells) {
        indices.push_back(cell.cellIndex);
    }
    return indices;
}

std::vector<GridScanCell> MakeUnknownCells(
    int startRow,
    int rows,
    int cols,
    const cv::Mat& gridRoi,
    const std::vector<cv::Rect>& gridCells,
    const GridScanOptions& scanOptions,
    const GridRecognitionOptions& options,
    cv::Size imageSize,
    const std::string& unknownTemplateId)
{
    std::vector<GridScanCell> cells;
    if (rows <= 0 || cols <= 0) {
        return cells;
    }

    cells.reserve(static_cast<std::size_t>(rows * cols));
    for (int row = 0; row < rows; ++row) {
        for (int col = 0; col < cols; ++col) {
            const std::size_t index = CellIndex(row, col, cols);
            if (index >= gridCells.size() || !IsOccupiedCell(gridRoi, gridCells[index], scanOptions)) {
                continue;
            }

            GridScanCell cell;
            cell.row = startRow + row;
            cell.col = col;
            cell.cellIndex = index;
            cell.templateId = unknownTemplateId;
            cell.visible = true;
            cell.screenCell = RoiToScreen(gridCells[index], options.detect, imageSize);
            cells.push_back(std::move(cell));
        }
    }
    return cells;
}

void ApplyClassifications(
    std::vector<GridScanCell>& cells,
    const GridClassificationResult& classification,
    int cols,
    int startRow,
    const std::string& unknownTemplateId)
{
    std::unordered_map<std::size_t, GridScanCell*> cellsByIndex;
    cellsByIndex.reserve(cells.size());
    for (GridScanCell& cell : cells) {
        cellsByIndex.emplace(cell.cellIndex, &cell);
    }

    for (const GridCellClassification& source : classification.cells) {
        const auto iter = cellsByIndex.find(source.cellIndex);
        if (iter == cellsByIndex.end()) {
            continue;
        }

        GridScanCell& target = *iter->second;
        const int localRow = cols > 0 ? static_cast<int>(source.cellIndex / static_cast<std::size_t>(cols)) : 0;
        const int col = cols > 0 ? static_cast<int>(source.cellIndex % static_cast<std::size_t>(cols)) : 0;
        target.row = startRow + localRow;
        target.col = col;
        target.cellIndex = source.cellIndex;
        target.screenCell = source.screenCell;
        target.visible = true;
        target.matched = source.matched;
        target.templateId = source.matched ? source.templateId : unknownTemplateId;
        target.score = source.score;
        target.templateScore = source.templateScore;
        target.hueScore = source.hueScore;
        target.phashDistance = source.phashDistance;
    }
}

void AdjustLeadingPartialRowsForDelta(
    GridDeltaResult& delta,
    const GridRecognitionResult& recognition,
    const GridScanOptions& options,
    cv::Size imageSize,
    int baseViewportStartRow,
    const SessionCells* sessionCells)
{
    const int rows = static_cast<int>(recognition.grid.rows.size());
    const int cols = static_cast<int>(recognition.grid.cols.size());
    const int leadingPartialRows = CountLeadingPartialRows(recognition.grid);
    if (!delta.reliable || !delta.hasProgress || delta.rowOffset <= 0 || leadingPartialRows <= 0) {
        return;
    }
    if (cols > 0 && delta.comparedCells > cols * 2) {
        return;
    }

    const std::vector<GridScanCell> currentCells = MakeUnknownCells(
        baseViewportStartRow + delta.rowOffset,
        rows,
        cols,
        recognition.grid.roi,
        recognition.grid.cells,
        options,
        options.recognition,
        imageSize,
        options.unknownTemplateId);

    bool shouldAdvance = !HasTrailingPartialRow(currentCells, cols);
    if (sessionCells != nullptr) {
        const std::vector<GridScanCell> advancedCells = MakeUnknownCells(
            baseViewportStartRow + delta.rowOffset + leadingPartialRows,
            rows,
            cols,
            recognition.grid.roi,
            recognition.grid.cells,
            options,
            options.recognition,
            imageSize,
            options.unknownTemplateId);
        shouldAdvance = CountNewVisibleSessionKeys(*sessionCells, advancedCells) > CountNewVisibleSessionKeys(*sessionCells, currentCells);
    }
    if (shouldAdvance) {
        delta.rowOffset += leadingPartialRows;
    }
    delta.newCellIndices =
        NewCellIndicesForOffset(MakeGridHashSnapshot(rows, cols, std::vector<Hash>(recognition.cellHashes)), delta.rowOffset);
    delta.hasProgress = !delta.newCellIndices.empty();
}

} // namespace recogrid
