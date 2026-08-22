#pragma once

#include <filesystem>
#include <map>
#include <mutex>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

#include "MapTeleportTypes.h"

namespace mapteleport
{

// 大地图传送锚点求解器：先由画面解出屏幕与区域底图之间的相似变换，
// 再到解出来的期望位置上确认锚点图标。求不出或确认不上就不给坐标。
class MapTeleportSolver
{
public:
    explicit MapTeleportSolver(std::vector<std::filesystem::path> imageRoots);

    std::optional<Viewport> SolveViewport(const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg);

    std::optional<SpotHit>
        ConfirmSpot(const cv::Mat& screen, const cv::Point2d& expected, double viewportScale, const SpotConfig& cfg);

    // 屏幕上不被 UI 遮挡、图标能完整显出来的区域
    static std::optional<PlayerMarkerHit> DetectPlayerMarker(
        const cv::Mat& screen,
        const cv::Point2d& expected,
        const PlayerMarkerConfig& cfg);

    static cv::Rect SafeArea(const cv::Size& screenSize, const ScreenMapRoi& roi, int margin);

    bool HasZone(const std::string& zone);

private:
    // 灰度图配掩膜去匹配，模板自己的饱和度另外留着判解锁。
    // 没有 alpha 的模板整块等权，掩膜留空
    struct IconTemplate
    {
        cv::Mat gray;
        cv::Mat mask;
        cv::Mat saturation;
    };

    const cv::Mat* LoadZoneBase(const std::string& zone);
    const IconTemplate* LoadIcon(const std::string& name);

    // 控制器自己的资源层排在基础层之前, 同名图标由前者胜出
    std::vector<std::filesystem::path> _imageRoots;
    std::mutex _mutex;
    std::map<std::string, cv::Mat> _zoneBases;

    // 加载失败的也留一条空记录，免得每帧都去翻一遍磁盘
    std::map<std::string, IconTemplate> _icons;
};

// 控制器类型进程内恒定, 只有第一次调用会拿它建资源层次
MapTeleportSolver& GetSolver(std::string_view controller_type);

} // namespace mapteleport
