#include <algorithm>
#include <cmath>
#include <thread>

#include <MaaUtils/Logger.h>

#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "navigation_session.h"
#include "position_provider.h"

namespace mapnavigator
{

MotionController::MotionController(
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    NaviPosition* position,
    bool enable_local_driver,
    std::function<bool()> should_stop)
    : action_wrapper_(action_wrapper)
    , position_provider_(position_provider)
    , session_(session)
    , position_(position)
    , should_stop_(std::move(should_stop))
    , enable_local_driver_(enable_local_driver)
{
    turn_scale_.units_per_degree = action_wrapper_->DefaultTurnUnitsPerDegree();
}

void MotionController::Stop()
{
    action_wrapper_->KeyUpSync(kKeyW, 0);
    action_wrapper_->KeyUpSync(kKeyA, 0);
    action_wrapper_->KeyUpSync(kKeyS, 0);
    action_wrapper_->KeyUpSync(kKeyD, 0);
    has_applied_action_ = false;
    is_moving_ = false;
}

void MotionController::SetAction(LocalDriverAction action, bool force)
{
    if (!force && has_applied_action_ && applied_action_ == action) {
        is_moving_ = ActionMovesForward(action);
        return;
    }

    Stop();
    switch (action) {
    case LocalDriverAction::Forward:
        action_wrapper_->KeyDownSync(kKeyW, 0);
        break;
    case LocalDriverAction::ForwardLeft:
        action_wrapper_->KeyDownSync(kKeyW, 0);
        action_wrapper_->KeyDownSync(kKeyA, 0);
        break;
    case LocalDriverAction::ForwardRight:
        action_wrapper_->KeyDownSync(kKeyW, 0);
        action_wrapper_->KeyDownSync(kKeyD, 0);
        break;
    case LocalDriverAction::JumpForward:
        action_wrapper_->KeyDownSync(kKeyW, 0);
        action_wrapper_->ClickKeySync(kKeySpace, kActionJumpHoldMs);
        break;
    case LocalDriverAction::RecoverLeft:
        action_wrapper_->KeyDownSync(kKeyS, 0);
        action_wrapper_->KeyDownSync(kKeyA, 0);
        action_wrapper_->ClickKeySync(kKeySpace, kActionJumpHoldMs);
        break;
    case LocalDriverAction::RecoverRight:
        action_wrapper_->KeyDownSync(kKeyS, 0);
        action_wrapper_->KeyDownSync(kKeyD, 0);
        action_wrapper_->ClickKeySync(kKeySpace, kActionJumpHoldMs);
        break;
    }

    applied_action_ = action;
    has_applied_action_ = true;
    is_moving_ = ActionMovesForward(action);
}

void MotionController::EnsureForwardMotion(bool force)
{
    if (enable_local_driver_) {
        SetAction(LocalDriverAction::Forward, force);
        return;
    }

    if (force || !is_moving_) {
        action_wrapper_->KeyDownSync(kKeyW, 0);
    }
    is_moving_ = true;
}

void MotionController::ResetForwardWalk(int release_millis)
{
    Stop();
    action_wrapper_->ResetForwardWalkSync(release_millis);
    applied_action_ = LocalDriverAction::Forward;
    has_applied_action_ = true;
    is_moving_ = true;
}

bool MotionController::IsMoving() const
{
    return is_moving_;
}

bool MotionController::HasAppliedAction() const
{
    return has_applied_action_;
}

void MotionController::ArmForwardCommit(double delta_degrees, const char* reason)
{
    if (session_->is_waiting_for_zone_switch() || std::abs(delta_degrees) < kPostTurnForwardCommitMinDegrees) {
        return;
    }

    turn_forward_commit_until_ = std::chrono::steady_clock::now() + std::chrono::milliseconds(kPostTurnForwardCommitMs);
    LogInfo << "Post-turn forward commit armed." << VAR(reason) << VAR(delta_degrees);
}

void MotionController::ClearForwardCommit()
{
    turn_forward_commit_until_ = {};
}

bool MotionController::IsForwardCommitActive(const std::chrono::steady_clock::time_point& now) const
{
    return turn_forward_commit_until_.time_since_epoch().count() > 0 && now < turn_forward_commit_until_;
}

bool MotionController::adaptive_mode_enabled() const
{
    return adaptive_mode_enabled_;
}

void MotionController::EnableAdaptiveMode(const char* reason, double metric)
{
    if (adaptive_mode_enabled_) {
        return;
    }

    adaptive_mode_enabled_ = true;
    turn_scale_.units_per_degree = action_wrapper_->DefaultTurnUnitsPerDegree();
    turn_scale_.accepted_samples = std::max(turn_scale_.accepted_samples, kTurnBootstrapTargetSamples);
    const double units_per_degree = turn_scale_.units_per_degree;

    LogWarn << "Adaptive turn calibration enabled." << VAR(reason) << VAR(metric) << VAR(units_per_degree);
}

bool MotionController::InjectMouseAndTrack(
    double delta_degrees,
    bool allow_learning,
    const std::string& expected_zone_id,
    int settle_wait_ms)
{
    NativeMouseTurnActuator turn_actuator(*action_wrapper_);
    TurnActuationResult actuation { turn_scale_.DegreesToUnits(delta_degrees) };

    if (actuation.units_sent == 0) {
        return false;
    }

    const double yaw_before = position_->angle;
    const double target_yaw = NormalizeHeading(yaw_before + delta_degrees);
    actuation = turn_actuator.TurnByUnits(actuation.units_sent, settle_wait_ms);

    const double predicted_turned_degrees = turn_scale_.PredictDegreesFromUnits(actuation.units_sent);
    session_->SyncVirtualYaw(NaviMath::NormalizeAngle(session_->virtual_yaw() + predicted_turned_degrees), "turn_predict");
    const int units_sent = actuation.units_sent;
    const double predicted_units_per_degree = turn_scale_.units_per_degree;
    const double virtual_yaw = session_->virtual_yaw();

    LogInfo << "Injected turn." << VAR(delta_degrees) << VAR(units_sent) << VAR(predicted_turned_degrees) << VAR(predicted_units_per_degree)
            << VAR(virtual_yaw);

    if (!ShouldLearnTurn(delta_degrees, allow_learning)) {
        return true;
    }

    const bool requires_forward_feedback = !IsMoving();
    if (settle_wait_ms > 0 && !requires_forward_feedback) {
        std::this_thread::sleep_for(std::chrono::milliseconds(settle_wait_ms));
    }

    NaviPosition after_pos;
    const bool sample_available = CaptureSettledTurnFeedback(&after_pos, expected_zone_id, requires_forward_feedback);
    double observed_turned_degrees = 0.0;
    bool same_direction = true;
    bool suspicious_sample = false;

    if (sample_available) {
        observed_turned_degrees = NaviMath::NormalizeAngle(after_pos.angle - yaw_before);
        same_direction = observed_turned_degrees == 0.0 || ((observed_turned_degrees > 0.0) == (actuation.units_sent > 0));

        *position_ = after_pos;
        suspicious_sample = IsTurnSampleSuspicious(delta_degrees, actuation.units_sent, sample_available, observed_turned_degrees);
        if (!suspicious_sample && same_direction) {
            session_->SyncVirtualYaw(after_pos.angle, "turn_feedback");
        }
    }

    if (suspicious_sample) {
        LogWarn << "Suspicious turn sample, switching to probe mode." << VAR(delta_degrees) << VAR(units_sent) << VAR(sample_available)
                << VAR(observed_turned_degrees);
        if (turn_scale_.NeedsBootstrap()) {
            const bool pending_turn_may_not_have_applied =
                !sample_available || std::abs(observed_turned_degrees) < kTurnProbeMinObservedDegrees;
            RunTurnProbe(target_yaw, expected_zone_id, pending_turn_may_not_have_applied ? actuation.units_sent : 0, yaw_before);
        }
        return true;
    }

    if (!sample_available) {
        LogWarn << "Turn learning skipped, post-turn locate failed." << VAR(expected_zone_id);
        return true;
    }

    if (!same_direction) {
        LogWarn << "Turn learning skipped, observed turn direction mismatched." << VAR(observed_turned_degrees) << VAR(units_sent);
        return true;
    }

    const double previous_units_per_degree = turn_scale_.units_per_degree;
    if (turn_scale_.UpdateFromSample(actuation.units_sent, observed_turned_degrees)) {
        const double updated_units_per_degree = turn_scale_.units_per_degree;
        const int accepted_samples = turn_scale_.accepted_samples;
        LogInfo << "Turn scale updated." << VAR(previous_units_per_degree) << VAR(updated_units_per_degree) << VAR(observed_turned_degrees)
                << VAR(units_sent) << VAR(accepted_samples);
    }

    return true;
}

bool MotionController::ActionMovesForward(LocalDriverAction action) const
{
    return action == LocalDriverAction::Forward || action == LocalDriverAction::ForwardLeft || action == LocalDriverAction::ForwardRight
           || action == LocalDriverAction::JumpForward;
}

bool MotionController::ShouldLearnTurn(double delta_degrees, bool allow_learning) const
{
    const double abs_delta_degrees = std::abs(delta_degrees);
    if (!adaptive_mode_enabled_) {
        return false;
    }
    return allow_learning && !session_->is_waiting_for_zone_switch() && abs_delta_degrees >= kTurnLearningMinCommandDegrees
           && abs_delta_degrees <= kTurnLearningMaxCommandDegrees && abs_delta_degrees >= kTurnContinuousLearningMinDegrees;
}

void MotionController::ReleaseFeedbackPulse(bool& feedback_key_down)
{
    if (feedback_key_down) {
        action_wrapper_->KeyUpSync(kKeyW, 0);
        feedback_key_down = false;
        if (kTurnProbePauseMs > 0) {
            std::this_thread::sleep_for(std::chrono::milliseconds(kTurnProbePauseMs));
        }
    }
}

bool MotionController::CaptureSettledTurnFeedback(NaviPosition* out_pos, const std::string& expected_zone_id, bool apply_feedback_pulse)
{
    bool feedback_key_down = false;
    const auto wait_start = std::chrono::steady_clock::now();
    if (apply_feedback_pulse) {
        action_wrapper_->KeyDownSync(kKeyW, 0);
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
            if (waited_ms > kTurnFeedbackTimeoutMs) {
                ReleaseFeedbackPulse(feedback_key_down);
                if (has_any_pos) {
                    LogWarn << "Turn feedback settle timeout, use latest sample." << VAR(expected_zone_id) << VAR(waited_ms);
                    return true;
                }
                return false;
            }
            std::this_thread::sleep_for(std::chrono::milliseconds(kTurnFeedbackPollIntervalMs));
            continue;
        }

        *out_pos = candidate_pos;
        has_any_pos = true;

        const auto held_ms = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - wait_start).count();
        const bool min_hold_satisfied = held_ms >= kTurnFeedbackMinHoldMs;

        const bool is_angle_stable =
            has_last_pos && candidate_pos.zone_id == last_pos.zone_id
            && std::abs(NaviMath::NormalizeAngle(candidate_pos.angle - last_pos.angle)) <= kTurnFeedbackStableAngleDegrees;

        if (min_hold_satisfied && is_angle_stable) {
            stable_hits++;
            if (stable_hits >= kTurnFeedbackStableHits) {
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
        if (waited_ms > kTurnFeedbackTimeoutMs) {
            ReleaseFeedbackPulse(feedback_key_down);
            LogWarn << "Turn feedback settle timeout, use latest sample." << VAR(expected_zone_id) << VAR(waited_ms);
            return true;
        }

        std::this_thread::sleep_for(std::chrono::milliseconds(kTurnFeedbackPollIntervalMs));
    }

    ReleaseFeedbackPulse(feedback_key_down);
    return has_any_pos;
}

bool MotionController::IsTurnSampleSuspicious(
    double commanded_delta_degrees,
    int units_sent,
    bool sample_available,
    double observed_delta_degrees) const
{
    const bool bootstrap_learning = turn_scale_.NeedsBootstrap();
    if (!adaptive_mode_enabled_) {
        return false;
    }
    const double trigger_min_degrees = bootstrap_learning ? kTurnBootstrapTriggerMinDegrees : kTurnProbeTriggerMinDegrees;
    if (std::abs(commanded_delta_degrees) < trigger_min_degrees) {
        return false;
    }
    if (!sample_available) {
        return true;
    }

    const double abs_observed_delta_degrees = std::abs(observed_delta_degrees);
    if (abs_observed_delta_degrees < kTurnProbeMinObservedDegrees) {
        return true;
    }

    const bool same_direction = observed_delta_degrees == 0.0 || ((observed_delta_degrees > 0.0) == (units_sent > 0));
    if (!same_direction) {
        return true;
    }

    const double predicted_delta_degrees = std::abs(turn_scale_.PredictDegreesFromUnits(units_sent));
    const double allowed_error = bootstrap_learning
                                     ? std::max(kTurnBootstrapResidualDegrees, predicted_delta_degrees * kTurnBootstrapResidualRatio)
                                     : std::max(kTurnProbeResidualDegrees, predicted_delta_degrees * kTurnProbeResidualRatio);
    if (std::abs(abs_observed_delta_degrees - predicted_delta_degrees) > allowed_error) {
        return true;
    }

    if (!bootstrap_learning
        && abs_observed_delta_degrees
               > predicted_delta_degrees
                     + std::max(kTurnProbeOvershootResidualDegrees, predicted_delta_degrees * kTurnProbeOvershootResidualRatio)) {
        return true;
    }

    return false;
}

bool MotionController::RunTurnProbe(double target_yaw, const std::string& expected_zone_id, int pending_units, double pending_yaw_before)
{
    NativeMouseTurnActuator turn_actuator(*action_wrapper_);

    LogWarn << "Entering turn probe mode." << VAR(target_yaw) << VAR(expected_zone_id) << VAR(pending_units);

    if (IsMoving()) {
        Stop();
        std::this_thread::sleep_for(std::chrono::milliseconds(kStopWaitMs));
    }

    bool pending_sample = pending_units != 0;
    for (int cycle = 0; cycle < kTurnProbeMaxCycles && !should_stop_(); ++cycle) {
        int cycle_units = 0;
        double yaw_before_cycle = position_->angle;

        if (pending_sample) {
            yaw_before_cycle = pending_yaw_before;
            pending_sample = false;
        }
        else {
            const double remaining_delta_degrees = NaviMath::NormalizeAngle(target_yaw - session_->virtual_yaw());
            if (std::abs(remaining_delta_degrees) <= kTurnProbeSuccessDegrees) {
                return true;
            }

            const double probe_delta_degrees =
                std::clamp(remaining_delta_degrees, -kTurnProbeMaxDegreesPerCycle, kTurnProbeMaxDegreesPerCycle);
            cycle_units = turn_scale_.DegreesToUnits(probe_delta_degrees);
            if (cycle_units == 0) {
                return true;
            }

            turn_actuator.TurnByUnits(cycle_units, 0);
            const double predicted_probe_delta_degrees = turn_scale_.PredictDegreesFromUnits(cycle_units);
            session_->SyncVirtualYaw(
                NaviMath::NormalizeAngle(session_->virtual_yaw() + predicted_probe_delta_degrees),
                "turn_probe_predict");
            const double virtual_yaw = session_->virtual_yaw();

            LogInfo << "Turn probe cycle." << VAR(cycle) << VAR(probe_delta_degrees) << VAR(cycle_units)
                    << VAR(predicted_probe_delta_degrees) << VAR(virtual_yaw);
        }

        NaviPosition probe_pos;
        if (!CaptureSettledTurnFeedback(&probe_pos, expected_zone_id, true)) {
            LogWarn << "Turn probe locate failed." << VAR(cycle) << VAR(expected_zone_id);
            continue;
        }

        const int sample_units = cycle_units != 0 ? cycle_units : pending_units;
        const double observed_delta_degrees = NaviMath::NormalizeAngle(probe_pos.angle - yaw_before_cycle);
        const bool same_direction = observed_delta_degrees == 0.0 || ((observed_delta_degrees > 0.0) == (sample_units > 0));

        *position_ = probe_pos;
        session_->SyncVirtualYaw(probe_pos.angle, "turn_probe_feedback");

        if (sample_units != 0 && same_direction) {
            const double previous_units_per_degree = turn_scale_.units_per_degree;
            if (turn_scale_.UpdateFromSample(sample_units, observed_delta_degrees)) {
                const double units_per_degree = turn_scale_.units_per_degree;
                const int accepted_samples = turn_scale_.accepted_samples;
                LogInfo << "Turn scale updated from probe." << VAR(previous_units_per_degree) << VAR(units_per_degree)
                        << VAR(observed_delta_degrees) << VAR(sample_units) << VAR(accepted_samples);
            }
        }

        const double remaining_delta_degrees = NaviMath::NormalizeAngle(target_yaw - session_->virtual_yaw());
        if (std::abs(remaining_delta_degrees) <= kTurnProbeSuccessDegrees) {
            return true;
        }
    }

    return false;
}

double MotionController::NormalizeHeading(double angle)
{
    angle = std::fmod(angle, 360.0);
    if (angle < 0.0) {
        angle += 360.0;
    }
    return angle;
}

} // namespace mapnavigator
