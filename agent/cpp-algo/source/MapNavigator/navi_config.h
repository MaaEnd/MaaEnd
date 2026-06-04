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
constexpr int32_t kPostHeadingForwardPulseMs = 270;
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
constexpr int32_t kDynamicRecoveryMaxAttemptsPerAnchor = 3;
constexpr double kDynamicRecoveryResetDistance = 2.0;
constexpr double kCloseGoalDetourSuppressSlack = 6.0;

// --- NavRunController (RUN corridor follower) ---
constexpr double kNavRunLookaheadLowSpeedM = 2.5;
constexpr double kNavRunLookaheadWalkM = 4.0;
constexpr double kNavRunLookaheadSprintM = 5.5;
constexpr double kNavRunLookaheadSharpTurnM = 2.0;
constexpr double kNavRunSharpTurnDeg = 55.0;
constexpr double kNavRunCrossTrackWarnM = 2.2;
constexpr double kNavRunCrossTrackFailM = 4.0;
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

constexpr const char* kDefaultDigEntry = "AutoCollectDigStart";
constexpr const char* kDigPipelineOverride = R"({"AutoCollectDigEnd":{"next":[]}})";
constexpr int32_t kDigPostSleepMs = 80;

} // namespace mapnavigator
