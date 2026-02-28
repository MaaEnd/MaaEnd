#pragma once

#include <string>
#include <optional>

namespace maplocator {

struct MapPosition {
  std::string zoneId;
  double x = 0.0;
  double y = 0.0;
  double score = 0.0;
  int sliceIndex = 0;
  double scale = 1.0;
  double angle = 0.0;
  long long latencyMs = 0;
};

struct MapLocatorConfig {
    std::string mapResourceDir;
    std::string yoloModelPath;
    int yoloThreads = 1;
};

struct LocateOptions {
    double minScoreThreshold = 0.55;  // 最低分数线
    double yoloConfThreshold = 0.70;
    bool forceGlobalSearch = false;   // 是否强制放弃当前追踪，进行全局全图搜
    std::string expectedZoneId = "";  // 预期所在区域 (可作为先验知识加速)
    int maxLostFrames = 3;            // 允许丢失追踪的帧数
};

// --- 返回结果枚举与封装 ---
enum class LocateStatus {
    Success,
    TrackingLost,      // 追踪丢失，且全局搜失败
    ScreenBlocked,     // 画面被UI大面积遮挡
    Teleported,        // 速度异常判定为传送
    YoloFailed,        // YOLO未识别出合法地图
    NotInitialized
};

struct LocateResult {
    LocateStatus status;
    std::optional<MapPosition> position;
    std::string debugMessage; // 用于向 Pipeline 输出日志
};

// roi及搜索相关常量
constexpr int MinimapROIOriginX = 49;
constexpr int MinimapROIOriginY = 51;
constexpr int MinimapROIWidth = 118;
constexpr int MinimapROIHeight = 120;
constexpr int MaxLostTrackingCount = 3;
constexpr double MinMatchScore = 0.7;
constexpr double MobileSearchRadius = 50.0;

struct TrackingConfig {
  double maxNormalSpeed = 40.0;        // px/s
  double screenBlockedThreshold = 0.4;     // NCC correlation below this means blocked
  int edgeSnapMargin = 1;
  double velocitySmoothingAlpha = 0.5;  // 平滑系数
  double maxDtForPrediction = 5.0;      // 超时则放弃速度预测
};
struct MatchConfig {
  int blurSize = 7;
  double coarseScale = 0.5;
  int fineSearchRadius = 40;            // 精搜半径(px)
  double passThreshold = 0.55;           // 全局搜索及格线, 容忍UI遮挡+光影
  double yoloConfThreshold = 0.60;
};

struct ImageProcessingConfig {
  double darkMapThreshold;
  int iconDiffThreshold;           // 黄/蓝图标与地图色差判定
  int centerMaskRadius;            // 玩家箭头遮蔽半径
  double gradientBaseWeight;       // 保底权重
  int minimapDarkMaskThreshold;    // 与暗部阈值对齐
  int borderMargin;
  int whiteDilate;
  int colorDilate;
  bool useHsvWhiteMask;
};

} // namespace maplocator
