#pragma once

#include "GridAlignment.h"
#include "RecoGridEngineTypes.h"
#include "RecoGridSession.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <cstddef>
#include <string>
#include <vector>

namespace recogrid
{

std::size_t CellIndex(int row, int col, int cols);
std::vector<std::size_t> NewCellIndicesForOffset(const GridHashSnapshot& current, int rowOffset);
int CountLeadingPartialRows(const GridResult& grid);
std::vector<std::size_t> CellIndices(const std::vector<GridScanCell>& cells);
std::vector<GridScanCell> MakeUnknownCells(
    int startRow,
    int rows,
    int cols,
    const cv::Mat& gridRoi,
    const std::vector<cv::Rect>& gridCells,
    const GridScanOptions& scanOptions,
    const GridRecognitionOptions& options,
    cv::Size imageSize,
    const std::string& unknownTemplateId);
void ApplyClassifications(
    std::vector<GridScanCell>& cells,
    const GridClassificationResult& classification,
    int cols,
    int startRow,
    const std::string& unknownTemplateId);
void AdjustLeadingPartialRowsForDelta(
    GridDeltaResult& delta,
    const GridRecognitionResult& recognition,
    const GridScanOptions& options,
    cv::Size imageSize,
    int baseViewportStartRow,
    const SessionCells* sessionCells);

} // namespace recogrid
