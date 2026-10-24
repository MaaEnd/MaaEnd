#include "jump_pad_action.h"

#include <chrono>
#include <cmath>
#include <cstdint>

#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../sensing/position_provider.h"
#include "heading_turn.h"
#include "semantic_helpers.h"
#include "strict_settle.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

Result FinishLanding(const Context& ctx)
{
    JumpPadState& state = ctx.runtime_state->semantic.jump_pad;
    LogInfo << "Jump pad landed." << VAR(ctx.position->zone_id) << VAR(ctx.position->x) << VAR(ctx.position->y)
            << VAR(ElapsedMs(state.launched_at)) << VAR(state.jumps) << VAR(state.nudges);
    state = {};
    return ResumeAfterRelocation(ctx, "jump_pad_landed");
}

// 转镜头和前进都会挪人, 做完要重新等站定
void NudgeTowardPad(const Context& ctx, JumpPadState& state, double offset)
{
    ++state.nudges;
    state.last_fix.reset();
    state.still_reads = 0;
    const double bearing = NaviMath::CalcTargetRotation(ctx.position->x, ctx.position->y, state.pad.x, state.pad.y);
    const double heading_delta = NaviMath::CalcDeltaRotation(ctx.position->angle, bearing);
    if (std::abs(heading_delta) > kStrictSettleHeadingToleranceDeg) {
        LogInfo << "Jump pad aligning toward the center." << VAR(offset) << VAR(bearing) << VAR(heading_delta) << VAR(state.nudges);
        TurnToHeadingOnce(ctx, heading_delta);
        SleepFor(kWaitAfterFirstTurnMs);
        return;
    }
    const StrictSettleInputProfile& input =
        ctx.action_wrapper->uses_touch_backend() ? kStrictSettleTouchProfile : kStrictSettleKeyboardProfile;
    const int hold_ms = PlanPulseHoldMs(offset, input.max_hold_ms);
    LogInfo << "Jump pad forward pulse." << VAR(offset) << VAR(hold_ms) << VAR(state.nudges);
    ctx.motion_controller->SetForwardState(true);
    SleepFor(hold_ms);
    ctx.motion_controller->SetForwardState(false);
}

Result TickLanding(const Context& ctx, const JumpPadState& state)
{
    if (state.still_reads >= kRelocationStableFixes) {
        return FinishLanding(ctx);
    }
    if (ElapsedMs(state.launched_at) > kRelocationWaitTimeoutMs) {
        return Failure("jump_pad_landing_timeout", "Jump pad: did not settle after launch before the timeout.");
    }
    return WaitNextFix(kRelocationRetryIntervalMs);
}

Result TickOnPad(const Context& ctx, JumpPadState& state, double offset)
{
    if (ElapsedMs(state.started) > kJumpPadBudgetMs) {
        return Failure("jump_pad_not_launched", "Jump pad: still on the pad when the budget ran out.");
    }
    if (state.jumps > 0 && ElapsedMs(state.jumped_at) < kJumpPadLaunchWindowMs) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }
    if (state.still_reads < 1) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    const double stand_band = state.jumps == 0 ? kStrictSettleAcceptBandWu : kJumpPadRetryBandWu;
    if (offset > stand_band) {
        if (state.nudges >= kJumpPadMaxNudges) {
            return Failure("jump_pad_not_reached", "Jump pad: could not stand on the center.");
        }
        NudgeTowardPad(ctx, state, offset);
        return Pending();
    }
    if (state.jumps >= kJumpPadMaxJumps) {
        return Failure("jump_pad_not_launched", "Jump pad: jumped on the center without launching.");
    }

    ++state.jumps;
    state.jumped_at = std::chrono::steady_clock::now();
    state.last_fix.reset();
    state.still_reads = 0;
    LogInfo << "Jump pad jump." << VAR(offset) << VAR(state.jumps) << VAR(state.nudges);
    ctx.action_executor->Jump();
    return Pending();
}

} // namespace

Result ArriveJumpPad(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance)
{
    StopMotionAndCommitment(ctx);
    ctx.session->NoteCanonicalFinalGoalConsumed(node_idx, *ctx.position, "jump_pad_reached");
    ctx.session->AdvanceToNextWaypoint(ActionType::JUMP_PAD, "jump_pad_reached");
    ctx.runtime_state->OnWaypointAdvance();

    JumpPadState& state = ctx.runtime_state->semantic.jump_pad;
    state = {};
    state.pad.x = waypoint.x;
    state.pad.y = waypoint.y;
    state.pad.zone_id = ctx.session->current_zone_id();
    state.started = std::chrono::steady_clock::now();
    LogInfo << "Action: JUMP_PAD reached." << VAR(actual_distance) << VAR(waypoint.x) << VAR(waypoint.y);
    ctx.session->UpdatePhase(NaviPhase::WaitJumpPad, "jump_pad_reached");
    return Handled();
}

Result TickJumpPad(const Context& ctx)
{
    JumpPadState& state = ctx.runtime_state->semantic.jump_pad;
    ctx.motion_controller->SetForwardState(false);

    // 起飞前要用可信的镜头朝向对准板心, 起飞后只看位置
    const bool captured = state.launched ? ctx.position_provider->Capture(ctx.position, false, {})
                                         : ctx.position_provider->CaptureForStrictGoal(ctx.position, state.pad.zone_id);
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

    const NaviPosition& fix = *ctx.position;
    const bool still = state.last_fix && fix.zone_id == state.last_fix->zone_id
                       && std::hypot(fix.x - state.last_fix->x, fix.y - state.last_fix->y) <= kStrictSettleStillWu;
    state.still_reads = still ? state.still_reads + 1 : 0;
    state.last_fix = fix;
    const double offset = std::hypot(fix.x - state.pad.x, fix.y - state.pad.y);

    if (!state.launched && (fix.zone_id != state.pad.zone_id || offset >= kRelocationResumeMinDistance)) {
        state.launched = true;
        state.launched_at = std::chrono::steady_clock::now();
        state.still_reads = 0;
        ctx.position_provider->ResetTracking();
        LogInfo << "Jump pad launched." << VAR(offset) << VAR(state.jumps) << VAR(state.nudges);
    }
    return state.launched ? TickLanding(ctx, state) : TickOnPad(ctx, state, offset);
}

} // namespace semantic_nodes

} // namespace mapnavigator
