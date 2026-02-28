#pragma once

#include "MapTypes.h"
#include <opencv2/opencv.hpp>
#include <chrono>
#include <memory>
#include <string>

namespace maplocator {

struct MatchFeature {
    cv::Mat image;      // 最终参与 matchTemplate 的图 (灰度图或梯度热力图)
    cv::Mat mask;       // 对应的权重 Mask
    cv::Mat templRaw;   // 给YOLO吃的未预处理原图
};

struct MatchResultRaw {
    double score = -1.0;
    cv::Point loc{0,0};

    // 置信度指标
    double secondScore = -1.0;
    double delta = 0.0;
    double psr = 0.0;
};

struct TrackingValidation {
    bool isValid;
    bool isEdgeSnapped;
    bool isTeleported;
    bool isScreenBlocked;
    double absX, absY;
};

class IMatchStrategy {
public:
    virtual ~IMatchStrategy() = default;

    // 预处理小地图（模板）
    virtual MatchFeature extractTemplateFeature(const cv::Mat& minimap) = 0;

    // 预处理大地图的搜索区域（Search ROI）
    virtual MatchFeature extractSearchFeature(const cv::Mat& mapRoi) = 0;

    // 追踪态的结果验证逻辑
    virtual TrackingValidation validateTracking(
        const MatchResultRaw& trackResult, 
        std::chrono::duration<double> dt,
        std::optional<MapPosition> lastPos,
        const cv::Rect& searchRect,
        int templCols,
        int templRows) = 0;

    // 全局搜索的结果验证逻辑
    virtual bool validateGlobalSearch(const MatchResultRaw& fineRes, double& outScore) = 0;

    // 用于Chamfer补偿
    virtual bool needsChamferCompensation() const { return false; }
};

enum class MatchMode { 
    Auto, 
    ForceStandard, 
    ForcePathHeatmap 
};

class MatchStrategyFactory {
public:
    static std::unique_ptr<IMatchStrategy> create(
        const std::string& zoneId,
        const TrackingConfig& trackingCfg,
        const MatchConfig& matchCfg,
        const ImageProcessingConfig& baseImgCfg,
        const ImageProcessingConfig& tierImgCfg,
        MatchMode mode = MatchMode::Auto);
};

std::optional<MatchResultRaw> CoreMatch(
    const cv::Mat& searchImgRaw,
    const cv::Mat& templRaw,
    const cv::Mat& weightMask,
    int blurSize = 5);

} // namespace maplocator
