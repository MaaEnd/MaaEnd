#pragma once

#include <filesystem>
#include <map>
#include <mutex>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

#include "WorldMapTypes.h"

namespace worldmap
{

// 选中某个分层时的大地图：分层贴回底图，四周压暗
struct TierPatch
{
    std::string name;
    // 分层在底图上的范围
    cv::Rect area;
    // gray 左上角在底图上的坐标
    cv::Point origin { 0, 0 };
    cv::Mat gray;
};

// 大地图求解器：先由画面解出屏幕与区域底图之间的相似变换，再到解出来的期望位置上确认图标。
// 求不出或确认不上就不给坐标。这一层只看图，不碰控制器
class WorldMapSolver
{
public:
    // navmeshPack 提供分层在底图上的摆放
    WorldMapSolver(std::vector<std::filesystem::path> imageRoots, std::filesystem::path navmeshPack);

    std::optional<Viewport> SolveViewport(const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg);

    // 在 at 所在的分层上解视口，at 不在任何分层里时返回空
    std::optional<Viewport>
        SolveViewportOnTiers(const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg, const cv::Point2d& at);

    std::optional<SpotHit> ConfirmSpot(const cv::Mat& screen, const cv::Point2d& expected, double viewportScale, const SpotConfig& cfg);

    static std::optional<PlayerMarkerHit>
        DetectPlayerMarker(const cv::Mat& screen, const cv::Point2d& expected, const PlayerMarkerConfig& cfg);

    // 屏幕上不被 UI 遮挡、图标能完整显出来的区域
    static cv::Rect SafeArea(const cv::Size& screenSize, const ScreenMapRoi& roi, int margin);

    bool HasZone(const std::string& zone);

    // 图标名 -> 认它的那套参数。表里没有这个名字就不给，调用方据此报错
    std::optional<IconSpec> ResolveIcon(const std::string& name);

private:
    // 灰度图配掩膜去匹配，模板自己的饱和度另外留着判解锁。
    // 没有 alpha 的模板整块等权，掩膜留空
    struct IconTemplate
    {
        cv::Mat gray;
        cv::Mat mask;
        cv::Mat saturation;

        // 图标本体上最厚实的那一点，相对模板中心，单位模板像素
        cv::Point2d hotspot { 0.0, 0.0 };
    };

    const cv::Mat* LoadZoneBase(const std::string& zone);
    const std::vector<TierPatch>& LoadZoneTiers(const std::string& zone, const cv::Mat& base);
    const IconTemplate* LoadIconTemplate(const std::string& name);
    void LoadIconTable();

    // 控制器自己的资源层排在基础层之前, 同名图标由前者胜出
    std::vector<std::filesystem::path> _imageRoots;
    std::filesystem::path _navmeshPack;
    std::mutex _mutex;
    std::map<std::string, cv::Mat> _zoneBases;

    // 没有分层的区域也留一条空记录
    std::map<std::string, std::vector<TierPatch>> _zoneTiers;

    // 加载失败的也留一条空记录，免得每帧都去翻一遍磁盘
    std::map<std::string, IconTemplate> _icons;

    std::map<std::string, IconSpec> _iconTable;
    bool _iconTableLoaded = false;
};

// 控制器类型进程内恒定, 只有第一次调用会拿它建资源层次
WorldMapSolver& GetSolver(std::string_view controller_type);

} // namespace worldmap
