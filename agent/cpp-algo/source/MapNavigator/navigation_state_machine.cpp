#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <string>
#include <thread>
#include <utility>

#include <MaaUtils/Logger.h>

#include "action_executor.h"
#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "navigation_state_machine.h"
#include "navmesh_path_expander.h"
#include "position_provider.h"
#include "route_tracker.h"
#include "semantic_nodes.h"
#include "steering_controller.h"

namespace mapnavigator
{

namespace
{

struct BootstrapWaypointCandidate
{
    size_t index = std::numeric_limits<size_t>::max();
    double distance = std::numeric_limits<double>::infinity();
};

struct BootstrapContinueCandidate
{
    size_t continue_index = std::numeric_limits<size_t>::max();
    double route_distance = std::numeric_limits<double>::infinity();
    const char* reason = "";
};

using DynamicAnchor = std::pair<size_t, Waypoint>;

bool IsZoneCompatible(const Waypoint& waypoint, const std::string& current_zone_id)
{
    if (!waypoint.HasPosition()) {
        return false;
    }
    return current_zone_id.empty() || waypoint.zone_id.empty() || waypoint.zone_id == current_zone_id;
}

bool IsRequiredSemanticAnchor(const Waypoint& waypoint)
{
    if (!waypoint.HasPosition()) {
        return waypoint.IsHeadingOnly() || waypoint.IsZoneDeclaration();
    }
    return waypoint.action != ActionType::RUN || waypoint.RequiresStrictArrival();
}

double ArrivalBandForStartupBypass(const Waypoint& waypoint)
{
    double arrival_band = waypoint.RequiresStrictArrival() ? waypoint.GetLookahead() + kMeasurementDefaultPositionQuantum
                                                           : waypoint.GetLookahead() + kWaypointArrivalSlack
                                                                 + kMeasurementDefaultPositionQuantum;
    if (waypoint.action == ActionType::PORTAL) {
        arrival_band = std::max(arrival_band, kPortalCommitDistance);
    }
    return arrival_band;
}

std::optional<DynamicAnchor> ResolveCurrentAnchorFrom(NavigationSession* session, const NaviPosition& position, size_t start_index)
{
    std::optional<DynamicAnchor> fallback;
    const size_t path_size = session->current_path().size();
    for (size_t index = std::min(start_index, path_size); index < path_size; ++index) {
        const Waypoint& waypoint = session->CurrentPathAt(index);
        const std::optional<size_t> canonical_index = session->CanonicalIndexAtCurrentPath(index);
        if (!canonical_index) {
            continue;
        }
        if (waypoint.IsZoneDeclaration()) {
            if (!waypoint.zone_id.empty() && !position.zone_id.empty() && waypoint.zone_id != position.zone_id) {
                return fallback;
            }
            continue;
        }
        if (!waypoint.HasPosition()) {
            if (IsRequiredSemanticAnchor(waypoint)) {
                return fallback;
            }
            continue;
        }
        if (!IsZoneCompatible(waypoint, position.zone_id)) {
            continue;
        }

        fallback = { *canonical_index, waypoint };
        if (IsRequiredSemanticAnchor(waypoint)) {
            return fallback;
        }
    }
    return fallback;
}

std::optional<DynamicAnchor> ResolveCurrentAnchor(NavigationSession* session, const NaviPosition& position)
{
    return ResolveCurrentAnchorFrom(session, position, session->current_node_idx());
}

std::optional<BootstrapContinueCandidate> FindProjectedContinueCandidate(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    std::optional<BootstrapContinueCandidate> best_candidate;
    for (size_t index = 0; index + 1 < path.size(); ++index) {
        const Waypoint& from = path[index];
        const Waypoint& to = path[index + 1];
        if (!IsZoneCompatible(from, position.zone_id) || !IsZoneCompatible(to, position.zone_id)) {
            continue;
        }
        if (!from.zone_id.empty() && !to.zone_id.empty() && from.zone_id != to.zone_id) {
            continue;
        }

        const double segment_x = to.x - from.x;
        const double segment_y = to.y - from.y;
        const double segment_len_sq = segment_x * segment_x + segment_y * segment_y;
        if (segment_len_sq <= std::numeric_limits<double>::epsilon()) {
            continue;
        }

        const double offset_x = position.x - from.x;
        const double offset_y = position.y - from.y;
        const double projection = (offset_x * segment_x + offset_y * segment_y) / segment_len_sq;
        if (projection < 0.0 || projection > 1.0) {
            continue;
        }

        const double projected_x = from.x + projection * segment_x;
        const double projected_y = from.y + projection * segment_y;
        const double route_distance = std::hypot(position.x - projected_x, position.y - projected_y);
        if (route_distance > kBootstrapOwnershipProjectionCorridor) {
            continue;
        }

        size_t continue_index = index + 1;
        const double distance_to_from = std::hypot(position.x - from.x, position.y - from.y);
        const double distance_to_to = std::hypot(position.x - to.x, position.y - to.y);
        if (projection <= kBootstrapOwnershipProjectionFrontThreshold) {
            continue_index = index;
        }
        else if (
            projection <= kBootstrapOwnershipProjectionMiddleThreshold
            && distance_to_from + kBootstrapOwnershipContinueBiasDistance < distance_to_to) {
            continue_index = index;
        }

        if (!best_candidate.has_value() || route_distance < best_candidate->route_distance) {
            best_candidate = BootstrapContinueCandidate {
                .continue_index = continue_index,
                .route_distance = route_distance,
                .reason = "projected_segment",
            };
        }
    }
    return best_candidate;
}

std::optional<BootstrapWaypointCandidate> FindNearestReachableWaypoint(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    std::optional<BootstrapWaypointCandidate> best_candidate;
    for (size_t index = 0; index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        if (!IsZoneCompatible(waypoint, position.zone_id)) {
            continue;
        }

        const double distance = std::hypot(position.x - waypoint.x, position.y - waypoint.y);
        if (distance > kBootstrapOwnershipMaxDistance) {
            continue;
        }

        if (!best_candidate.has_value() || distance < best_candidate->distance) {
            best_candidate = BootstrapWaypointCandidate { .index = index, .distance = distance };
        }
    }
    return best_candidate;
}

std::optional<BootstrapContinueCandidate> ResolveBootstrapContinueCandidate(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    const std::optional<BootstrapContinueCandidate> projected = FindProjectedContinueCandidate(path, position);
    if (projected.has_value()) {
        return projected;
    }

    const std::optional<BootstrapWaypointCandidate> nearest_waypoint = FindNearestReachableWaypoint(path, position);
    if (!nearest_waypoint.has_value()) {
        return std::nullopt;
    }

    return BootstrapContinueCandidate {
        .continue_index = nearest_waypoint->index,
        .route_distance = nearest_waypoint->distance,
        .reason = "nearest_waypoint",
    };
}

std::optional<DynamicAnchor> ResolveBootstrapNavmeshAnchor(
    const NaviParam& param,
    NavigationSession* session,
    const NaviPosition& position,
    size_t start_index)
{
    const size_t path_size = session->current_path().size();
    std::optional<DynamicAnchor> best_anchor;
    double best_cost = std::numeric_limits<double>::infinity();
    int planned_count = 0;

    for (size_t index = std::min(start_index, path_size); index < path_size; ++index) {
        const Waypoint& waypoint = session->CurrentPathAt(index);
        const std::optional<size_t> canonical_index = session->CanonicalIndexAtCurrentPath(index);
        if (!canonical_index) {
            continue;
        }
        if (waypoint.IsZoneDeclaration()) {
            if (!waypoint.zone_id.empty() && !position.zone_id.empty() && waypoint.zone_id != position.zone_id) {
                break;
            }
            continue;
        }
        if (!waypoint.HasPosition()) {
            if (IsRequiredSemanticAnchor(waypoint)) {
                break;
            }
            continue;
        }
        if (!IsZoneCompatible(waypoint, position.zone_id)) {
            continue;
        }

        const navmesh::WorldPoint start { .x = position.x, .y = position.y };
        const navmesh::WorldPoint goal { .x = waypoint.x, .y = waypoint.y };
        const auto route = PlanNavmeshRoute(param, position.zone_id, start, goal);
        if (route) {
            ++planned_count;
            if (route->cost < best_cost) {
                best_cost = route->cost;
                best_anchor = { *canonical_index, waypoint };
            }
        }
        if (IsRequiredSemanticAnchor(waypoint)) {
            break;
        }
    }

    if (best_anchor) {
        LogInfo << "Bootstrap navmesh anchor selected." << VAR(best_anchor->first) << VAR(best_cost) << VAR(planned_count)
                << VAR(start_index);
    }
    return best_anchor;
}

std::optional<DynamicAnchor> ResolveBootstrapAnchor(
    const NaviParam& param,
    NavigationSession* session,
    const NaviPosition& position)
{
    size_t start_index = 0;
    const std::optional<BootstrapContinueCandidate> continue_candidate =
        ResolveBootstrapContinueCandidate(session->original_path(), position);
    if (continue_candidate.has_value()) {
        start_index = continue_candidate->continue_index;
        LogInfo << "Bootstrap dynamic anchor scan adjusted." << VAR(continue_candidate->reason) << VAR(start_index)
                << VAR(continue_candidate->route_distance);
    }
    if (std::optional<DynamicAnchor> navmesh_anchor = ResolveBootstrapNavmeshAnchor(param, session, position, start_index)) {
        return navmesh_anchor;
    }
    return ResolveCurrentAnchorFrom(session, position, start_index);
}

semantic_nodes::Context BuildSemanticContext(
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    MotionController* motion_controller,
    IActionExecutor* action_executor,
    NaviPosition* position,
    NavigationRuntimeState* runtime_state,
    MaaContext* maa_context)
{
    semantic_nodes::Context ctx;
    ctx.action_wrapper = action_wrapper;
    ctx.position_provider = position_provider;
    ctx.session = session;
    ctx.motion_controller = motion_controller;
    ctx.action_executor = action_executor;
    ctx.position = position;
    ctx.runtime_state = runtime_state;
    ctx.maa_context = maa_context;
    return ctx;
}

} // namespace

NavigationStateMachine::NavigationStateMachine(
    const NaviParam& param,
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    MotionController* motion_controller,
    IActionExecutor* action_executor,
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
{
    LogInfo << "Navigation route runner selected. backend=orchestrated";
}

bool NavigationStateMachine::Run()
{
    if (!Bootstrap()) {
        StopMotion();
        return false;
    }

    while (!should_stop_() && session_->phase() != NaviPhase::Finished && session_->phase() != NaviPhase::Failed) {
        if (!TickPhase(session_->phase())) {
            StopMotion();
            return false;
        }
    }

    if (!should_stop_() && session_->phase() != NaviPhase::Failed) {
        session_->HasSatisfiedFinalSuccess(*position_, "navigation_complete");
    }

    StopMotion();
    return !should_stop_() && session_->success();
}

bool NavigationStateMachine::Bootstrap()
{
    runtime_state_.BeginNavigation(std::chrono::steady_clock::now());

    if (session_->HasSatisfiedFinalSuccess(*position_, "bootstrap_already_at_final_goal")) {
        return true;
    }

    const std::optional<DynamicAnchor> anchor = ResolveBootstrapAnchor(param_, session_, *position_);
    if (anchor && TryApplyDynamicOverlayToAnchor("bootstrap_navmesh_overlay", anchor->first, anchor->second, false)) {
        SelectPhaseForCurrentWaypoint("bootstrap_navmesh_overlay");
        return true;
    }

    const std::optional<BootstrapContinueCandidate> continue_candidate =
        ResolveBootstrapContinueCandidate(session_->original_path(), *position_);
    if (continue_candidate && continue_candidate->continue_index > 0
        && continue_candidate->continue_index < session_->original_path().size()) {
        session_->ApplyDynamicOverlay({}, continue_candidate->continue_index, *position_);
        runtime_state_.route.Reset();
        runtime_state_.nav_run_dirty = true;
        LogInfo << "Bootstrap serial route continue after navmesh overlay unavailable."
                << VAR(continue_candidate->continue_index) << VAR(continue_candidate->route_distance) << VAR(position_->x)
                << VAR(position_->y) << VAR(position_->zone_id);
        SelectPhaseForCurrentWaypoint("bootstrap_serial_continue");
        return true;
    }

    LogWarn << "Bootstrap ownership fallback to route head." << VAR(position_->x) << VAR(position_->y) << VAR(position_->zone_id);
    SelectPhaseForCurrentWaypoint("bootstrap_ready");
    return true;
}

bool NavigationStateMachine::TickPhase(NaviPhase phase)
{
    switch (phase) {
    case NaviPhase::Bootstrap:
        SelectPhaseForCurrentWaypoint("bootstrap_dispatch");
        return true;
    case NaviPhase::Navigate:
        return TickNavigate();
    case NaviPhase::WaitTransfer: {
        const semantic_nodes::Result semantic_result = semantic_nodes::TickSemanticFlow(
            BuildSemanticContext(
                action_wrapper_,
                position_provider_,
                session_,
                motion_controller_,
                action_executor_,
                position_,
                &runtime_state_,
                maa_context_),
            phase);
        if (semantic_result.request_failure) {
            return FailNavigation(semantic_result.failure_reason, semantic_result.failure_log_message, 0.0, 0.0, 0);
        }
        return true;
    }
    case NaviPhase::Finished:
    case NaviPhase::Failed:
        return true;
    }
    return false;
}

bool NavigationStateMachine::CaptureCurrentPosition(bool force_global_search)
{
    return position_provider_->Capture(position_, force_global_search, session_->current_zone_id());
}

bool NavigationStateMachine::TryApplyDynamicOverlayToAnchor(
    const char* reason,
    size_t continue_index,
    const Waypoint& anchor,
    bool use_detour,
    double route_heading)
{
    if (!anchor.HasPosition()) {
        LogWarn << "Dynamic navmesh overlay skipped: anchor has no position." << VAR(reason) << VAR(continue_index);
        return false;
    }

    const navmesh::WorldPoint start { .x = position_->x, .y = position_->y };
    const navmesh::WorldPoint goal { .x = anchor.x, .y = anchor.y };
    navmesh::WorldPoint detour_vertex {};
    const auto route = use_detour ? PlanNavmeshDetourRoute(param_, *position_, anchor, route_heading, &detour_vertex)
                                  : PlanNavmeshRoute(param_, position_->zone_id, start, goal);
    if (!route) {
        return false;
    }

    std::vector<Waypoint> generated_prefix;
    if (use_detour) {
        generated_prefix.emplace_back(detour_vertex.x, detour_vertex.y, ActionType::RUN);
        generated_prefix.back().strict_arrival = true;
    }
    else if (!AppendGeneratedNavmeshWaypoints(route->path, generated_prefix, false)) {
        LogWarn << "Dynamic navmesh overlay skipped: generated path is unusable." << VAR(reason) << VAR(continue_index)
                << VAR(route->path.points.size());
        return false;
    }
    const size_t generated_count = generated_prefix.size();
    session_->ApplyDynamicOverlay(std::move(generated_prefix), continue_index, *position_);
    runtime_state_.route.Reset();
    runtime_state_.nav_run_dirty = true;
    if (generated_count == 0 && std::hypot(anchor.x - position_->x, anchor.y - position_->y) <= ArrivalBandForStartupBypass(anchor)) {
        runtime_state_.route.startup_anchor_pos = *position_;
        runtime_state_.route.startup_anchor_initialized = true;
        runtime_state_.route.startup_motion_confirmed = true;
    }
    runtime_state_.dynamic_replan_requested = false;
    LogInfo << "Dynamic navmesh overlay selected." << VAR(reason) << VAR(use_detour) << VAR(continue_index)
            << VAR(generated_count);
    return true;
}

bool NavigationStateMachine::TryApplyDynamicOverlayToNextAnchor(const char* reason, bool use_detour, double route_heading)
{
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    if (!anchor) {
        runtime_state_.dynamic_replan_requested = false;
        LogInfo << "Dynamic navmesh overlay skipped: no future anchor." << VAR(reason) << VAR(position_->x) << VAR(position_->y)
                << VAR(position_->zone_id);
        return false;
    }
    return TryApplyDynamicOverlayToAnchor(reason, anchor->first, anchor->second, use_detour, route_heading);
}

bool NavigationStateMachine::HandleDynamicReplanRequest(const char* reason)
{
    if (TryApplyDynamicOverlayToNextAnchor(reason, false)) {
        return true;
    }
    if (session_->HasCurrentWaypoint() && session_->CurrentWaypoint().action == ActionType::NAVMESH) {
        return FailNavigation("dynamic_replan_failed", "Dynamic navmesh replan failed on required NAVMESH waypoint.", 0.0, 0.0, 0);
    }

    runtime_state_.dynamic_replan_requested = false;
    runtime_state_.route.Reset();
    session_->ResetProgress();
    LogWarn << "Dynamic navmesh replan unavailable; falling back to current route." << VAR(reason) << VAR(position_->x)
            << VAR(position_->y) << VAR(position_->zone_id);
    SelectPhaseForCurrentWaypoint("dynamic_replan_fallback");
    return true;
}

bool NavigationStateMachine::TickNavigate()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }

    const semantic_nodes::Context semantic_ctx = BuildSemanticContext(
        action_wrapper_,
        position_provider_,
        session_,
        motion_controller_,
        action_executor_,
        position_,
        &runtime_state_,
        maa_context_);
    const semantic_nodes::Result active_semantic_result = semantic_nodes::TickSemanticFlow(semantic_ctx, NaviPhase::Navigate);
    if (active_semantic_result.request_failure) {
        return FailNavigation(active_semantic_result.failure_reason, active_semantic_result.failure_log_message, 0.0, 0.0, 0);
    }
    if (runtime_state_.dynamic_replan_requested) {
        return HandleDynamicReplanRequest("dynamic_replan");
    }
    if (active_semantic_result.stay_in_current_tick) {
        return true;
    }

    if (!CaptureCurrentPosition(false)) {
        utils::SleepFor(kLocatorRetryIntervalMs);
        return true;
    }

    const semantic_nodes::Result inline_semantic_result = semantic_nodes::ConsumeInlineSemantics(semantic_ctx);
    if (inline_semantic_result.request_failure) {
        return FailNavigation(inline_semantic_result.failure_reason, inline_semantic_result.failure_log_message, 0.0, 0.0, 0);
    }
    if (runtime_state_.dynamic_replan_requested) {
        return HandleDynamicReplanRequest("dynamic_replan");
    }
    if (inline_semantic_result.stay_in_current_tick) {
        return true;
    }
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }

    if (runtime_state_.semantic.portal_transit_active || session_->phase() != NaviPhase::Navigate) {
        utils::SleepFor(kTargetTickMs);
        return true;
    }

    if (session_->CurrentWaypoint().IsZoneDeclaration()) {
        motion_controller_->SetForwardState(true);
        utils::SleepFor(kTargetTickMs);
        return true;
    }

    const auto now = std::chrono::steady_clock::now();
    const bool startup_grace_elapsed =
        runtime_state_.flow.navigate_started_at.time_since_epoch().count() > 0
        && std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.flow.navigate_started_at).count() >= 3000;
    const double current_heading = NaviMath::NormalizeAngle(position_->angle);
    const bool degraded_fix =
        position_provider_->LastCaptureWasHeld() || position_provider_->LastCaptureWasBlackScreen() || !position_->valid;

    const size_t node_idx_before_tracking = session_->current_node_idx();
    RouteTrackingState route = RouteTracker::Update(session_, &runtime_state_.route, *position_);
    if (session_->current_node_idx() != node_idx_before_tracking) {
        runtime_state_.recovery.Reset();
    }

    NavRunTickResult nav_run_result;
    if (route.valid && session_->HasCurrentWaypoint()) {
        const Waypoint& current_waypoint = session_->CurrentWaypoint();
        if (current_waypoint.HasPosition() && current_waypoint.action == ActionType::RUN) {
            // Strict RUN must be hit precisely, so its corridor anchor is the waypoint itself;
            // continuous RUN can lookahead through to the next semantic anchor.
            std::optional<DynamicAnchor> nav_run_anchor;
            if (current_waypoint.RequiresStrictArrival()) {
                nav_run_anchor = DynamicAnchor { session_->current_node_idx(), current_waypoint };
            }
            else {
                nav_run_anchor = ResolveCurrentAnchor(session_, *position_);
            }
            if (nav_run_anchor) {
                const bool sprint_proxy = route.startup_motion_confirmed && param_.sprint_threshold > 0.0
                                          && route.along_track_remaining > param_.sprint_threshold;
                nav_run_result = nav_run_controller_.tick(
                    session_, &runtime_state_, *position_, route, param_, nav_run_anchor->first, nav_run_anchor->second,
                    sprint_proxy, now);
            }
        }
    }

    // NavMesh corridor steering can legitimately carry the agent far off the original serial
    // waypoint line — far enough that serial cross-track exceeds the deviation-fail gate and
    // RouteTracker stops advancing the index. Left alone, the session latches the stale waypoint
    // while NavRun keeps steering toward a distant anchor, and the fallback heading points back
    // at that stale waypoint: the detour "circling". Consume the continuous-RUN waypoints the
    // corridor has already carried us past so the serial index — and the arrival gate, fallback
    // heading, and recovery anchor that all key off it — tracks real progress. This runs after
    // the tick because it depends on this tick's corridor projection.
    if (nav_run_result.passed_run_waypoints > 0) {
        size_t remaining_to_consume = nav_run_result.passed_run_waypoints;
        bool consumed_any = false;
        while (remaining_to_consume > 0 && session_->HasCurrentWaypoint()) {
            const Waypoint& corridor_passed = session_->CurrentWaypoint();
            if (!corridor_passed.HasPosition() || corridor_passed.action != ActionType::RUN
                || corridor_passed.RequiresStrictArrival()) {
                break;
            }
            session_->AdvanceToNextWaypoint(ActionType::RUN, "navmesh_corridor_passed_run_waypoint");
            consumed_any = true;
            --remaining_to_consume;
        }
        if (consumed_any) {
            // The corridor is unchanged (same anchor) — only the serial bookkeeping moved — so
            // leave nav_run_dirty clear and just recompute the serial projection for the new
            // current waypoint, keeping the arrival gate below consistent within this tick.
            runtime_state_.recovery.Reset();
            route = RouteTracker::Update(session_, &runtime_state_.route, *position_);
        }
    }

    // When NavRun is steering, corridor remaining is the true progress signal — chasing
    // route.progress_distance would fire spurious stalls while the agent legitimately
    // detours around obstacles.
    if (route.valid) {
        const double effective_progress = nav_run_result.has_corridor_heading
                                              ? nav_run_result.remaining_to_anchor
                                              : route.progress_distance;
        session_->ObserveProgress(session_->current_node_idx(), effective_progress, now);
    }
    // An OffCorridor replan rebuilds a genuinely different (usually longer) corridor, so reset the stall
    // counter to not penalize the new route. A ProgressRegression replan, by contrast, fires *because* the
    // agent is making no corridor progress — it regenerates the same corridor against a dynamic obstacle the
    // navmesh cannot see. Resetting on it would keep deferring the obstacle-recovery trigger that is the only
    // layer able to route around the obstacle, so leave the stall counter running in that case.
    if (nav_run_result.replanned_with == NavRunReplanReason::OffCorridor) {
        session_->ResetProgress();
    }
    if (runtime_state_.recovery.active) {
        const bool recovery_zone_changed = !runtime_state_.recovery.anchor_pos.zone_id.empty() && !position_->zone_id.empty()
                                           && runtime_state_.recovery.anchor_pos.zone_id != position_->zone_id;
        const double recovery_displacement =
            std::hypot(position_->x - runtime_state_.recovery.anchor_pos.x, position_->y - runtime_state_.recovery.anchor_pos.y);
        if (recovery_zone_changed || recovery_displacement >= kDynamicRecoveryResetDistance) {
            runtime_state_.recovery.Reset();
            runtime_state_.route.ResetTracking();
            runtime_state_.dynamic_replan_requested = false;
            runtime_state_.nav_run_dirty = true;
            session_->ResetProgress();
            LogInfo << "Dynamic recovery escaped obstacle." << VAR(recovery_zone_changed) << VAR(recovery_displacement);
            SelectPhaseForCurrentWaypoint("recovery_escape");
            return true;
        }
    }
    const int64_t stalled_ms = session_->StalledMs(now);

    if (!route.valid) {
        if (degraded_fix) {
            motion_controller_->SetForwardState(false);
        }
        utils::SleepFor(kTargetTickMs);
        return true;
    }

    const Waypoint waypoint = session_->CurrentWaypoint();
    const double arrival_distance =
        waypoint.action == ActionType::PORTAL ? std::max(route.arrival_band, kPortalCommitDistance) : route.arrival_band;
    if (route.waypoint_distance <= arrival_distance) {
        if (!route.startup_motion_confirmed) {
            LogDebug << "Arrival advance blocked before startup movement confirmed." << VAR(session_->current_node_idx())
                     << VAR(route.waypoint_distance) << VAR(arrival_distance) << VAR(route.progress_distance) << VAR(route.cross_track)
                     << VAR(route.projection_anchor);
        }
        else {
            const semantic_nodes::Result arrival_semantic_result =
                semantic_nodes::HandleArrivalSemantic(semantic_ctx, waypoint, route.waypoint_distance);
            if (arrival_semantic_result.request_failure) {
                return FailNavigation(
                    arrival_semantic_result.failure_reason,
                    arrival_semantic_result.failure_log_message,
                    route.waypoint_distance,
                    0.0,
                    stalled_ms);
            }
            if (arrival_semantic_result.consumed) {
                return true;
            }

            const std::optional<size_t> arrived_absolute_node_idx = session_->CurrentAbsoluteNodeIndex();
            if (waypoint.RequiresStrictArrival() && motion_controller_->IsMoving()) {
                motion_controller_->SetForwardState(false);
                utils::SleepFor(kStopWaitMs);
            }
            action_executor_->Execute(waypoint.action);
            session_->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *position_, "waypoint_action_completed");
            session_->AdvanceToNextWaypoint(waypoint.action, "waypoint_action_completed");
            runtime_state_.OnWaypointAdvance();
            if (!session_->HasCurrentWaypoint()) {
                session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
                return true;
            }
            SelectPhaseForCurrentWaypoint("waypoint_action_completed");
            return true;
        }
    }

    const bool near_strict_goal = waypoint.RequiresStrictArrival()
        && route.waypoint_distance <= arrival_distance + kCloseGoalDetourSuppressSlack;
    const bool should_try_recovery = session_->phase() == NaviPhase::Navigate && stalled_ms >= kObstacleRecoveryMinTriggerMs
                                     && (route.progress_distance > kNoProgressMinDistance || waypoint.RequiresStrictArrival())
                                     && !near_strict_goal;
    if (should_try_recovery) {
        const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
        if (anchor) {
            DynamicRecoveryState& recovery = runtime_state_.recovery;
            if (!recovery.active || recovery.anchor_index != anchor->first) {
                recovery.Reset();
                recovery.active = true;
                recovery.anchor_pos = *position_;
                recovery.started_at = now;
                recovery.anchor_index = anchor->first;
            }

            const int64_t recovery_elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - recovery.started_at).count();
            if (recovery_elapsed_ms > kDynamicRecoveryTotalTimeoutMs) {
                return FailNavigation(
                    "dynamic_recovery_timeout",
                    "Dynamic recovery timeout reached and navigation was terminated.",
                    route.progress_distance,
                    NaviMath::NormalizeAngle(route.route_heading - current_heading),
                    stalled_ms);
            }

            const bool retry_cooling_down = recovery.last_replan_at.time_since_epoch().count() > 0
                                            && std::chrono::duration_cast<std::chrono::milliseconds>(now - recovery.last_replan_at).count()
                                                   < kDynamicRecoveryRetryIntervalMs;
            if (!retry_cooling_down) {
                if (recovery.attempt_count >= kDynamicRecoveryMaxAttemptsPerAnchor) {
                    return FailNavigation(
                        "dynamic_recovery_exhausted",
                        "Dynamic recovery attempts were exhausted and navigation was terminated.",
                        route.progress_distance,
                        NaviMath::NormalizeAngle(route.route_heading - current_heading),
                        stalled_ms);
                }

                recovery.last_replan_at = now;
                const NaviPosition jump_start = *position_;
                LogInfo << "Dynamic recovery jump pulse issued." << VAR(recovery.attempt_count + 1);
                motion_controller_->SetAction(LocalDriverAction::JumpForward, true);
                utils::SleepFor(kActionJumpSettleMs);
                motion_controller_->SetForwardState(false);
                if (!CaptureCurrentPosition(false) || position_provider_->LastCaptureWasHeld()
                    || position_provider_->LastCaptureWasBlackScreen() || !position_->valid) {
                    LogWarn << "Dynamic recovery waiting for post-jump local tracking fix." << VAR(stalled_ms)
                            << VAR(recovery.attempt_count);
                    utils::SleepFor(kTargetTickMs);
                    return true;
                }

                const bool jump_zone_changed = !jump_start.zone_id.empty() && !position_->zone_id.empty()
                                               && jump_start.zone_id != position_->zone_id;
                const double jump_displacement = std::hypot(position_->x - jump_start.x, position_->y - jump_start.y);
                const double jump_waypoint_distance = std::hypot(waypoint.x - position_->x, waypoint.y - position_->y);
                const bool jump_made_progress = jump_waypoint_distance + kNoProgressDistanceEpsilon < route.waypoint_distance;
                const bool jump_moved_forward = jump_displacement >= kDynamicRecoveryResetDistance * 0.5 && jump_made_progress;
                if (jump_zone_changed || jump_displacement >= kDynamicRecoveryResetDistance || jump_moved_forward) {
                    recovery.Reset();
                    runtime_state_.route.ResetTracking();
                    runtime_state_.dynamic_replan_requested = false;
                    runtime_state_.nav_run_dirty = true;
                    session_->ResetProgress();
                    LogInfo << "Dynamic recovery jump escaped obstacle." << VAR(jump_zone_changed) << VAR(jump_displacement)
                            << VAR(jump_moved_forward);
                    SelectPhaseForCurrentWaypoint("recovery_jump_escape");
                    return true;
                }

                const std::optional<DynamicAnchor> post_jump_anchor = ResolveCurrentAnchor(session_, *position_);
                if (!post_jump_anchor) {
                    LogWarn << "Dynamic recovery skipped: no future anchor after post-jump local tracking." << VAR(position_->x)
                            << VAR(position_->y) << VAR(position_->zone_id);
                    utils::SleepFor(kTargetTickMs);
                    return true;
                }
                if (post_jump_anchor->first != recovery.anchor_index) {
                    recovery.Reset();
                    recovery.active = true;
                    recovery.anchor_pos = *position_;
                    recovery.started_at = now;
                    recovery.anchor_index = post_jump_anchor->first;
                    recovery.last_replan_at = now;
                }

                ++recovery.attempt_count;
                if (TryApplyDynamicOverlayToAnchor(
                        "recovery_navmesh_detour",
                        post_jump_anchor->first,
                        post_jump_anchor->second,
                        true,
                        route.route_heading)) {
                    SelectPhaseForCurrentWaypoint("recovery_navmesh_detour");
                    return true;
                }

                LogWarn << "Dynamic recovery detour attempt failed." << VAR(recovery.attempt_count) << VAR(post_jump_anchor->first)
                        << VAR(route.progress_distance) << VAR(stalled_ms);
                if (recovery.attempt_count >= kDynamicRecoveryMaxAttemptsPerAnchor) {
                    return FailNavigation(
                        "dynamic_recovery_exhausted",
                        "Dynamic recovery attempts were exhausted and navigation was terminated.",
                        route.progress_distance,
                        NaviMath::NormalizeAngle(route.route_heading - current_heading),
                        stalled_ms);
                }
                utils::SleepFor(kTargetTickMs);
                return true;
            }
        }
    }

    const double effective_route_heading =
        nav_run_result.has_corridor_heading ? nav_run_result.corridor_heading : route.route_heading;

    const double heading_error = NaviMath::NormalizeAngle(effective_route_heading - current_heading);
    const SteeringCommand steering = SteeringController::Update(heading_error, motion_controller_->IsMovingForward());

    motion_controller_->SetForwardState(true);

    double issued_delta_deg = 0.0;
    if (steering.issued) {
        const TurnCommandResult steering_result = motion_controller_->ApplySteering(steering.yaw_delta_deg);
        if (steering_result.issued) {
            issued_delta_deg = steering_result.issued_delta_degrees;
        }
    }

    LogDebug << "TickNavigate steering decision." << VAR(current_heading) << VAR(route.route_heading)
             << VAR(effective_route_heading) << VAR(nav_run_result.has_corridor_heading)
             << VAR(nav_run_result.cross_track) << VAR(heading_error) << VAR(steering.yaw_delta_deg) << VAR(issued_delta_deg)
             << VAR(route.waypoint_distance) << VAR(route.on_route);

    const bool turn_calm = !steering.issued || std::abs(steering.yaw_delta_deg) < 30.0;
    const bool target_requires_strict_arrival = waypoint.RequiresStrictArrival();
    const bool allow_sprint =
        turn_calm && motion_controller_->SupportsSprint() && startup_grace_elapsed && param_.sprint_threshold > 0.0
        && !target_requires_strict_arrival && route.along_track_remaining > param_.sprint_threshold
        && (runtime_state_.flow.last_auto_sprint_time.time_since_epoch().count() == 0
            || std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.flow.last_auto_sprint_time).count()
                   >= kAutoSprintCooldownMs);
    if (allow_sprint) {
        if (motion_controller_->TriggerSprint()) {
            runtime_state_.flow.last_auto_sprint_time = now;
        }
    }

    if (param_.arrival_timeout > 0 && stalled_ms > param_.arrival_timeout) {
        return FailNavigation(
            "no_progress_timeout",
            "No progress timeout reached and navigation was terminated.",
            route.progress_distance,
            NaviMath::NormalizeAngle(route.route_heading - current_heading),
            stalled_ms);
    }

    utils::SleepFor(kTargetTickMs);
    return true;
}

void NavigationStateMachine::SelectPhaseForCurrentWaypoint(const char* reason)
{
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return;
    }
    session_->UpdatePhase(NaviPhase::Navigate, reason);
}

void NavigationStateMachine::StopMotion()
{
    motion_controller_->SetForwardState(false);
}

bool NavigationStateMachine::FailNavigation(
    const char* reason,
    const char* log_message,
    double current_distance,
    double yaw_error,
    int64_t stalled_ms)
{
    StopMotion();
    runtime_state_.ResetNavigationAssistState();
    session_->UpdatePhase(NaviPhase::Failed, reason);
    LogError << log_message << VAR(current_distance) << VAR(yaw_error) << VAR(stalled_ms);
    return true;
}

} // namespace mapnavigator
