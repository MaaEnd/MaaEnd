#include "RecoGridPlacement.h"

namespace recogrid
{

std::vector<GridScanCell> PlaceGridCells(
    int viewportStartRow,
    const GridRecognitionResult& recognition,
    const GridScanOptions& options,
    cv::Size imageSize,
    const std::string& unknownTemplateId)
{
    return MakeUnknownCells(
        viewportStartRow,
        static_cast<int>(recognition.grid.rows.size()),
        static_cast<int>(recognition.grid.cols.size()),
        recognition.grid.roi,
        recognition.grid.cells,
        options,
        options.recognition,
        imageSize,
        unknownTemplateId);
}

} // namespace recogrid
