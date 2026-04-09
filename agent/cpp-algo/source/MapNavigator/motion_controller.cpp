#include <MaaUtils/Logger.h>

#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navigation_session.h"

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
    case LocalDriverAction::ForwardLeft:
        return { .forward = true, .left = true };
    case LocalDriverAction::ForwardRight:
        return { .forward = true, .right = true };
    case LocalDriverAction::RecoverLeft:
        return { .left = true, .backward = true };
    case LocalDriverAction::RecoverRight:
        return { .backward = true, .right = true };
    }
    return {};
}

bool ActionRequiresJump(LocalDriverAction action)
{
    return action == LocalDriverAction::JumpForward;
}

} // namespace

MotionController::MotionController(
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    NaviPosition* position,
    bool enable_local_driver,
    std::function<bool()> should_stop)
    : action_wrapper_(action_wrapper)
    , session_(session)
    , should_stop_(std::move(should_stop))
    , enable_local_driver_(enable_local_driver)
    , turn_execution_engine_(action_wrapper, position_provider, session, position, should_stop_)
{
}

void MotionController::Stop()
{
    turn_execution_engine_.CancelPendingRotation("motion_stop");
    HoldPosition();
}

void MotionController::HoldPosition()
{
    action_wrapper_->SetMovementStateSync(false, false, false, false, 0);
    has_applied_action_ = false;
    is_moving_ = false;
    is_moving_forward_ = false;
    sprint_active_ = false;
}

void MotionController::SetAction(LocalDriverAction action, bool force)
{
    if (!force && has_applied_action_ && applied_action_ == action) {
        is_moving_ = ActionProducesTranslation(action);
        is_moving_forward_ = ActionMovesForward(action);
        return;
    }

    const MovementState movement_state = BuildMovementState(action);
    action_wrapper_->SetMovementStateSync(movement_state.forward, movement_state.left, movement_state.backward, movement_state.right, 0);
    if (ActionRequiresJump(action)) {
        action_wrapper_->TriggerJumpSync(kActionJumpHoldMs);
    }

    applied_action_ = action;
    has_applied_action_ = true;
    is_moving_ = ActionProducesTranslation(action);
    is_moving_forward_ = ActionMovesForward(action);
}

void MotionController::EnsureForwardMotion(bool force)
{
    if (enable_local_driver_) {
        SetAction(LocalDriverAction::Forward, force);
        return;
    }

    if (force || !is_moving_) {
        action_wrapper_->SetMovementStateSync(true, false, false, false, 0);
    }
    is_moving_ = true;
    is_moving_forward_ = true;
}

void MotionController::ResetForwardWalk(int release_millis)
{
    Stop();
    action_wrapper_->ResetForwardWalkSync(release_millis);
    applied_action_ = LocalDriverAction::Forward;
    has_applied_action_ = true;
    is_moving_ = true;
    is_moving_forward_ = true;
    sprint_active_ = false;
}

void MotionController::NotifySprintTriggered()
{
    sprint_active_ = true;
}

bool MotionController::CancelSprintIfActive(int release_millis)
{
    if (!sprint_active_) {
        return false;
    }

    if (!is_moving_) {
        sprint_active_ = false;
        return false;
    }

    ResetForwardWalk(release_millis);
    LogInfo << "Cancelled active sprint state before action.";
    return true;
}

bool MotionController::IsMoving() const
{
    return is_moving_;
}

bool MotionController::IsMovingForward() const
{
    return is_moving_forward_;
}

bool MotionController::HasAppliedAction() const
{
    return has_applied_action_;
}

MotionPredictMode MotionController::predict_mode() const
{
    if (!is_moving_forward_) {
        return MotionPredictMode::Idle;
    }

    if (has_applied_action_ && applied_action_ != LocalDriverAction::Forward) {
        return MotionPredictMode::Corrective;
    }

    if (sprint_active_) {
        return MotionPredictMode::Sprint;
    }

    return MotionPredictMode::Walk;
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
    return turn_execution_engine_.adaptive_mode_enabled();
}

void MotionController::EnableAdaptiveMode(const char* reason, double metric)
{
    turn_execution_engine_.EnableAdaptiveMode(reason, metric);
}

TurnCommandResult MotionController::InjectMouseAndTrack(
    double delta_degrees,
    bool allow_learning,
    const std::string& expected_zone_id,
    int settle_wait_ms,
    TurnActionKind action_kind,
    uint64_t frame_seq,
    bool block_forward_until_confirmed,
    double confirm_heading_tolerance_degrees)
{
    return turn_execution_engine_.InjectMouseAndTrack(
        delta_degrees,
        allow_learning,
        expected_zone_id,
        settle_wait_ms,
        action_kind,
        frame_seq,
        block_forward_until_confirmed,
        confirm_heading_tolerance_degrees);
}

void MotionController::UpdateTurnLifecycle(uint64_t frame_seq)
{
    turn_execution_engine_.UpdatePendingTurnLifecycle(frame_seq);
}

bool MotionController::HasActiveTurnLifecycle() const
{
    return turn_execution_engine_.HasActiveTurnLifecycle();
}

bool MotionController::HasActiveSteeringTrimLifecycle() const
{
    return turn_execution_engine_.HasActiveSteeringTrimLifecycle();
}

bool MotionController::HasPendingTrimRetryRequest() const
{
    return turn_execution_engine_.HasPendingTrimRetryRequest();
}

bool MotionController::ShouldBlockForwardMotion() const
{
    return turn_execution_engine_.ShouldBlockForwardMotion();
}

void MotionController::ClearPendingTrimRetry(const char* reason)
{
    turn_execution_engine_.ClearPendingTrimRetry(reason);
}

bool MotionController::ActionMovesForward(LocalDriverAction action) const
{
    return action == LocalDriverAction::Forward || action == LocalDriverAction::ForwardLeft || action == LocalDriverAction::ForwardRight
           || action == LocalDriverAction::JumpForward;
}

bool MotionController::ActionProducesTranslation(LocalDriverAction action) const
{
    switch (action) {
    case LocalDriverAction::Forward:
    case LocalDriverAction::ForwardLeft:
    case LocalDriverAction::ForwardRight:
    case LocalDriverAction::JumpForward:
    case LocalDriverAction::RecoverLeft:
    case LocalDriverAction::RecoverRight:
        return true;
    }
    return false;
}

} // namespace mapnavigator
