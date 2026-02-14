#pragma once

#include <string>
#include <vector>

namespace maplocator {

struct MapPosition {
  std::string zoneId;
  double x;
  double y;
  double avgDiff;
  int sliceIndex;
};

// roi及搜索相关常量
constexpr int MinimapROIWidth = 117;
constexpr int MinimapROIHeight = 120;
constexpr int MaxLostTrackingCount = 3;
constexpr double MinMatchScore = 0.7;
constexpr double MobileSearchRadius = 50.0;

struct TrackingConfig {
  double maxNormalSpeed = 40.0;        // px/s
  double screenBlockedThreshold = 15000.0; // px^2
  int edgeSnapMargin = 1;
  double velocitySmoothingAlpha = 0.5;  // EMA平滑系数
  double maxDtForPrediction = 5.0;      // 超时则放弃速度预测
};

struct MatchConfig {
  int blurSize = 5;
  double coarseScale = 0.5;
  int fineSearchRadius = 40;            // 精搜半径(px)
  double passThreshold = 9000.0;       // 全局搜索及格线, 容忍UI遮挡+光影
  double yoloConfThreshold = 0.60;
};

struct ImageProcessingConfig {
  double darkMapThreshold = 40.0;
  int iconDiffThreshold = 40;           // 黄/蓝图标与地图色差判定
  int centerMaskRadius = 12;            // 玩家箭头遮蔽半径
  double gradientBaseWeight = 0.1;      // 保底权重
  int minimapDarkMaskThreshold = 40;    // 与PreprocessMap阈值对齐
};

} // namespace maplocator
