#pragma once

#include <cmath>
#include <cstddef>
#include <cstdint>

namespace mapnavigator
{

constexpr int32_t kWorkWidth = 1280;
constexpr int32_t kWorkHeight = 720;

// --- ActionWrapper Constants ---
constexpr double kTurn360UnitsPerWidth = 2.23006;
constexpr double kTurnDegreesPerCircle = 360.0;

struct AdbTouchTurnProfile
{
    double default_units_per_degree = 5.0;
    int32_t swipe_duration_ms = 70;
    int32_t post_swipe_settle_ms = 0;
};

inline constexpr AdbTouchTurnProfile kAdbTouchTurnProfile {};
constexpr double kAdbTurnScaleMinUnitsPerDegree = 1.0;
constexpr double kAdbTurnScaleMaxUnitsPerDegree = 4.0;
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

// 俯仰用的是同一个系数, 只是换成屏幕高这条边
inline double ComputeUnitsPerDegreeForHeight(int32_t screen_height)
{
    return kTurn360UnitsPerWidth * static_cast<double>(screen_height) / kTurnDegreesPerCircle;
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
// Braking buffer (world units) ahead of a strict-arrival waypoint: sprint stays allowed until within
// arrival_distance + this margin, leaving room to brake and land precisely.
constexpr double kStrictArrivalSprintBrakeDistance = 6.0;
constexpr int32_t kWalkResetReleaseMs = 120;
constexpr double kSamePointActionChainDistance = 0.2;

// --- Navigation Mainline Constants ---
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
// Steering ticks of held-forward-but-motionless before the hold is re-sent. Counted in ticks so slow frame
// capture does not stretch the wait: three of them is already past any single dropped fix or brake settle.
constexpr int32_t kForwardHoldReassertTicks = 3;
// Re-sends that changed nothing before recovery is let in early. One says the keydown was dropped and is worth
// repeating; a second with the agent still exactly put says it was not, so waiting out the stall clock only
// buys more seconds of walking into whatever is there.
constexpr int32_t kForwardHoldFutileReassertsBeforeRecovery = 2;
// How many navigate ticks a heading reference stays usable for. Sized to match the wall-clock cap this
// replaced at the loop period of a fast machine, so nothing changes there; on a slow one it stretches
// with the loop instead of silently discarding the rate.
constexpr uint64_t kSteeringRateMaxGapTicks = 4;
constexpr int32_t kSteeringRateReferenceMs = 100;
// How long a sent turn may still be owed before it is written off. Measured on device the game yaws at about
// 100 deg/s, so a capped command needs some 300ms to land; past double that the drag was swallowed, and holding
// the debt any longer would suppress steering against a turn that is never arriving.
constexpr int64_t kSteeringPendingLifetimeMs = 600;
// Turn batches one tick may spend. The backend's per-batch cap is a per-drag reliability limit, not a budget
// for the whole tick: a slow loop gets fewer, longer ticks, so one batch each would shrink the turn achieved
// per metre walked just as the lag makes more of it necessary. Bounded so a misread heading cannot spin far.
constexpr int32_t kSteeringMaxBatchesPerTick = 3;
constexpr double kSteeringHeadingChangeEpsilonDeg = 0.05;
constexpr int32_t kPostHeadingForwardPulseMs = 270;
constexpr double kHeadingAcceptToleranceDeg = 40.0;
constexpr int32_t kHeadingVerifyMaxRetries = 3;
constexpr int32_t kHeadingTurnStepIntervalMs = 100;     // step pacing floor; raised to the backend min send interval
constexpr double kHeadingStableReadToleranceDeg = 15.0; // two fresh reads must agree this closely to count
constexpr int32_t kHeadingStableReadIntervalMs = 120;
constexpr int32_t kHeadingStableReadMaxFrames = 4;      // read budget; no stable pair -> accept open-loop
constexpr int32_t kSerialRouteRetryDelayMs = 180;
constexpr double kBootstrapOwnershipProjectionCorridor = 3.0;
constexpr double kBootstrapOwnershipProjectionFrontThreshold = 0.35;
constexpr double kBootstrapOwnershipProjectionMiddleThreshold = 0.60;
constexpr double kBootstrapOwnershipContinueBiasDistance = 0.5;
constexpr double kBootstrapOwnershipMaxDistance = 18.0;
// Off-line bootstrap: skipping waypoints needs evidence, not just "this one happens to be nearest".
// Standing on a point, or being clear of every earlier point by this margin, counts as evidence.
constexpr double kBootstrapOwnershipStandingDistance = 1.5;
constexpr double kBootstrapOwnershipDecisiveMargin = 5.0;
constexpr double kSerialRouteHeadingEpsilon = 2.0;
constexpr double kSerialRouteDeviationThreshold = 1.5;
constexpr double kSerialRouteDeviationFailThreshold = 3.0;
constexpr double kSerialRouteCompensationMinDistance = 1.0;
constexpr double kWaypointArrivalSlack = 0.5;
// 判定圈按通道半宽收紧后的下限, 低于此值定位量化误差会让路点吃不掉
constexpr double kMinArrivalBand = 1.0;
constexpr int32_t kObstacleRecoveryMinTriggerMs = 3500;
constexpr int32_t kDynamicRecoveryRetryIntervalMs = kObstacleRecoveryMinTriggerMs;
constexpr int32_t kDynamicRecoveryTotalTimeoutMs = 30000;
// Jump is the primary obstacle response; only after this many jumps fail to break free does
// recovery fall back to a navmesh detour. Keeps the agent hopping low blockers before it
// abandons the precise route.
constexpr int32_t kRecoveryJumpAttemptsBeforeDetour = 2;
constexpr double kDynamicRecoveryResetDistance = 2.0;
constexpr double kCloseGoalDetourSuppressSlack = 6.0;
constexpr int32_t kRecoveryDetourAttemptsBeforeUnstick = 1;
constexpr double kUnstickSampleStepM = 0.5;  // per-ray on/off scan resolution (world units)
constexpr double kUnstickMaxRockCrossingM =
    2.0;                                     // tolerate this much off-mesh (the rock) before solid ground; longer = water => reject bearing
constexpr double kUnstickMeshMarginM = 1.0;  // step this far past the mesh edge so we land ON solid ground
constexpr double kUnstickMinDistanceM = 2.5; // shortest committed dislodge step
constexpr double kUnstickMaxDistanceM = 6.0; // ray-scan reach / longest dislodge step
constexpr int32_t kUnstickPulseMs = 270;     // per forward pulse after turning to the escape bearing
constexpr int32_t kUnstickMaxPulses = 8;     // committed-walk cap; displacement exit usually ends it sooner
constexpr double kUnstickResetDistanceM = 2.0;  // relocated this far from the unstick origin => reset bearing rotation
constexpr double kUnstickSuccessFraction = 0.6; // displacement >= this * planned dist => a real dislodge step
// When the locator yields no usable fix for a sustained period (e.g. the agent was shoved across a
// zone boundary into a sub-zone the active route was not planned in, so every fix fails zone
// validation), stop holding forward into the obstacle, hop periodically to dislodge, and fail-fast
// once the loss outlasts the timeout so the pipeline can retry instead of stalling forever.
constexpr int32_t kLocalizationLossUnstickIntervalMs = kObstacleRecoveryMinTriggerMs;
constexpr int32_t kLocalizationLossTimeoutMs = kDynamicRecoveryTotalTimeoutMs;

// River-fall recovery (see navigator-river-fall-teleport-gap): black-screen loss = fell in water, teleported to
// shore facing it. Stand still until the arrow is readable again, turn 180° once, then pulse inland until clear;
// hard clock bounds thin-shore re-fall loops.
constexpr int32_t kRiverFallRecoveryTimeoutMs = kDynamicRecoveryTotalTimeoutMs;   // 30s clean fail-fast
constexpr double kRiverFallRecoveryClearDistance = kDynamicRecoveryResetDistance; // walked 2m clear of shore
constexpr int32_t kRiverFallRecoveryPulseMs = kPostHeadingForwardPulseMs;         // proven heading-commit pulse
constexpr int32_t kRiverFallRecoverySettleMs = 2000;                              // 上岸后的读数要等它稳下来

// Off-route wedge watchdog. Corridor progress (what the stall clocks see) keeps advancing while the authored
// cursor is pinned far off-route, so a bad latch wanders with zero route progress until the action hard-fails.
// Runs only while off-corridor with no straight-line gain: replan first, then fail-fast so the pipeline retries.
constexpr int32_t kOffRouteWedgeReplanMs = 6000;
constexpr int32_t kOffRouteWedgeReplanCooldownMs = 4000;
constexpr int32_t kOffRouteWedgeFailMs = 12000;

// Cross-tier escape (wrong-tier fall): plan ONE navmesh corridor from a walkable FLOORED-tier fix back to the
// nearest reachable authored waypoint and follow it as a fixed corridor (riding the legitimate tier<->base
// oscillation). Exit needs BOTH arrival distance AND a floor-blind (base) zone — a shaft's lower loops pass under
// the rim in (x,y), so distance alone fires early on the tier.
constexpr double kCrossTierEscapeArrivalM = 3.0;
// Escape fast-fail (mode B: escape stays active but the corridor is walled/unfollowable). Keyed on the
// hard-progress clock, which mid-escape overlay re-applies cannot reset, so it measures genuine no-corridor-
// progress. Progress-aware: a long-but-advancing escape is never killed early; only a continuously stuck one trips.
constexpr int32_t kCrossTierEscapeHardStallMs = 12000;
// Wrong-tier thrash fast-fail (mode A: recover<->re-lose storm). Every global re-acquire resets every progress/
// loss clock, so no timeout fires; instead count consecutive global re-acquires since the last waypoint advance
// and fail here. Normal travel sees 0-1 transient losses per leg, so this many with zero advance is pathological.
constexpr int32_t kLocalizationThrashFailCount = 5;

// --- NavRunController (RUN corridor follower) ---
constexpr double kNavRunLookaheadLowSpeedM = 2.5;
// The lookahead point is measured forward along the corridor and supplies the direction the agent
// steers along, so it is the whole turn anticipation budget: the steering target only starts
// rotating once the corner is inside it. Held as a count of ticks worth of travel so it tracks both
// speed and loop period: a fixed distance over-anticipates whenever the agent slows, and leaves too
// little room to react when the link is slow enough that each tick covers more ground.
// Calibrated on device by blind A/B: 7 beat 5 in both presentation orders, beat 9, and 11 was
// clearly harmful (cut corners, wandered, took twice as long).
constexpr double kNavRunLookaheadPreviewTicks = 7.0;
constexpr double kNavRunLookaheadMinM = 2.0;
constexpr double kNavRunLookaheadMaxM = 14.0;
// How far the corridor may bend away from the leg the agent is on before the aim point stops advancing and
// waits at that vertex. Measured as the total turn accumulated from the current leg, not per vertex: a
// staircase of four 30 degree bends leaves the corridor just as badly as one sharp corner does.
constexpr double kNavRunLookaheadTurnBudgetDeg = 50.0;
// Ticks of travel before a bend at which the aim stops waiting for it and starts leading into it. Fewer than
// the preview ticks above, so the aim still stops short of the bend rather than reaching around it.
constexpr double kNavRunTurnCommitTicks = 4.0;
// Corridor edges shorter than this carry no usable direction: the planner places vertices a grid cell apart,
// so a sub-cell edge's heading is quantisation noise, and a spurious bend on one would park the aim point.
constexpr double kNavRunCorridorEdgeMinM = 0.75;
// The aim point is pushed at least this far along that direction. Heading error from a cross-track
// offset is atan(offset / reach), so without a floor the position quantum's contribution grows as
// the lookahead shrinks and starts clearing the steering deadband on its own. Never shortened below
// the lookahead itself: a reach tighter than the anticipation budget over-steers into the loop lag.
constexpr double kNavRunAimReachMinM = 6.0;
constexpr int32_t kNavRunSpeedWindowMs = 700;
constexpr int32_t kNavRunSpeedKeepMs = 2000;
constexpr size_t kNavRunSpeedMaxSamples = 8;
constexpr uint64_t kNavRunSpeedMaxSampleGapTicks = 8;
constexpr double kNavRunSpeedJumpMaxPxPerSec = 40.0;
constexpr double kNavRunUpcomingTurnLookaheadM = 8.0;
constexpr double kNavRunCrossTrackWarnM = 2.2;
constexpr double kNavRunCrossTrackFailM = 4.0;
constexpr double kNavRunProgressReplanMinCrossTrackM = 1.25;
constexpr int32_t kNavRunSoftReplanCooldownMs = 1200;
constexpr int32_t kNavRunSoftReplanMaxPerAnchor = 3;
constexpr int32_t kNavRunProgressRegressionMs = 800;
// A failed plan leaves nothing to invalidate, so without a cooldown the next tick rebuilds immediately.
// A failing plan is also the slowest kind (it walks the whole window-expansion ladder before giving up),
// so the retry storm costs the tick loop far more than it costs to fall back to waypoint steering.
constexpr int32_t kNavRunPlanFailureCooldownMs = 3000;

// --- Zone / Portal / Transfer Constants ---
constexpr int32_t kZoneConfirmRetryIntervalMs = 120;
constexpr int32_t kZoneConfirmTimeoutMs = 12000;
constexpr int32_t kZoneConfirmStableFrames = 2;
constexpr int32_t kRelocationRetryIntervalMs = 120;
constexpr int32_t kRelocationWaitTimeoutMs = 15000;
constexpr int32_t kRelocationStableFixes = 2;
constexpr double kRelocationResumeMinDistance = 3.0;

// --- Zipline Constants ---
// 一跳分三步: 站上架子(仅链首)、把镜头对准落点、按左键起滑。滑行途中人悬在半空, 位置一路在动,
// 所以判完成只认「进了落点圈」这一条, 任何「动了就算走完」的判据都会在起滑瞬间成立
constexpr double kZiplineLandingBandWu = 6.0;
constexpr int32_t kZiplineRideRetryIntervalMs = 150;
constexpr int32_t kZiplineRideTimeoutMs = 30000;
// 落点圈内还要连着读到这么多个非 held 定位才收工, 避免滑行途中恰好飞过落点上方就提前落地
constexpr int32_t kZiplineLandingStableFixes = 2;
// 站上架子后交互提示就没了, 所以确认只看「还认不认得出这条提示」。留一小段窗口是因为按下的
// 那一帧提示往往还在
constexpr int32_t kZiplineMountConfirmAttempts = 6;
constexpr int32_t kZiplineMountConfirmIntervalMs = 250;
// 起滑按完先等这么久再开始量位移, 免得把起步前的几帧当成没滑起来
constexpr int32_t kZiplineLaunchSettleMs = 400;
// 瞄准精度只能在按左键之前保证: 按下去人就滑走了, 半空里没有跟随层能把方向修回来。走路那套
// 40 度容差是靠跟随层善后才敢留的, 这里不能用
constexpr double kZiplineAimToleranceDeg = 6.0;
constexpr int32_t kZiplineAimMaxRetries = 3;
// 落差够大时镜头得抬到索的仰角上才起得了滑。小地图读不到俯仰, 所以这一下是开环发出去的,
// 不复核也不迭代
constexpr double kZiplinePitchDeadbandDeg = 8.0;
constexpr double kZiplinePitchMaxDeg = 60.0;
// 按下去没滑走就换一档俯仰再按。三次分别是: 按算出来的仰角、反向、完全不动俯仰
constexpr int32_t kZiplineLaunchAttempts = 3;
// 滑行中位置每拍都在变, 连着这么多拍几乎不动就说明这趟已经结束了
constexpr double kZiplineSettleMoveWu = 1.5;
constexpr int32_t kZiplineSettleFixes = 4;
// 下索是一次右键。站在架子上时移动指令会被架子的选点状态吃掉, 所以走路之前必须先下来
constexpr int32_t kZiplineDismountHoldMs = 80;
// 索没通电、两端根本没挂索时起滑是空响, 人还站在架子上。滑一趟是大位移, 所以「过了确认时间
// 还在原地」与「滑起来了但没到落点」分得开, 不必耗满整个滑行超时。两个值待实机核准
constexpr int32_t kZiplineMountConfirmMs = 5000;
constexpr double kZiplineMountMinMoveWu = 3.0;
// 同一个上索点最多让重规划试这么多次, 再要重规划就当这根架子够不着, 退索改走路。楔死看门狗
// 6s 重规划一次、12s 掐掉整趟导航, 所以这里必须小到能在它掐之前让出路来。滑索省下的那点路
// 远不值一次导航失败, 判错方向只损失一段捷径
constexpr int32_t kZiplineApproachReplanBudget = 1;

// 退索后的第一帧可能仍是滑行期间的错误跟踪结果。恢复只接受贴近 navmesh、连续数帧彼此一致的
// 新定位；超时直接结束本次导航，绝不拿预计算的离索路线从错误落点继续走。
constexpr int32_t kZiplineRecoveryStableFixes = 3;
constexpr double kZiplineRecoveryStableRadiusWu = 3.0;
constexpr int32_t kZiplineRecoveryRetryIntervalMs = 120;
constexpr int32_t kZiplineRecoveryTimeoutMs = 6000;
// 可达锚点扫描的串行 A* 预算。每次不可达都要跑满一次搜索(秒级), 曾出现 34 连败额外卡约 70s;
// 预算用尽就放弃这轮扫描, 交给调用侧的下一级回退。
constexpr int32_t kReachableAnchorPlanAttemptsMax = 8;

// 封禁跳与规划候选的配对半径。链首封禁记录的是上索走位点而不是架子本身(相差供电桩让位
// 那一点点), 太紧封不住; 太松会顺带罚掉旁边平行的另一根索, 代价只是少一条捷径。
constexpr double kZiplineHopBanMatchWu = 10.0;
// 一趟导航里弃索这么多次说明这一带的标定或定位整体不可靠, 重展开不再让滑索参与,
// 顺带兜住"封一跳、换一链、再失败"的重试链条。
constexpr int32_t kZiplineAbandonWalkFallbackCount = 3;

// 按了一次没认出来之后的判定圈。交互给的是离身位最近的那台设备, 认不出就得挪身位再认 ——
// 判定圈收到这里, 让人真把那点距离走完(有备用站位就是走过去, 没有就是再走近点)。
// 再往下收就到定位噪声底下了, 收不拢只会白等看门狗
constexpr double kZiplineRestandBandWu = 1.0;

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

// Prompt-driven actions (collect / async interact), three nodes per kind: entry, authoritative recognition, exit.
// The recognition node is also the ROI source, the pre-warm target and where the route's text is injected.
constexpr const char* kCollectEntryNode = "AutoCollectClickStart";
constexpr const char* kCollectRecognitionNode = "AutoCollectClick";
constexpr const char* kCollectExitNode = "AutoCollectClickEnd";
constexpr const char* kInteractEntryNode = "MapNavigatorInteractStart";
constexpr const char* kInteractRecognitionNode = "MapNavigatorInteract";
constexpr const char* kInteractExitNode = "MapNavigatorInteractEnd";
// 上索走的也是这套三节点交互, 只是提示文字由节点自己带, 不由路线注入。
// 确认上索另配一对: 只认图标, 不必为一次确认付 OCR 的钱。确认要的恰恰是「认不出」,
// 所以照样得走 Start 节点 —— 直接派发识别节点会让认不出的那一趟耗满节点超时。
constexpr const char* kZiplineMountEntryNode = "MapNavigatorZiplineMountStart";
constexpr const char* kZiplineMountRecognitionNode = "MapNavigatorZiplineMount";
constexpr const char* kZiplineMountExitNode = "MapNavigatorZiplineMountEnd";
constexpr const char* kZiplineMountScanEntryNode = "MapNavigatorZiplineMountScanStart";
constexpr const char* kZiplineMountScanNode = "MapNavigatorZiplineMountScan";
constexpr int32_t kPromptPostSleepMs = 80;

// Resolution every pipeline ROI is authored against; the scanner rescales it to whatever the frame really is.
constexpr int32_t kPipelineRoiBaseWidth = 1280;
constexpr int32_t kPipelineRoiBaseHeight = 720;

// Every interactable raises the same prompt icon, so both kinds share this pre-filter. The threshold is loose on
// purpose: it only decides whether the subtask is worth running, and the subtask recognizes again before acting.
// These are the last resort: the shipped scan node below carries the same values, and a route may name its own.
constexpr const char* kPromptIconRelativePath = "resource/image/RealTimeTask/AutoPick.png";
constexpr double kPromptIconMatchThreshold = 0.75;
// TemplateMatch node holding the interact pre-filter's roi/template/threshold, so a business whose prompt looks
// different or sits elsewhere retargets it in JSON. Missing (old resources, new agent) -> the constants above.
constexpr const char* kInteractScanNode = "MapNavigatorInteractScan";
// The bands and pacing below are shared by both kinds: both need the prompt to stay on screen long enough to act on.
// Sole pacing gate for detection-triggered attempts; the clock only advances on an actual attempt.
constexpr int32_t kPromptScanIntervalMs = 1200;
// Collect points sit 2.1-3.7 apart, closer than the normal 3.25 band, which swallows a whole run of them.
constexpr double kCollectArrivalBandWu = 1.5;
// Tightening must not add a way to get stuck: this long without progress falls back to the normal band.
constexpr int32_t kCollectArrivalRelaxMs = 6000;
// The route ends the tick the last such point is consumed, so the scanner never gets a second chance.
constexpr int32_t kCollectTailGraceMs = 1500;
constexpr double kCollectSprintSuppressBandWu = 8.0;
constexpr int32_t kSprintCancelReleaseMs = 60;

// Walk near these points so the interact prompt stays up long enough to act on. Enter/exit differ for
// hysteresis (each press flips game state); the enter band stays inside the sprint-suppress band.
constexpr double kCollectWalkEnterBandWu = 3.0;
constexpr double kCollectWalkExitBandWu = 4.5;
static_assert(kCollectWalkEnterBandWu < kCollectSprintSuppressBandWu, "walk band must sit inside the sprint-suppress band");

// Acting on a prompt is gated on the same band that engages walking: the prompt icon fires for every interactable
// on screen, so without this a route carrying one such point would stop at strangers all the way along.
constexpr double kPromptTriggerBandWu = kCollectWalkEnterBandWu;

// Walking halves both speed and turn rate, so jogging-sized windows are doubled while engaged.
constexpr int32_t kWalkModeSlowFactor = 2;
constexpr int32_t kActionWalkTogglePressMs = 30;

// Blocking-device removal: a device parked in the way is carried off instead of jumped over. The probe reads
// the same ROI the pipeline's interact-button check uses, so the JSON stays the single source of truth. Its
// threshold sits below the pipeline default (0.7) on purpose: the probe only decides whether the subtask is
// worth running, and the subtask re-checks authoritatively before touching anything.
constexpr const char* kObstacleDeviceEntry = "MapNavigatorObstacleDevice";
constexpr const char* kObstacleDeviceProbeNode = "__MapNavigatorObstacleDevice_InteractPre";
constexpr const char* kObstacleDeviceTemplateRelativePath = "resource/image/MapNavigator/ObstacleDevice/InteractButton.png";
constexpr double kObstacleDeviceMatchThreshold = 0.65;
// One attempt per anchor: the subtask's own timeouts can spend ~15s of the kDynamicRecoveryTotalTimeoutMs
// budget, and whatever is left has to still cover jump -> detour -> unstick.
constexpr int32_t kRecoveryDeviceAttempts = 1;

constexpr const char* kDefaultDigEntry = "AutoCollectDigStart";
constexpr const char* kDigPipelineOverride = R"({"AutoCollectDigEnd":{"next":[]}})";
constexpr int32_t kDigPostSleepMs = 80;

} // namespace mapnavigator
