#pragma once

#include <meojson/json.hpp>
#include <optional>
#include <string>

#include <MaaUtils/NoWarningCV.hpp>

namespace maplocator
{

struct MapPosition
{
    std::string zoneId;
    double x = 0.0;
    double y = 0.0;
    double score = 0.0;
    int sliceIndex = 0;
    double scale = 1.0;
    double angle = 0.0;
    long long latencyMs = 0;
    bool isHeld = false;
};

struct MapLocatorConfig
{
    std::string mapResourceDir;
    std::string yoloModelPath;
    int yoloThreads = 1;
};

struct LocateOptions
{
    double loc_threshold = 0.55;      // 最低分数线
    double yolo_threshold = 0.70;
    bool force_global_search = false; // 是否强制放弃当前追踪，进行全局全图搜
    int max_lost_frames = 3;          // 允许丢失追踪的帧数
    std::string expected_zone_id;     // 非空时仅接受该区域的定位结果

    MEO_JSONIZATION(
        MEO_OPT loc_threshold,
        MEO_OPT yolo_threshold,
        MEO_OPT force_global_search,
        MEO_OPT max_lost_frames,
        MEO_OPT expected_zone_id)
};

// --- 返回结果枚举与封装 ---
enum class LocateStatus
{
    Success,
    TrackingLost,  // 追踪丢失，且全局搜失败
    ScreenBlocked, // 画面被UI大面积遮挡
    Teleported,    // 速度异常判定为传送
    YoloFailed,    // YOLO未识别出合法地图
    NotInitialized
};

struct LocateResult
{
    LocateStatus status;
    std::optional<MapPosition> position;
    std::string debugMessage; // 用于向 Pipeline 输出日志
};

enum class GlobalSearchMode
{
    LegacyCoarse,
    FullMapFine,
    RoiFine,
};

struct SearchConstraint
{
    GlobalSearchMode mode = GlobalSearchMode::LegacyCoarse;
    bool yolo_validated = false;
    cv::Rect roi {};
};

struct YoloCoarseResult
{
    bool valid = false;
    bool is_none = false;

    std::string raw_class;
    std::string base_class;
    std::string zone_id;
    float confidence = 0.0f;

    bool has_roi = false;
    int roi_x = 0;
    int roi_y = 0;
    int roi_w = 0;
    int roi_h = 0;
    int infer_margin = 0;
};

struct MinimapRoiConfig
{
    int x = 0;
    int y = 0;
    int width = 0;
    int height = 0;
};

// roi及搜索相关常量
constexpr MinimapRoiConfig kDefaultMinimapRoi { 49, 51, 118, 120 };
constexpr double kAdbMinimapFullImageScale = 0.8;
constexpr int kAdbMinimapXOffset = 0;
constexpr int kAdbMinimapYOffset = -7;
constexpr MinimapRoiConfig kAdbMinimapRoi {
    kDefaultMinimapRoi.x + kAdbMinimapXOffset,
    kDefaultMinimapRoi.y + kAdbMinimapYOffset,
    kDefaultMinimapRoi.width,
    kDefaultMinimapRoi.height,
};
constexpr int MinimapROIOriginX = kDefaultMinimapRoi.x;
constexpr int MinimapROIOriginY = kDefaultMinimapRoi.y;
constexpr int MinimapROIWidth = kDefaultMinimapRoi.width;
constexpr int MinimapROIHeight = kDefaultMinimapRoi.height;

inline const MinimapRoiConfig& GetMinimapRoiConfig(bool use_adb_minimap_roi)
{
    return use_adb_minimap_roi ? kAdbMinimapRoi : kDefaultMinimapRoi;
}

inline bool TryExtractMinimap(const cv::Mat& image, bool use_adb_minimap_roi, cv::Mat* out_minimap)
{
    if (out_minimap == nullptr || image.empty()) {
        return false;
    }

    cv::Mat scaled_image = image;
    if (use_adb_minimap_roi && kAdbMinimapFullImageScale != 1.0) {
        cv::resize(image, scaled_image, cv::Size(), kAdbMinimapFullImageScale, kAdbMinimapFullImageScale, cv::INTER_AREA);
    }

    const auto& roi_config = GetMinimapRoiConfig(use_adb_minimap_roi);
    const cv::Rect roi(roi_config.x, roi_config.y, roi_config.width, roi_config.height);
    const cv::Rect image_bounds(0, 0, scaled_image.cols, scaled_image.rows);
    const cv::Rect clipped_roi = roi & image_bounds;
    if (clipped_roi.width != roi.width || clipped_roi.height != roi.height || clipped_roi.width <= 0 || clipped_roi.height <= 0) {
        return false;
    }

    *out_minimap = scaled_image(clipped_roi).clone();
    return !out_minimap->empty();
}

constexpr int MaxLostTrackingCount = 3;
constexpr double MinMatchScore = 0.7;
constexpr double MobileSearchRadius = 50.0;

inline bool IsPathHeatmapZone(const std::string& zoneId)
{
    constexpr const char* kPathHeatmapZoneMarkers[] = { "OMVBase" };
    for (const char* marker : kPathHeatmapZoneMarkers) {
        if (zoneId.find(marker) != std::string::npos) {
            return true;
        }
    }
    return false;
}

struct TrackingConfig
{
    double maxNormalSpeed = 40.0;        // px/s
    double screenBlockedThreshold = 0.4; // NCC correlation below this means blocked
    int edgeSnapMargin = 1;
    double velocitySmoothingAlpha = 0.5; // 平滑系数
    double maxDtForPrediction = 5.0;     // 超时则放弃速度预测
};

struct MatchConfig
{
    int blurSize = 7;
    double coarseScale = 0.5;
    int fineSearchRadius = 40;   // 精搜半径(px)
    double passThreshold = 0.55; // 全局搜索及格线, 容忍UI遮挡+光影
    double yoloConfThreshold = 0.60;
};

struct ImageProcessingConfig
{
    double darkMapThreshold;
    int iconDiffThreshold;        // 黄/蓝图标与地图色差判定
    int centerMaskRadius;         // 玩家箭头遮蔽半径
    double gradientBaseWeight;    // 保底权重
    int minimapDarkMaskThreshold; // 与暗部阈值对齐
    int borderMargin;
    int whiteDilate;
    int colorDilate;
    bool useHsvWhiteMask;
};

} // namespace maplocator
