#pragma once

#include "RecoGridScanCells.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <string>
#include <vector>

namespace recogrid
{

std::vector<GridScanCell> PlaceGridCells(
    int viewportStartRow,
    const GridRecognitionResult& recognition,
    const GridScanOptions& options,
    cv::Size imageSize,
    const std::string& unknownTemplateId);

} // namespace recogrid
