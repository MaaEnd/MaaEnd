#pragma once

#include "RecoGridPlacement.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <vector>

namespace recogrid
{

void HandleBeamTransition(
    SessionState& session,
    GridScanResult& result,
    const GridRecognitionResult& recognition,
    const std::vector<GridClassifyTemplate>& templates,
    const GridScanOptions& options,
    const GridClassifyOptions& classifyOptions,
    const GridHashSnapshot& currentSnapshot,
    const GridDeltaResult& delta,
    cv::Size imageSize);

} // namespace recogrid
