#pragma once

#include <cmath>
#include <cstddef>
#include <cstdint>

namespace mapnavigator
{

constexpr int32_t kWorkWidth = 1280;
constexpr int32_t kWorkHeight = 720;

constexpr double kNavigationCameraMinConfidence = 0.3;

constexpr double kTurn360UnitsPerWidth = 2.23006;
constexpr double kTurnDegreesPerCircle = 360.0;
constexpr double kPitchDegreesPerRange = 180.0;

struct AdbTouchTurnProfile
{
    double default_units_per_degree = 3.0;
    double default_pitch_units_per_degree =
        default_units_per_degree * kTurnDegreesPerCircle * kWorkHeight / (kWorkWidth * kPitchDegreesPerRange);
    int32_t swipe_duration_ms = 70;
    int32_t post_swipe_settle_ms = 0;
    // 移动指令后这段时间内发的转向会被游戏吞掉
    int32_t action_quiet_period_ms = 60;
};

inline constexpr AdbTouchTurnProfile kAdbTouchTurnProfile {};
constexpr double kWin32TurnScaleMinUnitsPerDegree = 1.0;
constexpr double kWin32TurnScaleMaxUnitsPerDegree = 50.0;

inline int ComputeTurn360Units(int32_t screen_width)
{
    return static_cast<int>(std::lround(kTurn360UnitsPerWidth * static_cast<double>(screen_width)));
}

inline double ComputeUnitsPerDegreeForWidth(int32_t screen_width)
{
    return static_cast<double>(ComputeTurn360Units(screen_width)) / kTurnDegreesPerCircle;
}

inline double ComputeDefaultUnitsPerDegree()
{
    return ComputeUnitsPerDegreeForWidth(kWorkWidth);
}

inline double ComputeUnitsPerDegreeForHeight(int32_t screen_height)
{
    return kTurn360UnitsPerWidth * static_cast<double>(screen_height) / kPitchDegreesPerRange;
}

inline double ComputeDefaultPitchUnitsPerDegree()
{
    return ComputeUnitsPerDegreeForHeight(kWorkHeight);
}

constexpr int32_t kActionSprintPressMs = 30;
constexpr int32_t kActionJumpHoldMs = 50;
constexpr int32_t kActionJumpSettleMs = 500;
constexpr int32_t kActionInteractAttempts = 5;
constexpr int32_t kActionInteractHoldMs = 100;
constexpr int32_t kAutoSprintCooldownMs = 1500;
constexpr double kAutoSprintMaxHeadingErrorDeg = 25.0;
constexpr double kAutoSprintMaxUpcomingTurnDeg = 40.0;
// 严判点前的刹车余量, 世界单位, 进入到点距离加这段之内就停止冲刺
constexpr double kStrictArrivalSprintBrakeDistance = 6.0;
constexpr int32_t kWalkResetReleaseMs = 120;
constexpr double kSamePointActionChainDistance = 0.2;

constexpr double kStrictSettleAcceptBandWu = 0.5;
constexpr int32_t kStrictSettleMaxPulses = 3;
// 连续这么多次读数都在首次读数的这个距离内才算站稳
constexpr double kStrictSettleStillWu = 0.05;
constexpr int32_t kStrictSettleStillReads = 3;
// 本进程还没有实测的按住时长与位移配对时, 先发这么长的试探脉冲
constexpr int32_t kStrictSettleProbeHoldMs = 150;
constexpr int32_t kStrictSettleMinHoldMs = 80; // 更短的按键可能被游戏整次漏掉

// slide_ms 是松键后角色继续滑行的最长时长
struct StrictSettleInputProfile
{
    int32_t slide_ms = 0;
    int32_t max_hold_ms = 0;
};

inline constexpr StrictSettleInputProfile kStrictSettleTouchProfile { .slide_ms = 1000, .max_hold_ms = 1500 };
inline constexpr StrictSettleInputProfile kStrictSettleKeyboardProfile { .slide_ms = 1300, .max_hold_ms = 1000 };

constexpr size_t kStrictSettlePulseHistory = 16;
constexpr int32_t kStrictSettlePulseSpreadMs = 30;               // 按住时长相差不到这么多的两次脉冲不用于拟合斜率
constexpr double kStrictSettlePulseDirectionToleranceDeg = 30.0; // 位移方向偏离超过此角的脉冲视为被地形推偏, 不计入
constexpr int32_t kStrictSettleBudgetMs = 6000;
constexpr int32_t kStrictSettleFixIntervalMs = 120;
constexpr int32_t kStrictSettleFixMaxFrames = 12;
constexpr double kStrictSettleHeadingToleranceDeg = 5.0;
constexpr int32_t kStrictSettleMaxHeadingTurns = 3;

constexpr int32_t kLocatorWaitMaxRetries = 100;
constexpr int32_t kLocatorWaitIntervalMs = 100;
constexpr int32_t kWaitAfterFirstTurnMs = 300;
constexpr double kLookaheadRadius = 2.5;
constexpr double kStrictArrivalLookaheadRadius = 2.0;
constexpr double kMicroThreshold = 3.0;
constexpr int32_t kLocatorRetryIntervalMs = 20;
constexpr int32_t kHighLatencyCaptureMs = 180;
constexpr int32_t kStopWaitMs = 150;
constexpr int32_t kTargetTickMs = 33;
// 按住前进却没移动的拍数, 到数就重发按键
constexpr int32_t kForwardHoldReassertTicks = 3;
constexpr int32_t kForwardHoldFutileReassertsBeforeRecovery = 2;
// 转速估计用的朝向参考最多隔这么多拍仍可用
constexpr uint64_t kSteeringRateMaxGapTicks = 4;
constexpr int32_t kSteeringRateReferenceMs = 100;
// 已发转向记为在途的时长, 过期视为被游戏吞掉
constexpr int64_t kSteeringPendingLifetimeMs = 600;
// 同一在途时长按转向拍数计, 与毫秒版取较长者
constexpr int64_t kSteeringPendingLifetimeTicks = 2;
// 转向周期取最近这么多拍间隔的中位数, 每个间隔先截到这个上限
constexpr int64_t kSteerPeriodMaxSampleMs = 1000;
constexpr size_t kSteerPeriodWindow = 8;
// 循环周期慢到这个值时, 不小于这个角的转向改为原地完成
constexpr int64_t kSlowLoopPivotPeriodMs = 300;
constexpr double kSlowLoopPivotTurnDeg = 45.0;
// 循环周期未知前, 首个达到此角的转向原地完成
constexpr double kStartupPivotTurnDeg = 90.0;
// 箭头与镜头朝向相差超过此角时视为箭头还在跟转, 转向改跟镜头
constexpr double kSteerArrowLagCameraDeg = 30.0;
// 慢循环下距弯道不足这么多拍行程时每个弯刹一次
constexpr double kCornerBrakeLeadTicks = 2.0;
constexpr double kCornerBrakeScanM = 8.0;
// 重规划后离已刹过的弯这么近的弯视为同一个
constexpr double kCornerBrakeLatchRadiusM = 1.5;
constexpr double kYawRateDegPerSec = 320.0;
constexpr int32_t kSteeringMaxBatchesPerTick = 3;
constexpr double kSteeringHeadingChangeEpsilonDeg = 0.05;
constexpr int32_t kPostHeadingForwardPulseMs = 270;
constexpr double kHeadingAcceptToleranceDeg = 40.0;
constexpr int32_t kHeadingVerifyMaxRetries = 3;
constexpr int32_t kHeadingTurnStepIntervalMs = 100; // 下限, 实际取它与后端最小发送间隔的较大者
constexpr double kHeadingStableReadToleranceDeg = 15.0;
constexpr int32_t kHeadingStableReadIntervalMs = 120;
constexpr int32_t kHeadingStableReadMaxFrames = 4;
constexpr double kCameraAlignMinDegrees = 10.0;
constexpr int32_t kSerialRouteRetryDelayMs = 180;
constexpr double kBootstrapOwnershipProjectionCorridor = 3.0;
constexpr double kBootstrapOwnershipProjectionFrontThreshold = 0.35;
constexpr double kBootstrapOwnershipProjectionMiddleThreshold = 0.60;
constexpr double kBootstrapOwnershipContinueBiasDistance = 0.5;
constexpr double kBootstrapOwnershipMaxDistance = 18.0;
// 起步不在线路上时, 站在某点上或比之前所有点都近出这个余量, 才允许跳过前面的路点
constexpr double kBootstrapOwnershipStandingDistance = 1.5;
constexpr double kBootstrapOwnershipDecisiveMargin = 5.0;
constexpr double kSerialRouteHeadingEpsilon = 2.0;
constexpr double kSerialRouteDeviationThreshold = 1.5;
constexpr double kSerialRouteDeviationFailThreshold = 3.0;
constexpr double kSerialRouteCompensationMinDistance = 1.0;
constexpr double kWaypointArrivalSlack = 0.5;
// 判定圈按通道半宽收紧后的下限
constexpr double kMinArrivalBand = 1.0;
constexpr int32_t kObstacleRecoveryMinTriggerMs = 3500;
constexpr int32_t kDynamicRecoveryRetryIntervalMs = kObstacleRecoveryMinTriggerMs;
constexpr int32_t kDynamicRecoveryTotalTimeoutMs = 30000;
constexpr int32_t kRecoveryJumpAttemptsBeforeDetour = 2;
constexpr double kDynamicRecoveryResetDistance = 2.0;
constexpr int32_t kRecoveryDetourAttemptsBeforeUnstick = 3;
// 卡住处前方放的禁行圆, 同处再卡半径加一档, 角色和目标锚点须在圆外留出间距
constexpr double kVirtualNoGoRadius = 2.0;
constexpr double kVirtualNoGoRadiusStep = 1.5;
constexpr double kVirtualNoGoRadiusMax = 6.5;
constexpr double kVirtualNoGoRadiusMin = 1.0;
constexpr double kVirtualNoGoStandoff = 1.0;
constexpr double kVirtualNoGoGoalClearance = 1.0;
constexpr double kUnstickSampleStepM = 0.5;
constexpr double kUnstickMaxRockCrossingM = 2.0; // 射线上允许越过的网格外长度, 更长视为水面, 放弃该方向
constexpr double kUnstickMeshMarginM = 1.0;      // 越过网格边缘后再多走的距离
constexpr double kUnstickMinDistanceM = 2.5;
constexpr double kUnstickMaxDistanceM = 6.0;     // 射线扫描距离, 也是最长脱困步
constexpr int32_t kUnstickPulseMs = 270;
constexpr int32_t kUnstickMaxPulses = 8;
constexpr double kUnstickResetDistanceM = 2.0;  // 离脱困起点这么远后重置方向轮换
constexpr double kUnstickSuccessFraction = 0.6; // 实际位移达到计划距离的这个比例算脱困成功
// 持续定位不到时每隔这么久跳一次, 超过超时直接失败
constexpr int32_t kLocalizationLossUnstickIntervalMs = kObstacleRecoveryMinTriggerMs;
constexpr int32_t kLocalizationLossTimeoutMs = kDynamicRecoveryTotalTimeoutMs;

constexpr int32_t kRiverFallRecoveryTimeoutMs = kDynamicRecoveryTotalTimeoutMs;
constexpr double kRiverFallRecoveryClearDistance = kDynamicRecoveryResetDistance;
constexpr int32_t kRiverFallRecoveryPulseMs = kPostHeadingForwardPulseMs;
constexpr int32_t kRiverFallRecoverySettleMs = 2000;

// 偏离通道且直线距离无进展时, 到时先重规划, 再到时直接失败
constexpr int32_t kOffRouteWedgeReplanMs = 6000;
constexpr int32_t kOffRouteWedgeReplanCooldownMs = 4000;
constexpr int32_t kOffRouteWedgeFailMs = 12000;

// 这么久没走出以锁存点为圆心的这个半径就失败, 半径单位为世界像素
constexpr double kDwellWatchdogRadius = 20.0;
constexpr int32_t kDwellWatchdogFailMs = 120000;

// 跨层脱困的到达距离, 须同时已回到基础区才算退出
constexpr double kCrossTierEscapeArrivalM = 3.0;
constexpr int32_t kCrossTierEscapeHardStallMs = 12000;
// 路点无推进期间全局重定位达到这么多次就失败
constexpr int32_t kLocalizationThrashFailCount = 5;

constexpr double kNavRunLookaheadLowSpeedM = 2.5;
// 前视距离按这么多拍的行程计, 再夹在下面的上下限之间
constexpr double kNavRunLookaheadPreviewTicks = 7.0;
constexpr double kNavRunLookaheadMinM = 2.0;
constexpr double kNavRunLookaheadMaxM = 14.0;
// 通道相对当前段的累计转角超过此值时瞄点停在该顶点
constexpr double kNavRunLookaheadTurnBudgetDeg = 50.0;
// 距弯道不足这么多拍行程时瞄点不再等待, 开始引入弯道
constexpr double kNavRunTurnCommitTicks = 4.0;
// 短于此的通道边方向只是网格量化噪声, 不取其方向
constexpr double kNavRunCorridorEdgeMinM = 0.75;
// 瞄点沿前视方向至少推出这么远, 且不短于前视距离
constexpr double kNavRunAimReachMinM = 6.0;
constexpr int32_t kNavRunSpeedWindowMs = 700;
constexpr int32_t kNavRunSpeedKeepMs = 2000;
constexpr size_t kNavRunSpeedMaxSamples = 8;
constexpr uint64_t kNavRunSpeedMaxSampleGapTicks = 8;
constexpr double kNavRunSpeedJumpMaxPxPerSec = 40.0;
constexpr double kNavRunUpcomingTurnLookaheadM = 8.0;
constexpr double kNavRunCrossTrackWarnM = 2.2;
constexpr double kNavRunCrossTrackFailM = 4.0;
// 单拍走廊投影沿线推进超出实际位移的上限
constexpr double kNavRunProjectionAdvanceSlackM = 6.5;
constexpr double kNavRunProgressReplanMinCrossTrackM = 1.25;
constexpr int32_t kNavRunSoftReplanCooldownMs = 1200;
constexpr int32_t kNavRunSoftReplanMaxPerAnchor = 3;
constexpr int32_t kNavRunProgressRegressionMs = 800;
constexpr int32_t kNavRunPlanFailureCooldownMs = 3000;

constexpr int32_t kZoneConfirmRetryIntervalMs = 120;
constexpr int32_t kZoneConfirmTimeoutMs = 12000;
constexpr int32_t kRelocationRetryIntervalMs = 120;
constexpr int32_t kRelocationWaitTimeoutMs = 15000;
constexpr int32_t kRelocationStableFixes = 2;
constexpr double kRelocationResumeMinDistance = 3.0;

// 按跳没起飞时收紧到离板心这一圈内再按
constexpr double kJumpPadRetryBandWu = 0.25;
constexpr int32_t kJumpPadLaunchWindowMs = 1200; // 按跳后等起飞的时长, 过后仍在板上算没起飞
constexpr int32_t kJumpPadMaxJumps = 3;
constexpr int32_t kJumpPadMaxNudges = 8;         // 转镜头与前进脉冲合计
constexpr int32_t kJumpPadBudgetMs = 20000;      // 从到板算到起飞

// 只认进入落点这一圈算滑完, 按位移判会在起滑瞬间成立
constexpr double kZiplineLandingBandWu = 6.0;
// 落地冷启动时围绕各落点开的定位搜索窗半径, 不含模板半宽
constexpr double kZiplineLandingHintRadiusWu = 24.0;
constexpr int32_t kZiplineRideRetryIntervalMs = 150;
constexpr int32_t kZiplineRideTimeoutMs = 30000;
// 起滑后连续这么多帧定位不到算已滑出, 滑行中小地图整个隐藏
constexpr int32_t kZiplineRideLostFixes = 3;
// 起滑后等这么久再开始量位移
constexpr int32_t kZiplineLaunchSettleMs = 400;
// 小地图重新出现后等这么久再采信定位
constexpr int32_t kZiplineLandingSettleMs = 400;
constexpr double kZiplineAimToleranceDeg = 6.0;
// 有邻架抢吸附时停点向远离它的一侧偏, 每滑错一次加一档
constexpr double kZiplineAimBiasStepDeg = 3.0;
// 偏过头没吸住时偏置减半重按, 减到这以下改换俯仰档
constexpr double kZiplineAimBiasMinDeg = 1.5;
// 先在停点外侧这么远处压俯仰再横扫回来
constexpr double kZiplineAimSweepLeadDeg = 15.0;
static_assert(kZiplineAimSweepLeadDeg > 2.0 * kZiplineAimToleranceDeg);
// 上索后稳定等待与水平修正共用的截止时间, 按初始残差放大, 不低于此值
constexpr int32_t kZiplineAimHeadingTimeoutMs = 6000;
// 架上小批转向会多转一截, 后续批次按实测转角与指令之比缩小, 比值封顶于此
constexpr double kZiplineAimResponseRatioMax = 3.0;
constexpr double kZiplineAimResponseMinCmdDeg = 2.0;
// 下索前朝向对准下一段走路方向的容差
constexpr double kZiplineExitAimToleranceDeg = 15.0;
constexpr int32_t kZiplineTurnBudgetBaseMs = 1500;
constexpr double kZiplineTurnBudgetPerDegMs = 45.0;
// 俯仰开环控制, 以上仰硬限位为 +90 度基准
constexpr double kZiplinePitchDeadbandDeg = 8.0;
constexpr double kZiplinePitchMaximumElevationDeg = 90.0;
constexpr double kZiplinePitchMaximumDepressionDeg = 60.0;
// 复位时超出硬限位多发的角度, 撞到限位后不会再转
constexpr double kZiplinePitchResetOvershootDeg = 30.0;
// 三次依次用算出的仰角、反向、不动俯仰
constexpr int32_t kZiplineLaunchAttempts = 3;
// 连续这么多拍位移都小于此值算滑行结束
constexpr double kZiplineSettleMoveWu = 1.5;
constexpr int32_t kZiplineSettleFixes = 4;
// 下索是一次右键, 站在架上时移动指令会被吃掉, 走路前必须先下索
constexpr int32_t kZiplineDismountHoldMs = 80;
// 真起滑会丢定位, 空响时坐标仍有效且不动, 在这段时间内区分两者
constexpr int32_t kZiplineLaunchConfirmMs = 1500;
constexpr double kZiplineMountMinMoveWu = 3.0;
// 架上提示读到一帧即认上索, 地面读数须连续多帧, 按键后 settle 期内不采信地面读数
constexpr int32_t kZiplineMountSettleMs = 600;
constexpr int32_t kZiplineMountWindowMs = 4000;
constexpr int32_t kZiplineMountOnGroundFixes = 2;
constexpr int32_t kZiplineMountPressBudget = 2;
// 同一上索点允许的重规划次数, 用完改走路, 须赶在偏航看门狗判失败之前
constexpr int32_t kZiplineApproachReplanBudget = 1;

// 退索后首帧可能仍是滑行中的错误结果, 只接受贴近网格且连续几帧一致的定位
constexpr int32_t kZiplineRecoveryStableFixes = 3;
constexpr double kZiplineRecoveryStableRadiusWu = 3.0;
constexpr int32_t kZiplineRecoveryRetryIntervalMs = 120;
constexpr int32_t kZiplineRecoveryTimeoutMs = 6000;
constexpr int32_t kReachableAnchorPlanAttemptsMax = 8;

// 封禁跳与规划候选的配对半径, 链首封禁记的是上索站位点
constexpr double kZiplineHopBanMatchWu = 10.0;
constexpr int32_t kZiplineAimFailBanCount = 2;
// 一趟导航弃索达到这么多次后不再让滑索参与规划
constexpr int32_t kZiplineAbandonWalkFallbackCount = 3;

// 按了一次没认出设备后收紧到的判定圈
constexpr double kZiplineRestandBandWu = 1.0;
// 顶着设备走不动这么久就换站位, 须早于恢复阶梯触发
constexpr int32_t kZiplineMountSpotStallMs = 2000;
static_assert(kZiplineMountSpotStallMs < kObstacleRecoveryMinTriggerMs);
constexpr int32_t kZiplineHopRetryBudget = 2;
// 基准时长, 两倍未下来再按一次, 四倍未下来算卡住
constexpr int32_t kZiplineDismountTimeoutMs = 2000;
// 落地定位对不上时给冷启动的时间
constexpr int32_t kZiplineUnknownTimeoutMs = 8000;
// 两个节点相距不到此值算同一根架子
constexpr double kZiplineTowerIdentityWu = 2.0;

constexpr double kNoProgressDistanceEpsilon = 0.5;
constexpr double kRouteProgressEpsilon = 0.5;
constexpr double kNoProgressMinDistance = 3.0;
constexpr double kMeasurementDefaultPositionQuantum = 0.25;
constexpr double kWaypointPassThroughCorridor = 3.0;
constexpr double kZoneTransitionIsolationDistance = 5.0;
constexpr double kPortalCommitDistance = 4.0;
constexpr double kSevereDivergenceYawDegrees = 85.0;
constexpr double kSevereDivergenceDistance = 5.0;
constexpr int32_t kSevereDivergenceStallMs = 800;
constexpr int32_t kPostTurnForwardCommitMs = 500;
constexpr double kPostTurnForwardCommitMinDegrees = 15.0;

constexpr const char* kDefaultNavmeshRelativePath = "assets/resource/model/map/navmesh/base.nav";
constexpr const char* kDefaultCompressedNavmeshRelativePath = "assets/resource/model/map/navmesh/base.nav.gz";
constexpr const char* kNoGoTableRelativePath = "data/MapNavigator/nogo_zones.json";

// 识别节点同时是 ROI 来源和路线文字的注入点
constexpr const char* kCollectEntryNode = "AutoCollectClickStart";
constexpr const char* kCollectRecognitionNode = "AutoCollectClick";
constexpr const char* kCollectExitNode = "AutoCollectClickEnd";
constexpr const char* kInteractEntryNode = "MapNavigatorInteractStart";
constexpr const char* kInteractRecognitionNode = "MapNavigatorInteract";
constexpr const char* kInteractExitNode = "MapNavigatorInteractEnd";
// 绳底交互点的识别文字取自该 OCR 节点的 expected
constexpr const char* kRopeInteractTextNode = "MapNavigatorRopeUseText";
// 确认上索要的是认不出, 必须走 Start 节点, 直接派发识别节点会耗满超时
constexpr const char* kZiplineMountEntryNode = "MapNavigatorZiplineMountStart";
constexpr const char* kZiplineMountRecognitionNode = "MapNavigatorZiplineMount";
constexpr const char* kZiplineMountExitNode = "MapNavigatorZiplineMountEnd";
constexpr const char* kZiplineMountScanNode = "MapNavigatorZiplineMountScan";
constexpr const char* kZiplinePitchResetNode = "MapNavigatorZiplinePitchReset";
constexpr const char* kZiplineOnGroundEntryNode = "MapNavigatorZiplineOnGroundStart";
constexpr const char* kZiplineOnGroundNode = "MapNavigatorZiplineOnGround";
constexpr const char* kZiplineOnTowerHintEntryNode = "MapNavigatorZiplineOnTowerHintStart";
constexpr const char* kZiplineOnTowerHintNode = "MapNavigatorZiplineOnTowerHint";
constexpr int32_t kPromptPostSleepMs = 80;

constexpr int32_t kPipelineRoiBaseWidth = 1280;
constexpr int32_t kPipelineRoiBaseHeight = 720;

// 交互预筛的兜底参数, 优先用扫描节点或路线自带的配置
constexpr const char* kPromptIconRelativePath = "RealTimeTask/AutoPick.png";
constexpr double kPromptIconMatchThreshold = 0.75;
constexpr const char* kInteractScanNode = "MapNavigatorInteractScan";
// 检测触发尝试的唯一节流, 只有真正尝试时才计时
constexpr int32_t kPromptScanIntervalMs = 1200;
constexpr double kCollectArrivalBandWu = 1.5;
// 这么久无进展就退回常规判定圈
constexpr int32_t kCollectArrivalRelaxMs = 6000;
// 吃掉最后一个采集点那一拍路线就结束, 收尾再给扫描器这么久
constexpr int32_t kCollectTailGraceMs = 1500;
constexpr double kCollectSprintSuppressBandWu = 8.0;
constexpr int32_t kSprintCancelReleaseMs = 60;

// 进出带分开是为了迟滞, 每按一次走路键都会翻转游戏状态
constexpr double kCollectWalkEnterBandWu = 3.0;
constexpr double kCollectWalkExitBandWu = 4.5;
constexpr double kDigWalkEnterBandWu = 5.0;
constexpr double kDigWalkExitBandWu = 7.5;
constexpr double kZiplineWalkEnterBandWu = 5.0;
constexpr double kZiplineWalkExitBandWu = 7.5;
static_assert(kCollectWalkEnterBandWu < kCollectWalkExitBandWu, "collect walk enter band must be smaller than its exit band");
static_assert(kDigWalkEnterBandWu < kDigWalkExitBandWu, "dig walk enter band must be smaller than its exit band");
static_assert(
    kStrictArrivalLookaheadRadius + kMeasurementDefaultPositionQuantum < kDigWalkEnterBandWu,
    "dig walking must start before arrival");
static_assert(kDigWalkExitBandWu < kCollectSprintSuppressBandWu, "dig walk bands must sit inside the sprint-suppress band");
static_assert(kZiplineWalkEnterBandWu < kZiplineWalkExitBandWu, "zipline walk enter band must be smaller than its exit band");
static_assert(kZiplineWalkExitBandWu < kCollectSprintSuppressBandWu, "walk bands must sit inside the sprint-suppress band");

// 提示图标对屏上任何可交互物都会亮, 触发须留在这个窄圈内
constexpr double kPromptTriggerBandWu = 3.0;
static_assert(kPromptTriggerBandWu < kZiplineWalkEnterBandWu, "zipline walking must start before prompt triggering");

// 走路时速度和转速减半, 按跑步定的时间窗乘这个倍数
constexpr int32_t kWalkModeSlowFactor = 2;
constexpr int32_t kActionWalkTogglePressMs = 30;

constexpr const char* kObstacleDeviceEntry = "MapNavigatorObstacleDevice";
constexpr const char* kObstacleDeviceProbeNode = "__MapNavigatorObstacleDevice_InteractPre";
constexpr const char* kObstacleDeviceTemplateRelativePath = "resource/image/MapNavigator/ObstacleDevice/InteractButton.png";
constexpr double kObstacleDeviceMatchThreshold = 0.65;
constexpr const char* kObstacleDeviceMoveNode = "__MapNavigatorObstacleDevice_MoveDevice";
constexpr const char* kObstacleAicCoreMoveNode = "__MapNavigatorObstacleDevice_MoveAICCore";
constexpr const char* kObstacleDeviceWalkOutEntry = "MapNavigatorObstacleDeviceWalkOut";
constexpr const char* kObstacleAicCoreWalkOutEntry = "MapNavigatorObstacleDeviceWalkOutAICCore";
constexpr int32_t kRecoveryDeviceAttempts = 1;

constexpr const char* kDefaultDigEntry = "AutoCollectDigStart";
constexpr const char* kDigPipelineOverride = R"({"AutoCollectDigEnd":{"next":[]}})";
constexpr int32_t kDigPostSleepMs = 80;

// find_text 注入此 OCR 节点的 expected 后逐帧调用, 不派发
constexpr const char* kFindInlineOcrNode = "MapNavigatorFind";
constexpr double kFindSearchStepDeg = 30.0;
constexpr int32_t kFindMissGraceTicks = 3;
// 框中心离画面中线的容差, 1280 基准帧像素
constexpr int32_t kFindAlignTolerancePx = 80;
// 偏移在此以内边走边转, 超出则先站定转正
constexpr int32_t kFindWalkWhileTurningPx = kFindAlignTolerancePx * 2;
constexpr int32_t kFindStopProbeWindowMs = 700;
constexpr int32_t kFindStopProbeIntervalMs = 120;
// 框中心低于画面高度的这个比例算走过头
constexpr double kFindPassedCenterYRatio = 0.667;
constexpr int32_t kFindBackwardPulseMs = 200;
constexpr double kFindSteerGain = 0.33;
// 每步末尾的节流, 兼作下一步转向的静默期
constexpr int32_t kFindStepSleepMs = 120;
constexpr int32_t kFindMaxSteps = 48;
constexpr int32_t kFindBudgetMs = 60000;
static_assert(kFindStepSleepMs > kAdbTouchTurnProfile.action_quiet_period_ms, "find pacing must outlast the steering quiet period");

// 行进中跑 trigger_node 的间隔
constexpr int32_t kTriggerProbeIntervalMs = 300;

} // namespace mapnavigator
