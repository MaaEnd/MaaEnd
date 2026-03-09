#pragma once

#include <cstdint>

namespace mapnavigator
{

constexpr int32_t kKeyW = 'W';
constexpr int32_t kKeyA = 'A';
constexpr int32_t kKeyS = 'S';
constexpr int32_t kKeyD = 'D';
constexpr int32_t kKeyF = 'F';
constexpr int32_t kKeySpace = 0x20; // VK_SPACE

constexpr int32_t kWorkWidth = 1280;
constexpr int32_t kWorkHeight = 720;
constexpr int32_t kWorkCx = kWorkWidth / 2;
constexpr int32_t kWorkCy = kWorkHeight / 2;
constexpr int32_t kHoverTouchContactId = 0;
constexpr int32_t kPrimaryTouchContactId = 1;
constexpr int32_t kDefaultTouchPressure = 0;

// --- ActionWrapper Constants ---
constexpr double kDefaultPixelsPerDegree = 5690.0 / 360.0;
constexpr int32_t kActionSprintPressMs = 30;
constexpr int32_t kActionJumpHoldMs = 50;
constexpr int32_t kActionJumpSettleMs = 500;
constexpr int32_t kActionInteractAttempts = 5;
constexpr int32_t kActionInteractHoldMs = 100;
constexpr int32_t kAutoSprintCooldownMs = 1500;
constexpr int32_t kWalkResetReleaseMs = 120;
constexpr int32_t kWalkResetSettleMs = 100;
constexpr double kSamePointActionChainDistance = 0.2;

// --- NaviController Constants ---
constexpr int32_t kLocatorWaitMaxRetries = 100;
constexpr int32_t kLocatorWaitIntervalMs = 100;
constexpr int32_t kWaitAfterFirstTurnMs = 300;
constexpr double kLookaheadRadius = 2.5;
constexpr double kStrictArrivalLookaheadRadius = 0.5;
constexpr double kStrictArrivalWalkResetDistance = 3.0;
constexpr double kMicroThreshold = 3.0;
constexpr double kRunSpeedMps = 5.5;
constexpr int32_t kLocatorRetryIntervalMs = 20;
constexpr double kMaxErrorYawWithoutStop = 90.0;
constexpr int32_t kStopWaitMs = 150;
constexpr int32_t kStableFramesThreshold = 15;
constexpr int32_t kMaxLatencyForCorrectionMs = 80;
constexpr double kMaxYawDeviationForCorrection = 5.0;
constexpr int32_t kTargetTickMs = 33;
constexpr int32_t kMinSleepMs = 5;
constexpr int32_t kExactTargetLocatorRetryIntervalMs = 50;
constexpr double kExactTargetDistanceThreshold = 1.0;
constexpr int32_t kExactTargetTimeoutMs = 15000;
constexpr double kExactTargetRotationDeviationThreshold = 5.0;
constexpr int32_t kExactTargetRotationWaitMs = 100;
constexpr int32_t kExactTargetMoveWaitMs = 60;
constexpr int32_t kExactTargetStopWaitMs = 50;
constexpr int32_t kZoneConfirmRetryIntervalMs = 120;
constexpr int32_t kZoneConfirmTimeoutMs = 12000;
constexpr int32_t kZoneConfirmStableFrames = 2;
constexpr int32_t kRelocationRetryIntervalMs = 120;
constexpr int32_t kRelocationWaitTimeoutMs = 15000;
constexpr int32_t kRelocationStableFixes = 2;
constexpr double kRelocationResumeMinDistance = 3.0;
constexpr int32_t kRespawnRecoveryPauseMs = 2000;
constexpr double kRespawnTeleportDistance = 8.0;
constexpr double kRespawnDistanceIncreaseThreshold = 5.0;
constexpr int32_t kZoneBlindRecoveryStartMs = 700;
constexpr int32_t kZoneBlindRecoveryIntervalMs = 900;
constexpr int32_t kZoneBlindStrafePulseMs = 220;
constexpr int32_t kTurnLearningMinSampleUnits = 30;
constexpr double kTurnLearningMinObservedDegrees = 3.0;
constexpr double kTurnLearningMaxObservedDegrees = 180.0;
constexpr double kTurnLearningMinCommandDegrees = 5.0;
constexpr double kTurnLearningMaxCommandDegrees = 170.0;
constexpr double kTurnBootstrapMinCommandDegrees = 2.5;
constexpr double kTurnBootstrapTriggerMinDegrees = 4.0;
constexpr double kTurnBootstrapResidualRatio = 0.20;
constexpr double kTurnBootstrapResidualDegrees = 8.0;
constexpr double kTurnProbeTriggerMinDegrees = 8.0;
constexpr double kTurnProbeMinObservedDegrees = 1.5;
constexpr double kTurnProbeMaxDegreesPerCycle = 45.0;
constexpr double kTurnProbeResidualRatio = 0.35;
constexpr double kTurnProbeResidualDegrees = 12.0;
constexpr double kTurnProbeSuccessDegrees = 6.0;
constexpr double kTurnProbeOvershootResidualDegrees = 20.0;
constexpr double kTurnProbeOvershootResidualRatio = 0.75;
constexpr int32_t kTurnProbeMoveMs = 120;
constexpr int32_t kTurnProbePauseMs = 100;
constexpr int32_t kTurnProbeMaxCycles = 3;
constexpr int32_t kTurnFeedbackMinHoldMs = 220;
constexpr int32_t kTurnFeedbackPollIntervalMs = 50;
constexpr int32_t kTurnFeedbackTimeoutMs = 500;
constexpr int32_t kTurnFeedbackStableHits = 2;
constexpr double kTurnFeedbackStableAngleDegrees = 1.5;
constexpr double kTurnContinuousLearningMinDegrees = 10.0;
constexpr int32_t kTurnBootstrapTargetSamples = 3;
constexpr double kTurnScaleSmoothingAlpha = 0.15;
constexpr double kTurnScaleMinUnitsPerDegree = 4.0;
constexpr double kTurnScaleMaxUnitsPerDegree = 40.0;
constexpr double kAdaptiveActivationYawMismatchDegrees = 18.0;
constexpr double kAdaptiveActivationSevereYawMismatchDegrees = 55.0;
constexpr int32_t kAdaptiveActivationStallMs = 2200;
constexpr double kAdaptiveActivationMinDistance = 6.0;
constexpr double kAdaptiveActivationDistanceSlack = 0.75;
constexpr double kNoProgressDistanceEpsilon = 0.5;
constexpr double kNoProgressMinDistance = 3.0;
constexpr int32_t kArrivalTimeoutMinRecoveryAttempts = 4;
constexpr double kWaypointPassThroughCorridor = 3.0;
constexpr double kZoneTransitionIsolationDistance = 5.0;
constexpr double kPortalCommitDistance = 4.0;
constexpr double kRejoinReverseRejectDegrees = 150.0;
constexpr double kRejoinCloseApproachDistance = 4.0;
constexpr double kRejoinHeadingPenaltyWeight = 0.03;
constexpr double kRejoinBacktrackPenaltyPerNode = 1.25;
constexpr double kRejoinForwardBonusPerNode = 0.15;
constexpr int32_t kRejoinForwardBonusMaxNodes = 6;
constexpr double kRejoinSegmentPreferenceBonus = 0.75;
constexpr double kRejoinSegmentFrontThreshold = 0.35;
constexpr double kRejoinSegmentMiddleThreshold = 0.60;
constexpr double kRejoinSegmentContinueBiasDistance = 0.5;
constexpr int32_t kLocalDriverBlockDetectMs = 350;
constexpr double kLocalDriverProgressDistanceDelta = 0.35;
constexpr double kLocalDriverProgressMoveDelta = 0.30;
constexpr double kLocalDriverJumpPreferredYawDegrees = 8.0;
constexpr double kZoneTransitionJumpPreferredYawDegrees = 20.0;
constexpr double kLocalDriverSideAvoidYawDegrees = 10.0;
constexpr double kLocalDriverTurnInPlaceYawDegrees = 55.0;
constexpr int32_t kTurnInPlaceStallMs = 600;
constexpr int32_t kPostTurnForwardCommitMs = 500;
constexpr double kPostTurnForwardCommitMinDegrees = 15.0;
constexpr int32_t kLocalDriverMicroCommitMs = 450;
constexpr int32_t kLocalDriverJumpCommitMs = 500;
constexpr int32_t kLocalDriverRecoverCommitMs = 650;
constexpr int32_t kLocalDriverJumpCooldownMs = 900;
constexpr int32_t kLocalDriverMicroFailuresBeforeRecover = 2;
constexpr int32_t kLocalDriverRecoverFailuresBeforeRejoin = 2;

} // namespace mapnavigator
