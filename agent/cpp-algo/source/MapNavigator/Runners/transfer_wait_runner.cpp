#include <chrono>
#include <cmath>
#include <thread>

#include <MaaUtils/Logger.h>

#include "../motion_controller.h"
#include "../navi_config.h"
#include "../navigation_session.h"
#include "../position_provider.h"
#include "common/runner_phase_utils.h"
#include "transfer_wait_runner.h"

namespace mapnavigator
{

TransferWaitRunner::TransferWaitRunner(
    PositionProvider* position_provider,
    NavigationSession* session,
    MotionController* motion_controller,
    NaviPosition* position,
    NavigationRuntimeState* runtime_state,
    std::function<bool()> should_stop)
    : position_provider_(position_provider)
    , session_(session)
    , motion_controller_(motion_controller)
    , position_(position)
    , runtime_state_(runtime_state)
    , should_stop_(std::move(should_stop))
{
}

bool TransferWaitRunner::Tick()
{
    if (!session_->HasCurrentWaypoint()) {
        ClearTransferWaitState();
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }

    if (motion_controller_->IsMoving()) {
        StopMotionAndCommitment(motion_controller_);
    }

    const auto now = std::chrono::steady_clock::now();
    if (runtime_state_->transfer_wait_started_.time_since_epoch().count() == 0) {
        runtime_state_->transfer_wait_started_ = now;
    }

    if (!position_provider_->Capture(position_, false, session_->CurrentExpectedZone())) {
        if (HasTimedOut(now)) {
            const int64_t waited_ms =
                std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_->transfer_wait_started_).count();
            return FailTransferWait("transfer_wait_timeout", "TRANSFER wait timed out before capture stabilized.", waited_ms);
        }
        SleepFor(kRelocationRetryIntervalMs);
        return true;
    }

    const int64_t waited_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_->transfer_wait_started_).count();
    if (position_provider_->LastCaptureWasHeld()) {
        runtime_state_->transfer_stable_hits_ = 0;
        if (HasTimedOut(now)) {
            return FailTransferWait("transfer_wait_timeout", "TRANSFER wait timed out while locator fix stayed held.", waited_ms);
        }
        SleepFor(kRelocationRetryIntervalMs);
        return true;
    }

    const double moved_from_anchor = std::hypot(
        position_->x - runtime_state_->transfer_anchor_pos_.x,
        position_->y - runtime_state_->transfer_anchor_pos_.y);
    const bool movement_observed =
        position_->zone_id != runtime_state_->transfer_anchor_pos_.zone_id || moved_from_anchor >= kRelocationResumeMinDistance;
    if (!movement_observed) {
        runtime_state_->transfer_stable_hits_ = 0;
        if (HasTimedOut(now)) {
            return FailTransferWait("transfer_wait_timeout", "TRANSFER wait timed out without external movement.", waited_ms);
        }
        SleepFor(kRelocationRetryIntervalMs);
        return true;
    }

    ++runtime_state_->transfer_stable_hits_;
    if (runtime_state_->transfer_stable_hits_ < kRelocationStableFixes) {
        SleepFor(kRelocationRetryIntervalMs);
        return true;
    }

    session_->SyncVirtualYaw(position_->angle);
    session_->ResetDriverProgressTracking();
    ClearTransferWaitState();

    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }

    // Keep waypoint action semantics on mainline: do not consume here, just leave transfer wait.
    SelectPhaseForCurrentWaypoint(session_, position_, "transfer_wait_complete");
    return true;
}

bool TransferWaitRunner::HasTimedOut(const std::chrono::steady_clock::time_point& now) const
{
    return std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_->transfer_wait_started_).count()
           > kRelocationWaitTimeoutMs;
}

bool TransferWaitRunner::FailTransferWait(const char* reason, const char* log_message, int64_t waited_ms) const
{
    StopMotionAndCommitment(motion_controller_);
    session_->UpdatePhase(NaviPhase::Failed, reason);
    const size_t current_node_idx = session_->current_node_idx();
    LogError << log_message << VAR(current_node_idx) << VAR(waited_ms);
    return true;
}

void TransferWaitRunner::ClearTransferWaitState()
{
    runtime_state_->transfer_wait_started_ = {};
    runtime_state_->transfer_anchor_pos_ = {};
    runtime_state_->transfer_stable_hits_ = 0;
}

void TransferWaitRunner::SleepFor(int millis) const
{
    if (millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(millis));
}

} // namespace mapnavigator
