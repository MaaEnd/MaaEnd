#include "localization.h"

#include <chrono>
#include <cmath>
#include <optional>
#include <string>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
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
    if (context == nullptr || controller == nullptr) {
        return true;
    }
    if (!MaaControllerCachedImage(controller, image.Get())) {
        return true;
    }
    if (MaaImageBufferIsEmpty(image.Get())) {
        return true;
    }
    for (const char* node : { kSceneRecoveryStoryNode, kSceneRecoveryDialogNode }) {
        NodeSighting sighting {};
        const bool recognized = RunRecognitionNode(context, node, "{}", image.Get(), &sighting);
        if (!recognized || sighting.hit) {
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

void update_dwell_watchdog(Navigator* nav, bool captured)
{
    DwellWatchdogState& dwell = nav->runtime_state.dwell;
    const bool usable = captured && nav->position->valid && !nav->position_provider->last_capture_was_black_screen;
    if (!usable || nav->session->phase != NaviPhase::Navigate) {
        dwell.last_usable = {};
        return;
    }

    const auto now = std::chrono::steady_clock::now();
    const bool zone_changed = ZonesDifferInGeometry(*nav->param, dwell.center_zone, nav->position->zone_id);
    if (!dwell.latched || zone_changed
        || std::hypot(nav->position->x - dwell.center_x, nav->position->y - dwell.center_y) > kDwellWatchdogRadius) {
        dwell.latched = true;
        dwell.center_x = nav->position->x;
        dwell.center_y = nav->position->y;
        dwell.center_zone = nav->position->zone_id;
        dwell.dwell_ms = 0;
    }
    else if (dwell.last_usable.time_since_epoch().count() > 0) {
        dwell.dwell_ms += std::chrono::duration_cast<std::chrono::milliseconds>(now - dwell.last_usable).count();
    }
    dwell.last_usable = now;
}

void record_capture(Navigator* nav, bool captured, uint64_t heading_epoch, const std::optional<double>& camera_heading_prior)
{
    if (captured) {
        nav->prior_observation_epoch = heading_epoch;
        nav->consecutive_prior_uses = camera_heading_prior ? nav->consecutive_prior_uses + 1 : 0;
        LogDebug << "MapNavigator camera heading prior" << VAR(camera_heading_prior.value_or(-1.0)) << VAR(nav->consecutive_prior_uses);
    }
    else {
        nav->consecutive_prior_uses = 0;
    }
    SampleFailureTrail(
        &nav->failure_trail,
        std::chrono::steady_clock::now(),
        *nav->position,
        captured,
        nav->position_provider->last_locate_message,
        nav->session->current_node_idx,
        navi_phase_name(nav->session->phase));
    update_dwell_watchdog(nav, captured);
}

bool arm_river_fall_recovery_if_black_screen_loss(Navigator* nav, const char* via)
{
    if (!nav->runtime_state.localization_loss.saw_black_screen) {
        return false;
    }
    ArmRiverFallRecovery(&nav->runtime_state.river_fall, *nav->position, NormalizeAngle(nav->position->angle));
    // 落水前的卡死恢复已经过时, 留着会抢在上岸流程前面
    ClearDynamicRecovery(&nav->runtime_state.recovery);
    ResetHardProgress(nav->session);
    LogInfo << "River-fall recovery armed (black-screen loss recovered)." << VAR(via) << VAR(nav->position->x) << VAR(nav->position->y)
            << VAR(nav->runtime_state.river_fall.water_heading);
    return true;
}

// 丢定位由落水造成时返回 true, 此时落水自救已挂上
bool end_localization_loss(Navigator* nav)
{
    LocalizationLossState& loss = nav->runtime_state.localization_loss;
    if (loss.started_at == std::chrono::steady_clock::time_point {}) {
        loss = {};
        return false;
    }
    ExcludeFromStallClocks(nav->session, std::chrono::steady_clock::now() - loss.started_at);
    const auto loss_ms = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - loss.started_at).count();
    const bool armed = arm_river_fall_recovery_if_black_screen_loss(nav, "normal_reacquire");
    LogInfo << "Localization recovered via normal tracking." << VAR(loss_ms) << VAR(loss.saw_black_screen) << VAR(armed)
            << VAR(nav->position->x) << VAR(nav->position->y);
    NoteTimedFailure(&nav->failure_trail, std::chrono::steady_clock::now(), "localization recovered", loss_ms);
    loss = {};
    return armed;
}

bool scene_recovery_allowed(Navigator* nav)
{
    const LocalizationLossState& loss = nav->runtime_state.localization_loss;
    const bool attempted = nav->runtime_state.scene_recovery_attempted;
    const bool foreign_ui = nav->position_provider->last_capture_minimap_absent && !loss.saw_black_screen;
    const bool own_ui = semantic_nodes::IsTriggerPending(*nav->session)
                        || ElapsedMs(nav->session->last_interaction_at, loss.started_at) < kSceneRecoveryInteractionQuietMs;
    if (attempted || !foreign_ui || own_ui) {
        LogInfo << "Scene recovery declined." << VAR(attempted) << VAR(foreign_ui) << VAR(own_ui);
        return false;
    }
    if (dialog_on_screen(nav->maa_context, nav->input_backend->ctrl)) {
        LogInfo << "Scene recovery declined: dialog on screen.";
        return false;
    }
    return true;
}

// 场景管理跑完后取回有效定位才返回 true
bool run_scene_recovery(Navigator* nav)
{
    nav->runtime_state.scene_recovery_attempted = true;
    return ReturnToWorld(nav);
}

bool resume_after_global_reacquire(Navigator* nav, const std::string& prior_zone, std::chrono::milliseconds loss_elapsed)
{
    LocalizationLossState& loss = nav->runtime_state.localization_loss;
    const bool zone_changed = !nav->position->zone_id.empty() && nav->position->zone_id != prior_zone;
    const bool near_last_fix = std::hypot(nav->position->x - loss.last_fix.x, nav->position->y - loss.last_fix.y) < kDwellWatchdogRadius;
    ExcludeFromStallClocks(nav->session, std::chrono::steady_clock::now() - loss.started_at);

    if (zone_changed && !loss.saw_black_screen && near_last_fix
        && NavmeshZonesShareGeometry(*nav->param, prior_zone, nav->position->zone_id)) {
        LogInfo << "Zone flipped within the same geometry; keeping the corridor." << VAR(prior_zone) << VAR(nav->position->zone_id)
                << VAR(nav->position->x) << VAR(nav->position->y);
        UpdateCurrentZone(nav->session, nav->position->zone_id);
        loss = {};
        return true;
    }

    if (zone_changed) {
        // 不更新的话下次取位还按旧区域找, 会接着失败
        UpdateCurrentZone(nav->session, nav->position->zone_id);
    }
    const bool dropped_frames = !zone_changed && !loss.saw_black_screen
                                && loss_elapsed < std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs) && near_last_fix;
    if (!dropped_frames) {
        CountGlobalReacquire(&nav->runtime_state);
    }
    if (!dropped_frames && nav->runtime_state.global_reacquire_streak >= kLocalizationThrashFailCount) {
        return FailNavigation(
            nav,
            "localization_thrash",
            "Re-acquired the route repeatedly without advancing a waypoint (wrong-tier fall thrashing "
            "recover<->re-lose); terminating so the pipeline can retry.",
            0.0,
            0.0,
            0);
    }
    LogInfo << "Localization recovered via global re-acquire; resuming navigation." << VAR(loss_elapsed.count()) << VAR(prior_zone)
            << VAR(nav->position->zone_id) << VAR(nav->position->x) << VAR(nav->position->y)
            << VAR(nav->runtime_state.global_reacquire_streak) << VAR(dropped_frames);
    NoteTimedFailure(&nav->failure_trail, std::chrono::steady_clock::now(), "localization reacquired globally", loss_elapsed.count());
    arm_river_fall_recovery_if_black_screen_loss(nav, "global_reacquire");
    loss = {};
    ResetRouteTracking(&nav->runtime_state.route);
    MarkNavRunDirty(&nav->runtime_state);
    ResetProgress(nav->session);
    return true;
}

} // namespace

bool CaptureTrackedFix(Navigator* nav)
{
    const uint64_t heading_epoch = nav->input_backend->heading_epoch;
    const bool prior_usable = nav->param->heading_source == HeadingSource::Camera && nav->position->valid && nav->prior_observation_epoch
                              && *nav->prior_observation_epoch == heading_epoch && nav->session->phase == NaviPhase::Navigate;
    const std::optional<double> camera_heading_prior = prior_usable ? std::optional<double>(nav->position->angle) : std::nullopt;
    const bool captured = CaptureTracked(nav->position_provider, nav->position, nav->session->current_zone_id, {}, camera_heading_prior);
    record_capture(nav, captured, heading_epoch, camera_heading_prior);
    return captured;
}

bool CaptureGlobalFix(Navigator* nav)
{
    const uint64_t heading_epoch = nav->input_backend->heading_epoch;
    const std::optional<double> camera_heading_prior = std::nullopt;
    const bool captured = CaptureGlobal(nav->position_provider, nav->position, nav->session->current_zone_id, {}, camera_heading_prior);
    record_capture(nav, captured, heading_epoch, camera_heading_prior);
    return captured;
}

TickStep CaptureNavigateFix(Navigator* nav, NavigateTick* tick)
{
    const auto capture_started_at = std::chrono::steady_clock::now();
    const bool position_captured = CaptureTrackedFix(nav);
    tick->capture_ms = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - capture_started_at).count();
    // 须在定位丢失分支之前: 待命中的画面可能盖住小地图
    const semantic_nodes::Result trigger_result = semantic_nodes::ProbeTriggerWhileNavigating(nav);
    if (trigger_result.request_failure) {
        return EndTickWith(FailNavigation(nav, trigger_result.failure_reason, trigger_result.failure_log_message, 0.0, 0.0, 0));
    }
    if (trigger_result.consumed) {
        return TickStep::EndTick;
    }
    if (!position_captured) {
        return EndTickWith(HandleLocalizationLoss(nav));
    }
    if (end_localization_loss(nav)) {
        return TickStep::EndTick;
    }
    return TickStep::Next;
}

TickStep CheckDwellWatchdog(Navigator* nav)
{
    if (nav->runtime_state.dwell.dwell_ms < kDwellWatchdogFailMs || !HasCurrentWaypoint(*nav->session)) {
        return TickStep::Next;
    }
    const Waypoint& pinned_at = CurrentWaypoint(*nav->session);
    if (pinned_at.action == ActionType::ZIPLINE) {
        LogWarn << "Dwell watchdog tripped at a zipline tower; dropping the chain and walking." << VAR(nav->runtime_state.dwell.dwell_ms)
                << VAR(nav->position->x) << VAR(nav->position->y);
        semantic_nodes::AbandonZipline(nav, "zipline_dwell_watchdog", "never left the tower's dwell radius");
        nav->runtime_state.dwell = {};
        return TickStep::EndTick;
    }
    LogError << "Dwell watchdog tripped; the agent never left its dwell radius." << VAR(nav->runtime_state.dwell.dwell_ms)
             << VAR(nav->runtime_state.dwell.center_x) << VAR(nav->runtime_state.dwell.center_y) << VAR(nav->position->x)
             << VAR(nav->position->y) << VAR(nav->session->current_node_idx);
    return EndTickWith(FailNavigation(
        nav,
        "dwell_watchdog",
        "Agent stayed inside the dwell radius past the watchdog budget; terminating so the pipeline can retry.",
        std::hypot(nav->position->x - pinned_at.x, nav->position->y - pinned_at.y),
        0.0,
        nav->runtime_state.dwell.dwell_ms));
}

bool HandleLocalizationLoss(Navigator* nav)
{
    const auto now = std::chrono::steady_clock::now();
    LocalizationLossState& loss = nav->runtime_state.localization_loss;
    if (loss.started_at == std::chrono::steady_clock::time_point {}) {
        loss.started_at = now;
        loss.last_fix = *nav->position;
        NoteFailure(&nav->failure_trail, now, "localization lost");
    }
    PauseWedge(&nav->runtime_state.offroute, now);
    // 丢定位期间截到黑屏就是落水了
    if (nav->position_provider->last_capture_was_black_screen) {
        loss.saw_black_screen = true;
    }
    HoldPosition(nav->motion_controller);

    const auto loss_elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(now - loss.started_at);
    if (loss_elapsed >= std::chrono::milliseconds(kLocalizationLossTimeoutMs)) {
        bool recovered = false;
        if (scene_recovery_allowed(nav)) {
            recovered = run_scene_recovery(nav);
        }
        if (recovered) {
            return resume_after_global_reacquire(nav, nav->session->current_zone_id, loss_elapsed);
        }
        return FailNavigation(
            nav,
            "localization_lost_timeout",
            "Localization lost beyond timeout (re-acquire failed; likely shoved off-route into another zone); terminating navigation.",
            0.0,
            0.0,
            0);
    }

    const bool relocalize_cooling = nav->last_global_relocalize_at != std::chrono::steady_clock::time_point {}
                                    && std::chrono::duration_cast<std::chrono::milliseconds>(now - nav->last_global_relocalize_at)
                                           < std::chrono::milliseconds(kRelocationRetryIntervalMs);
    if (!relocalize_cooling) {
        nav->last_global_relocalize_at = now;
        const std::string prior_zone = nav->session->current_zone_id;
        const bool reacquired = CaptureGlobal(nav->position_provider, nav->position, /*expected_zone_id=*/std::string());
        NoteFailure(&nav->failure_trail, now, "global relocalize " + nav->position_provider->last_locate_message);
        if (reacquired) {
            return resume_after_global_reacquire(nav, prior_zone, loss_elapsed);
        }
    }

    const bool unstick_cooling = loss.last_unstick_at != std::chrono::steady_clock::time_point {}
                                 && std::chrono::duration_cast<std::chrono::milliseconds>(now - loss.last_unstick_at)
                                        < std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs);
    // 有 TRIGGER 在等命中时不盲跳, 按键会干扰它
    if (loss_elapsed >= std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs) && !unstick_cooling
        && !semantic_nodes::IsTriggerPending(*nav->session)) {
        loss.last_unstick_at = now;
        LogInfo << "Localization lost; blind unstick hop issued." << VAR(loss_elapsed.count());
        NoteFailure(&nav->failure_trail, now, "localization lost blind hop");
        ArmSteeringQuietPeriod(nav->motion_controller);
        ApplyLocalAction(nav->motion_controller, LocalDriverAction::JumpForward);
        SleepFor(kActionJumpSettleMs);
        HoldPosition(nav->motion_controller);
        return true;
    }

    SleepFor(kLocatorRetryIntervalMs);
    return true;
}

bool ReturnToWorld(Navigator* nav)
{
    MaaSize esc_used = 0;
    if (!MaaContextGetHitCount(nav->maa_context, kSceneRecoveryEscNode, &esc_used)) {
        LogWarn << "Scene recovery skipped: hit count unavailable.";
        return false;
    }
    const std::optional<std::vector<std::string>> handled =
        RunTaskForCompletedNodes(nav->maa_context, kSceneRecoveryEntryNode, scene_recovery_override(esc_used));
    const bool restored = WaitForFix(
        nav->position_provider,
        nav->position,
        std::string(),
        kSceneRecoveryFixAttempts,
        kRelocationRetryIntervalMs,
        nav->should_stop);
    LogInfo << "Scene recovery finished." << VAR(restored) << VAR(handled) << VAR(esc_used);
    NoteFailure(&nav->failure_trail, std::chrono::steady_clock::now(), restored ? "scene recovery restored" : "scene recovery failed");
    return restored;
}

TickStep RunRiverFallRecovery(Navigator* nav, const NavigateTick& tick)
{
    if (!nav->runtime_state.river_fall.pending) {
        return TickStep::Next;
    }
    const RouteMeasurement& route = tick.route;
    PauseWedge(&nav->runtime_state.offroute, tick.now);
    if (HardStalledMs(*nav->session, tick.now) > kRiverFallRecoveryTimeoutMs) {
        return EndTickWith(FailNavigation(
            nav,
            "river_fall_recovery_timeout",
            "River-fall recovery made no net progress past the timeout; terminating navigation.",
            route.progress_distance,
            NormalizeAngle(route.route_heading - tick.fix.current_heading),
            tick.stalled_ms));
    }
    if (AdvanceRiverFallRecovery(
            &nav->runtime_state.river_fall,
            nav->input_backend,
            nav->motion_controller,
            *nav->position,
            tick.fix.current_heading)
        == RiverFallStep::Cleared) {
        return EndTickWith(ResumeAfterEscape(nav, "river_fall_recovered"));
    }
    return TickStep::EndTick;
}

} // namespace mapnavigator
