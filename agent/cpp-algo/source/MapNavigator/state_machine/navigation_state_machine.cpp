#include <chrono>
#include <utility>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../semantic/action_executor.h"
#include "../semantic/async_prompt_action.h"
#include "../semantic/heading_turn.h"
#include "../semantic/semantic_helpers.h"
#include "../semantic/semantic_nodes.h"
#include "../semantic/transit_wait.h"
#include "../sensing/latency_observer.h"
#include "../sensing/position_provider.h"
#include "../sensing/sensitivity_observer.h"
#include "internal.h"

namespace mapnavigator
{

NavigationStateMachine::NavigationStateMachine(
    const NaviParam& param,
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    MotionController* motion_controller,
    ActionExecutor* action_executor,
    NaviPosition* position,
    std::function<bool()> should_stop,
    MaaContext* maa_context)
    : param_(param)
    , action_wrapper_(action_wrapper)
    , position_provider_(position_provider)
    , session_(session)
    , motion_controller_(motion_controller)
    , action_executor_(action_executor)
    , position_(position)
    , should_stop_(std::move(should_stop))
    , maa_context_(maa_context)
    , collect_prompt_(kCollectPromptSpec, maa_context, session, position)
    , interact_prompt_(kInteractPromptSpec, maa_context, session, position)
    , device_recovery_(maa_context, motion_controller, position_provider, session, position)
    , walk_mode_(action_wrapper)
{
    LogInfo << "Navigation route runner selected. backend=orchestrated";
}

NavigationStateMachine::~NavigationStateMachine()
{
    // 帧回调里持有 this, 析构前必须先拆掉
    stopScanners();
}

semantic_nodes::Context NavigationStateMachine::semanticContext()
{
    return {
        .action_wrapper = action_wrapper_,
        .position_provider = position_provider_,
        .session = session_,
        .motion_controller = motion_controller_,
        .action_executor = action_executor_,
        .position = position_,
        .runtime_state = &runtime_state_,
        .maa_context = maa_context_,
    };
}

bool NavigationStateMachine::run()
{
    latency::BeginRun();

    if (!bootstrap()) {
        stopMotion();
        sensitivity::EndRun(maa_context_);
        return false;
    }

    preWarmPromptRecognition();

    startScanners();

    while (!should_stop_() && session_->phase() != NaviPhase::Finished && session_->phase() != NaviPhase::Failed) {
        if (!tickPhase(session_->phase())) {
            stopScanners();
            stopMotion();
            sensitivity::EndRun(maa_context_);
            return false;
        }
    }

    if (!should_stop_() && session_->phase() != NaviPhase::Failed) {
        session_->hasSatisfiedFinalSuccess(*position_, "navigation_complete");
    }

    tryRunPromptSubtaskAtRouteTail();

    stopScanners();
    stopMotion();

    const bool succeeded = !should_stop_() && session_->success();
    sensitivity::EndRun(maa_context_);
    return succeeded;
}

bool NavigationStateMachine::tickPhase(NaviPhase phase)
{
    if (!last_tick_phase_ || *last_tick_phase_ != phase) {
        last_tick_phase_ = phase;
        action_wrapper_->noteHeadingDisturbed();
    }
    // 放在所有提前返回之前, 否则可能一直停在走路模式
    updateWalkMode(phase);

    switch (phase) {
    case NaviPhase::Bootstrap:
        selectPhaseForCurrentWaypoint("bootstrap_dispatch");
        return true;
    case NaviPhase::Navigate:
        return tickNavigate();
    case NaviPhase::WaitTransfer:
    case NaviPhase::WaitZipline:
    case NaviPhase::WaitFind:
    case NaviPhase::WaitJumpPad:
    case NaviPhase::WaitRope: {
        runtime_state_.flow.motionless_hold_ticks = 0;
        runtime_state_.flow.futile_forward_reasserts = 0;
        device_recovery_.forgetObservation();
        const semantic_nodes::Result semantic_result = semantic_nodes::TickWaitPhase(semanticContext(), phase);
        if (semantic_result.request_failure) {
            return failNavigation(semantic_result.failure_reason, semantic_result.failure_log_message, 0.0, 0.0, 0);
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

bool NavigationStateMachine::tickNavigate()
{
    NavigateTick tick = beginNavigateTick();
    const TickStep step = FirstToClaim(
        [&] { return runSemanticFlow(); },
        [&] { return captureNavigateFix(tick); },
        [&] { return checkDwellWatchdog(); },
        [&] { return runInlineSemantics(); },
        [&] { return idleWhileNotWalking(); },
        [&] { trackRoute(tick); },
        [&] { return checkRouteProgress(tick); },
        [&] { tick.waypoint = session_->currentWaypoint(); },
        [&] { return handlePrompts(tick); },
        [&] { tick.arrival_distance = resolveArrivalDistance(tick); },
        [&] { return handleWaypointArrival(tick); },
        [&] { return runRiverFallRecovery(tick); },
        [&] { return checkOffRouteWedge(tick); },
        [&] { return runObstacleRecovery(tick); },
        [&] { tick.heading_error = steerAlongCorridor(tick); },
        [&] { return finishNavigateTick(tick); });
    return step != TickStep::Abort;
}

NavigationStateMachine::NavigateTick NavigationStateMachine::beginNavigateTick()
{
    NavigateTick tick;
    tick.started_at = std::chrono::steady_clock::now();
    tick.gap_ms =
        runtime_state_.flow.last_tick_started_at.time_since_epoch().count() > 0
            ? std::chrono::duration_cast<std::chrono::milliseconds>(tick.started_at - runtime_state_.flow.last_tick_started_at).count()
            : 0;
    runtime_state_.flow.last_tick_started_at = tick.started_at;
    ++runtime_state_.flow.tick_seq;
    return tick;
}

TickStep NavigationStateMachine::runSemanticFlow()
{
    if (!session_->hasCurrentWaypoint()) {
        session_->noteRouteTailConsumed(*position_, "route_tail_consumed");
        return TickStep::EndTick;
    }
    return concludeSemanticStep(semantic_nodes::TickPortalTransit(semanticContext()));
}

TickStep NavigationStateMachine::concludeSemanticStep(const semantic_nodes::Result& result)
{
    if (result.request_failure) {
        return EndTickWith(failNavigation(result.failure_reason, result.failure_log_message, 0.0, 0.0, 0));
    }
    // 必须在下面的重规划分支之前, 那里会提前返回
    if (result.stay_in_current_tick) {
        runtime_state_.dwell.last_usable = {};
    }
    if (runtime_state_.dynamic_replan_requested) {
        return EndTickWith(
            runtime_state_.zipline_recovery.pending ? handleZiplineRecoveryReplan() : handleDynamicReplanRequest("dynamic_replan"));
    }
    if (result.stay_in_current_tick) {
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

TickStep NavigationStateMachine::runInlineSemantics()
{
    // 先对齐镜头, 后面的 HEADING 转身才从对齐后的朝向算起
    if (runtime_state_.camera_align_pending) {
        runtime_state_.camera_align_pending = false;
        semantic_nodes::AlignCameraToHeadingOnce(semanticContext());
        if (!position_->valid) {
            return EndTickWith(handleLocalizationLoss());
        }
    }
    endCrossTierEscapeAtRejoin();
    if (const TickStep step = concludeSemanticStep(semantic_nodes::ConsumeInlineSemantics(semanticContext())); step != TickStep::Next) {
        return step;
    }
    if (!position_->valid) {
        return EndTickWith(handleLocalizationLoss());
    }
    if (!session_->hasCurrentWaypoint()) {
        session_->noteRouteTailConsumed(*position_, "route_tail_consumed");
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

TickStep NavigationStateMachine::idleWhileNotWalking()
{
    if (runtime_state_.semantic.portal.active() || session_->phase() != NaviPhase::Navigate) {
        SleepFor(kTargetTickMs);
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

TickStep NavigationStateMachine::finishNavigateTick(const NavigateTick& tick)
{
    // 必须在冲刺判定之前
    updatePromptSprintSuppression();
    updateAutoSprint(tick);

    if (param_.arrival_timeout > 0 && tick.stalled_ms > param_.arrival_timeout) {
        return EndTickWith(failNavigation(
            "no_progress_timeout",
            "No progress timeout reached and navigation was terminated.",
            tick.route.progress_distance,
            NaviMath::normalizeAngle(tick.route.route_heading - tick.fix.current_heading),
            tick.stalled_ms));
    }

    SleepFor(kTargetTickMs);
    return TickStep::EndTick;
}

void NavigationStateMachine::selectPhaseForCurrentWaypoint(const char* reason)
{
    semantic_nodes::SelectPhaseForCurrentWaypoint(semanticContext(), reason);
}

bool NavigationStateMachine::resumeAfterEscape(const char* reason)
{
    runtime_state_.flow.futile_forward_reasserts = 0;
    runtime_state_.flow.motionless_hold_ticks = 0;
    runtime_state_.recovery.reset();
    runtime_state_.recovery_escalation.reset();
    runtime_state_.route.resetTracking();
    runtime_state_.dynamic_replan_requested = false;
    runtime_state_.nav_run_dirty = true;
    session_->resetProgress();
    selectPhaseForCurrentWaypoint(reason);
    return true;
}

void NavigationStateMachine::stopMotion()
{
    motion_controller_->setForwardState(false);
}

bool NavigationStateMachine::failNavigation(
    const char* reason,
    const char* log_message,
    double current_distance,
    double yaw_error,
    int64_t stalled_ms)
{
    stopMotion();
    runtime_state_.resetNavigationAssistState();
    session_->updatePhase(NaviPhase::Failed, reason);
    LogError << log_message << VAR(current_distance) << VAR(yaw_error) << VAR(stalled_ms);
    return true;
}

} // namespace mapnavigator
