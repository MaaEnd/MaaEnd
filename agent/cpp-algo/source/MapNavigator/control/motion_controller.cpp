#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <limits>

#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "motion_controller.h"
#include "view_turn.h"
#include "walk_mode.h"

namespace mapnavigator
{

namespace
{

struct LocalActionTraits
{
    MovementState movement;
    bool jumps = false;
    bool translates = false;
    bool moves_forward = false;
};

LocalActionTraits local_action_traits_of(LocalDriverAction action)
{
    switch (action) {
    case LocalDriverAction::Forward:
        return { .movement = { .forward = true }, .translates = true, .moves_forward = true };
    case LocalDriverAction::JumpForward:
        return { .movement = { .forward = true }, .jumps = true, .translates = true, .moves_forward = true };
    case LocalDriverAction::BackwardJump:
        return { .movement = { .backward = true }, .jumps = true, .translates = true };
    }
    return {};
}

bool steering_gate_open(const Motion& motion, double yaw_delta_deg, std::chrono::steady_clock::time_point now)
{
    if (motion.steering_quiet_until.time_since_epoch().count() > 0 && now < motion.steering_quiet_until) {
        return false;
    }
    const int64_t elapsed_ms =
        IsUnset(motion.last_steering_sent_at) ? std::numeric_limits<int64_t>::max() : ElapsedMs(motion.last_steering_sent_at, now);
    return std::abs(yaw_delta_deg) >= motion.steering_profile.min_emit_delta_deg
           && elapsed_ms >= motion.steering_profile.min_send_interval_ms;
}

int64_t plan_batch_count(const Motion& motion, double yaw_delta_deg, int64_t tick_gap_ms)
{
    int64_t batches = std::clamp<int64_t>(
        static_cast<int64_t>(std::ceil(std::abs(yaw_delta_deg) / motion.steering_profile.max_batch_delta_deg)),
        1,
        kSteeringMaxBatchesPerTick);
    if (motion.steering_profile.min_send_interval_ms > 0) {
        batches = std::max<int64_t>(1, std::min(batches, tick_gap_ms / motion.steering_profile.min_send_interval_ms));
    }
    return batches;
}

TurnCommandResult send_view_delta(sensitivity::SensitivityRun* sensitivity, const Motion& motion, double delta_degrees)
{
    TurnCommandResult result;
    if (std::abs(delta_degrees) <= std::numeric_limits<double>::epsilon()) {
        result.issued = true;
        return result;
    }

    const int units = YawUnitsAtLeastOne(*motion.input_backend, *sensitivity, delta_degrees);
    const auto send_started_at = std::chrono::steady_clock::now();
    const bool sent = SendYawDeltaAndNoteTurn(motion.input_backend, sensitivity, units, 0);
    const int64_t send_ms = ElapsedMs(send_started_at);
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

TurnCommandResult emit_batches(sensitivity::SensitivityRun* sensitivity, const Motion& motion, double yaw_delta_deg, int64_t batches)
{
    const double batch_cap = motion.steering_profile.max_batch_delta_deg;
    const double tick_cap = batch_cap * static_cast<double>(batches);
    const double want_deg = std::clamp(yaw_delta_deg, -tick_cap, tick_cap);

    TurnCommandResult result;
    double emit_deg = 0.0;
    int64_t send_ms_total = 0;
    for (int64_t batch = 0; batch < batches; ++batch) {
        const double remaining = want_deg - emit_deg;
        if (std::abs(remaining) < motion.steering_profile.min_emit_delta_deg) {
            break;
        }
        const double step_deg = std::clamp(remaining, -batch_cap, batch_cap);
        const TurnCommandResult sent = send_view_delta(sensitivity, motion, step_deg);
        send_ms_total += sent.send_ms;
        if (!sent.issued) {
            break;
        }
        result = sent;
        emit_deg += step_deg;
    }
    result.send_ms = send_ms_total;
    if (result.issued) {
        result.issued_delta_degrees = emit_deg;
    }
    return result;
}

} // namespace

Motion MakeMotion(InputBackend* input_backend)
{
    Motion motion;
    motion.input_backend = input_backend;
    if (motion.input_backend != nullptr) {
        motion.steering_profile = SteeringProfileOf(*motion.input_backend);
    }
    return motion;
}

void HoldPosition(Motion* motion)
{
    SetMovementStateSync(motion->input_backend, {}, 0);
    motion->has_applied_action = false;
    motion->is_moving = false;
    motion->is_moving_forward = false;
    motion->sprint_active = false;
    motion->steering_quiet_until = {};
}

void MoveForward(Motion* motion)
{
    if (motion->enable_local_driver) {
        if (motion->is_moving_forward && motion->has_applied_action && motion->applied_action == LocalDriverAction::Forward) {
            motion->is_moving = true;
            return;
        }
        ArmSteeringQuietPeriod(motion);
        ApplyLocalAction(motion, LocalDriverAction::Forward);
        return;
    }

    if (!motion->is_moving_forward) {
        SetMovementStateSync(motion->input_backend, { .forward = true }, 0);
    }
    motion->is_moving = true;
    motion->is_moving_forward = true;
}

void ReassertForward(Motion* motion)
{
    if (!motion->is_moving_forward) {
        MoveForward(motion);
        return;
    }
    // 后端会跳过与缓存状态相同的按键, 须松开再按, 且不开静默期以免清掉在途转向
    ResetForwardWalkSync(motion->input_backend, kWalkResetReleaseMs);
}

SteeringGate SteeringGateOf(const Motion& motion, double yaw_delta_deg, std::chrono::steady_clock::time_point now)
{
    if (!steering_gate_open(motion, yaw_delta_deg, now)) {
        return SteeringGate::Closed;
    }
    const bool pauses_move = !motion.steering_profile.supports_concurrent_move_and_look && motion.is_moving;
    return pauses_move ? SteeringGate::OpenPausingMove : SteeringGate::Open;
}

void PauseMotionForTurn(Motion* motion)
{
    SetMovementStateSync(motion->input_backend, {}, 0);
}

TurnCommandResult SendSteeringBatches(Motion* motion, sensitivity::SensitivityRun* sensitivity, double yaw_delta_deg, int64_t tick_gap_ms)
{
    return emit_batches(sensitivity, *motion, yaw_delta_deg, plan_batch_count(*motion, yaw_delta_deg, tick_gap_ms));
}

void NoteSteeringSent(Motion* motion, SteeringGate gate, std::chrono::steady_clock::time_point now)
{
    motion->last_steering_sent_at = now;
    if (gate == SteeringGate::OpenPausingMove) {
        motion->is_moving = false;
        motion->is_moving_forward = false;
        motion->has_applied_action = false;
    }
}

void ArmSteeringQuietPeriod(Motion* motion)
{
    if (motion->steering_profile.action_quiet_period_ms <= 0) {
        return;
    }
    motion->steering_quiet_until =
        std::chrono::steady_clock::now() + std::chrono::milliseconds(motion->steering_profile.action_quiet_period_ms);
}

bool CanSprint(const Motion& motion)
{
    return motion.enable_sprint && motion.input_backend != nullptr && SupportsSprint(*motion.input_backend);
}

bool SprintAllowed(const Motion& motion)
{
    return !motion.sprint_suppressed && CanSprint(motion);
}

void MarkSprintActive(Motion* motion)
{
    motion->sprint_active = true;
    LogInfo << "Sprint state armed.";
}

void TriggerSprint(Motion* motion, walkmode::WalkBelief* walk)
{
    ArmSteeringQuietPeriod(motion);
    TriggerSprintSync(motion->input_backend);
    walkmode::NoteSprintTriggered(walk);
    MarkSprintActive(motion);
}

bool SprintCancellable(const Motion& motion)
{
    return !motion.sprint_suppressed && motion.sprint_active && motion.is_moving_forward && motion.input_backend != nullptr;
}

void CancelSprint(Motion* motion)
{
    ResetForwardWalkSync(motion->input_backend, kSprintCancelReleaseMs);
    motion->sprint_active = false;
    LogInfo << "Sprint cancelled near collect point (dropped to walking speed).";
}

void ApplyLocalAction(Motion* motion, LocalDriverAction action)
{
    const LocalActionTraits traits = local_action_traits_of(action);
    SetMovementStateSync(motion->input_backend, traits.movement, 0);
    if (traits.jumps) {
        TriggerJumpSync(motion->input_backend, kActionJumpHoldMs);
    }

    motion->applied_action = action;
    motion->has_applied_action = true;
    motion->is_moving = traits.translates;
    motion->is_moving_forward = traits.moves_forward;
    motion->sprint_active = false;
}

} // namespace mapnavigator
