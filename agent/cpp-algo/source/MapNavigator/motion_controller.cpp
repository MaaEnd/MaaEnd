#include <chrono>
#include <cmath>
#include <cstdint>
#include <limits>

#include <MaaUtils/Logger.h>

#include "action_wrapper.h"
#include "latency_observer.h"
#include "motion_controller.h"
#include "navi_config.h"

namespace mapnavigator
{

namespace
{

struct MovementState
{
    bool forward = false;
    bool left = false;
    bool backward = false;
    bool right = false;
};

MovementState BuildMovementState(LocalDriverAction action)
{
    switch (action) {
    case LocalDriverAction::Forward:
    case LocalDriverAction::JumpForward:
        return { .forward = true };
    case LocalDriverAction::BackwardJump:
        return { .backward = true };
    }
    return {};
}

bool ActionRequiresJump(LocalDriverAction action)
{
    return action == LocalDriverAction::JumpForward || action == LocalDriverAction::BackwardJump;
}

} // namespace

MotionController::MotionController(ActionWrapper* action_wrapper, bool enable_local_driver)
    : action_wrapper_(action_wrapper)
    , enable_local_driver_(enable_local_driver)
{
    if (action_wrapper_ != nullptr) {
        steering_profile_ = action_wrapper_->SteeringProfile();
    }
}

void MotionController::Stop()
{
    HoldPosition();
}

void MotionController::SetForwardState(bool forward)
{
    if (!forward) {
        HoldPosition();
        return;
    }

    if (enable_local_driver_) {
        SetAction(LocalDriverAction::Forward, !is_moving_forward_);
        return;
    }

    if (!is_moving_forward_) {
        action_wrapper_->SetMovementStateSync(true, false, false, false, 0);
    }
    is_moving_ = true;
    is_moving_forward_ = true;
}

void MotionController::ReassertForward()
{
    if (!is_moving_forward_) {
        SetForwardState(true);
        return;
    }
    // Re-send a hold the tracked state already believes is applied, because a keydown the game drops leaves
    // the agent standing still while the controller keeps steering as if it were walking. It has to go out as
    // a release and a fresh press: the backends skip any key whose cached state already matches the request,
    // so plain SetMovementStateSync would be a no-op in exactly this case. Bypasses SetAction on purpose --
    // this fires mid-stall, and clearing pending steering there would throw away good aim.
    action_wrapper_->ResetForwardWalkSync(kWalkResetReleaseMs);
}

TurnCommandResult MotionController::ApplySteering(double yaw_delta_deg, int64_t tick_gap_ms)
{
    TurnCommandResult result;
    if (std::abs(yaw_delta_deg) <= std::numeric_limits<double>::epsilon()) {
        result.issued = true;
        return result;
    }

    const auto now = std::chrono::steady_clock::now();
    if (steering_quiet_until_.time_since_epoch().count() > 0 && now < steering_quiet_until_) {
        return result;
    }

    const auto elapsed_ms = last_steering_sent_at_.time_since_epoch().count() == 0
                                ? std::numeric_limits<int64_t>::max()
                                : std::chrono::duration_cast<std::chrono::milliseconds>(now - last_steering_sent_at_).count();

    if (std::abs(yaw_delta_deg) < steering_profile_.min_emit_delta_deg) {
        return result;
    }

    if (elapsed_ms < steering_profile_.min_send_interval_ms) {
        return result;
    }

    const bool should_pause_motion = !steering_profile_.supports_concurrent_move_and_look && IsMoving();
    if (should_pause_motion) {
        action_wrapper_->SetMovementStateSync(false, false, false, false, 0);
    }

    // Spend a long tick on several batches rather than one, so the turn achieved per metre walked holds up as
    // the loop slows down. The transport's own minimum spacing still bounds how many actually fit.
    const double batch_cap = steering_profile_.max_batch_delta_deg;
    int64_t batches = std::clamp<int64_t>(tick_gap_ms / kSteeringRateReferenceMs, 1, kSteeringMaxBatchesPerTick);
    if (steering_profile_.min_send_interval_ms > 0) {
        batches = std::max<int64_t>(1, std::min(batches, tick_gap_ms / steering_profile_.min_send_interval_ms));
    }
    const double tick_cap = batch_cap * static_cast<double>(batches);
    const double want_deg = std::clamp(yaw_delta_deg, -tick_cap, tick_cap);

    double emit_deg = 0.0;
    int64_t send_ms_total = 0;
    for (int64_t batch = 0; batch < batches; ++batch) {
        const double remaining = want_deg - emit_deg;
        if (std::abs(remaining) < steering_profile_.min_emit_delta_deg) {
            break;
        }
        const TurnCommandResult sent = SendViewDelta(std::clamp(remaining, -batch_cap, batch_cap));
        send_ms_total += sent.send_ms;
        if (!sent.issued) {
            break;
        }
        result = sent;
        emit_deg += std::clamp(remaining, -batch_cap, batch_cap);
    }
    // 一拍分几批下发要合起来算，不然差额落到「其余」头上；被拒的那批也花了时间。
    result.send_ms = send_ms_total;
    if (send_ms_total > 0) {
        latency::RecordStage(latency::Stage::Steer, send_ms_total);
    }
    if (result.issued) {
        last_steering_sent_at_ = now;
        if (should_pause_motion) {
            is_moving_ = false;
            is_moving_forward_ = false;
            has_applied_action_ = false;
        }
        result.issued_delta_degrees = emit_deg;
    }
    return result;
}

bool MotionController::SupportsSprint() const
{
    return action_wrapper_ != nullptr && action_wrapper_->SupportsSprint();
}

void MotionController::SetSprintSuppressed(bool suppressed)
{
    if (suppressed && !sprint_suppressed_ && sprint_active_ && is_moving_forward_ && action_wrapper_ != nullptr) {
        action_wrapper_->ResetForwardWalkSync(kSprintCancelReleaseMs);
        sprint_active_ = false;
        LogInfo << "Sprint cancelled near collect point (dropped to walking speed).";
    }
    sprint_suppressed_ = suppressed;
}

bool MotionController::TriggerSprint()
{
    if (sprint_suppressed_) {
        return false;
    }
    if (!SupportsSprint()) {
        return false;
    }

    ClearPendingSteering();
    ArmSteeringQuietPeriod();
    action_wrapper_->TriggerSprintSync();
    sprint_active_ = true;
    LogInfo << "Sprint state armed.";
    return true;
}

void MotionController::HoldPosition()
{
    action_wrapper_->SetMovementStateSync(false, false, false, false, 0);
    has_applied_action_ = false;
    is_moving_ = false;
    is_moving_forward_ = false;
    sprint_active_ = false;
    ClearPendingSteering();
    steering_quiet_until_ = {};
}

void MotionController::SetAction(LocalDriverAction action, bool force)
{
    if (!force && has_applied_action_ && applied_action_ == action) {
        is_moving_ = ActionProducesTranslation(action);
        is_moving_forward_ = ActionMovesForward(action);
        return;
    }

    ClearPendingSteering();
    ArmSteeringQuietPeriod();

    const MovementState movement_state = BuildMovementState(action);
    action_wrapper_->SetMovementStateSync(movement_state.forward, movement_state.left, movement_state.backward, movement_state.right, 0);
    if (ActionRequiresJump(action)) {
        action_wrapper_->TriggerJumpSync(kActionJumpHoldMs);
    }

    applied_action_ = action;
    has_applied_action_ = true;
    is_moving_ = ActionProducesTranslation(action);
    is_moving_forward_ = ActionMovesForward(action);
    sprint_active_ = false;
}

bool MotionController::IsMoving() const
{
    return is_moving_;
}

bool MotionController::IsMovingForward() const
{
    return is_moving_forward_;
}

TurnCommandResult MotionController::SendViewDelta(double delta_degrees)
{
    TurnCommandResult result;
    if (std::abs(delta_degrees) <= std::numeric_limits<double>::epsilon()) {
        result.issued = true;
        return result;
    }

    int units = static_cast<int>(std::lround(delta_degrees * action_wrapper_->DefaultTurnUnitsPerDegree()));
    if (units == 0) {
        units = delta_degrees > 0.0 ? 1 : -1;
    }
    const auto send_started_at = std::chrono::steady_clock::now();
    const bool sent = action_wrapper_->SendViewDeltaSync(units, 0);
    const int64_t send_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - send_started_at).count();
    result.send_ms = send_ms;
    if (!sent) {
        LogWarn << "Steering command rejected by the input backend." << VAR(delta_degrees) << VAR(units) << VAR(send_ms);
        return result;
    }

    result.issued = true;
    result.issued_delta_degrees = delta_degrees;
    LogDebug << "Steering command issued." << VAR(delta_degrees) << VAR(units) << VAR(send_ms);
    return result;
}

bool MotionController::ActionMovesForward(LocalDriverAction action) const
{
    return action == LocalDriverAction::Forward || action == LocalDriverAction::JumpForward;
}

bool MotionController::ActionProducesTranslation(LocalDriverAction action) const
{
    switch (action) {
    case LocalDriverAction::Forward:
    case LocalDriverAction::JumpForward:
    case LocalDriverAction::BackwardJump:
        return true;
    }
    return false;
}

void MotionController::ArmSteeringQuietPeriod()
{
    if (steering_profile_.action_quiet_period_ms <= 0) {
        return;
    }
    steering_quiet_until_ = std::chrono::steady_clock::now() + std::chrono::milliseconds(steering_profile_.action_quiet_period_ms);
}

void MotionController::ClearPendingSteering()
{
    pending_yaw_deg_ = 0.0;
}

} // namespace mapnavigator
