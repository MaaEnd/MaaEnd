#pragma once

#include <optional>

#include "WorldMapAssets.h"
#include "WorldMapTypes.h"

namespace worldmap
{

// 内部持 assets->mutex
std::optional<SpotHit>
    ConfirmSpot(WorldMapAssets* assets, const cv::Mat& screen, const cv::Point2d& expected, double viewport_scale, const SpotConfig& cfg);

std::optional<PlayerMarkerHit> DetectPlayerMarker(const cv::Mat& screen, const cv::Point2d& expected, const PlayerMarkerConfig& cfg);

} // namespace worldmap
