#include <chrono>
#include <cmath>
#include <optional>
#include <string>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../planning/navmesh_queries.h"
#include "../semantic/semantic_nodes.h"
#include "../semantic/trigger_action.h"
#include "../sensing/maa_task.h"
#include "../sensing/position_provider.h"
#include "../zipline/zipline_action.h"
#include "internal.h"
#include "river_fall_recovery.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace
{

// 读不到画面也当作有对话, 宁可不接
bool dialog_on_screen(MaaContext* context, MaaController* controller)
{
    ScopedImageBuffer image;
    if (context == nullptr || controller == nullptr || !MaaControllerCachedImage(controller, image.Get())
        || MaaImageBufferIsEmpty(image.Get())) {
        return true;
    }
    for (const char* node : { kSceneRecoveryStoryNode, kSceneRecoveryDialogNode }) {
        NodeSighting sighting {};
        if (!RunRecognitionNode(context, node, "{}", image.Get(), &sighting) || sighting.hit) {
            return true;
        }
    }
    return false;
}

std::string scene_recovery_override(MaaSize esc_used)
{
    const json::object nodes {
        { kSceneRecoveryEntryNode, json::object { { "timeout", kSceneRecoveryEntryTimeoutMs } } },
        { kSceneRecoveryStoryNode, json::object { { "enabled", false } } },
        { kSceneRecoveryDialogNode, json::object { { "enabled", false } } },
        { kSceneRecoveryStableCheckNode, json::object { { "enabled", false } } },
        { kSceneRecoveryEscNode, json::object { { "max_hit", esc_used + kSceneRecoveryEscBudget } } },
    };
    return json::value(nodes).dumps();
}

} // namespace

bool NavigationStateMachine::captureCurrentPosition(bool force_global_search)
{
    const uint64_t heading_epoch = action_wrapper_->headingEpoch();
    const bool prior_usable = !force_global_search && param_.heading_source == HeadingSource::Camera && position_->valid
                              && prior_observation_epoch_ && *prior_observation_epoch_ == heading_epoch
                              && session_->phase() == NaviPhase::Navigate;
    const std::optional<double> camera_heading_prior = prior_usable ? std::optional<double>(position_->angle) : std::nullopt;
    const bool captured = position_provider_->capture(position_, force_global_search, session_->currentZoneId(), {}, camera_heading_prior);
    if (captured) {
        prior_observation_epoch_ = heading_epoch;
        consecutive_prior_uses_ = camera_heading_prior ? consecutive_prior_uses_ + 1 : 0;
        LogDebug << "MapNavigator camera heading prior" << VAR(camera_heading_prior.value_or(-1.0)) << VAR(consecutive_prior_uses_);
    }
    else {
        consecutive_prior_uses_ = 0;
    }
    SampleFailureTrail(
        &failure_trail_,
        std::chrono::steady_clock::now(),
        *position_,
        captured,
        position_provider_->lastLocateMessage(),
        session_->currentNodeIdx(),
        navi_phase_name(session_->phase()));
    updateDwellWatchdog(captured);
    return captured;
}

void NavigationStateMachine::updateDwellWatchdog(bool captured)
{
    DwellWatchdogState& dwell = runtime_state_.dwell;
    const bool usable = captured && position_->valid && !position_provider_->lastCaptureWasBlackScreen();
    if (!usable || session_->phase() != NaviPhase::Navigate) {
        dwell.last_usable = {};
        return;
    }

    const auto now = std::chrono::steady_clock::now();
    const bool zone_changed = ZonesDifferInGeometry(param_, dwell.center_zone, position_->zone_id);
    if (!dwell.latched || zone_changed || std::hypot(position_->x - dwell.center_x, position_->y - dwell.center_y) > kDwellWatchdogRadius) {
        dwell.latched = true;
        dwell.center_x = position_->x;
        dwell.center_y = position_->y;
        dwell.center_zone = position_->zone_id;
        dwell.dwell_ms = 0;
    }
    else if (dwell.last_usable.time_since_epoch().count() > 0) {
        dwell.dwell_ms += std::chrono::duration_cast<std::chrono::milliseconds>(now - dwell.last_usable).count();
    }
    dwell.last_usable = now;
}

TickStep NavigationStateMachine::captureNavigateFix(NavigateTick& tick)
{
    const auto capture_started_at = std::chrono::steady_clock::now();
    const bool position_captured = captureCurrentPosition(false);
    tick.capture_ms = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - capture_started_at).count();
    // 须在定位丢失分支之前: 待命中的画面可能盖住小地图
    const semantic_nodes::Result trigger_result = semantic_nodes::ProbeTriggerWhileNavigating(semanticContext());
    if (trigger_result.request_failure) {
        return EndTickWith(failNavigation(trigger_result.failure_reason, trigger_result.failure_log_message, 0.0, 0.0, 0));
    }
    if (trigger_result.consumed) {
        return TickStep::EndTick;
    }
    if (!position_captured) {
        return EndTickWith(handleLocalizationLoss());
    }
    if (endLocalizationLoss()) {
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

bool NavigationStateMachine::endLocalizationLoss()
{
    LocalizationLossState& loss = runtime_state_.localization_loss;
    if (loss.started_at == std::chrono::steady_clock::time_point {}) {
        loss = {};
        return false;
    }
    session_->excludeFromStallClocks(std::chrono::steady_clock::now() - loss.started_at);
    const auto loss_ms = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - loss.started_at).count();
    const bool armed = armRiverFallRecoveryIfBlackScreenLoss("normal_reacquire");
    LogInfo << "Localization recovered via normal tracking." << VAR(loss_ms) << VAR(loss.saw_black_screen) << VAR(armed)
            << VAR(position_->x) << VAR(position_->y);
    NoteTimedFailure(&failure_trail_, std::chrono::steady_clock::now(), "localization recovered", loss_ms);
    loss = {};
    return armed;
}

TickStep NavigationStateMachine::checkDwellWatchdog()
{
    if (runtime_state_.dwell.dwell_ms < kDwellWatchdogFailMs || !session_->hasCurrentWaypoint()) {
        return TickStep::Next;
    }
    const Waypoint& pinned_at = session_->currentWaypoint();
    if (pinned_at.action == ActionType::ZIPLINE) {
        LogWarn << "Dwell watchdog tripped at a zipline tower; dropping the chain and walking." << VAR(runtime_state_.dwell.dwell_ms)
                << VAR(position_->x) << VAR(position_->y);
        semantic_nodes::AbandonZipline(semanticContext(), "zipline_dwell_watchdog", "never left the tower's dwell radius");
        runtime_state_.dwell = {};
        return TickStep::EndTick;
    }
    LogError << "Dwell watchdog tripped; the agent never left its dwell radius." << VAR(runtime_state_.dwell.dwell_ms)
             << VAR(runtime_state_.dwell.center_x) << VAR(runtime_state_.dwell.center_y) << VAR(position_->x) << VAR(position_->y)
             << VAR_AS(session_->current_node_idx(), session_->currentNodeIdx());
    return EndTickWith(failNavigation(
        "dwell_watchdog",
        "Agent stayed inside the dwell radius past the watchdog budget; terminating so the pipeline can retry.",
        std::hypot(position_->x - pinned_at.x, position_->y - pinned_at.y),
        0.0,
        runtime_state_.dwell.dwell_ms));
}

bool NavigationStateMachine::handleLocalizationLoss()
{
    const auto now = std::chrono::steady_clock::now();
    LocalizationLossState& loss = runtime_state_.localization_loss;
    if (loss.started_at == std::chrono::steady_clock::time_point {}) {
        loss.started_at = now;
        loss.last_fix = *position_;
        NoteFailure(&failure_trail_, now, "localization lost");
    }
    PauseWedge(&runtime_state_.offroute, now);
    // 丢定位期间截到黑屏就是落水了
    if (position_provider_->lastCaptureWasBlackScreen()) {
        loss.saw_black_screen = true;
    }
    motion_controller_->setForwardState(false);

    const auto loss_elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(now - loss.started_at);
    if (loss_elapsed >= std::chrono::milliseconds(kLocalizationLossTimeoutMs)) {
        if (sceneRecoveryAllowed() && runSceneRecovery()) {
            return resumeAfterGlobalReacquire(session_->currentZoneId(), loss_elapsed);
        }
        return failNavigation(
            "localization_lost_timeout",
            "Localization lost beyond timeout (re-acquire failed; likely shoved off-route into another zone); terminating navigation.",
            0.0,
            0.0,
            0);
    }

    const bool relocalize_cooling = last_global_relocalize_at_ != std::chrono::steady_clock::time_point {}
                                    && std::chrono::duration_cast<std::chrono::milliseconds>(now - last_global_relocalize_at_)
                                           < std::chrono::milliseconds(kRelocationRetryIntervalMs);
    if (!relocalize_cooling) {
        last_global_relocalize_at_ = now;
        const std::string prior_zone = session_->currentZoneId();
        const bool reacquired = position_provider_->capture(position_, /*force_global_search=*/true, /*expected_zone_id=*/std::string());
        NoteFailure(&failure_trail_, now, "global relocalize " + position_provider_->lastLocateMessage());
        if (reacquired) {
            return resumeAfterGlobalReacquire(prior_zone, loss_elapsed);
        }
    }

    const bool unstick_cooling = loss.last_unstick_at != std::chrono::steady_clock::time_point {}
                                 && std::chrono::duration_cast<std::chrono::milliseconds>(now - loss.last_unstick_at)
                                        < std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs);
    // 有 TRIGGER 在等命中时不盲跳, 按键会干扰它
    if (loss_elapsed >= std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs) && !unstick_cooling
        && !semantic_nodes::IsTriggerPending(*session_)) {
        loss.last_unstick_at = now;
        LogInfo << "Localization lost; blind unstick hop issued." << VAR(loss_elapsed.count());
        NoteFailure(&failure_trail_, now, "localization lost blind hop");
        motion_controller_->setAction(LocalDriverAction::JumpForward, true);
        SleepFor(kActionJumpSettleMs);
        motion_controller_->setForwardState(false);
        return true;
    }

    SleepFor(kLocatorRetryIntervalMs);
    return true;
}

bool NavigationStateMachine::sceneRecoveryAllowed()
{
    const LocalizationLossState& loss = runtime_state_.localization_loss;
    const bool attempted = runtime_state_.scene_recovery_attempted;
    const bool foreign_ui = position_provider_->lastCaptureMinimapAbsent() && !loss.saw_black_screen;
    const bool own_ui = semantic_nodes::IsTriggerPending(*session_)
                        || ElapsedMs(session_->lastInteractionAt(), loss.started_at) < kSceneRecoveryInteractionQuietMs;
    if (attempted || !foreign_ui || own_ui) {
        LogInfo << "Scene recovery declined." << VAR(attempted) << VAR(foreign_ui) << VAR(own_ui);
        return false;
    }
    if (dialog_on_screen(maa_context_, action_wrapper_->getCtrl())) {
        LogInfo << "Scene recovery declined: dialog on screen.";
        return false;
    }
    return true;
}

bool NavigationStateMachine::runSceneRecovery()
{
    runtime_state_.scene_recovery_attempted = true;
    return returnToWorld();
}

bool NavigationStateMachine::returnToWorld()
{
    MaaSize esc_used = 0;
    if (!MaaContextGetHitCount(maa_context_, kSceneRecoveryEscNode, &esc_used)) {
        LogWarn << "Scene recovery skipped: hit count unavailable.";
        return false;
    }
    const std::optional<std::vector<std::string>> handled =
        RunTaskForCompletedNodes(maa_context_, kSceneRecoveryEntryNode, scene_recovery_override(esc_used));
    const bool restored =
        position_provider_->waitForFix(position_, std::string(), kSceneRecoveryFixAttempts, kRelocationRetryIntervalMs, should_stop_);
    LogInfo << "Scene recovery finished." << VAR(restored) << VAR(handled) << VAR(esc_used);
    NoteFailure(&failure_trail_, std::chrono::steady_clock::now(), restored ? "scene recovery restored" : "scene recovery failed");
    return restored;
}

bool NavigationStateMachine::resumeAfterGlobalReacquire(const std::string& prior_zone, std::chrono::milliseconds loss_elapsed)
{
    LocalizationLossState& loss = runtime_state_.localization_loss;
    const bool zone_changed = !position_->zone_id.empty() && position_->zone_id != prior_zone;
    const bool near_last_fix = std::hypot(position_->x - loss.last_fix.x, position_->y - loss.last_fix.y) < kDwellWatchdogRadius;
    session_->excludeFromStallClocks(std::chrono::steady_clock::now() - loss.started_at);

    if (zone_changed && !loss.saw_black_screen && near_last_fix && NavmeshZonesShareGeometry(param_, prior_zone, position_->zone_id)) {
        LogInfo << "Zone flipped within the same geometry; keeping the corridor." << VAR(prior_zone) << VAR(position_->zone_id)
                << VAR(position_->x) << VAR(position_->y);
        session_->updateCurrentZone(position_->zone_id);
        loss = {};
        return true;
    }

    if (zone_changed) {
        // 不更新的话下次取位还按旧区域找, 会接着失败
        session_->updateCurrentZone(position_->zone_id);
    }
    const bool dropped_frames = !zone_changed && !loss.saw_black_screen
                                && loss_elapsed < std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs) && near_last_fix;
    if (!dropped_frames && ++runtime_state_.global_reacquire_streak >= kLocalizationThrashFailCount) {
        return failNavigation(
            "localization_thrash",
            "Re-acquired the route repeatedly without advancing a waypoint (wrong-tier fall thrashing "
            "recover<->re-lose); terminating so the pipeline can retry.",
            0.0,
            0.0,
            0);
    }
    LogInfo << "Localization recovered via global re-acquire; resuming navigation." << VAR(loss_elapsed.count()) << VAR(prior_zone)
            << VAR(position_->zone_id) << VAR(position_->x) << VAR(position_->y) << VAR(runtime_state_.global_reacquire_streak)
            << VAR(dropped_frames);
    NoteTimedFailure(&failure_trail_, std::chrono::steady_clock::now(), "localization reacquired globally", loss_elapsed.count());
    armRiverFallRecoveryIfBlackScreenLoss("global_reacquire");
    loss = {};
    ResetRouteTracking(&runtime_state_.route);
    runtime_state_.nav_run_dirty = true;
    session_->resetProgress();
    return true;
}

bool NavigationStateMachine::armRiverFallRecoveryIfBlackScreenLoss(const char* via)
{
    if (!runtime_state_.localization_loss.saw_black_screen) {
        return false;
    }
    ArmRiverFallRecovery(&runtime_state_.river_fall, *position_, NormalizeAngle(position_->angle));
    // 落水前的卡死恢复已经过时, 留着会抢在上岸流程前面
    runtime_state_.recovery = {};
    session_->resetHardProgress();
    LogInfo << "River-fall recovery armed (black-screen loss recovered)." << VAR(via) << VAR(position_->x) << VAR(position_->y)
            << VAR(runtime_state_.river_fall.water_heading);
    return true;
}

TickStep NavigationStateMachine::runRiverFallRecovery(const NavigateTick& tick)
{
    if (!runtime_state_.river_fall.pending) {
        return TickStep::Next;
    }
    const RouteMeasurement& route = tick.route;
    PauseWedge(&runtime_state_.offroute, tick.now);
    if (session_->hardStalledMs(tick.now) > kRiverFallRecoveryTimeoutMs) {
        return EndTickWith(failNavigation(
            "river_fall_recovery_timeout",
            "River-fall recovery made no net progress past the timeout; terminating navigation.",
            route.progress_distance,
            NormalizeAngle(route.route_heading - tick.fix.current_heading),
            tick.stalled_ms));
    }
    if (AdvanceRiverFallRecovery(&runtime_state_.river_fall, action_wrapper_, motion_controller_, *position_, tick.fix.current_heading)
        == RiverFallStep::Cleared) {
        return EndTickWith(resumeAfterEscape("river_fall_recovered"));
    }
    return TickStep::EndTick;
}

} // namespace mapnavigator
