#pragma once

#include <chrono>
#include <string>

#include <MaaUtils/NoWarningCV.hpp>

#include "MapTypes.h"

namespace maplocator
{

struct MatchFeature
{
    cv::Mat image;    // 最终参与 matchTemplate 的图 (灰度图或梯度热力图)
    cv::Mat mask;     // 对应的权重 Mask
    cv::Mat templRaw; // 给YOLO吃的未预处理原图
};

struct MatchResultRaw
{
    double score = -1.0;
    cv::Point2d loc { 0.0, 0.0 };

    // 置信度指标
    double secondScore = -1.0;
    double delta = 0.0;
    double psr = 0.0;
};

struct PreparedSearchFeature
{
    cv::Mat image;
};

enum class TemplateFeatureKind
{
    StandardBase,
    StandardTier,
    PathHeatmapBase,
    PathHeatmapTier,
};

struct TrackingValidation
{
    bool isValid;
    bool isEdgeSnapped;
    bool isTeleported;
    bool isScreenBlocked;
    double absX, absY;
};

enum class MatchStrategyKind
{
    Standard,
    PathHeatmap,
};

struct MatchStrategy
{
    MatchStrategyKind kind = MatchStrategyKind::Standard;
    bool is_base = false;
    TrackingConfig tracking;
    MatchConfig match;
    ImageProcessingConfig base_image {};
    ImageProcessingConfig tier_image {};
};

MatchStrategy MakeMatchStrategy(
    const std::string& zoneId,
    const TrackingConfig& trackingCfg,
    const MatchConfig& matchCfg,
    const ImageProcessingConfig& baseImgCfg,
    const ImageProcessingConfig& tierImgCfg);

// 预处理小地图（模板）
MatchFeature ExtractTemplateFeature(const MatchStrategy& strategy, const cv::Mat& minimap);

// 预处理大地图的搜索区域（Search ROI）
MatchFeature ExtractSearchFeature(const MatchStrategy& strategy, const cv::Mat& mapRoi);

TemplateFeatureKind TemplateKindOf(const MatchStrategy& strategy);

// 追踪态的结果验证逻辑
TrackingValidation ValidateTracking(
    const MatchStrategy& strategy,
    const MatchResultRaw& trackResult,
    std::chrono::duration<double> dt,
    std::optional<MapPosition> lastPos,
    const cv::Rect& searchRect,
    int templCols,
    int templRows);

// 全局搜索的结果验证逻辑
bool ValidateGlobalSearch(const MatchStrategy& strategy, double score);

// 用于Chamfer补偿
bool NeedsChamferCompensation(const MatchStrategy& strategy);

enum class PeakRefineMode
{
    // 三点抛物线。相关面的离散格点就是精度上限，够粗扫把精修窗放到正确位置
    Parabola,
    // 在全分辨率上连续求极大，不受相关面格点限制。最终输出的坐标走这条
    Continuous,
};

double DeliveredScore(TemplateFeatureKind kind, const MatchResultRaw& window, const MatchResultRaw& refined);
std::optional<MatchResultRaw> CoreMatch(const cv::Mat& searchImgRaw, const cv::Mat& templRaw, const cv::Mat& weightMask);
PreparedSearchFeature PrepareSearchFeature(const cv::Mat& searchImgRaw);
std::optional<MatchResultRaw> CoreMatchPrepared(
    const PreparedSearchFeature& searchFeature,
    const cv::Mat& templRaw,
    const cv::Mat& weightMask,
    PeakRefineMode refineMode = PeakRefineMode::Parabola);

} // namespace maplocator
