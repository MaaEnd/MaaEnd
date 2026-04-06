#pragma once

#include <chrono>
#include <cstdint>
#include <functional>
#include <optional>
#include <string>

#include "../navi_domain_types.h"
#include "../turn_scale_estimator.h"

namespace mapnavigator
{

class ActionWrapper;
class PositionProvider;
struct NavigationSession;
struct NaviPosition;

class TurnExecutionEngine
{
public:
    TurnExecutionEngine(
        ActionWrapper* action_wrapper,
        PositionProvider* position_provider,
        NavigationSession* session,
        NaviPosition* position,
        std::function<bool()> should_stop);

    bool adaptive_mode_enabled() const;
    void EnableAdaptiveMode(const char* reason, double metric);
    TurnCommandResult InjectMouseAndTrack(
        double delta_degrees,
        bool allow_learning,
        const std::string& expected_zone_id,
        int settle_wait_ms,
        TurnActionKind action_kind,
        uint64_t frame_seq = 0,
        bool block_forward_until_confirmed = false,
        double confirm_heading_tolerance_degrees = 0.0);
    void UpdatePendingTurnLifecycle(uint64_t frame_seq = 0);
    void CancelPendingRotation(const char* reason);
    bool HasActiveTurnLifecycle() const;
    bool HasActiveSteeringTrimLifecycle() const;
    bool HasPendingTrimRetryRequest() const;
    bool ShouldBlockForwardMotion() const;
    void ClearPendingTrimRetry(const char* reason);

private:
    enum class TurnFeedbackMode
    {
        PassiveObserve,
        ForwardPulse,
    };

    enum class RotationLifecycleResolution
    {
        Pending,
        AwaitingFreshObservation,
        AwaitingSecondPostActionObservation,
        Confirmed,
        ClosedWithoutRetry,
        UnderperformedFinal,
        Cancelled,
        Expired,
    };

    struct PendingRotationAction
    {
        TurnActionKind action_kind = TurnActionKind::RouteResume;
        RotationLifecycleResolution resolution = RotationLifecycleResolution::Pending;
        double requested_delta_degrees = 0.0;
        double predicted_delta_degrees = 0.0;
        TurnScaleBucketId turn_bucket = TurnScaleBucketId::Global;
        double heading_error_before = 0.0;
        double yaw_before = 0.0;
        double target_yaw = 0.0;
        bool has_issue_translation_reference = false;
        double issue_x = 0.0;
        double issue_y = 0.0;
        int units_sent = 0;
        bool allow_learning = false;
        bool block_forward_until_confirmed = false;
        int retry_budget = 0;
        int retries_used = 0;
        uint64_t issued_frame_seq = 0;
        uint64_t review_not_before_frame_seq = 0;
        uint64_t first_review_frame_seq = 0;
        uint64_t verdict_frame_seq = 0;
        std::chrono::steady_clock::time_point started_at {};
        std::chrono::steady_clock::time_point issue_observation_at {};
        std::chrono::steady_clock::time_point reviewed_observation_at {};
        std::chrono::steady_clock::time_point first_review_observation_at {};
        std::chrono::steady_clock::time_point review_after {};
        std::chrono::steady_clock::time_point retry_eligible_after {};
        std::chrono::steady_clock::time_point expires_after {};
        std::chrono::milliseconds expected_elapsed {};
        double first_review_observed_yaw = 0.0;
        double first_review_observed_delta_degrees = 0.0;
        double first_review_residual_heading_degrees = 0.0;
        double confirm_heading_tolerance_degrees = 0.0;
        std::string expected_zone_id;
    };

    struct PendingTrimRetryRequest
    {
        bool block_forward_until_confirmed = false;
        int retry_budget = 0;
        int next_retry_count = 0;
        uint64_t retry_requested_frame_seq = 0;
        uint64_t retry_not_before_frame_seq = 0;
        std::chrono::steady_clock::time_point observed_at {};
        std::chrono::steady_clock::time_point eligible_after {};
        std::chrono::steady_clock::time_point expires_after {};
        double confirm_heading_tolerance_degrees = 0.0;
        std::string expected_zone_id;
    };

    bool ShouldLearnTurn(double delta_degrees, bool allow_learning) const;
    bool ResolveLearningPermission(TurnActionKind action_kind, bool allow_learning, bool block_forward_until_confirmed) const;
    void ResetTurnScaleProfile();
    bool StartTurnAction(
        double delta_degrees,
        bool allow_learning,
        const std::string& expected_zone_id,
        int settle_wait_ms,
        TurnActionKind action_kind,
        int retry_budget,
        int retries_used,
        const char* predict_reason,
        uint64_t frame_seq,
        bool block_forward_until_confirmed,
        double confirm_heading_tolerance_degrees);
    bool ShouldBlockUntilCompletion(TurnActionKind action_kind) const;
    void WaitForPendingRotation(const char* reason);
    void UpdatePendingRotation(const std::chrono::steady_clock::time_point& now, bool allow_capture, uint64_t frame_seq);
    bool TryGetPendingRotationObservation(const PendingRotationAction& action, NaviPosition* out_pos, bool allow_capture);
    bool QueueTrimRetryRequest(
        const PendingRotationAction& action,
        const std::chrono::steady_clock::time_point& observed_at,
        uint64_t frame_seq);
    bool CanConsumePendingTrimRetry(
        const PendingTrimRetryRequest& request,
        const std::chrono::steady_clock::time_point& now,
        uint64_t frame_seq) const;
    bool ObservationShowsContinuedProgress(
        const PendingRotationAction& action,
        double observed_turned_degrees,
        double residual_heading_degrees) const;
    void HandleImmediateTurnLearning(
        double delta_degrees,
        bool allow_learning,
        const std::string& expected_zone_id,
        int settle_wait_ms,
        double yaw_before,
        double target_yaw,
        const TurnActuationResult& actuation);
    void HandleObservedTurnLearning(const PendingRotationAction& action, double observed_turned_degrees);
    bool CaptureSettledTurnFeedback(NaviPosition* out_pos, const std::string& expected_zone_id, TurnFeedbackMode feedback_mode);
    bool IsTurnSampleSuspicious(double commanded_delta_degrees, int units_sent, bool sample_available, double observed_delta_degrees) const;
    bool RunTurnProbe(double target_yaw, const std::string& expected_zone_id, int pending_units, double pending_yaw_before);
    TurnFeedbackMode LearningFeedbackMode() const;
    TurnFeedbackMode ProbeFeedbackMode() const;

    static bool SameTurnDirection(double lhs, double rhs);
    static const char* TurnActionKindName(TurnActionKind action_kind);
    static const char* RotationLifecycleResolutionName(RotationLifecycleResolution resolution);
    static double NormalizeHeading(double angle);
    void ReleaseFeedbackPulse(bool& feedback_key_down);

    ActionWrapper* action_wrapper_ = nullptr;
    PositionProvider* position_provider_ = nullptr;
    NavigationSession* session_ = nullptr;
    NaviPosition* position_ = nullptr;
    std::function<bool()> should_stop_;
    bool adaptive_mode_enabled_ = false;
    TurnScaleEstimator turn_scale_ {};
    std::optional<PendingRotationAction> pending_rotation_;
    std::optional<PendingTrimRetryRequest> pending_trim_retry_;
};

} // namespace mapnavigator
