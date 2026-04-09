#include <algorithm>
#include <thread>

#include <MaaUtils/Logger.h>

#include "../action_wrapper.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navigation_session.h"
#include "../position_provider.h"
#include "backend_turn_actuator.h"
#include "turn_execution_engine.h"

namespace mapnavigator
{

TurnExecutionEngine::TurnExecutionEngine(
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    NaviPosition* position,
    std::function<bool()> should_stop)
    : action_wrapper_(action_wrapper)
    , position_provider_(position_provider)
    , session_(session)
    , position_(position)
    , should_stop_(std::move(should_stop))
{
    ResetTurnScaleProfile();
    adaptive_mode_enabled_ = action_wrapper_ != nullptr && action_wrapper_->uses_touch_backend();
}

bool TurnExecutionEngine::adaptive_mode_enabled() const
{
    return adaptive_mode_enabled_;
}

void TurnExecutionEngine::EnableAdaptiveMode(const char* reason, double metric)
{
    if (adaptive_mode_enabled_) {
        return;
    }

    adaptive_mode_enabled_ = true;
    ResetTurnScaleProfile();
    turn_scale_.PrimeAcceptedSamples(kTurnLearningConfig.bootstrap_target_samples);
    const double units_per_degree = turn_scale_.UnitsPerDegreeForDegrees(metric);

    LogWarn << "Adaptive turn calibration enabled." << VAR(reason) << VAR(metric) << VAR(units_per_degree);
}

TurnCommandResult TurnExecutionEngine::InjectMouseAndTrack(
    double delta_degrees,
    bool allow_learning,
    const std::string& expected_zone_id,
    int settle_wait_ms,
    TurnActionKind action_kind,
    uint64_t frame_seq,
    bool block_forward_until_confirmed,
    double confirm_heading_tolerance_degrees)
{
    TurnCommandResult result;
    const auto now = std::chrono::steady_clock::now();

    double requested_delta_degrees = delta_degrees;
    bool requested_allow_learning = allow_learning;
    std::string requested_expected_zone_id = expected_zone_id;
    int requested_settle_wait_ms = settle_wait_ms;
    int retry_budget = action_kind == TurnActionKind::SteeringTrim ? kTurnLifecycleConfig.trim_retry_max_count : 0;
    int retries_used = 0;
    const char* predict_reason = "turn_predict";
    bool consuming_pending_retry = false;

    if (action_kind == TurnActionKind::SteeringTrim && pending_trim_retry_.has_value()) {
        const PendingTrimRetryRequest retry_request = *pending_trim_retry_;
        const double effective_confirm_tolerance =
            confirm_heading_tolerance_degrees > 0.0 ? confirm_heading_tolerance_degrees : retry_request.confirm_heading_tolerance_degrees;
        const bool retry_still_needed = retry_request.block_forward_until_confirmed
                                            ? std::abs(delta_degrees) > effective_confirm_tolerance
                                            : std::abs(delta_degrees) >= kTurnLifecycleConfig.trim_retry_min_residual_degrees;
        if (!retry_still_needed) {
            LogInfo << "Dropping queued trim retry intent because the latest steering frame no longer needs a retry." << VAR(delta_degrees)
                    << VAR(frame_seq);
            ClearPendingTrimRetry("retry_no_longer_needed");
            return result;
        }
        else if (!CanConsumePendingTrimRetry(retry_request, now, frame_seq)) {
            LogInfo << "Deferring queued trim retry until a later frame." << VAR(delta_degrees) << VAR(frame_seq)
                    << VAR(retry_request.retry_not_before_frame_seq);
            return result;
        }
        else {
            requested_allow_learning = false;
            requested_settle_wait_ms = 0;
            retry_budget = retry_request.retry_budget;
            retries_used = retry_request.next_retry_count;
            predict_reason = "turn_retry_recompute_predict";
            requested_expected_zone_id =
                retry_request.expected_zone_id.empty() ? requested_expected_zone_id : retry_request.expected_zone_id;
            block_forward_until_confirmed = block_forward_until_confirmed || retry_request.block_forward_until_confirmed;
            if (confirm_heading_tolerance_degrees <= 0.0) {
                confirm_heading_tolerance_degrees = retry_request.confirm_heading_tolerance_degrees;
            }
            result.consumed_pending_retry = true;
            consuming_pending_retry = true;
        }
    }
    else if (action_kind != TurnActionKind::SteeringTrim && pending_trim_retry_.has_value()) {
        ClearPendingTrimRetry("superseded_by_non_trim_turn_request");
    }

    if (pending_rotation_.has_value()) {
        const char* pending_action_kind = TurnActionKindName(pending_rotation_->action_kind);
        const char* pending_resolution = RotationLifecycleResolutionName(pending_rotation_->resolution);
        LogInfo << "Suppressing turn request while prior rotation remains unconfirmed." << VAR(delta_degrees) << VAR(frame_seq)
                << VAR(pending_action_kind) << VAR(pending_resolution);
        return result;
    }

    if (!StartTurnAction(
            requested_delta_degrees,
            requested_allow_learning,
            requested_expected_zone_id,
            requested_settle_wait_ms,
            action_kind,
            retry_budget,
            retries_used,
            predict_reason,
            frame_seq,
            block_forward_until_confirmed,
            confirm_heading_tolerance_degrees)) {
        return result;
    }

    result.issued = true;
    result.issued_delta_degrees = requested_delta_degrees;

    if (consuming_pending_retry) {
        ClearPendingTrimRetry("retry_consumed");
        LogInfo << "Consumed queued trim retry on a later steering frame." << VAR(frame_seq) << VAR(requested_delta_degrees)
                << VAR(retries_used);
    }

    if (ShouldBlockUntilCompletion(action_kind) && pending_rotation_.has_value()) {
        WaitForPendingRotation("blocking_turn_request");
    }
    return result;
}

void TurnExecutionEngine::UpdatePendingTurnLifecycle(uint64_t frame_seq)
{
    const auto now = std::chrono::steady_clock::now();
    if (pending_trim_retry_.has_value() && now >= pending_trim_retry_->expires_after) {
        LogWarn << "Pending trim retry intent expired before a new steering frame consumed it." << VAR(frame_seq)
                << VAR(pending_trim_retry_->retry_not_before_frame_seq);
        pending_trim_retry_.reset();
    }
    UpdatePendingRotation(now, false, frame_seq);
}

void TurnExecutionEngine::CancelPendingRotation(const char* reason)
{
    if (pending_rotation_.has_value()) {
        pending_rotation_->resolution = RotationLifecycleResolution::Cancelled;
        const char* action_kind = TurnActionKindName(pending_rotation_->action_kind);
        const char* resolution = RotationLifecycleResolutionName(pending_rotation_->resolution);
        LogInfo << "Cancelling pending rotation action." << VAR(reason) << VAR(action_kind) << VAR(resolution);
        pending_rotation_.reset();
    }
    ClearPendingTrimRetry(reason);
}

bool TurnExecutionEngine::HasActiveTurnLifecycle() const
{
    return pending_rotation_.has_value();
}

bool TurnExecutionEngine::HasActiveSteeringTrimLifecycle() const
{
    return (pending_rotation_.has_value() && pending_rotation_->action_kind == TurnActionKind::SteeringTrim)
           || pending_trim_retry_.has_value();
}

bool TurnExecutionEngine::HasPendingTrimRetryRequest() const
{
    return pending_trim_retry_.has_value();
}

bool TurnExecutionEngine::ShouldBlockForwardMotion() const
{
    return (pending_rotation_.has_value() && pending_rotation_->block_forward_until_confirmed)
           || (pending_trim_retry_.has_value() && pending_trim_retry_->block_forward_until_confirmed);
}

bool TurnExecutionEngine::StartTurnAction(
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
    double confirm_heading_tolerance_degrees)
{
    const bool effective_allow_learning = ResolveLearningPermission(action_kind, allow_learning, block_forward_until_confirmed);
    const TurnScaleBucketId requested_bucket = turn_scale_.ResolveBucketForDegrees(delta_degrees);
    BackendTurnActuator turn_actuator(*action_wrapper_, should_stop_);
    TurnActuationResult actuation { turn_scale_.DegreesToUnits(delta_degrees) };
    if (actuation.units_sent == 0) {
        return false;
    }

    const auto start_time = std::chrono::steady_clock::now();
    const double yaw_before = position_->angle;
    const double target_yaw = NormalizeHeading(yaw_before + delta_degrees);

    actuation = turn_actuator.TurnByUnits(actuation.units_sent, settle_wait_ms);
    if (actuation.units_sent == 0) {
        return false;
    }

    const double predicted_turned_degrees = turn_scale_.PredictDegreesFromUnits(actuation.units_sent);
    const TurnScaleBucketId applied_bucket = turn_scale_.ResolveBucketForUnits(actuation.units_sent);
    (void)predict_reason;
    session_->SyncVirtualYaw(NaviMath::NormalizeAngle(session_->virtual_yaw() + predicted_turned_degrees));
    const int units_sent = actuation.units_sent;
    const double predicted_units_per_degree = turn_scale_.UnitsPerDegreeForUnits(actuation.units_sent);
    const double virtual_yaw = session_->virtual_yaw();
    const char* action_name = TurnActionKindName(action_kind);
    const char* requested_bucket_name = TurnScaleEstimator::BucketName(requested_bucket);
    const char* applied_bucket_name = TurnScaleEstimator::BucketName(applied_bucket);

    LogInfo << "Injected turn." << VAR(action_name) << VAR(delta_degrees) << VAR(units_sent) << VAR(predicted_turned_degrees)
            << VAR(predicted_units_per_degree) << VAR(requested_bucket_name) << VAR(applied_bucket_name) << VAR(virtual_yaw);

    if (!actuation.requires_completion_tracking) {
        HandleImmediateTurnLearning(
            delta_degrees,
            effective_allow_learning,
            expected_zone_id,
            settle_wait_ms,
            yaw_before,
            target_yaw,
            actuation);
        return true;
    }

    PendingRotationAction pending_action;
    pending_action.action_kind = action_kind;
    pending_action.resolution = RotationLifecycleResolution::Pending;
    pending_action.requested_delta_degrees = delta_degrees;
    pending_action.predicted_delta_degrees = predicted_turned_degrees;
    pending_action.turn_bucket = applied_bucket;
    pending_action.heading_error_before = delta_degrees;
    pending_action.yaw_before = yaw_before;
    pending_action.target_yaw = target_yaw;
    pending_action.has_issue_translation_reference = position_ != nullptr && position_->valid;
    pending_action.issue_x = pending_action.has_issue_translation_reference ? position_->x : 0.0;
    pending_action.issue_y = pending_action.has_issue_translation_reference ? position_->y : 0.0;
    pending_action.units_sent = units_sent;
    pending_action.allow_learning = effective_allow_learning;
    pending_action.block_forward_until_confirmed = block_forward_until_confirmed;
    pending_action.retry_budget = retry_budget;
    pending_action.retries_used = retries_used;
    pending_action.issued_frame_seq = frame_seq;
    pending_action.review_not_before_frame_seq = frame_seq == 0 ? 0 : frame_seq + 1;
    pending_action.started_at = start_time;
    pending_action.issue_observation_at = position_ != nullptr && position_->valid ? position_->timestamp : start_time;
    pending_action.expected_elapsed = actuation.expected_elapsed;
    pending_action.review_after = start_time + actuation.expected_elapsed;
    pending_action.retry_eligible_after =
        pending_action.review_after + std::chrono::milliseconds(kTurnLifecycleConfig.trim_retry_cooldown_ms);
    pending_action.expires_after = pending_action.review_after + std::chrono::milliseconds(kTurnLifecycleConfig.review_grace_ms);
    pending_action.confirm_heading_tolerance_degrees = confirm_heading_tolerance_degrees;
    pending_action.expected_zone_id = expected_zone_id;
    pending_rotation_ = pending_action;

    const auto expected_elapsed_ms = pending_action.expected_elapsed.count();
    LogInfo << "Turn action entered pending lifecycle." << VAR(action_name) << VAR(delta_degrees) << VAR(expected_elapsed_ms)
            << VAR(retry_budget) << VAR(retries_used) << VAR(frame_seq);
    return true;
}

bool TurnExecutionEngine::ShouldBlockUntilCompletion(TurnActionKind action_kind) const
{
    if (action_kind == TurnActionKind::SteeringTrim) {
        return false;
    }
    if (action_wrapper_ == nullptr || !action_wrapper_->uses_touch_backend()) {
        return true;
    }

    // Touch navigation needs the main loop to keep forward input engaged while
    // later frames observe the applied turn, otherwise the joystick keeps
    // getting released and heading never settles into a walking state.
    return action_kind == TurnActionKind::HeadingAlign;
}

void TurnExecutionEngine::WaitForPendingRotation(const char* reason)
{
    while (!should_stop_() && pending_rotation_.has_value()) {
        const auto now = std::chrono::steady_clock::now();
        UpdatePendingRotation(now, true, 0);
        if (!pending_rotation_.has_value()) {
            return;
        }

        auto sleep_duration = std::chrono::milliseconds(kTurnLifecycleConfig.pending_poll_interval_ms);
        if (now < pending_rotation_->review_after) {
            const auto until_review = std::chrono::duration_cast<std::chrono::milliseconds>(pending_rotation_->review_after - now);
            sleep_duration = std::min(sleep_duration, until_review);
        }
        if (sleep_duration.count() > 0) {
            std::this_thread::sleep_for(sleep_duration);
        }
    }

    if (pending_rotation_.has_value()) {
        CancelPendingRotation(reason);
    }
}

void TurnExecutionEngine::UpdatePendingRotation(const std::chrono::steady_clock::time_point& now, bool allow_capture, uint64_t frame_seq)
{
    if (!pending_rotation_.has_value()) {
        return;
    }

    const PendingRotationAction action = *pending_rotation_;
    if (now < action.review_after) {
        return;
    }
    if (action.review_not_before_frame_seq > 0 && frame_seq > 0 && frame_seq < action.review_not_before_frame_seq) {
        return;
    }

    NaviPosition observed_pos;
    if (!TryGetPendingRotationObservation(action, &observed_pos, allow_capture)) {
        if (pending_rotation_->resolution != RotationLifecycleResolution::AwaitingSecondPostActionObservation) {
            pending_rotation_->resolution = RotationLifecycleResolution::AwaitingFreshObservation;
        }
        if (now >= action.expires_after) {
            pending_rotation_->resolution = RotationLifecycleResolution::Expired;
            const char* action_name = TurnActionKindName(action.action_kind);
            LogWarn << "Pending rotation expired before a trusted observation arrived." << VAR(action_name)
                    << VAR(action.requested_delta_degrees) << VAR(frame_seq);
            pending_rotation_.reset();
        }
        return;
    }

    if (action.action_kind == TurnActionKind::SteeringTrim && action.first_review_frame_seq > 0) {
        const bool later_frame_ready = frame_seq == 0 || frame_seq > action.first_review_frame_seq;
        const bool later_observation_ready = observed_pos.timestamp > action.first_review_observation_at;
        if (!later_frame_ready || !later_observation_ready) {
            pending_rotation_->resolution = RotationLifecycleResolution::AwaitingSecondPostActionObservation;
            if (now >= action.expires_after) {
                pending_rotation_->resolution = RotationLifecycleResolution::Expired;
                const char* action_name = TurnActionKindName(action.action_kind);
                LogWarn << "Pending steering trim expired while waiting for a second post-action observation." << VAR(action_name)
                        << VAR(action.requested_delta_degrees) << VAR(frame_seq);
                pending_rotation_.reset();
            }
            return;
        }
    }

    if (action.action_kind == TurnActionKind::SteeringTrim && !action.block_forward_until_confirmed && action_wrapper_ != nullptr
        && action_wrapper_->uses_touch_backend() && action.has_issue_translation_reference) {
        const double translation_since_issue = std::hypot(observed_pos.x - action.issue_x, observed_pos.y - action.issue_y);
        const double min_translation_before_review = kMeasurementDefaultPositionQuantum;
        if (translation_since_issue < min_translation_before_review) {
            pending_rotation_->resolution = RotationLifecycleResolution::AwaitingFreshObservation;
            if (now >= action.expires_after) {
                pending_rotation_->resolution = RotationLifecycleResolution::Expired;
                const char* action_name = TurnActionKindName(action.action_kind);
                LogWarn << "Pending steering trim expired before forward motion propagated the turn observation." << VAR(action_name)
                        << VAR(action.requested_delta_degrees) << VAR(frame_seq) << VAR(translation_since_issue)
                        << VAR(min_translation_before_review);
                pending_rotation_.reset();
            }
            return;
        }
    }

    const double observed_turned_degrees = NaviMath::NormalizeAngle(observed_pos.angle - action.yaw_before);
    const double residual_heading_degrees = NaviMath::NormalizeAngle(action.heading_error_before - observed_turned_degrees);
    const bool same_direction = SameTurnDirection(observed_turned_degrees, action.predicted_delta_degrees);
    const double expected_min_degrees = std::max(
        kTurnLifecycleConfig.min_confirmed_observed_degrees,
        std::abs(action.predicted_delta_degrees) * kTurnLifecycleConfig.min_expected_completion_ratio);
    const double confirmed_residual_limit = std::max(
        kTurnLifecycleConfig.confirmed_residual_degrees,
        std::abs(action.heading_error_before) * kTurnLifecycleConfig.confirmed_residual_ratio);
    const double forward_release_heading_limit =
        action.confirm_heading_tolerance_degrees > 0.0 ? action.confirm_heading_tolerance_degrees : confirmed_residual_limit;
    const bool heading_entered_allowed_band = std::abs(residual_heading_degrees) <= forward_release_heading_limit;
    const bool confirmed = action.block_forward_until_confirmed ? (same_direction && heading_entered_allowed_band)
                                                                : (same_direction
                                                                   && (std::abs(observed_turned_degrees) >= expected_min_degrees
                                                                       || std::abs(residual_heading_degrees) <= confirmed_residual_limit));

    pending_rotation_->reviewed_observation_at = observed_pos.timestamp;
    *position_ = observed_pos;
    session_->SyncVirtualYaw(observed_pos.angle);

    if (confirmed) {
        pending_rotation_->resolution = RotationLifecycleResolution::Confirmed;
        pending_rotation_->verdict_frame_seq = frame_seq;
        HandleObservedTurnLearning(action, observed_turned_degrees);
        const char* action_name = TurnActionKindName(action.action_kind);
        LogInfo << "Pending rotation confirmed." << VAR(action_name) << VAR(observed_turned_degrees) << VAR(residual_heading_degrees)
                << VAR(expected_min_degrees) << VAR(forward_release_heading_limit) << VAR(frame_seq);
        pending_rotation_.reset();
        return;
    }

    if (action.action_kind == TurnActionKind::SteeringTrim && action.first_review_frame_seq == 0) {
        pending_rotation_->resolution = RotationLifecycleResolution::AwaitingSecondPostActionObservation;
        pending_rotation_->first_review_frame_seq = frame_seq;
        pending_rotation_->first_review_observation_at = observed_pos.timestamp;
        pending_rotation_->first_review_observed_yaw = observed_pos.angle;
        pending_rotation_->first_review_observed_delta_degrees = observed_turned_degrees;
        pending_rotation_->first_review_residual_heading_degrees = residual_heading_degrees;
        LogInfo << "Pending steering trim needs a second post-action observation before any underperform verdict." << VAR(frame_seq)
                << VAR(observed_turned_degrees) << VAR(residual_heading_degrees) << VAR(expected_min_degrees);
        return;
    }

    if (action.action_kind == TurnActionKind::SteeringTrim
        && ObservationShowsContinuedProgress(action, observed_turned_degrees, residual_heading_degrees)) {
        pending_rotation_->resolution = RotationLifecycleResolution::ClosedWithoutRetry;
        pending_rotation_->verdict_frame_seq = frame_seq;
        LogInfo << "Pending steering trim closed without retry because the second post-action observation still shows progress."
                << VAR(frame_seq) << VAR(observed_turned_degrees) << VAR(residual_heading_degrees)
                << VAR(action.first_review_residual_heading_degrees);
        pending_rotation_.reset();
        return;
    }

    const bool trim_retry_candidate = action.action_kind == TurnActionKind::SteeringTrim
                                      && std::abs(action.requested_delta_degrees) <= kTurnLifecycleConfig.trim_retry_max_request_degrees
                                      && same_direction
                                      && std::abs(residual_heading_degrees) >= kTurnLifecycleConfig.trim_retry_min_residual_degrees
                                      && action.retry_budget > action.retries_used;

    if (trim_retry_candidate && QueueTrimRetryRequest(action, observed_pos.timestamp, frame_seq)) {
        const char* action_name = TurnActionKindName(action.action_kind);
        LogWarn << "Pending steering trim underperformed on a second observation and queued a retry intent for a later frame."
                << VAR(action_name) << VAR(observed_turned_degrees) << VAR(residual_heading_degrees) << VAR(same_direction)
                << VAR(frame_seq) << VAR(pending_trim_retry_->retry_not_before_frame_seq);
        pending_rotation_.reset();
        return;
    }

    pending_rotation_->resolution = RotationLifecycleResolution::UnderperformedFinal;
    pending_rotation_->verdict_frame_seq = frame_seq;
    HandleObservedTurnLearning(action, observed_turned_degrees);
    const char* action_name = TurnActionKindName(action.action_kind);
    LogWarn << "Pending rotation underperformed." << VAR(action_name) << VAR(observed_turned_degrees) << VAR(residual_heading_degrees)
            << VAR(same_direction) << VAR(frame_seq);
    pending_rotation_.reset();
}

bool TurnExecutionEngine::TryGetPendingRotationObservation(const PendingRotationAction& action, NaviPosition* out_pos, bool allow_capture)
{
    if (out_pos == nullptr || position_ == nullptr || position_provider_ == nullptr) {
        return false;
    }

    const auto zone_matches = [&](const NaviPosition& pos) {
        return action.expected_zone_id.empty() || pos.zone_id == action.expected_zone_id;
    };
    const auto observation_is_fresh = [&](const NaviPosition& pos) {
        return pos.timestamp >= action.review_after && pos.timestamp > action.issue_observation_at;
    };

    const bool requires_explicit_touch_confirm =
        allow_capture && action_wrapper_ != nullptr && action_wrapper_->uses_touch_backend()
        && (action.action_kind != TurnActionKind::SteeringTrim || action.block_forward_until_confirmed);
    if (requires_explicit_touch_confirm) {
        NaviPosition captured_pos;
        if (!CaptureSettledTurnFeedback(&captured_pos, action.expected_zone_id, TurnFeedbackMode::PassiveObserve)) {
            return false;
        }
        if (!observation_is_fresh(captured_pos) || !zone_matches(captured_pos)) {
            return false;
        }

        *position_ = captured_pos;
        *out_pos = captured_pos;
        return true;
    }

    if (position_->valid && observation_is_fresh(*position_) && zone_matches(*position_) && !position_provider_->LastCaptureWasHeld()) {
        *out_pos = *position_;
        return true;
    }

    if (!allow_capture) {
        return false;
    }

    NaviPosition captured_pos;
    if (!position_provider_->Capture(&captured_pos, false, action.expected_zone_id)) {
        return false;
    }

    if (position_provider_->LastCaptureWasHeld() || !observation_is_fresh(captured_pos) || !zone_matches(captured_pos)) {
        return false;
    }

    *position_ = captured_pos;
    *out_pos = captured_pos;
    return true;
}

bool TurnExecutionEngine::QueueTrimRetryRequest(
    const PendingRotationAction& action,
    const std::chrono::steady_clock::time_point& observed_at,
    uint64_t frame_seq)
{
    pending_trim_retry_ = PendingTrimRetryRequest {
        .block_forward_until_confirmed = action.block_forward_until_confirmed,
        .retry_budget = action.retry_budget,
        .next_retry_count = action.retries_used + 1,
        .retry_requested_frame_seq = frame_seq,
        .retry_not_before_frame_seq = frame_seq == 0 ? 0 : frame_seq + 1,
        .observed_at = observed_at,
        .eligible_after = action.retry_eligible_after,
        .expires_after = action.retry_eligible_after + std::chrono::milliseconds(kTurnLifecycleConfig.trim_retry_request_grace_ms),
        .confirm_heading_tolerance_degrees = action.confirm_heading_tolerance_degrees,
        .expected_zone_id = action.expected_zone_id,
    };

    LogInfo << "Queued trim retry intent for a later steering frame." << VAR(pending_trim_retry_->next_retry_count) << VAR(frame_seq)
            << VAR(pending_trim_retry_->retry_not_before_frame_seq);
    return true;
}

bool TurnExecutionEngine::CanConsumePendingTrimRetry(
    const PendingTrimRetryRequest& request,
    const std::chrono::steady_clock::time_point& now,
    uint64_t frame_seq) const
{
    if (position_ == nullptr || position_provider_ == nullptr || !position_->valid || position_provider_->LastCaptureWasHeld()) {
        return false;
    }

    return (request.retry_not_before_frame_seq == 0 || frame_seq >= request.retry_not_before_frame_seq) && now >= request.eligible_after
           && now < request.expires_after && position_->timestamp > request.observed_at;
}

void TurnExecutionEngine::ClearPendingTrimRetry(const char* reason)
{
    if (!pending_trim_retry_.has_value()) {
        return;
    }

    LogInfo << "Clearing pending trim retry intent." << VAR(reason) << VAR(pending_trim_retry_->retry_requested_frame_seq)
            << VAR(pending_trim_retry_->next_retry_count);
    pending_trim_retry_.reset();
}

bool TurnExecutionEngine::ObservationShowsContinuedProgress(
    const PendingRotationAction& action,
    double observed_turned_degrees,
    double residual_heading_degrees) const
{
    if (action.first_review_frame_seq == 0 || !SameTurnDirection(observed_turned_degrees, action.predicted_delta_degrees)) {
        return false;
    }

    return std::abs(residual_heading_degrees) < std::abs(action.first_review_residual_heading_degrees)
           || std::abs(observed_turned_degrees) > std::abs(action.first_review_observed_delta_degrees);
}

void TurnExecutionEngine::HandleImmediateTurnLearning(
    double delta_degrees,
    bool allow_learning,
    const std::string& expected_zone_id,
    int settle_wait_ms,
    double yaw_before,
    double target_yaw,
    const TurnActuationResult& actuation)
{
    if (!ShouldLearnTurn(delta_degrees, allow_learning)) {
        return;
    }

    if (settle_wait_ms > 0) {
        std::this_thread::sleep_for(std::chrono::milliseconds(settle_wait_ms));
    }

    NaviPosition after_pos;
    const bool sample_available = CaptureSettledTurnFeedback(&after_pos, expected_zone_id, LearningFeedbackMode());
    double observed_turned_degrees = 0.0;
    bool same_direction = true;
    bool suspicious_sample = false;

    if (sample_available) {
        observed_turned_degrees = NaviMath::NormalizeAngle(after_pos.angle - yaw_before);
        same_direction = SameTurnDirection(observed_turned_degrees, static_cast<double>(actuation.units_sent));
        *position_ = after_pos;
        suspicious_sample = IsTurnSampleSuspicious(delta_degrees, actuation.units_sent, sample_available, observed_turned_degrees);
        if (!suspicious_sample && same_direction) {
            session_->SyncVirtualYaw(after_pos.angle);
        }
    }

    if (suspicious_sample) {
        LogWarn << "Suspicious turn sample, switching to probe mode." << VAR(delta_degrees) << VAR(actuation.units_sent)
                << VAR(sample_available) << VAR(observed_turned_degrees);
        if (turn_scale_.NeedsBootstrapForUnits(actuation.units_sent)) {
            const bool pending_turn_may_not_have_applied =
                !sample_available || std::abs(observed_turned_degrees) < kTurnProbeConfig.min_observed_degrees;
            RunTurnProbe(target_yaw, expected_zone_id, pending_turn_may_not_have_applied ? actuation.units_sent : 0, yaw_before);
        }
        return;
    }

    if (!sample_available) {
        LogWarn << "Turn learning skipped, post-turn locate failed." << VAR(expected_zone_id);
        return;
    }

    if (!same_direction) {
        LogWarn << "Turn learning skipped, observed turn direction mismatched." << VAR(observed_turned_degrees)
                << VAR(actuation.units_sent);
        return;
    }

    const char* bucket_name = turn_scale_.BucketNameForUnits(actuation.units_sent);
    const double previous_units_per_degree = turn_scale_.UnitsPerDegreeForUnits(actuation.units_sent);
    if (turn_scale_.UpdateFromSample(actuation.units_sent, observed_turned_degrees)) {
        const double updated_units_per_degree = turn_scale_.UnitsPerDegreeForUnits(actuation.units_sent);
        const int accepted_samples = turn_scale_.AcceptedSamplesForUnits(actuation.units_sent);
        LogInfo << "Turn scale bucket updated." << VAR(bucket_name) << VAR(previous_units_per_degree) << VAR(updated_units_per_degree)
                << VAR(observed_turned_degrees) << VAR(actuation.units_sent) << VAR(accepted_samples);
    }
}

void TurnExecutionEngine::HandleObservedTurnLearning(const PendingRotationAction& action, double observed_turned_degrees)
{
    if (!ShouldLearnTurn(action.requested_delta_degrees, action.allow_learning)) {
        return;
    }

    const bool suspicious_sample = IsTurnSampleSuspicious(action.requested_delta_degrees, action.units_sent, true, observed_turned_degrees);
    if (suspicious_sample) {
        LogWarn << "Suspicious pending turn sample, switching to probe mode." << VAR(observed_turned_degrees) << VAR(action.units_sent);
        if (turn_scale_.NeedsBootstrapForUnits(action.units_sent)) {
            const bool pending_turn_may_not_have_applied = std::abs(observed_turned_degrees) < kTurnProbeConfig.min_observed_degrees;
            RunTurnProbe(
                action.target_yaw,
                action.expected_zone_id,
                pending_turn_may_not_have_applied ? action.units_sent : 0,
                action.yaw_before);
        }
        return;
    }

    if (!SameTurnDirection(observed_turned_degrees, static_cast<double>(action.units_sent))) {
        LogWarn << "Turn learning skipped, observed turn direction mismatched." << VAR(observed_turned_degrees) << VAR(action.units_sent);
        return;
    }

    const char* bucket_name = TurnScaleEstimator::BucketName(action.turn_bucket);
    const double previous_units_per_degree = turn_scale_.UnitsPerDegreeForUnits(action.units_sent);
    if (turn_scale_.UpdateFromSample(action.units_sent, observed_turned_degrees)) {
        const double updated_units_per_degree = turn_scale_.UnitsPerDegreeForUnits(action.units_sent);
        const int accepted_samples = turn_scale_.AcceptedSamplesForUnits(action.units_sent);
        LogInfo << "Turn scale bucket updated from pending observation." << VAR(bucket_name) << VAR(previous_units_per_degree)
                << VAR(updated_units_per_degree) << VAR(observed_turned_degrees) << VAR(action.units_sent) << VAR(accepted_samples);
    }
}

bool TurnExecutionEngine::ResolveLearningPermission(TurnActionKind action_kind, bool allow_learning, bool block_forward_until_confirmed)
    const
{
    if (allow_learning || action_wrapper_ == nullptr || !action_wrapper_->uses_touch_backend()) {
        return allow_learning;
    }

    if (block_forward_until_confirmed) {
        return true;
    }

    switch (action_kind) {
    case TurnActionKind::SteeringTrim:
    case TurnActionKind::TurnInPlace:
    case TurnActionKind::HeadingAlign:
    case TurnActionKind::RouteResume:
        return true;
    case TurnActionKind::ExactTarget:
        return false;
    }
    return false;
}

void TurnExecutionEngine::ResetTurnScaleProfile()
{
    if (action_wrapper_ != nullptr && action_wrapper_->uses_touch_backend()) {
        turn_scale_.ConfigureGlobal(
            action_wrapper_->DefaultTurnUnitsPerDegree(),
            kAdbTurnScaleMinUnitsPerDegree,
            kAdbTurnScaleMaxUnitsPerDegree,
            kTurnLearningConfig.min_observed_degrees);
        return;
    }

    turn_scale_.ConfigureGlobal(
        action_wrapper_ != nullptr ? action_wrapper_->DefaultTurnUnitsPerDegree() : ComputeDefaultUnitsPerDegree(),
        kWin32TurnScaleMinUnitsPerDegree,
        kWin32TurnScaleMaxUnitsPerDegree,
        kTurnLearningConfig.min_observed_degrees);
}

bool TurnExecutionEngine::ShouldLearnTurn(double delta_degrees, bool allow_learning) const
{
    const double abs_delta_degrees = std::abs(delta_degrees);
    if (!adaptive_mode_enabled_) {
        return false;
    }
    if (!allow_learning || session_->is_waiting_for_zone_switch() || abs_delta_degrees < kTurnLearningConfig.min_command_degrees
        || abs_delta_degrees > kTurnLearningConfig.max_command_degrees) {
        return false;
    }

    if (action_wrapper_ != nullptr && action_wrapper_->uses_touch_backend()) {
        return true;
    }

    return abs_delta_degrees >= kTurnLearningConfig.continuous_learning_min_degrees;
}

TurnExecutionEngine::TurnFeedbackMode TurnExecutionEngine::LearningFeedbackMode() const
{
    if (action_wrapper_ != nullptr && action_wrapper_->uses_touch_backend()) {
        return TurnFeedbackMode::PassiveObserve;
    }
    return TurnFeedbackMode::ForwardPulse;
}

TurnExecutionEngine::TurnFeedbackMode TurnExecutionEngine::ProbeFeedbackMode() const
{
    if (action_wrapper_ != nullptr && action_wrapper_->uses_touch_backend()) {
        return TurnFeedbackMode::PassiveObserve;
    }
    return TurnFeedbackMode::ForwardPulse;
}

bool TurnExecutionEngine::SameTurnDirection(double lhs, double rhs)
{
    if (std::abs(lhs) < 1e-6 || std::abs(rhs) < 1e-6) {
        return true;
    }
    return (lhs > 0.0) == (rhs > 0.0);
}

const char* TurnExecutionEngine::TurnActionKindName(TurnActionKind action_kind)
{
    switch (action_kind) {
    case TurnActionKind::SteeringTrim:
        return "SteeringTrim";
    case TurnActionKind::TurnInPlace:
        return "TurnInPlace";
    case TurnActionKind::HeadingAlign:
        return "HeadingAlign";
    case TurnActionKind::ExactTarget:
        return "ExactTarget";
    case TurnActionKind::RouteResume:
        return "RouteResume";
    }
    return "Unknown";
}

const char* TurnExecutionEngine::RotationLifecycleResolutionName(RotationLifecycleResolution resolution)
{
    switch (resolution) {
    case RotationLifecycleResolution::Pending:
        return "Pending";
    case RotationLifecycleResolution::AwaitingFreshObservation:
        return "AwaitingFreshObservation";
    case RotationLifecycleResolution::AwaitingSecondPostActionObservation:
        return "AwaitingSecondPostActionObservation";
    case RotationLifecycleResolution::Confirmed:
        return "Confirmed";
    case RotationLifecycleResolution::ClosedWithoutRetry:
        return "ClosedWithoutRetry";
    case RotationLifecycleResolution::UnderperformedFinal:
        return "UnderperformedFinal";
    case RotationLifecycleResolution::Cancelled:
        return "Cancelled";
    case RotationLifecycleResolution::Expired:
        return "Expired";
    }
    return "Unknown";
}

void TurnExecutionEngine::ReleaseFeedbackPulse(bool& feedback_key_down)
{
    if (feedback_key_down) {
        action_wrapper_->SetMovementStateSync(false, false, false, false, 0);
        feedback_key_down = false;
        if (kTurnProbeConfig.pause_ms > 0) {
            std::this_thread::sleep_for(std::chrono::milliseconds(kTurnProbeConfig.pause_ms));
        }
    }
}

bool TurnExecutionEngine::CaptureSettledTurnFeedback(
    NaviPosition* out_pos,
    const std::string& expected_zone_id,
    TurnFeedbackMode feedback_mode)
{
    bool feedback_key_down = false;
    const auto wait_start = std::chrono::steady_clock::now();
    if (feedback_mode == TurnFeedbackMode::ForwardPulse) {
        action_wrapper_->SetMovementStateSync(true, false, false, false, 0);
        feedback_key_down = true;
    }

    NaviPosition last_pos;
    bool has_last_pos = false;
    bool has_any_pos = false;
    int stable_hits = 0;

    while (!should_stop_()) {
        NaviPosition candidate_pos;
        if (!position_provider_->Capture(&candidate_pos, false, expected_zone_id)) {
            const auto waited_ms =
                std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - wait_start).count();
            if (waited_ms > kTurnFeedbackConfig.timeout_ms) {
                ReleaseFeedbackPulse(feedback_key_down);
                if (has_any_pos) {
                    LogWarn << "Turn feedback settle timeout, use latest sample." << VAR(expected_zone_id) << VAR(waited_ms);
                    return true;
                }
                return false;
            }
            std::this_thread::sleep_for(std::chrono::milliseconds(kTurnFeedbackConfig.poll_interval_ms));
            continue;
        }

        *out_pos = candidate_pos;
        has_any_pos = true;

        const auto held_ms = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - wait_start).count();
        const bool min_hold_satisfied = held_ms >= kTurnFeedbackConfig.min_hold_ms;

        const bool is_angle_stable =
            has_last_pos && candidate_pos.zone_id == last_pos.zone_id
            && std::abs(NaviMath::NormalizeAngle(candidate_pos.angle - last_pos.angle)) <= kTurnFeedbackConfig.stable_angle_degrees;

        if (min_hold_satisfied && is_angle_stable) {
            stable_hits++;
            if (stable_hits >= kTurnFeedbackConfig.stable_hits) {
                ReleaseFeedbackPulse(feedback_key_down);
                return true;
            }
        }
        else {
            stable_hits = 0;
        }

        last_pos = candidate_pos;
        has_last_pos = true;

        const auto waited_ms = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - wait_start).count();
        if (waited_ms > kTurnFeedbackConfig.timeout_ms) {
            ReleaseFeedbackPulse(feedback_key_down);
            return has_any_pos;
        }

        std::this_thread::sleep_for(std::chrono::milliseconds(kTurnFeedbackConfig.poll_interval_ms));
    }

    ReleaseFeedbackPulse(feedback_key_down);
    return has_any_pos;
}

bool TurnExecutionEngine::IsTurnSampleSuspicious(
    double commanded_delta_degrees,
    int units_sent,
    bool sample_available,
    double observed_delta_degrees) const
{
    if (!sample_available) {
        return true;
    }

    if (std::abs(commanded_delta_degrees) < kTurnProbeConfig.trigger_min_degrees) {
        return false;
    }

    if (std::abs(observed_delta_degrees) < kTurnProbeConfig.min_observed_degrees) {
        return true;
    }

    if ((commanded_delta_degrees > 0.0) != (observed_delta_degrees > 0.0)) {
        return true;
    }

    const double predicted = turn_scale_.PredictDegreesFromUnits(units_sent);
    if (std::abs(predicted) < 1e-6) {
        return true;
    }

    const double residual_degrees = std::abs(predicted - observed_delta_degrees);
    const double residual_ratio = residual_degrees / std::max(std::abs(predicted), 1e-6);
    return residual_degrees > kTurnProbeConfig.residual_degrees && residual_ratio > kTurnProbeConfig.residual_ratio;
}

double TurnExecutionEngine::NormalizeHeading(double angle)
{
    angle = std::fmod(angle, 360.0);
    if (angle < 0.0) {
        angle += 360.0;
    }
    return angle;
}

bool TurnExecutionEngine::RunTurnProbe(double target_yaw, const std::string& expected_zone_id, int pending_units, double pending_yaw_before)
{
    if (position_provider_ == nullptr || action_wrapper_ == nullptr || session_ == nullptr || position_ == nullptr) {
        return false;
    }

    const double pending_predicted_delta = pending_units == 0 ? 0.0 : turn_scale_.PredictDegreesFromUnits(pending_units);
    const double baseline_yaw = pending_units == 0 ? position_->angle : NormalizeHeading(pending_yaw_before + pending_predicted_delta);

    if (pending_units != 0) {
        session_->SyncVirtualYaw(baseline_yaw);
    }

    const double probe_delta = NaviMath::NormalizeAngle(target_yaw - baseline_yaw);
    if (std::abs(probe_delta) < kTurnProbeConfig.trigger_min_degrees) {
        return false;
    }

    const int probe_units = turn_scale_.DegreesToUnits(probe_delta);
    if (probe_units == 0) {
        return false;
    }

    BackendTurnActuator turn_actuator(*action_wrapper_, should_stop_);
    const TurnActuationResult actuation = turn_actuator.TurnByUnits(probe_units, kTurnProbeConfig.move_ms);
    if (actuation.units_sent == 0) {
        return false;
    }
    const double predicted_probe_degrees = turn_scale_.PredictDegreesFromUnits(actuation.units_sent);
    const double predicted_yaw = NormalizeHeading(baseline_yaw + predicted_probe_degrees);
    session_->SyncVirtualYaw(predicted_yaw);

    NaviPosition after_probe;
    if (!CaptureSettledTurnFeedback(&after_probe, expected_zone_id, ProbeFeedbackMode())) {
        return false;
    }

    *position_ = after_probe;
    const double observed_probe_delta = NaviMath::NormalizeAngle(after_probe.angle - baseline_yaw);
    if (std::abs(observed_probe_delta) < kTurnProbeConfig.min_observed_degrees) {
        return false;
    }

    if ((probe_delta > 0.0) != (observed_probe_delta > 0.0)) {
        return false;
    }

    session_->SyncVirtualYaw(after_probe.angle);
    turn_scale_.UpdateFromSample(actuation.units_sent, observed_probe_delta);
    return true;
}

} // namespace mapnavigator
