#include <cmath>

#include "local_driver_lite.h"
#include "navi_config.h"

namespace mapnavigator
{

LocalDriverLite::LocalDriverLite()
{
    Reset();
}

void LocalDriverLite::Reset()
{
    state_ = LocalDriverState::Cruise;
    action_ = LocalDriverAction::Forward;
    commit_until_ = {};
    last_jump_time_ = {};
    blocked_since_ = {};
    has_last_jump_time_ = false;
    has_blocked_since_ = false;
    awaiting_commit_result_ = false;
    last_side_left_ = false;
    last_recover_left_ = false;
    consecutive_micro_failures_ = 0;
    consecutive_recover_failures_ = 0;
}

void LocalDriverLite::OnWaypointChanged()
{
    Reset();
}

void LocalDriverLite::CancelCommitment()
{
    commit_until_ = {};
    awaiting_commit_result_ = false;
    state_ = LocalDriverState::Cruise;
    action_ = LocalDriverAction::Forward;
}

LocalDriverAction LocalDriverLite::CurrentAction() const
{
    return action_;
}

LocalDriverState LocalDriverLite::CurrentState() const
{
    return state_;
}

bool LocalDriverLite::HasCommitment(const std::chrono::steady_clock::time_point& now) const
{
    return commit_until_.time_since_epoch().count() > 0 && now < commit_until_;
}

void LocalDriverLite::StartAction(
    LocalDriverState state,
    LocalDriverAction action,
    int commitment_ms,
    const std::chrono::steady_clock::time_point& now)
{
    state_ = state;
    action_ = action;
    commit_until_ = now + std::chrono::milliseconds(commitment_ms);
    awaiting_commit_result_ = true;

    if (action == LocalDriverAction::ForwardLeft || action == LocalDriverAction::RecoverLeft) {
        last_side_left_ = true;
    }
    else if (action == LocalDriverAction::ForwardRight || action == LocalDriverAction::RecoverRight) {
        last_side_left_ = false;
    }
    if (action == LocalDriverAction::RecoverLeft) {
        last_recover_left_ = true;
    }
    else if (action == LocalDriverAction::RecoverRight) {
        last_recover_left_ = false;
    }
}

void LocalDriverLite::ResolveCommitOutcome(bool still_blocked, bool made_progress)
{
    if (!awaiting_commit_result_) {
        return;
    }

    awaiting_commit_result_ = false;
    if (made_progress || !still_blocked) {
        consecutive_micro_failures_ = 0;
        consecutive_recover_failures_ = 0;
        state_ = LocalDriverState::Cruise;
        action_ = LocalDriverAction::Forward;
        return;
    }

    if (state_ == LocalDriverState::Recover) {
        consecutive_recover_failures_++;
        consecutive_micro_failures_ = kLocalDriverMicroFailuresBeforeRecover;
    }
    else {
        consecutive_micro_failures_++;
    }
}

LocalDriverAction LocalDriverLite::PickRecoverAction()
{
    return last_recover_left_ ? LocalDriverAction::RecoverRight : LocalDriverAction::RecoverLeft;
}

LocalDriverDecision LocalDriverLite::Evaluate(const LocalDriverObservation& observation)
{
    LocalDriverDecision decision;
    const double abs_yaw_error = std::abs(observation.yaw_error);
    const double jump_preferred_yaw_degrees =
        observation.prefer_jump_recovery ? kZoneTransitionJumpPreferredYawDegrees : kLocalDriverJumpPreferredYawDegrees;
    const bool made_progress =
        observation.distance_delta > kLocalDriverProgressDistanceDelta || observation.moved_distance > kLocalDriverProgressMoveDelta;

    if (made_progress || observation.actual_distance <= kNoProgressMinDistance || abs_yaw_error > kLocalDriverTurnInPlaceYawDegrees) {
        has_blocked_since_ = false;
    }
    else if (!has_blocked_since_) {
        blocked_since_ = observation.now;
        has_blocked_since_ = true;
    }

    const bool blocked =
        has_blocked_since_
        && std::chrono::duration_cast<std::chrono::milliseconds>(observation.now - blocked_since_).count() >= kLocalDriverBlockDetectMs;

    if (awaiting_commit_result_) {
        if (made_progress || !blocked) {
            ResolveCommitOutcome(false, true);
        }
        else if (!HasCommitment(observation.now)) {
            ResolveCommitOutcome(true, false);
        }
    }

    if (abs_yaw_error > kLocalDriverTurnInPlaceYawDegrees) {
        CancelCommitment();
    }

    if (HasCommitment(observation.now)) {
        decision.state = state_;
        decision.action = action_;
        decision.commitment_active = true;
        decision.blocked = blocked;
        decision.request_rejoin = blocked && consecutive_recover_failures_ >= kLocalDriverRecoverFailuresBeforeRejoin;
        return decision;
    }

    if (!blocked) {
        const LocalDriverAction previous_action = action_;
        state_ = LocalDriverState::Cruise;
        action_ = LocalDriverAction::Forward;
        decision.state = state_;
        decision.action = action_;
        decision.action_changed = previous_action != action_;
        decision.blocked = false;
        return decision;
    }

    if (consecutive_recover_failures_ >= kLocalDriverRecoverFailuresBeforeRejoin) {
        decision.state = LocalDriverState::Recover;
        decision.action = action_;
        decision.blocked = true;
        decision.request_rejoin = true;
        return decision;
    }

    const LocalDriverAction previous_action = action_;
    if (consecutive_micro_failures_ >= kLocalDriverMicroFailuresBeforeRecover) {
        StartAction(LocalDriverState::Recover, PickRecoverAction(), kLocalDriverRecoverCommitMs, observation.now);
    }
    else if (
        abs_yaw_error <= jump_preferred_yaw_degrees
        && (!has_last_jump_time_
            || std::chrono::duration_cast<std::chrono::milliseconds>(observation.now - last_jump_time_).count()
                   >= kLocalDriverJumpCooldownMs)) {
        StartAction(LocalDriverState::MicroAvoid, LocalDriverAction::JumpForward, kLocalDriverJumpCommitMs, observation.now);
        last_jump_time_ = observation.now;
        has_last_jump_time_ = true;
    }
    else if (observation.yaw_error > kLocalDriverSideAvoidYawDegrees) {
        StartAction(LocalDriverState::MicroAvoid, LocalDriverAction::ForwardRight, kLocalDriverMicroCommitMs, observation.now);
    }
    else if (observation.yaw_error < -kLocalDriverSideAvoidYawDegrees) {
        StartAction(LocalDriverState::MicroAvoid, LocalDriverAction::ForwardLeft, kLocalDriverMicroCommitMs, observation.now);
    }
    else {
        const LocalDriverAction side_action = last_side_left_ ? LocalDriverAction::ForwardRight : LocalDriverAction::ForwardLeft;
        StartAction(LocalDriverState::MicroAvoid, side_action, kLocalDriverMicroCommitMs, observation.now);
    }

    decision.state = state_;
    decision.action = action_;
    decision.action_changed = previous_action != action_;
    decision.commitment_active = true;
    decision.blocked = true;
    return decision;
}

} // namespace mapnavigator
