#pragma once

#include <optional>
#include <string>
#include <vector>

#include "WorldMapTypes.h"

namespace worldmap
{

// 屏幕上不被 UI 遮挡、图标能完整显出来的区域
cv::Rect SafeArea(const cv::Size& screen_size, const ScreenMapRoi& roi, int margin);

cv::Point2d ViewportToScreen(const Viewport& viewport, const cv::Point2d& base);

std::optional<Viewport> SolveViewport(const cv::Mat& base, const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg);

// 地图已停在这张分层上时解视口
std::optional<Viewport>
    SolveViewportOnTier(const TierPatch& tier, const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg);

// 在 at 所在的分层上解视口，at 不在任何分层里时返回空
std::optional<Viewport> SolveViewportOnTiers(
    const std::vector<TierPatch>& tiers,
    const cv::Mat& screen,
    const std::string& zone,
    const ViewportConfig& cfg,
    const cv::Point2d& at);

} // namespace worldmap
