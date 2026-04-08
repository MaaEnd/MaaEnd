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
    double default_units_per_degree = 3.0;
    int32_t swipe_duration_ms = 100;
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
constexpr int32_t kPostHeadingForwardPulseMs = 60;
constexpr int32_t kSerialRouteRetryDelayMs = 180;
constexpr double kBootstrapOwnershipProjectionCorridor = 3.0;
constexpr double kBootstrapOwnershipProjectionFrontThreshold = 0.35;
constexpr double kBootstrapOwnershipProjectionMiddleThreshold = 0.60;
constexpr double kBootstrapOwnershipContinueBiasDistance = 0.5;
constexpr double kBootstrapOwnershipMaxDistance = 18.0;
constexpr double kSerialRouteHeadingEpsilon = 2.0;
constexpr double kSerialRoutePathFollowLookahead = 1.5;
constexpr double kSerialRouteDeviationThreshold = 1.5;
constexpr double kSerialRouteDeviationFailThreshold = 3.0;
constexpr double kSerialRouteCompensationMinDistance = 1.0;

// --- Zone / Portal / Transfer Constants ---
constexpr int32_t kZoneConfirmRetryIntervalMs = 120;
constexpr int32_t kZoneConfirmTimeoutMs = 12000;
constexpr int32_t kZoneConfirmStableFrames = 2;
constexpr int32_t kRelocationRetryIntervalMs = 120;
constexpr int32_t kRelocationWaitTimeoutMs = 15000;
constexpr int32_t kRelocationStableFixes = 2;
constexpr double kRelocationResumeMinDistance = 3.0;
constexpr int32_t kZoneBlindRecoveryStartMs = 700;
constexpr int32_t kZoneBlindRecoveryIntervalMs = 900;
constexpr int32_t kZoneBlindStrafePulseMs = 220;

struct TurnFeedbackConfig
{
    int32_t min_hold_ms = 220;
    int32_t poll_interval_ms = 50;
    int32_t timeout_ms = 500;
    int32_t stable_hits = 2;
    double stable_angle_degrees = 1.5;
};

struct TurnProbeConfig
{
    double trigger_min_degrees = 8.0;
    double min_observed_degrees = 1.5;
    double max_degrees_per_cycle = 45.0;
    double residual_ratio = 0.35;
    double residual_degrees = 12.0;
    double success_degrees = 6.0;
    double overshoot_residual_degrees = 20.0;
    double overshoot_residual_ratio = 0.75;
    int32_t move_ms = 120;
    int32_t pause_ms = 100;
    int32_t max_cycles = 3;
};

struct TurnLearningConfig
{
    double min_sample_units = 30;
    double min_observed_degrees = 3.0;
    double max_observed_degrees = 180.0;
    double min_command_degrees = 5.0;
    double max_command_degrees = 170.0;
    double bootstrap_min_command_degrees = 2.5;
    double bootstrap_trigger_min_degrees = 4.0;
    double bootstrap_residual_ratio = 0.20;
    double bootstrap_residual_degrees = 8.0;
    double continuous_learning_min_degrees = 10.0;
    int32_t bootstrap_target_samples = 3;
    double turn_scale_smoothing_alpha = 0.382;
    double turn_scale_min_units_per_degree = 1.0;
    double turn_scale_max_units_per_degree = 4.0;
};

struct TurnLifecycleConfig
{
    int32_t adb_input_landing_buffer_ms = 120;
    int32_t pending_poll_interval_ms = 40;
    int32_t review_grace_ms = 220;
    int32_t trim_retry_cooldown_ms = 90;
    int32_t trim_retry_request_grace_ms = 220;
    int32_t trim_retry_max_count = 1;
    double min_confirmed_observed_degrees = 1.5;
    double min_expected_completion_ratio = 0.55;
    double confirmed_residual_ratio = 0.35;
    double confirmed_residual_degrees = 2.0;
    double trim_retry_max_request_degrees = 18.0;
    double trim_retry_min_residual_degrees = 3.0;
    double trim_retry_scale = 0.65;
    double trim_retry_min_degrees = 2.0;
    double trim_retry_max_degrees = 8.0;
};

inline constexpr TurnFeedbackConfig kTurnFeedbackConfig {};
inline constexpr TurnProbeConfig kTurnProbeConfig {};
inline constexpr TurnLearningConfig kTurnLearningConfig {};
inline constexpr TurnLifecycleConfig kTurnLifecycleConfig {};

constexpr double kNoProgressDistanceEpsilon = 0.5;
constexpr double kRouteProgressEpsilon = 0.5;
constexpr double kNoProgressMinDistance = 3.0;
constexpr double kMeasurementDefaultPositionQuantum = 0.25;
constexpr double kWaypointPassThroughCorridor = 3.0;
constexpr double kZoneTransitionIsolationDistance = 5.0;
constexpr double kPortalCommitDistance = 4.0;
constexpr double kLocalDriverTurnInPlaceYawDegrees = 55.0;
constexpr int32_t kTurnInPlaceStallMs = 600;
constexpr double kSevereDivergenceYawDegrees = 85.0;
constexpr double kSevereDivergenceDistance = 5.0;
constexpr int32_t kSevereDivergenceStallMs = 800;
constexpr int32_t kPostTurnForwardCommitMs = 500;
constexpr double kPostTurnForwardCommitMinDegrees = 15.0;

} // namespace mapnavigator
