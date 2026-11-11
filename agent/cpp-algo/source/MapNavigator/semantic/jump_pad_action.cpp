#include "jump_pad_action.h"

#include <chrono>
#include <cmath>
#include <cstdint>

#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../process_state.h"
#include "../sensing/position_provider.h"
#include "../state_machine/navigation_state_machine.h"
#include "heading_turn.h"
#include "semantic_helpers.h"
#include "strict_settle.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

Result finish_landing(Navigator* nav, JumpPadState* state)
{
    const int64_t since_launch_ms = ElapsedMs(state->launched_at);
    LogInfo << "Jump pad landed." << VAR(nav->position->zone_id) << VAR(nav->position->x) << VAR(nav->position->y)
            << VAR_AS(ElapsedMs(state.launched_at), since_launch_ms) << VAR_AS(state.jumps, state->jumps)
            << VAR_AS(state.nudges, state->nudges);
    *state = {};
    ResumeAfterRelocation(nav, "jump_pad_landed");
    return Handled();
}

// 转镜头和前进都会挪人, 做完要重新等站定
void nudge_toward_pad(Navigator* nav, JumpPadState* state, double offset)
{
    ++state->nudges;
    state->last_fix.reset();
    state->still_reads = 0;
    const double bearing = CalcTargetRotation(nav->position->x, nav->position->y, state->pad.x, state->pad.y);
    const double heading_delta = CalcDeltaRotation(nav->position->angle, bearing);
    if (std::abs(heading_delta) > kStrictSettleHeadingToleranceDeg) {
        LogInfo << "Jump pad aligning toward the center." << VAR(offset) << VAR(bearing) << VAR(heading_delta)
                << VAR_AS(state.nudges, state->nudges);
        TurnToHeadingOnce(nav, heading_delta);
        SleepFor(kWaitAfterFirstTurnMs);
        return;
    }
    const StrictSettleInputProfile& input =
        UsesTouchBackend(*nav->input_backend) ? kStrictSettleTouchProfile : kStrictSettleKeyboardProfile;
    const int hold_ms = PlanPulseHoldMs(&nav->process->pulses, offset, input.max_hold_ms);
    LogInfo << "Jump pad forward pulse." << VAR(offset) << VAR(hold_ms) << VAR_AS(state.nudges, state->nudges);
    MoveForward(nav->motion_controller);
    SleepFor(hold_ms);
    HoldPosition(nav->motion_controller);
}

Result tick_landing(Navigator* nav, JumpPadState* state)
{
    if (state->still_reads >= kRelocationStableFixes) {
        return finish_landing(nav, state);
    }
    if (ElapsedMs(state->launched_at) > kRelocationWaitTimeoutMs) {
        return Failure("jump_pad_landing_timeout", "Jump pad: did not settle after launch before the timeout.");
    }
    return WaitNextFix(kRelocationRetryIntervalMs);
}

Result tick_on_pad(Navigator* nav, JumpPadState* state, double offset)
{
    if (ElapsedMs(state->started) > kJumpPadBudgetMs) {
        return Failure("jump_pad_not_launched", "Jump pad: still on the pad when the budget ran out.");
    }
    if (state->jumps > 0 && ElapsedMs(state->jumped_at) < kJumpPadLaunchWindowMs) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }
    if (state->still_reads < 1) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    const double stand_band = state->jumps == 0 ? kStrictSettleAcceptBandWu : kJumpPadRetryBandWu;
    if (offset > stand_band) {
        if (state->nudges >= kJumpPadMaxNudges) {
            return Failure("jump_pad_not_reached", "Jump pad: could not stand on the center.");
        }
        nudge_toward_pad(nav, state, offset);
        return Pending();
    }
    if (state->jumps >= kJumpPadMaxJumps) {
        return Failure("jump_pad_not_launched", "Jump pad: jumped on the center without launching.");
    }

    ++state->jumps;
    state->jumped_at = std::chrono::steady_clock::now();
    state->last_fix.reset();
    state->still_reads = 0;
    LogInfo << "Jump pad jump." << VAR(offset) << VAR_AS(state.jumps, state->jumps) << VAR_AS(state.nudges, state->nudges);
    HoldPosition(nav->motion_controller);
    ExecuteJump(nav->input_backend);
    return Pending();
}

} // namespace

Result ArriveJumpPad(Navigator* nav, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance)
{
    HoldAndArmCameraAlign(nav);
    NoteCanonicalFinalGoalConsumed(nav->session, node_idx, *nav->position, "jump_pad_reached");
    NoteInteraction(nav->session);
    LeaveCurrentWaypoint(nav->session, ActionType::JUMP_PAD, "jump_pad_reached");
    ResetOnWaypointAdvance(&nav->runtime_state);

    JumpPadState& state = nav->runtime_state.semantic.jump_pad;
    state = {};
    state.pad.x = waypoint.x;
    state.pad.y = waypoint.y;
    state.pad.zone_id = nav->session->current_zone_id;
    state.started = std::chrono::steady_clock::now();
    LogInfo << "Action: JUMP_PAD reached." << VAR(actual_distance) << VAR(waypoint.x) << VAR(waypoint.y);
    UpdatePhase(nav->session, NaviPhase::WaitJumpPad, "jump_pad_reached");
    return Handled();
}

Result TickJumpPad(Navigator* nav)
{
    JumpPadState& state = nav->runtime_state.semantic.jump_pad;
    HoldPosition(nav->motion_controller);

    // 起飞前要用可信的镜头朝向对准板心, 起飞后只看位置
    const bool captured = state.launched ? CaptureTracked(CapturePartsOf(*nav), nav->position, {})
                                         : CaptureForStrictGoal(CapturePartsOf(*nav), nav->position, state.pad.zone_id);
    if (!captured) {
        state.last_fix.reset();
        state.still_reads = 0;
        if (state.launched && ElapsedMs(state.launched_at) > kRelocationWaitTimeoutMs) {
            return Failure("jump_pad_landing_timeout", "Jump pad: no fix after launch before the timeout.");
        }
        if (!state.launched && ElapsedMs(state.started) > kJumpPadBudgetMs) {
            return Failure("jump_pad_not_launched", "Jump pad: no usable fix on the pad before the timeout.");
        }
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    const NaviPosition& fix = *nav->position;
    const bool still = state.last_fix && fix.zone_id == state.last_fix->zone_id
                       && std::hypot(fix.x - state.last_fix->x, fix.y - state.last_fix->y) <= kStrictSettleStillWu;
    state.still_reads = still ? state.still_reads + 1 : 0;
    state.last_fix = fix;
    const double offset = std::hypot(fix.x - state.pad.x, fix.y - state.pad.y);

    if (!state.launched && (fix.zone_id != state.pad.zone_id || offset >= kRelocationResumeMinDistance)) {
        state.launched = true;
        state.launched_at = std::chrono::steady_clock::now();
        state.still_reads = 0;
        ResetSensorTracking(nav->position_provider);
        LogInfo << "Jump pad launched." << VAR(offset) << VAR(state.jumps) << VAR(state.nudges);
    }
    return state.launched ? tick_landing(nav, &state) : tick_on_pad(nav, &state, offset);
}

} // namespace semantic_nodes

} // namespace mapnavigator
