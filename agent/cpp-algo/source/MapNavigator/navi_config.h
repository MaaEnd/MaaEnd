#pragma once

#include <cmath>
#include <cstdint>

namespace mapnavigator
{

constexpr int32_t kWorkWidth = 1280;

// --- ActionWrapper Constants ---
constexpr double kTurn360UnitsPerWidth = 2.23006;
constexpr double kTurnDegreesPerCircle = 360.0;

struct AdbTouchTurnProfile
{
    double default_units_per_degree = 3.5;
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
constexpr int32_t kSteeringRateMaxGapMs = 400;
constexpr int32_t kSteeringRateReferenceMs = 100;
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
constexpr double kSerialRouteHeadingEpsilon = 2.0;
constexpr double kSerialRouteDeviationThreshold = 1.5;
constexpr double kSerialRouteDeviationFailThreshold = 3.0;
constexpr double kSerialRouteCompensationMinDistance = 1.0;
constexpr double kWaypointArrivalSlack = 0.5;
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
constexpr double kUnstickSampleStepM = 0.5;        // per-ray on/off scan resolution (world units)
constexpr double kUnstickMaxRockCrossingM = 2.0;   // tolerate this much off-mesh (the rock) before solid ground; longer = water => reject bearing
constexpr double kUnstickMeshMarginM = 1.0;        // step this far past the mesh edge so we land ON solid ground
constexpr double kUnstickMinDistanceM = 2.5;       // shortest committed dislodge step
constexpr double kUnstickMaxDistanceM = 6.0;       // ray-scan reach / longest dislodge step
constexpr int32_t kUnstickPulseMs = 270;           // per forward pulse after turning to the escape bearing
constexpr int32_t kUnstickMaxPulses = 8;           // committed-walk cap; displacement exit usually ends it sooner
constexpr double kUnstickResetDistanceM = 2.0;     // relocated this far from the unstick origin => reset bearing rotation
constexpr double kUnstickSuccessFraction = 0.6;    // displacement >= this * planned dist => a real dislodge step
// When the locator yields no usable fix for a sustained period (e.g. the agent was shoved across a
// zone boundary into a sub-zone the active route was not planned in, so every fix fails zone
// validation), stop holding forward into the obstacle, hop periodically to dislodge, and fail-fast
// once the loss outlasts the timeout so the pipeline can retry instead of stalling forever.
constexpr int32_t kLocalizationLossUnstickIntervalMs = kObstacleRecoveryMinTriggerMs;
constexpr int32_t kLocalizationLossTimeoutMs = kDynamicRecoveryTotalTimeoutMs;

// River-fall recovery (see navigator-river-fall-teleport-gap): black-screen loss = fell in water, teleported to
// shore facing it. Turn 180° away then pulse inland until clear; hard clock bounds thin-shore re-fall loops.
constexpr int32_t kRiverFallRecoveryTimeoutMs = kDynamicRecoveryTotalTimeoutMs;     // 30s clean fail-fast
constexpr double kRiverFallRecoveryClearDistance = kDynamicRecoveryResetDistance;   // walked 2m clear of shore
constexpr int32_t kRiverFallRecoveryPulseMs = kPostHeadingForwardPulseMs;           // proven heading-commit pulse

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
constexpr double kNavRunLookaheadWalkM = 4.0;
constexpr double kNavRunLookaheadSprintM = 5.5;
constexpr double kNavRunLookaheadSharpTurnM = 2.5;
constexpr double kNavRunSharpTurnDeg = 55.0;
constexpr double kNavRunUpcomingTurnLookaheadM = 8.0;
constexpr double kNavRunCrossTrackWarnM = 2.2;
constexpr double kNavRunCrossTrackFailM = 4.0;
constexpr double kNavRunProgressReplanMinCrossTrackM = 1.25;
constexpr int32_t kNavRunSoftReplanCooldownMs = 1200;
constexpr int32_t kNavRunSoftReplanMaxPerAnchor = 3;
constexpr int32_t kNavRunProgressRegressionMs = 800;

// --- Zone / Portal / Transfer Constants ---
constexpr int32_t kZoneConfirmRetryIntervalMs = 120;
constexpr int32_t kZoneConfirmTimeoutMs = 12000;
constexpr int32_t kZoneConfirmStableFrames = 2;
constexpr int32_t kRelocationRetryIntervalMs = 120;
constexpr int32_t kRelocationWaitTimeoutMs = 15000;
constexpr int32_t kRelocationStableFixes = 2;
constexpr double kRelocationResumeMinDistance = 3.0;

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

constexpr const char* kDefaultCollectEntry = "AutoCollectClickStart";
constexpr const char* kCollectPipelineOverride = R"({"AutoCollectClickEnd":{"next":[]}})";
constexpr int32_t kCollectPostSleepMs = 80;

constexpr const char* kCollectPrewarmOverride =
    R"({"AutoCollectClick":{"action":{"type":"DoNothing"},"next":[]},"AutoCollectClickEnd":{"next":[]}})";
constexpr const char* kCollectRoiNode = "AutoCollectClick";
constexpr int32_t kCollectRoiBaseWidth = 1280;
constexpr int32_t kCollectRoiBaseHeight = 720;

constexpr const char* kCollectIconRelativePath = "resource/image/RealTimeTask/AutoPick.png";
constexpr double kCollectIconMatchThreshold = 0.75;
constexpr int32_t kCollectLabelBrightThreshold = 210;  // 0-255 luma; near-white glyphs survive, grass (~200) drops
constexpr int32_t kCollectLabelMorphWidth = 8;         // horizontal close width that merges glyphs into a word
constexpr int32_t kCollectLabelMinWidth = 24;          // ~2-char CJK name floor (the 5-char label measured 78px)
constexpr int32_t kCollectLabelMinHeight = 7;          // reject thin specks (label glyph row ~14px)
constexpr int32_t kCollectLabelMaxHeight = 26;         // reject tall non-text structures
constexpr double kCollectLabelMaxFill = 0.80;          // text is sparse (label fill ~0.4-0.66); solid blob = panel/icon
constexpr int32_t kCollectScanIntervalMs = 1500;
constexpr double kCollectRetryMinMoveWu = 2.5;
constexpr double kCollectSprintSuppressBandWu = 8.0;
constexpr int32_t kSprintCancelReleaseMs = 60;

constexpr const char* kDefaultDigEntry = "AutoCollectDigStart";
constexpr const char* kDigPipelineOverride = R"({"AutoCollectDigEnd":{"next":[]}})";
constexpr int32_t kDigPostSleepMs = 80;

} // namespace mapnavigator
