#pragma once

#include <array>
#include <chrono>
#include <cstdint>
#include <future>
#include <map>
#include <memory>
#include <mutex>
#include <optional>
#include <string>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>

#include "CameraOrientationPredictor.h"
#include "MapTypes.h"
#include "MatchStrategy.h"
#include "MotionTracker.h"
#include "ScaleExecutor.h"
#include "YoloPredictor.h"

namespace maplocator
{

struct GlobalSearchFeatureCacheKey
{
    std::string zone_id;
    TemplateFeatureKind kind = TemplateFeatureKind::StandardBase;
    cv::Rect roi {};
    std::uint64_t generation = 0;

    bool operator==(const GlobalSearchFeatureCacheKey&) const = default;
};

struct CachedGlobalSearchFeature
{
    std::mutex mutex;
    std::optional<GlobalSearchFeatureCacheKey> key;
    MatchFeature feature;
};

// 初始化后前台和后台搜索线程共用，特征缓存和两个模型各自带锁
struct LocatorResources
{
    MapLocatorConfig config;
    std::map<std::string, cv::Mat> zones;
    std::array<CachedGlobalSearchFeature, 2> search_feature_cache;
    std::uint64_t zone_generation = 0;
    std::unique_ptr<YoloModel> yolo;
    std::unique_ptr<OrientationModel> orientation;
    TrackingConfig tracking_config;
    ImageProcessingConfig base_image_config = { .dark_map_threshold = 20.0,
                                                .icon_diff_threshold = 40,
                                                .center_mask_radius = 18,
                                                .gradient_base_weight = 0.1,
                                                .border_margin = 10,
                                                .white_dilate = 11,
                                                .color_dilate = 3 };
    ImageProcessingConfig tier_image_config = { .dark_map_threshold = 20.0,
                                                .icon_diff_threshold = 40,
                                                .center_mask_radius = 8,
                                                .gradient_base_weight = 0.1,
                                                .border_margin = 8,
                                                .white_dilate = 9,
                                                .color_dilate = 3 };
    // 倒序析构时线程池最先停并等后台任务跑完；用 optional 直接持有，停的过程中任务仍能访问它和其余字段
    std::optional<MapLocatorScaleExecutor> executor;
};

struct AsyncYoloState
{
    std::uint64_t frame_id = 0;
    std::shared_future<YoloCoarseResult> future;
    bool zone_guard_applied = false;
};

// 每隔几秒在后台跑一次 YOLO，用来发现跟踪没察觉的换区和小地图消失
struct PeriodicYolo
{
    std::mutex mutex;
    std::optional<AsyncYoloState> pending;
    std::chrono::steady_clock::time_point last_check {};
};

// 小地图被遮挡的起始时刻，未遮挡时为默认值；超时放行后置位以免重复打日志
struct OcclusionGate
{
    std::chrono::steady_clock::time_point since {};
    bool timed_out = false;
};

struct MapLocator
{
    bool initialized = false;
    LocatorResources resources;
    MatchConfig match_config;
    MotionTrack track;
    std::string zone_id;
    std::optional<MapPosition> stable_position;
    std::vector<MapPosition> cold_start;
    OcclusionGate occlusion;
    std::uint64_t frame_id = 0;
    std::uint64_t active_frame_id = 0;
    // 最后声明、最先析构：后台 YOLO 推理要用 resources.yolo，析构时先等它跑完
    PeriodicYolo periodic_yolo;
};

bool InitializeLocator(MapLocator* locator, const MapLocatorConfig& config);
LocateResult Locate(MapLocator* locator, const cv::Mat& minimap, const LocateOptions& options);
void ResetTrackingState(MapLocator* locator);

} // namespace maplocator
