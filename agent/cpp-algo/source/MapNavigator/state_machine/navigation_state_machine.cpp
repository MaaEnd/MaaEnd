#include <chrono>
#include <utility>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../semantic/async_prompt_action.h"
#include "../semantic/heading_turn.h"
#include "../semantic/semantic_helpers.h"
#include "../semantic/semantic_nodes.h"
#include "../semantic/transit_wait.h"
#include "../sensing/latency_observer.h"
#include "../sensing/position_provider.h"
#include "../sensing/sensitivity_observer.h"
#include "bootstrap.h"
#include "internal.h"
#include "localization.h"
#include "prompts.h"
#include "recovery.h"
#include "replan.h"
#include "steering.h"
#include "tracking.h"

namespace mapnavigator
{

namespace
{

NavigateTick begin_navigate_tick(Navigator* nav)
{
    NavigateTick tick;
    tick.started_at = std::chrono::steady_clock::now();
    tick.gap_ms =
        nav->runtime_state.flow.last_tick_started_at.time_since_epoch().count() > 0
            ? std::chrono::duration_cast<std::chrono::milliseconds>(tick.started_at - nav->runtime_state.flow.last_tick_started_at).count()
            : 0;
    nav->runtime_state.flow.last_tick_started_at = tick.started_at;
    ++nav->runtime_state.flow.tick_seq;
    return tick;
}

TickStep conclude_semantic_step(Navigator* nav, const semantic_nodes::Result& result)
{
    if (result.request_failure) {
        return EndTickWith(FailNavigation(nav, result.failure_reason, result.failure_log_message, 0.0, 0.0, 0));
    }
    // 必须在下面的重规划分支之前, 那里会提前返回
    if (result.stay_in_current_tick) {
        nav->runtime_state.dwell.last_usable = {};
    }
    if (nav->runtime_state.dynamic_replan_requested) {
        return EndTickWith(
            nav->runtime_state.zipline_recovery.pending ? HandleZiplineRecoveryReplan(nav)
                                                        : HandleDynamicReplanRequest(nav, "dynamic_replan"));
    }
    if (result.stay_in_current_tick) {
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

TickStep run_semantic_flow(Navigator* nav)
{
    if (!HasCurrentWaypoint(*nav->session)) {
        NoteRouteTailConsumed(nav->session, *nav->position, "route_tail_consumed");
        return TickStep::EndTick;
    }
    return conclude_semantic_step(nav, semantic_nodes::TickPortalTransit(nav));
}

TickStep run_inline_semantics(Navigator* nav)
{
    // 先对齐镜头, 后面的 HEADING 转身才从对齐后的朝向算起
    if (nav->runtime_state.camera_align_pending) {
        ClearCameraAlignRequest(&nav->runtime_state);
        semantic_nodes::AlignCameraAndRecapture(nav);
        if (!nav->position->valid) {
            return EndTickWith(HandleLocalizationLoss(nav));
        }
    }
    const TickStep step = conclude_semantic_step(nav, semantic_nodes::ConsumeInlineSemantics(nav));
    if (step != TickStep::Next) {
        return step;
    }
    if (!nav->position->valid) {
        return EndTickWith(HandleLocalizationLoss(nav));
    }
    if (!HasCurrentWaypoint(*nav->session)) {
        NoteRouteTailConsumed(nav->session, *nav->position, "route_tail_consumed");
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

TickStep idle_while_not_walking(const Navigator& nav)
{
    if (IsPortalTransitActive(nav.runtime_state.semantic.portal) || nav.session->phase != NaviPhase::Navigate) {
        SleepFor(kTargetTickMs);
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

TickStep finish_navigate_tick(Navigator* nav, const NavigateTick& tick)
{
    // 必须在冲刺判定之前
    UpdatePromptSprintSuppression(nav);
    UpdateAutoSprint(nav, tick);

    if (nav->param->arrival_timeout > 0 && tick.stalled_ms > nav->param->arrival_timeout) {
        return EndTickWith(FailNavigation(
            nav,
            "no_progress_timeout",
            "No progress timeout reached and navigation was terminated.",
            tick.route.progress_distance,
            NormalizeAngle(tick.route.route_heading - tick.fix.current_heading),
            tick.stalled_ms));
    }

    SleepFor(kTargetTickMs);
    return TickStep::EndTick;
}

// 依次执行, 第一个不是 Next 的结果就是这一拍的结果
TickStep run_navigate_steps(Navigator* nav, NavigateTick* tick)
{
    TickStep step = run_semantic_flow(nav);
    if (step != TickStep::Next) {
        return step;
    }
    step = CaptureNavigateFix(nav, tick);
    if (step != TickStep::Next) {
        return step;
    }
    step = CheckDwellWatchdog(nav);
    if (step != TickStep::Next) {
        return step;
    }
    step = run_inline_semantics(nav);
    if (step != TickStep::Next) {
        return step;
    }
    step = idle_while_not_walking(*nav);
    if (step != TickStep::Next) {
        return step;
    }
    TrackRoute(nav, tick);
    step = CheckRouteProgress(nav, tick);
    if (step != TickStep::Next) {
        return step;
    }
    tick->waypoint = CurrentWaypoint(*nav->session);
    step = HandlePrompts(nav, *tick);
    if (step != TickStep::Next) {
        return step;
    }
    tick->arrival_distance = ResolveArrivalDistance(*nav, *tick);
    step = HandleWaypointArrival(nav, tick);
    if (step != TickStep::Next) {
        return step;
    }
    step = RunRiverFallRecovery(nav, *tick);
    if (step != TickStep::Next) {
        return step;
    }
    step = CheckOffRouteWedge(nav, *tick);
    if (step != TickStep::Next) {
        return step;
    }
    step = RunObstacleRecovery(nav, *tick);
    if (step != TickStep::Next) {
        return step;
    }
    tick->heading_error = SteerAlongCorridor(nav, *tick);
    return finish_navigate_tick(nav, *tick);
}

bool tick_navigate(Navigator* nav)
{
    NavigateTick tick = begin_navigate_tick(nav);
    const TickStep step = run_navigate_steps(nav, &tick);
    return step != TickStep::Abort;
}

} // namespace

Navigator MakeNavigator(
    const NaviParam& param,
    InputBackend* input_backend,
    PositionSensor* position_provider,
    Session* session,
    Motion* motion_controller,
    NaviPosition* position,
    std::function<bool()> should_stop,
    MaaContext* maa_context)
{
    return Navigator {
        .param = &param,
        .input_backend = input_backend,
        .position_provider = position_provider,
        .session = session,
        .motion_controller = motion_controller,
        .position = position,
        .should_stop = std::move(should_stop),
        .maa_context = maa_context,
        .collect_prompt = MakeAsyncPromptAction(kCollectPromptSpec, maa_context, session, position),
        .interact_prompt = MakeAsyncPromptAction(kInteractPromptSpec, maa_context, session, position),
        .device_recovery = MakeDeviceRecovery(maa_context, motion_controller, position_provider, session, position),
        .walk_mode = walkmode::OpenToggle(input_backend),
    };
}

bool RunNavigator(Navigator* nav)
{
    latency::BeginRun();

    if (!Bootstrap(nav)) {
        HoldPosition(nav->motion_controller);
        sensitivity::EndRun(nav->maa_context);
        return false;
    }

    PreWarmPrompts(nav);

    StartScanners(nav);

    while (!nav->should_stop() && nav->session->phase != NaviPhase::Finished && nav->session->phase != NaviPhase::Failed) {
        const NaviPhase phase = nav->session->phase;
        AdvanceHeadingEpochOnPhaseChange(nav, phase);
        if (!TickPhase(nav, phase)) {
            StopScanners(nav);
            HoldPosition(nav->motion_controller);
            sensitivity::EndRun(nav->maa_context);
            return false;
        }
    }

    if (!nav->should_stop() && nav->session->phase != NaviPhase::Failed) {
        if (FinalSuccessReady(*nav->session, *nav->position)) {
            CommitFinalSuccess(nav->session, *nav->position, "navigation_complete");
        }
    }

    TryRunPromptSubtaskAtRouteTail(nav);

    StopScanners(nav);
    HoldPosition(nav->motion_controller);

    const bool succeeded = !nav->should_stop() && nav->session->success;
    sensitivity::EndRun(nav->maa_context);
    return succeeded;
}

void AdvanceHeadingEpochOnPhaseChange(Navigator* nav, NaviPhase phase)
{
    if (!nav->last_tick_phase || *nav->last_tick_phase != phase) {
        nav->last_tick_phase = phase;
        ++nav->input_backend->heading_epoch;
    }
}

bool TickPhase(Navigator* nav, NaviPhase phase)
{
    // 放在所有提前返回之前, 否则可能一直停在走路模式
    UpdateWalkMode(nav, phase);

    switch (phase) {
    case NaviPhase::Bootstrap:
        semantic_nodes::SelectPhaseOrFinishRoute(nav, "bootstrap_dispatch");
        return true;
    case NaviPhase::Navigate:
        return tick_navigate(nav);
    case NaviPhase::WaitTransfer:
    case NaviPhase::WaitZipline:
    case NaviPhase::WaitFind:
    case NaviPhase::WaitJumpPad:
    case NaviPhase::WaitRope: {
        ResetStallCounters(&nav->runtime_state.flow);
        ForgetDeviceObservation(&nav->device_recovery);
        const semantic_nodes::Result semantic_result = semantic_nodes::TickWaitPhase(nav, phase);
        if (semantic_result.request_failure) {
            return FailNavigation(nav, semantic_result.failure_reason, semantic_result.failure_log_message, 0.0, 0.0, 0);
        }
        return true;
    }
    case NaviPhase::Finished:
    case NaviPhase::Failed:
        return true;
    }
    LogError << "Unhandled navigation phase." << VAR(static_cast<int>(phase));
    return false;
}

bool ResumeAfterEscape(Navigator* nav, const char* reason)
{
    ResetStallCounters(&nav->runtime_state.flow);
    ClearDynamicRecovery(&nav->runtime_state.recovery);
    nav->runtime_state.recovery_escalation = {};
    ResetRouteTracking(&nav->runtime_state.route);
    ClearDynamicReplanRequest(&nav->runtime_state);
    MarkNavRunDirty(&nav->runtime_state);
    ResetProgress(nav->session);
    semantic_nodes::SelectPhaseOrFinishRoute(nav, reason);
    return true;
}

bool FailNavigation(
    Navigator* nav,
    const char* reason,
    const char* log_message,
    double current_distance,
    double yaw_error,
    int64_t stalled_ms)
{
    HoldPosition(nav->motion_controller);
    ResetNavigationAssistState(&nav->runtime_state);
    UpdatePhase(nav->session, NaviPhase::Failed, reason);
    const TimePoint failed_at = std::chrono::steady_clock::now();
    PruneFailureTrail(&nav->failure_trail, failed_at);
    LogFailureTrail(nav->failure_trail, failed_at);
    LogError << log_message << VAR(current_distance) << VAR(yaw_error) << VAR(stalled_ms);
    return true;
}

} // namespace mapnavigator
