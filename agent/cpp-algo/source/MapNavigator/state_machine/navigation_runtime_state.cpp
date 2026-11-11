#include "navigation_runtime_state.h"

#include <algorithm>
#include <chrono>
#include <cstddef>
#include <cstdint>
#include <limits>
#include <optional>

#include "../navi_config.h"
#include "../navi_time.h"
#include "../zipline/zipline_ride_machine.h"
#include "../zipline/zipline_types.h"

namespace mapnavigator
{

void ResetRouteTracking(RouteTrackerState* route)
{
    route->passed_waypoint_idx = std::numeric_limits<size_t>::max();
    route->passed_waypoint_latched = false;
    route->best_projection_on_segment = 0.0;
    route->last_segment_from_idx = std::numeric_limits<size_t>::max();
    route->last_segment_to_idx = std::numeric_limits<size_t>::max();
}

void RestartRouteTracking(RouteTrackerState* route)
{
    *route = {};
}

void ConfirmStartupAt(RouteTrackerState* route, const NaviPosition& position)
{
    route->startup_anchor_pos = position;
    route->startup_anchor_initialized = true;
    route->startup_motion_confirmed = true;
}

void ResetStallCounters(FlowState* flow)
{
    flow->motionless_hold_ticks = 0;
    flow->futile_forward_reasserts = 0;
}

void BeginTransferWait(TransferWaitState* transfer, const NaviPosition& anchor, TimePoint started)
{
    *transfer = { .started = started, .anchor = anchor };
}

void ClearTransferWait(TransferWaitState* transfer)
{
    *transfer = {};
}

bool IsPortalTransitActive(const PortalTransitState& portal)
{
    return portal.phase != PortalTransitPhase::Idle;
}

void BeginPortalTransit(PortalTransitState* portal, TimePoint started)
{
    *portal = { .phase = PortalTransitPhase::AwaitZone, .started = started };
}

void EndPortalTransit(PortalTransitState* portal)
{
    *portal = {};
}

void BeginRopeRide(RopeRideState* rope, int64_t ride_ms, TimePoint started)
{
    *rope = { .started = started, .ride_ms = ride_ms };
}

void RestartDynamicRecovery(DynamicRecoveryState* recovery, const NaviPosition& position, size_t anchor, TimePoint now)
{
    *recovery = {};
    recovery->active = true;
    recovery->anchor_pos = position;
    recovery->started_at = now;
    recovery->anchor_index = anchor;
}

void ClearDynamicRecovery(DynamicRecoveryState* recovery)
{
    *recovery = {};
}

void RetargetRecoveryEscalation(RecoveryEscalationState* escalation, size_t anchor)
{
    *escalation = {};
    escalation->anchor_index = anchor;
}

void ResetRecoveryEscalation(RecoveryEscalationState* escalation)
{
    *escalation = {};
}

void ArmRiverFallRecovery(RiverFallRecoveryState* river_fall, const NaviPosition& position, double heading)
{
    *river_fall = {};
    river_fall->pending = true;
    river_fall->anchor_pos = position;
    river_fall->water_heading = heading;
}

void BeginLateralBypass(LateralBypassState* bypass, const NaviPosition& position)
{
    bypass->active = true;
    bypass->origin = position;
    bypass->count = 0;
}

void PauseWedge(OffRouteWedgeState* wedge, TimePoint now)
{
    if (wedge->active && wedge->blind_since == std::chrono::steady_clock::time_point {}) {
        wedge->blind_since = now;
    }
}

int64_t ResumeWedge(OffRouteWedgeState* wedge, TimePoint now)
{
    if (wedge->blind_since == std::chrono::steady_clock::time_point {}) {
        return 0;
    }
    const auto blind = now - wedge->blind_since;
    wedge->since += blind;
    if (wedge->last_replan_at != std::chrono::steady_clock::time_point {}) {
        wedge->last_replan_at += blind;
    }
    wedge->blind_since = {};
    return std::chrono::duration_cast<std::chrono::milliseconds>(blind).count();
}

void DropWedgeBaseline(OffRouteWedgeState* wedge)
{
    wedge->best_distance = std::numeric_limits<double>::max();
}

bool HasWedgeBaseline(const OffRouteWedgeState& wedge)
{
    return wedge.best_distance != std::numeric_limits<double>::max();
}

WedgeAction StepWedge(OffRouteWedgeState* wedge, double progress, TimePoint now)
{
    const double progress_epsilon = std::max(kNoProgressDistanceEpsilon, kMeasurementDefaultPositionQuantum);
    const bool retaking_baseline = wedge->active && !HasWedgeBaseline(*wedge);
    if (!wedge->active || retaking_baseline || progress + progress_epsilon < wedge->best_distance) {
        wedge->active = true;
        wedge->best_distance = progress;
        if (!retaking_baseline) {
            wedge->since = now;
        }
    }
    const int64_t wedge_ms = ElapsedMs(wedge->since, now);
    if (wedge_ms >= kOffRouteWedgeFailMs) {
        return WedgeAction::Fail;
    }
    const bool replan_cooling =
        wedge->last_replan_at.time_since_epoch().count() > 0 && ElapsedMs(wedge->last_replan_at, now) < kOffRouteWedgeReplanCooldownMs;
    if (wedge_ms >= kOffRouteWedgeReplanMs && !replan_cooling) {
        wedge->last_replan_at = now;
        return WedgeAction::Replan;
    }
    return WedgeAction::None;
}

void ForgetDwellSample(DwellWatchdogState* dwell)
{
    dwell->last_usable = {};
}

void RetargetZiplineApproach(ZiplineApproachState* approach, size_t anchor)
{
    approach->anchor_index = anchor;
    approach->replans = 0;
    approach->press_missed = false;
    approach->spot_index = 0;
}

void FollowMountTower(ZiplineApproachState* approach, const ZiplineNodeRef& mount)
{
    if (!SameTower(approach->spot_tower, mount)) {
        approach->spot_tower = mount;
        approach->spot_index = 0;
        approach->press_missed = false;
    }
}

void CountZiplineApproachReplan(ZiplineApproachState* approach)
{
    ++approach->replans;
}

bool PromptMissedAt(const InteractApproachState& approach, const std::optional<size_t>& node)
{
    return node.has_value() && approach.prompt_missed_node == node;
}

void BeginZiplineRecovery(ZiplineRecoveryState* recovery, TimePoint now)
{
    recovery->started_at = now;
    recovery->stable_pos = {};
    recovery->stable_hits = 0;
    recovery->pending = true;
}

void ArmCameraAlign(NavigationRuntimeState* runtime)
{
    runtime->camera_align_pending = true;
}

void ClearCameraAlignRequest(NavigationRuntimeState* runtime)
{
    runtime->camera_align_pending = false;
}

void RequestDynamicReplan(NavigationRuntimeState* runtime)
{
    runtime->dynamic_replan_requested = true;
}

void ClearDynamicReplanRequest(NavigationRuntimeState* runtime)
{
    runtime->dynamic_replan_requested = false;
}

void MarkNavRunDirty(NavigationRuntimeState* runtime)
{
    runtime->nav_run_dirty = true;
}

void ClearNavRunDirty(NavigationRuntimeState* runtime)
{
    runtime->nav_run_dirty = false;
}

void CountGlobalReacquire(NavigationRuntimeState* runtime)
{
    ++runtime->global_reacquire_streak;
}

void ResetGlobalReacquireStreak(NavigationRuntimeState* runtime)
{
    runtime->global_reacquire_streak = 0;
}

void ResetNavigationAssistState(NavigationRuntimeState* runtime)
{
    ResetRouteTracking(&runtime->route);
    ClearDynamicRecovery(&runtime->recovery);
    runtime->recovery_escalation = {};
    runtime->steering_rate = {};
    runtime->offroute = {};
    runtime->zipline_recovery = {};
    ClearDynamicReplanRequest(runtime);
    MarkNavRunDirty(runtime);
}

void BeginNavigation(NavigationRuntimeState* runtime, TimePoint now)
{
    RestartRouteTracking(&runtime->route);
    runtime->semantic = {};
    ClearDynamicRecovery(&runtime->recovery);
    runtime->recovery_escalation = {};
    runtime->localization_loss = {};
    runtime->scene_recovery_attempted = false;
    runtime->river_fall = {};
    runtime->bypass = {};
    runtime->steering_rate = {};
    runtime->offroute = {};
    runtime->dwell = {};
    runtime->zipline_approach = {};
    runtime->interact_approach.prompt_missed_node.reset();
    runtime->prompt_repeat = {};
    runtime->zipline_recovery = {};
    runtime->find = {};
    runtime->trigger = {};
    ResetZiplineNavigation(&runtime->zipline_ride);
    runtime->virtual_no_go.clear();
    runtime->progress_identity = {};
    ResetGlobalReacquireStreak(runtime);
    ClearDynamicReplanRequest(runtime);
    MarkNavRunDirty(runtime);
    ArmCameraAlign(runtime);
    runtime->flow.navigate_started_at = now;
    runtime->flow.last_auto_sprint_time = {};
    runtime->flow.last_tick_started_at = {};
    runtime->flow.steer_periods_ms.clear();
    runtime->flow.last_steer_tick_at = {};
    runtime->flow.braked_corner.reset();
    runtime->flow.corner_walk = false;
}

void ResetOnWaypointAdvance(NavigationRuntimeState* runtime)
{
    ResetRouteTracking(&runtime->route);
    ClearDynamicRecovery(&runtime->recovery);
    runtime->recovery_escalation = {};
    runtime->river_fall = {};
    runtime->bypass = {};
    runtime->offroute = {};
    runtime->zipline_recovery = {};
    runtime->find = {};
    ResetGlobalReacquireStreak(runtime);
    ClearDynamicReplanRequest(runtime);
    MarkNavRunDirty(runtime);
    runtime->flow.last_auto_sprint_time = {};
}

bool IsZiplineMounted(const NavigationRuntimeState& runtime)
{
    return ZiplineOnTower(runtime.zipline_ride);
}

} // namespace mapnavigator
