#include <algorithm>
#include <array>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <string>
#include <thread>
#include <utility>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "action_executor.h"
#include "action_wrapper.h"
#include "async_prompt_action.h"
#include "latency_observer.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "navigation_state_machine.h"
#include "navmesh_path_expander.h"
#include "position_provider.h"
#include "prompt_scan_profile.h"
#include "roi_template_scanner.h"
#include "route_tracker.h"
#include "semantic_nodes.h"
#include "sensitivity_observer.h"
#include "steering_controller.h"
#include "zipline_action.h"

#include "../utils.h"

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

constexpr size_t kUnaddressableAnchorIndex = std::numeric_limits<size_t>::max();

bool IsRequiredSemanticAnchor(const Waypoint& waypoint)
{
    if (!waypoint.HasPosition()) {
        return waypoint.IsHeadingOnly() || waypoint.IsZoneDeclaration();
    }
    return waypoint.action != ActionType::RUN || waypoint.RequiresStrictArrival();
}

double ArrivalBandForStartupBypass(const Waypoint& waypoint)
{
    double arrival_band = waypoint.ArrivalBand(kMeasurementDefaultPositionQuantum);
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

std::vector<BootstrapWaypointCandidate> CollectReachableWaypoints(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    std::vector<BootstrapWaypointCandidate> candidates;
    for (size_t index = 0; index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        if (!IsZoneCompatible(waypoint, position.zone_id)) {
            continue;
        }

        const double distance = std::hypot(position.x - waypoint.x, position.y - waypoint.y);
        if (distance > kBootstrapOwnershipMaxDistance) {
            continue;
        }

        candidates.push_back(BootstrapWaypointCandidate { .index = index, .distance = distance });
    }
    return candidates;
}

// Walk back along the route while each predecessor is still within the ownership radius of the anchor.
// Distances are anchor-relative, not chained, so the walk-back stays bounded; positionless waypoints
// (ZONE / HEADING markers) end it, and leading markers alone mean the route head.
size_t RewindToEarliestNearby(const std::vector<Waypoint>& path, const NaviPosition& position, size_t index)
{
    const Waypoint& anchor = path[index];
    if (!anchor.HasPosition()) {
        return index;
    }

    size_t rewound = index;
    while (rewound > 0) {
        const Waypoint& previous = path[rewound - 1];
        if (!previous.HasPosition() || !IsZoneCompatible(previous, position.zone_id)) {
            break;
        }
        if (std::hypot(previous.x - anchor.x, previous.y - anchor.y) > kBootstrapOwnershipMaxDistance) {
            break;
        }
        --rewound;
    }

    const auto rewound_begin = path.begin() + static_cast<std::ptrdiff_t>(rewound);
    if (std::none_of(path.begin(), rewound_begin, [](const Waypoint& waypoint) { return waypoint.HasPosition(); })) {
        return 0;
    }
    return rewound;
}

std::optional<BootstrapContinueCandidate> ResolveBootstrapContinueCandidate(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    const std::optional<BootstrapContinueCandidate> projected = FindProjectedContinueCandidate(path, position);
    if (projected.has_value()) {
        return projected;
    }

    // Off the line, "nearest" alone decided ownership and lost whole clusters of waypoints to
    // sub-metre gaps. Skipping now needs evidence; without it the route is resumed further back.
    const std::vector<BootstrapWaypointCandidate> reachable = CollectReachableWaypoints(path, position);
    if (reachable.empty()) {
        return std::nullopt;
    }

    // Standing on a waypoint. Earliest one wins so a loop route's coincident head is not read as its tail.
    const auto standing = std::find_if(reachable.begin(), reachable.end(), [](const BootstrapWaypointCandidate& candidate) {
        return candidate.distance <= kBootstrapOwnershipStandingDistance;
    });
    if (standing != reachable.end()) {
        return BootstrapContinueCandidate {
            .continue_index = standing->index,
            .route_distance = standing->distance,
            .reason = "standing_on_waypoint",
        };
    }

    const BootstrapWaypointCandidate* nearest = &reachable.front();
    for (const BootstrapWaypointCandidate& candidate : reachable) {
        if (candidate.distance < nearest->distance) {
            nearest = &candidate;
        }
    }

    std::optional<double> nearest_before;
    for (const BootstrapWaypointCandidate& candidate : reachable) {
        if (candidate.index >= nearest->index) {
            break;
        }
        nearest_before = nearest_before ? std::min(*nearest_before, candidate.distance) : candidate.distance;
    }

    // Nothing earlier to lose, or clear of all of it by a wide margin: the route really is behind us.
    if (!nearest_before.has_value() || nearest->distance + kBootstrapOwnershipDecisiveMargin < *nearest_before) {
        return BootstrapContinueCandidate {
            .continue_index = nearest->index,
            .route_distance = nearest->distance,
            .reason = "decisive_nearest",
        };
    }

    return BootstrapContinueCandidate {
        .continue_index = RewindToEarliestNearby(path, position, nearest->index),
        .route_distance = nearest->distance,
        .reason = "rewound_to_earliest",
    };
}

std::optional<DynamicAnchor>
    ResolveBootstrapNavmeshAnchor(const NaviParam& param, NavigationSession* session, const NaviPosition& position, size_t start_index)
{
    const size_t path_size = session->current_path().size();
    std::optional<DynamicAnchor> anchor;
    double anchor_cost = std::numeric_limits<double>::infinity();
    int plan_attempts = 0;

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
        // 只钉终点: 够不到那张面的候选就不该被选中。第一个规划得通的点就是入口 ——
        // 再往后比价挑更近的, 等于在归属判定之后又做一次"就近吞点"。
        ++plan_attempts;
        const auto route = PlanNavmeshRoute(param, position.zone_id, start, goal, waypoint.target_deck_y);
        if (route) {
            anchor_cost = route->cost;
            anchor = { *canonical_index, waypoint };
            break;
        }
        if (IsRequiredSemanticAnchor(waypoint)) {
            break;
        }
    }

    // plan_attempts - 1 waypoints ahead of the anchor turned out unreachable.
    if (anchor) {
        LogInfo << "Bootstrap navmesh anchor selected." << VAR(anchor->first) << VAR(anchor_cost) << VAR(plan_attempts) << VAR(start_index);
    }
    return anchor;
}

std::optional<DynamicAnchor> ResolveBootstrapAnchor(const NaviParam& param, NavigationSession* session, const NaviPosition& position)
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
    , collect_prompt_(kCollectPromptSpec, maa_context, session, position)
    , interact_prompt_(kInteractPromptSpec, maa_context, session, position)
    , device_recovery_(maa_context, motion_controller, position_provider, session, position)
    , walk_mode_(action_wrapper)
{
    LogInfo << "Navigation route runner selected. backend=orchestrated";
}

bool NavigationStateMachine::Run()
{
    latency::BeginRun();

    if (!Bootstrap()) {
        StopMotion();
        sensitivity::EndRun(maa_context_, true);
        return false;
    }

    // Pay the recognition cold start while still stopped, so it can never land on a walking tick.
    PreWarmPromptRecognition();

    // Background detectors, off the nav thread on pure OpenCV; the nav loop only reacts to their flags.
    StartScanners();

    while (!should_stop_() && session_->phase() != NaviPhase::Finished && session_->phase() != NaviPhase::Failed) {
        if (!TickPhase(session_->phase())) {
            StopScanners();
            StopMotion();
            sensitivity::EndRun(maa_context_, true);
            return false;
        }
    }

    if (!should_stop_() && session_->phase() != NaviPhase::Failed) {
        session_->HasSatisfiedFinalSuccess(*position_, "navigation_complete");
    }

    TryRunPromptSubtaskAtRouteTail();

    StopScanners();
    StopMotion();

    // 用户主动停的不算走坏，别借着这个把门槛放下来。
    const bool stopped_by_user = should_stop_();
    const bool succeeded = !stopped_by_user && session_->success();
    sensitivity::EndRun(maa_context_, !succeeded && !stopped_by_user);
    return succeeded;
}

bool NavigationStateMachine::Bootstrap()
{
    runtime_state_.BeginNavigation(std::chrono::steady_clock::now());
    sensitivity::BeginRun();

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
        LogInfo << "Bootstrap serial route continue after navmesh overlay unavailable." << VAR(continue_candidate->continue_index)
                << VAR(continue_candidate->route_distance) << VAR(position_->x) << VAR(position_->y) << VAR(position_->zone_id);
        SelectPhaseForCurrentWaypoint("bootstrap_serial_continue");
        return true;
    }

    LogWarn << "Bootstrap ownership fallback to route head." << VAR(position_->x) << VAR(position_->y) << VAR(position_->zone_id);
    SelectPhaseForCurrentWaypoint("bootstrap_ready");
    return true;
}

bool NavigationStateMachine::TickPhase(NaviPhase phase)
{
    // Ahead of every early return, so no branch or phase can strand the game in walking mode.
    UpdateWalkMode(phase);

    switch (phase) {
    case NaviPhase::Bootstrap:
        SelectPhaseForCurrentWaypoint("bootstrap_dispatch");
        return true;
    case NaviPhase::Navigate:
        return TickNavigate();
    case NaviPhase::WaitTransfer:
    case NaviPhase::WaitZipline: {
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

// A sustained run of unusable fixes (commonly: the agent was shoved across a zone boundary into a
// sub-zone the route was not planned in, so every locate fails zone validation) would otherwise hold
// forward into the obstacle forever — the recovery block and its timeout sit past this point and are
// never reached. Stop pressing forward, hop periodically to dislodge, and fail-fast on timeout.
bool NavigationStateMachine::HandleLocalizationLoss()
{
    const auto now = std::chrono::steady_clock::now();
    LocalizationLossState& loss = runtime_state_.localization_loss;
    if (loss.started_at == std::chrono::steady_clock::time_point {}) {
        loss.started_at = now;
    }
    // River-fall discriminator: a black capture during a loss = fell in water (the locator folds it into a
    // generic TrackingLost). Latch it so the re-acquire below can arm recovery. See navigator-river-fall.
    if (position_provider_->LastCaptureWasBlackScreen()) {
        loss.saw_black_screen = true;
    }
    motion_controller_->SetForwardState(false);

    const auto loss_elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(now - loss.started_at);
    if (loss_elapsed >= std::chrono::milliseconds(kLocalizationLossTimeoutMs)) {
        return FailNavigation(
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
        const std::string prior_zone = session_->current_zone_id();
        if (position_provider_->Capture(position_, /*force_global_search=*/true, /*expected_zone_id=*/std::string())) {
            const bool zone_changed = !position_->zone_id.empty() && position_->zone_id != prior_zone;

            if (runtime_state_.cross_tier_escape.active) {
                if (NavmeshZonesShareGeometry(param_, runtime_state_.cross_tier_escape.anchor_zone, position_->zone_id)) {
                    LogInfo << "Cross-tier escape rode a zone flip; preserving the corridor." << VAR(prior_zone) << VAR(position_->zone_id)
                            << VAR(position_->x) << VAR(position_->y);
                    if (zone_changed) {
                        session_->UpdateCurrentZone(position_->zone_id);
                    }
                    loss.Reset();
                    return true;
                }
                LogInfo << "Cross-tier escape: re-acquired zone left the pit span; reverting to loss handling." << VAR(prior_zone)
                        << VAR(position_->zone_id);
                runtime_state_.cross_tier_escape.Reset();
            }
            else if (zone_changed && TryEnterCrossTierEscape()) {
                loss.Reset();
                return true;
            }

            if (zone_changed) {
                // Pin subsequent tracking ticks to the zone we actually re-acquired in, else the next
                // CaptureCurrentPosition(false) would re-impose the stale expected_zone and fail again.
                session_->UpdateCurrentZone(position_->zone_id);
            }
            if (++runtime_state_.global_reacquire_streak >= kLocalizationThrashFailCount) {
                return FailNavigation(
                    "localization_thrash",
                    "Re-acquired the route repeatedly without advancing a waypoint (wrong-tier fall thrashing "
                    "recover<->re-lose); terminating so the pipeline can retry.",
                    0.0,
                    0.0,
                    0);
            }
            LogInfo << "Localization recovered via global re-acquire; resuming navigation." << VAR(loss_elapsed.count()) << VAR(prior_zone)
                    << VAR(position_->zone_id) << VAR(position_->x) << VAR(position_->y) << VAR(runtime_state_.global_reacquire_streak);
            ArmRiverFallRecoveryIfBlackScreenLoss("global_reacquire");
            loss.Reset();
            runtime_state_.route.ResetTracking();
            runtime_state_.nav_run_dirty = true;
            session_->ResetProgress();
            return true;
        }
    }

    const bool unstick_cooling = loss.last_unstick_at != std::chrono::steady_clock::time_point {}
                                 && std::chrono::duration_cast<std::chrono::milliseconds>(now - loss.last_unstick_at)
                                        < std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs);
    if (loss_elapsed >= std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs) && !unstick_cooling) {
        loss.last_unstick_at = now;
        LogInfo << "Localization lost; blind unstick hop issued." << VAR(loss_elapsed.count());
        motion_controller_->SetAction(LocalDriverAction::JumpForward, true);
        utils::SleepFor(kActionJumpSettleMs);
        motion_controller_->SetForwardState(false);
        return true;
    }

    utils::SleepFor(kLocatorRetryIntervalMs);
    return true;
}

bool NavigationStateMachine::ArmRiverFallRecoveryIfBlackScreenLoss(const char* via)
{
    if (!runtime_state_.localization_loss.saw_black_screen) {
        return false;
    }
    // Full reset first: a re-fall must redo the settle and the about-face, not inherit the last episode's one-shots.
    runtime_state_.river_fall.Reset();
    runtime_state_.river_fall.pending = true;
    runtime_state_.river_fall.anchor_pos = *position_;
    // Arm-time facing, logged so a post-mortem can tell it apart from the settled read the about-face actually uses.
    runtime_state_.river_fall.water_heading = NaviMath::NormalizeAngle(position_->angle);
    // River-fall owns the recovery: the pre-fall dynamic-recovery anchor is stale after the teleport, and a live
    // recovery's escaped-obstacle check (runs before the river-fall block) would otherwise pre-empt the escape.
    runtime_state_.recovery.Reset();
    session_->ResetHardProgress();
    LogInfo << "River-fall recovery armed (black-screen loss recovered)." << VAR(via) << VAR(position_->x) << VAR(position_->y)
            << VAR(runtime_state_.river_fall.water_heading);
    return true;
}

bool NavigationStateMachine::TryApplyDynamicOverlayToAnchor(
    const char* reason,
    size_t continue_index,
    const Waypoint& anchor,
    bool use_detour,
    double route_heading,
    bool emit_interior_corners)
{
    if (!anchor.HasPosition()) {
        LogWarn << "Dynamic navmesh overlay skipped: anchor has no position." << VAR(reason) << VAR(continue_index);
        return false;
    }

    const navmesh::WorldPoint start { .x = position_->x, .y = position_->y };
    const navmesh::WorldPoint goal { .x = anchor.x, .y = anchor.y };
    navmesh::WorldPoint detour_vertex {};
    const auto route = use_detour ? PlanNavmeshDetourRoute(param_, *position_, anchor, route_heading, &detour_vertex)
                                  : PlanNavmeshRoute(param_, position_->zone_id, start, goal, anchor.target_deck_y);
    if (!route) {
        return false;
    }

    std::vector<Waypoint> generated_prefix;
    if (use_detour) {
        generated_prefix.emplace_back(detour_vertex.x, detour_vertex.y, ActionType::RUN);
        generated_prefix.back().strict_arrival = true;
    }
    else if (!AppendGeneratedNavmeshWaypoints(
                 param_,
                 position_->zone_id,
                 *route,
                 generated_prefix,
                 /*include_goal=*/false,
                 emit_interior_corners,
                 /*strict_segment_breaks=*/false)) {
        LogWarn << "Dynamic navmesh overlay skipped: generated path is unusable." << VAR(reason) << VAR(continue_index)
                << VAR(route->path.points.size());
        return false;
    }
    if (generated_prefix.empty() && std::hypot(anchor.x - position_->x, anchor.y - position_->y) > ArrivalBandForStartupBypass(anchor)) {
        generated_prefix.emplace_back(position_->x, position_->y, ActionType::RUN);
        LogInfo << "Dynamic overlay seeded leading node to avoid single-point path." << VAR(reason) << VAR(continue_index)
                << VAR(position_->x) << VAR(position_->y);
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
    const size_t planned_points = route->path.points.size();
    LogInfo << "Dynamic navmesh overlay selected." << VAR(reason) << VAR(use_detour) << VAR(continue_index) << VAR(generated_count)
            << VAR(planned_points) << VAR(detour_vertex.x) << VAR(detour_vertex.y) << VAR(anchor.x) << VAR(anchor.y);
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
    return TryApplyDynamicOverlayToAnchor(
        reason,
        anchor->first,
        anchor->second,
        use_detour,
        route_heading,
        /*emit_interior_corners=*/false);
}

// 上索点是必到的语义点, 重规划总是把人瞄回它, 所以那根架子要是根本走不到(导入的坐标跟实际
// 对不上, 或者脚下那片面接不上), 就会一路重规划到楔死超时把整趟导航掐掉。同一个上索点试够了
// 就当它够不着, 丢掉这条链改走路 —— 索是捷径不是必经之路。
bool NavigationStateMachine::GiveUpUnreachableZipline(const char* reason)
{
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    if (!anchor || anchor->second.action != ActionType::ZIPLINE) {
        return false;
    }
    ZiplineApproachState& approach = runtime_state_.zipline_approach;
    if (approach.anchor_index != anchor->first) {
        approach.anchor_index = anchor->first;
        approach.replans = 0;
        approach.press_missed = false;
    }
    if (++approach.replans <= kZiplineApproachReplanBudget) {
        return false;
    }

    LogWarn << "Zipline mount tower unreachable after repeated replans; walking instead." << VAR(reason) << VAR(approach.replans)
            << VAR(anchor->second.x) << VAR(anchor->second.y) << VAR(position_->x) << VAR(position_->y);
    runtime_state_.dynamic_replan_requested = false;
    semantic_nodes::AbandonZipline(
        BuildSemanticContext(
            action_wrapper_,
            position_provider_,
            session_,
            motion_controller_,
            action_executor_,
            position_,
            &runtime_state_,
            maa_context_),
        "zipline_unreachable",
        reason);
    return true;
}

bool NavigationStateMachine::HandleDynamicReplanRequest(const char* reason)
{
    if (GiveUpUnreachableZipline(reason)) {
        return true;
    }
    if (TryApplyDynamicOverlayToNextAnchor(reason, false)) {
        return true;
    }

    // 单次重规划失败不终止导航:退回当前路线继续走,持续无进展由卡死恢复的各级超时收口。
    // 路线没有被替换,只清跟随进度,起步门不能跟着一起清掉
    runtime_state_.dynamic_replan_requested = false;
    runtime_state_.route.ResetTracking();
    session_->ResetProgress();
    LogWarn << "Dynamic navmesh replan unavailable; falling back to current route." << VAR(reason) << VAR(position_->x) << VAR(position_->y)
            << VAR(position_->zone_id);
    SelectPhaseForCurrentWaypoint("dynamic_replan_fallback");
    return true;
}

bool NavigationStateMachine::PlanCrossTierEscapeCorridorFromHere(const char* reason)
{
    const double heading = NaviMath::NormalizeAngle(position_->angle);
    const std::vector<Waypoint>& path = session_->current_path();
    for (size_t index = session_->current_node_idx(); index < path.size(); ++index) {
        const Waypoint& candidate = session_->CurrentPathAt(index);
        if (!candidate.HasPosition()) {
            continue;
        }
        const std::optional<size_t> continue_index = session_->CanonicalIndexAtCurrentPath(index);
        if (!continue_index) {
            continue; // a generated overlay waypoint (no canonical index) is not a rejoin target
        }
        if (TryApplyDynamicOverlayToAnchor(
                reason,
                *continue_index,
                candidate,
                /*use_detour=*/false,
                heading,
                /*emit_interior_corners=*/true)) {
            runtime_state_.cross_tier_escape.goal_x = candidate.x;
            runtime_state_.cross_tier_escape.goal_y = candidate.y;
            LogInfo << "Cross-tier escape corridor planned." << VAR(reason) << VAR(position_->zone_id) << VAR(position_->x)
                    << VAR(position_->y) << VAR(*continue_index) << VAR(candidate.x) << VAR(candidate.y);
            return true;
        }
    }
    LogInfo << "Cross-tier escape: on a wrong tier but no reachable authored waypoint." << VAR(reason) << VAR(position_->zone_id)
            << VAR(position_->x) << VAR(position_->y);
    return false;
}

bool NavigationStateMachine::TryEnterCrossTierEscape()
{
    // Positive-ID: the fresh fix must sit on a real FLOORED tier (not a geometry / "…_Base" overview zone).
    const float tier_floor = NavmeshFloorYForZone(param_, position_->zone_id);
    if (tier_floor <= navmesh::kBaseNavFloorYValidMin) {
        LogInfo << "Cross-tier escape declined: zone is not a floored tier." << VAR(position_->zone_id) << VAR(tier_floor)
                << VAR(position_->x) << VAR(position_->y);
        return false;
    }
    if (ResolveCurrentAnchor(session_, *position_)) {
        LogInfo << "Cross-tier escape declined: route has a zone-compatible anchor here (normal travel)." << VAR(position_->zone_id)
                << VAR(position_->x) << VAR(position_->y);
        return false;
    }

    if (!PlanCrossTierEscapeCorridorFromHere("crosstier_escape")) {
        return false; // on a wrong tier but no reachable authored waypoint; defer to loss handling
    }
    runtime_state_.cross_tier_escape.active = true;
    runtime_state_.cross_tier_escape.anchor_zone = position_->zone_id;
    session_->ResetHardProgress();
    LogInfo << "Cross-tier escape engaged: routing out of a wrong tier via navmesh." << VAR(position_->zone_id) << VAR(position_->x)
            << VAR(position_->y) << VAR(runtime_state_.cross_tier_escape.goal_x) << VAR(runtime_state_.cross_tier_escape.goal_y);
    return true;
}

bool NavigationStateMachine::ExecutePhysicalUnstick(double stuck_heading)
{
    LateralBypassState& unstick = runtime_state_.bypass;
    // Relocated since the last unstick => a new spot; restart the bearing rotation.
    if (unstick.active && std::hypot(position_->x - unstick.origin.x, position_->y - unstick.origin.y) > kUnstickResetDistanceM) {
        unstick.Reset();
    }
    if (!unstick.active) {
        unstick.active = true;
        unstick.origin = *position_;
        unstick.count = 0;
    }

    double distance = kUnstickMinDistanceM;
    const std::optional<navmesh::WorldPoint> target = PlanUnstickTarget(param_, *position_, stuck_heading, unstick.count, &distance);
    if (!target) {
        ++unstick.count;
        LogWarn << "Physical unstick: no on-mesh escape bearing found." << VAR(stuck_heading) << VAR(unstick.count);
        return false;
    }

    const double target_heading = NaviMath::CalcTargetRotation(position_->x, position_->y, target->x, target->y);
    const double heading_delta = NaviMath::CalcDeltaRotation(position_->angle, target_heading);
    motion_controller_->SetForwardState(false);
    utils::SleepFor(kStopWaitMs);
    int units = static_cast<int>(std::lround(heading_delta * action_wrapper_->DefaultTurnUnitsPerDegree()));
    if (units == 0) {
        units = heading_delta > 0.0 ? 1 : -1;
    }
    action_wrapper_->SendViewDeltaSync(units, 0);

    const NaviPosition step_start = *position_;
    double moved = 0.0;
    for (int pulse = 0; pulse < kUnstickMaxPulses; ++pulse) {
        action_wrapper_->PulseForwardSync(kUnstickPulseMs);
        // Stop the moment tracking goes blind (held / black screen = a likely river fall) so we don't keep
        // driving forward into the water; the next tick's loss handling takes over.
        if (!CaptureCurrentPosition(false) || position_provider_->LastCaptureWasHeld() || position_provider_->LastCaptureWasBlackScreen()
            || !position_->valid) {
            break;
        }
        moved = std::hypot(position_->x - step_start.x, position_->y - step_start.y);
        if (moved >= distance * kUnstickSuccessFraction) {
            break;
        }
    }
    motion_controller_->SetForwardState(false);
    ++unstick.count;
    const bool dislodged = moved >= distance * kUnstickSuccessFraction;
    LogInfo << "Physical unstick step executed." << VAR(distance) << VAR(moved) << VAR(dislodged) << VAR(unstick.count)
            << VAR(target_heading) << VAR(target->x) << VAR(target->y);

    if (TryApplyDynamicOverlayToNextAnchor("recovery_unstick_replan", false)) {
        session_->ResetProgress();
        SelectPhaseForCurrentWaypoint("recovery_unstick_replan");
        return true;
    }
    runtime_state_.route.ResetTracking();
    runtime_state_.dynamic_replan_requested = false;
    runtime_state_.nav_run_dirty = true;
    session_->ResetProgress();
    SelectPhaseForCurrentWaypoint("recovery_physical_unstick");
    return true;
}

bool NavigationStateMachine::TickNavigate()
{
    const auto tick_started_at = std::chrono::steady_clock::now();
    const int64_t tick_gap_ms =
        runtime_state_.flow.last_tick_started_at.time_since_epoch().count() > 0
            ? std::chrono::duration_cast<std::chrono::milliseconds>(tick_started_at - runtime_state_.flow.last_tick_started_at).count()
            : 0;
    runtime_state_.flow.last_tick_started_at = tick_started_at;
    ++runtime_state_.flow.tick_seq;

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

    const auto capture_started_at = std::chrono::steady_clock::now();
    const bool position_captured = CaptureCurrentPosition(false);
    const int64_t capture_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - capture_started_at).count();
    if (!position_captured) {
        return HandleLocalizationLoss();
    }
    {
        LocalizationLossState& loss = runtime_state_.localization_loss;
        if (loss.started_at != std::chrono::steady_clock::time_point {}) {
            const auto loss_ms =
                std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - loss.started_at).count();
            const bool armed = ArmRiverFallRecoveryIfBlackScreenLoss("normal_reacquire");
            LogInfo << "Localization recovered via normal tracking." << VAR(loss_ms) << VAR(loss.saw_black_screen) << VAR(armed)
                    << VAR(position_->x) << VAR(position_->y);
            loss.Reset();
            if (armed) {
                return true;
            }
        }
        else {
            loss.Reset();
        }
    }

    if (runtime_state_.cross_tier_escape.active) {
        const double distance_to_goal =
            std::hypot(position_->x - runtime_state_.cross_tier_escape.goal_x, position_->y - runtime_state_.cross_tier_escape.goal_y);
        const bool on_floorless_zone = NavmeshFloorYForZone(param_, position_->zone_id) <= navmesh::kBaseNavFloorYValidMin;
        if (distance_to_goal <= kCrossTierEscapeArrivalM && on_floorless_zone) {
            LogInfo << "Cross-tier escape reached the rejoin point on the base floor; resuming authored route." << VAR(distance_to_goal)
                    << VAR(position_->zone_id) << VAR(position_->x) << VAR(position_->y);
            runtime_state_.cross_tier_escape.Reset();
        }
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
    // Gap between the screencap this tick's fix came from and the decision below: the locate itself plus the work
    // in between. Every successful capture restamps, held ones included, so how stale the coordinates themselves
    // are is held_fix_streak, not this.
    const int64_t fix_age_ms = position_->timestamp.time_since_epoch().count() > 0
                                   ? std::chrono::duration_cast<std::chrono::milliseconds>(now - position_->timestamp).count()
                                   : 0;
    const int held_fix_streak = position_provider_->HeldFixStreak();

    const size_t node_idx_before_tracking = session_->current_node_idx();
    RouteTrackingState route = RouteTracker::Update(session_, &runtime_state_.route, *position_);
    if (session_->current_node_idx() != node_idx_before_tracking) {
        runtime_state_.recovery.Reset();
    }

    NavRunTickResult nav_run_result;
    std::optional<size_t> nav_run_anchor_index;
    if (route.valid && session_->HasCurrentWaypoint()) {
        const Waypoint& current_waypoint = session_->CurrentWaypoint();
        if (current_waypoint.HasPosition() && current_waypoint.action == ActionType::RUN) {
            // Strict RUN must be hit precisely, so its corridor anchor is the waypoint itself;
            // continuous RUN can lookahead through to the next semantic anchor.
            std::optional<DynamicAnchor> nav_run_anchor;
            if (current_waypoint.RequiresStrictArrival()) {
                nav_run_anchor =
                    DynamicAnchor { session_->CanonicalIndexAtCurrent().value_or(kUnaddressableAnchorIndex), current_waypoint };
            }
            else {
                nav_run_anchor = ResolveCurrentAnchor(session_, *position_);
            }
            if (nav_run_anchor) {
                nav_run_anchor_index = nav_run_anchor->first;
                nav_run_result =
                    nav_run_controller_
                        .tick(session_, &runtime_state_, *position_, route, param_, nav_run_anchor->first, nav_run_anchor->second, now);
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
            if (!corridor_passed.HasPosition() || corridor_passed.action != ActionType::RUN || corridor_passed.RequiresStrictArrival()) {
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
            // Passing corridor waypoints is discrete forward progress the thrash fast-fail must honour: a
            // long leg with several transient losses would otherwise reach the re-acquire cap and wrongly
            // fail. A stationary recover<->re-lose storm passes none, so it stays storm-proof.
            runtime_state_.global_reacquire_streak = 0;
            route = RouteTracker::Update(session_, &runtime_state_.route, *position_);
        }
    }

    const double effective_progress = ObserveNavigationProgress(route, nav_run_result.straight_to_anchor, nav_run_anchor_index, now);
    // Cross-tier escape: follow the ONE planned corridor (arrival above is the success exit). Fast-fail when the
    // corridor makes no genuine progress for too long. Keys on the hard-progress clock, which the escape's own
    // overlay re-applies can't reset, so it trips only on a continuously stuck (walled/unfollowable) escape, never
    // a slow-but-advancing one. The orthogonal recover<->re-lose thrash is caught by the re-acquire streak above.
    if (runtime_state_.cross_tier_escape.active && session_->HardStalledMs(now) >= kCrossTierEscapeHardStallMs) {
        const double goal_dist =
            std::hypot(position_->x - runtime_state_.cross_tier_escape.goal_x, position_->y - runtime_state_.cross_tier_escape.goal_y);
        runtime_state_.cross_tier_escape.Reset();
        return FailNavigation(
            "crosstier_escape_stalled",
            "Cross-tier escape made no corridor progress (walled or unfollowable); terminating so the pipeline can retry.",
            goal_dist,
            0.0,
            session_->HardStalledMs(now));
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
            LogInfo << "Dynamic recovery escaped obstacle." << VAR(recovery_zone_changed) << VAR(recovery_displacement);
            return ResumeAfterEscape("recovery_escape");
        }
    }
    const int64_t stalled_ms = session_->StalledMs(now);
    // Warm the device probe from the first stalled tick, so by the time the recovery ladder runs its answer is
    // already latched and reading it costs nothing.
    device_recovery_.UpdateFeeding(stalled_ms, runtime_state_.recovery_escalation.device_attempt_count < kRecoveryDeviceAttempts);

    if (!route.valid) {
        if (degraded_fix) {
            motion_controller_->SetForwardState(false);
        }
        utils::SleepFor(kTargetTickMs);
        return true;
    }

    const Waypoint waypoint = session_->CurrentWaypoint();
    if (TryRunPromptSubtaskWhileWalking(route)) {
        return true;
    }

    if (TryZiplineMountPrompt(waypoint, route)) {
        runtime_state_.semantic.zipline_prompt_probe = true;
        const semantic_nodes::Result prompt_result = semantic_nodes::HandleArrivalSemantic(semantic_ctx, waypoint, route.waypoint_distance);
        runtime_state_.semantic.zipline_prompt_probe = false;
        if (prompt_result.request_failure) {
            return FailNavigation(
                prompt_result.failure_reason,
                prompt_result.failure_log_message,
                route.waypoint_distance,
                0.0,
                stalled_ms);
        }
        if (prompt_result.consumed) {
            return true;
        }
    }

    double arrival_distance = route.arrival_band;
    if (waypoint.action == ActionType::PORTAL) {
        arrival_distance = std::max(arrival_distance, kPortalCommitDistance);
    }
    // 提示驱动的点判定圈收窄了, 真站不上去(硬性无进展这么久)就放回常规值, 别多出一种卡死
    else if (waypoint.StopsOnPromptDetection() && session_->HardStalledMs(now) > kCollectArrivalRelaxMs) {
        const double relaxed = waypoint.ArrivalBand(kMeasurementDefaultPositionQuantum, /*relax_tight_band=*/true);
        if (relaxed > arrival_distance && route.waypoint_distance <= relaxed) {
            LogInfo << "Prompt-point arrival band relaxed after no progress." << VAR(session_->current_node_idx())
                    << VAR(route.waypoint_distance) << VAR(arrival_distance) << VAR(relaxed);
        }
        arrival_distance = std::max(arrival_distance, relaxed);
    }
    // 上索认的是跟采集一样的交互提示 —— 面板给的是离身位最近的那台设备, 按常规判定圈停下时人还
    // 差着够不着架子, 所以判定圈按同一套收紧。只收链首那一次: 链中间的点人是从索上落到台面上的,
    // 不用再认提示, 而台面上迈不动步, 收紧了就是站在原地等超时。真收不拢也放回去, 别多出一种卡死
    if (waypoint.action == ActionType::ZIPLINE && !runtime_state_.semantic.zipline_mounted
        && session_->HardStalledMs(now) <= kCollectArrivalRelaxMs) {
        arrival_distance = std::min(arrival_distance, kCollectArrivalBandWu);
        // 按空一次就当人站得还不够近: 收紧了接着走(有备用站位的已经改瞄它了), 原地重按只会得到
        // 同一个答案。收不拢由上索点自己的重规划预算收口, 用完就退索走路
        if (runtime_state_.zipline_approach.press_missed) {
            arrival_distance = std::min(arrival_distance, kZiplineRestandBandWu);
        }
    }
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

    if (runtime_state_.river_fall.pending) {
        RiverFallRecoveryState& rf = runtime_state_.river_fall;
        if (session_->HardStalledMs(now) > kRiverFallRecoveryTimeoutMs) {
            return FailNavigation(
                "river_fall_recovery_timeout",
                "River-fall recovery made no net progress past the timeout; terminating navigation.",
                route.progress_distance,
                NaviMath::NormalizeAngle(route.route_heading - current_heading),
                stalled_ms);
        }
        const double rf_displacement = std::hypot(position_->x - rf.anchor_pos.x, position_->y - rf.anchor_pos.y);
        if (rf_displacement >= kRiverFallRecoveryClearDistance) {
            rf.Reset();
            LogInfo << "River-fall recovery cleared; resuming navigation." << VAR(rf_displacement) << VAR(current_heading);
            return ResumeAfterEscape("river_fall_recovered");
        }
        motion_controller_->SetForwardState(false);
        // 站定两秒再动。上岸那一瞬的箭头读数是最不可信的(尖端会翻转、追踪还在 hold), 拿它算转身
        // 就是赌运气 —— 三次落水里有一次读成 -1°, 目标算出来正好等于当前朝向, 等于没转就往前走。
        if (!rf.settled) {
            rf.settled = true;
            utils::SleepFor(kRiverFallRecoverySettleMs);
            LogInfo << "River-fall recovery settling before the about-face." << VAR(rf_displacement) << VAR(position_->x)
                    << VAR(position_->y);
            return true;
        }
        // 180° 只发一次, 发完就认。之前是每拍拿转到一半的箭头重算误差再补发, 转身还没兑现就先迈了步,
        // 于是每一拍都朝着还没转完的方向往水里走 —— 两次再落水都紧跟着恢复自己的前进脉冲。
        if (!rf.turned) {
            rf.turned = true;
            rf.water_heading = current_heading;
            const int turn_units = static_cast<int>(std::lround(180.0 * action_wrapper_->DefaultTurnUnitsPerDegree()));
            action_wrapper_->SendViewDeltaSync(turn_units, 0);
            utils::SleepFor(kWaitAfterFirstTurnMs);
            LogInfo << "River-fall recovery about-face issued." << VAR(rf.water_heading) << VAR(turn_units) << VAR(rf_displacement);
        }
        // 视角转完了, 角色朝向要迈一步才会跟上(见 navigator-turn-requires-forward-step), 而这一步是
        // 按相机方向走的, 所以它离水而去。
        action_wrapper_->PulseForwardSync(kRiverFallRecoveryPulseMs);
        motion_controller_->SetForwardState(false);
        LogInfo << "River-fall recovery inland pulse." << VAR(current_heading) << VAR(rf_displacement) << VAR(position_->x)
                << VAR(position_->y);
        return true;
    }

    // Off-route wedge watchdog (see kOffRouteWedge* in navi_config.h). Only corridor (non-strict RUN) waypoints,
    // where on_route is meaningful; the stall clocks above are fed corridor progress and miss a pinned-off-route
    // cursor. Fed straight-line distance, so the timer only grows while genuinely off-route with no inward gain.
    // A non-finite cross_track means the projection could not be computed at all, not that the agent left the
    // route, so it must not arm the watchdog; the no-progress clocks still cover that case.
    if (session_->phase() == NaviPhase::Navigate && waypoint.action == ActionType::RUN && !waypoint.RequiresStrictArrival()
        && !route.on_route && std::isfinite(route.cross_track) && !runtime_state_.cross_tier_escape.active) {
        OffRouteWedgeState& wedge = runtime_state_.offroute;
        const double progress_epsilon = std::max(kNoProgressDistanceEpsilon, kMeasurementDefaultPositionQuantum);
        if (!wedge.active || route.progress_distance + progress_epsilon < wedge.best_distance) {
            wedge.active = true;
            wedge.best_distance = route.progress_distance;
            wedge.since = now;
        }
        const int64_t wedge_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - wedge.since).count();
        if (wedge_ms >= kOffRouteWedgeFailMs) {
            return FailNavigation(
                "offroute_wedge_timeout",
                "Off-route with no route progress past the wedge timeout; terminating so the pipeline can retry.",
                route.progress_distance,
                NaviMath::NormalizeAngle(route.route_heading - current_heading),
                stalled_ms);
        }
        const bool replan_cooling =
            wedge.last_replan_at.time_since_epoch().count() > 0
            && std::chrono::duration_cast<std::chrono::milliseconds>(now - wedge.last_replan_at).count() < kOffRouteWedgeReplanCooldownMs;
        if (wedge_ms >= kOffRouteWedgeReplanMs && !replan_cooling) {
            wedge.last_replan_at = now;
            LogWarn << "Off-route wedge detected; replanning from current position." << VAR(wedge_ms) << VAR(route.waypoint_distance)
                    << VAR(route.cross_track) << VAR(session_->current_node_idx());
            HandleDynamicReplanRequest("offroute_wedge");
            return true;
        }
    }
    else {
        runtime_state_.offroute.Reset();
    }

    // Near a strict-arrival goal only the *detour* is unsafe (it routes away from the exact point);
    // a jump is still a safe nudge, so recovery is allowed to enter here and the suppression is
    // applied to the detour step alone, below.
    const bool near_strict_goal =
        waypoint.RequiresStrictArrival() && route.waypoint_distance <= arrival_distance + kCloseGoalDetourSuppressSlack;
    // "Too close to bother recovering" is measured on the same signal the stall clock runs on: while NavRun
    // steers, corridor remaining is the true distance left, and the serial waypoint is a breadcrumb that can sit
    // a pixel away with the whole leg still ahead. Reading the breadcrumb here closed the gate on an agent pinned
    // beside it — arrival needs startup motion it cannot make, recovery saw "already there", nothing ran.
    // A held forward that has been re-sent twice with the fix never leaving its quantum is the same evidence the
    // stall clock is still gathering, only earlier and stronger: aimed, commanding no turn, and going nowhere.
    // Waiting out the remaining wall clock just spends it walking into the obstacle, so it opens recovery too.
    const bool forward_hold_futile = runtime_state_.flow.futile_forward_reasserts >= kForwardHoldFutileReassertsBeforeRecovery;
    const bool should_try_recovery =
        session_->phase() == NaviPhase::Navigate && (stalled_ms >= kObstacleRecoveryMinTriggerMs || forward_hold_futile)
        && (effective_progress > kNoProgressMinDistance || waypoint.RequiresStrictArrival()) && !runtime_state_.cross_tier_escape.active;
    if (should_try_recovery) {
        const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
        if (anchor) {
            DynamicRecoveryState& recovery = runtime_state_.recovery;
            RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
            if (!recovery.active || recovery.anchor_index != anchor->first) {
                recovery.Reset();
                recovery.active = true;
                recovery.anchor_pos = *position_;
                recovery.started_at = now;
                recovery.anchor_index = anchor->first;
            }
            if (escalation.anchor_index != anchor->first) {
                // Handing a new anchor an attempt requires a fresh look: a hit latched while fighting the
                // previous one describes a frame from before the path was renumbered. Not on the first anchor,
                // where nothing has been fought yet and the probe has been warming since the stall began.
                const bool fought_another_anchor = escalation.anchor_index != std::numeric_limits<size_t>::max();
                escalation.Reset();
                escalation.anchor_index = anchor->first;
                if (fought_another_anchor) {
                    device_recovery_.ForgetObservation();
                }
            }

            // Measure the recovery timeout from the session hard-progress clock, not this episode's
            // recovery.started_at. Every small jump "escape" goes through ResumeAfterEscape, which
            // re-stamps started_at and the ordinary stall clock — so a started_at-based elapsed never grows and
            // the agent can thrash in place indefinitely (observed: ~6 min, 1094 jumps, 0 timeouts). The hard
            // clock only advances on genuine net progress toward the waypoint, so a livelock now trips the
            // emergency stop after kDynamicRecoveryTotalTimeoutMs of real no-progress.
            const int64_t recovery_elapsed_ms = session_->HardStalledMs(now);
            if (recovery_elapsed_ms > kDynamicRecoveryTotalTimeoutMs) {
                // 卡在索边收不拢, 先退索走路, 别直接判导航失败。丢链会把硬时钟归零, 再超时时手上
                // 已经是走路点了, 那一次才是真失败, 所以这里不会绕回来。
                if (waypoint.action == ActionType::ZIPLINE) {
                    LogWarn << "Recovery timed out at a zipline tower; dropping the chain and walking." << VAR(recovery_elapsed_ms)
                            << VAR(waypoint.x) << VAR(waypoint.y) << VAR(position_->x) << VAR(position_->y);
                    semantic_nodes::AbandonZipline(
                        BuildSemanticContext(
                            action_wrapper_,
                            position_provider_,
                            session_,
                            motion_controller_,
                            action_executor_,
                            position_,
                            &runtime_state_,
                            maa_context_),
                        "zipline_recovery_timeout",
                        "stuck at the tower after recovery ran out");
                    return true;
                }
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
                recovery.last_replan_at = now;

                // The device removal runs ahead of the jump, not instead of it: if the agent is still pinned
                // afterwards the jump below fires this same tick and the ladder keeps climbing. Every outcome
                // but NotAttempted has run the subtask, so it spends the attempt either way.
                const DeviceRemovalOutcome device_outcome =
                    device_recovery_.TryRemove(route, waypoint, escalation.device_attempt_count < kRecoveryDeviceAttempts);
                if (device_outcome != DeviceRemovalOutcome::NotAttempted) {
                    ++escalation.device_attempt_count;
                }
                if (device_outcome == DeviceRemovalOutcome::Escaped) {
                    return ResumeAfterEscape("recovery_device_move_escape");
                }
                if (device_outcome == DeviceRemovalOutcome::NeedsFreshFix) {
                    return true;
                }

                ++escalation.jump_attempt_count;
                const NaviPosition jump_start = *position_;
                LogInfo << "Dynamic recovery jump pulse issued." << VAR(escalation.jump_attempt_count)
                        << VAR(escalation.detour_attempt_count);
                motion_controller_->SetAction(LocalDriverAction::JumpForward, true);
                utils::SleepFor(kActionJumpSettleMs);
                motion_controller_->SetForwardState(false);
                if (!CaptureCurrentPosition(false) || position_provider_->LastCaptureWasHeld()
                    || position_provider_->LastCaptureWasBlackScreen() || !position_->valid) {
                    LogWarn << "Dynamic recovery waiting for post-jump local tracking fix." << VAR(stalled_ms)
                            << VAR(escalation.jump_attempt_count);
                    utils::SleepFor(kTargetTickMs);
                    return true;
                }

                const bool jump_zone_changed =
                    !jump_start.zone_id.empty() && !position_->zone_id.empty() && jump_start.zone_id != position_->zone_id;
                const double jump_displacement = std::hypot(position_->x - jump_start.x, position_->y - jump_start.y);
                const double jump_waypoint_distance = std::hypot(waypoint.x - position_->x, waypoint.y - position_->y);
                const bool jump_made_progress = jump_waypoint_distance + kNoProgressDistanceEpsilon < route.waypoint_distance;
                const bool jump_moved_forward = jump_displacement >= kDynamicRecoveryResetDistance * 0.5 && jump_made_progress;
                if (jump_zone_changed || jump_displacement >= kDynamicRecoveryResetDistance || jump_moved_forward) {
                    LogInfo << "Dynamic recovery jump escaped obstacle." << VAR(jump_zone_changed) << VAR(jump_displacement)
                            << VAR(jump_moved_forward);
                    return ResumeAfterEscape("recovery_jump_escape");
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
                if (post_jump_anchor->first != escalation.anchor_index) {
                    escalation.Reset();
                    escalation.anchor_index = post_jump_anchor->first;
                    escalation.jump_attempt_count = 1;
                }

                // The jump pulse above runs every recovery tick, so a fresh stall always tries to hop free
                // first; the rest of the ladder only opens after the jump has failed
                // kRecoveryJumpAttemptsBeforeDetour times for this anchor. Of the two escalations only the
                // detour is unsafe next to a strict goal — it re-routes to a bypass vertex and gives up the
                // exact point. The physical unstick just dislodges sideways and re-approaches the same anchor,
                // so it stays available there; otherwise a stall inside the strict band has nothing left but
                // the jump until the hard-progress timeout.
                const bool escalated = escalation.jump_attempt_count >= kRecoveryJumpAttemptsBeforeDetour;
                const bool detour_allowed = escalated && !near_strict_goal;
                if (detour_allowed && escalation.detour_attempt_count < kRecoveryDetourAttemptsBeforeUnstick) {
                    ++escalation.detour_attempt_count;
                    if (TryApplyDynamicOverlayToAnchor(
                            "recovery_navmesh_detour",
                            post_jump_anchor->first,
                            post_jump_anchor->second,
                            true,
                            route.route_heading)) {
                        SelectPhaseForCurrentWaypoint("recovery_navmesh_detour");
                        return true;
                    }
                    LogWarn << "Dynamic recovery detour attempt failed; switching to physical unstick."
                            << VAR(escalation.detour_attempt_count) << VAR(escalation.jump_attempt_count) << VAR(post_jump_anchor->first)
                            << VAR(route.progress_distance) << VAR(stalled_ms);
                }
                if (escalated && ExecutePhysicalUnstick(route.route_heading)) {
                    return true;
                }
                utils::SleepFor(kTargetTickMs);
                return true;
            }
        }
    }

    const double effective_route_heading = nav_run_result.has_corridor_heading ? nav_run_result.corridor_heading : route.route_heading;

    // Heading observed to have moved since the last turn was sent, and how far that lands from the commanded
    // delta. It is the whole observed change, not the turn in isolation: forward motion and camera follow are in
    // there too. Only ticks that really send a turn stamp the reference below, so on a tick that sends nothing
    // these keep reporting the previous command as it settles — the elapsed field tells the two apart.
    double turn_achieved_deg = 0.0;
    double turn_residual_deg = 0.0;
    int64_t turn_elapsed_ms = 0;
    if (runtime_state_.steering_rate.has_cmd) {
        turn_achieved_deg = NaviMath::NormalizeAngle(current_heading - runtime_state_.steering_rate.cmd_heading_deg);
        turn_residual_deg = NaviMath::NormalizeAngle(turn_achieved_deg - runtime_state_.steering_rate.cmd_delta_deg);
        turn_elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.steering_rate.cmd_at).count();
    }

    const uint64_t tick_seq = runtime_state_.flow.tick_seq;

    double heading_rate_deg = 0.0;
    double heading_rate_raw_delta_deg = 0.0;
    int64_t heading_rate_gap_ms = 0;
    uint64_t heading_rate_gap_ticks = 0;
    if (runtime_state_.steering_rate.has_prev) {
        heading_rate_raw_delta_deg = NaviMath::NormalizeAngle(current_heading - runtime_state_.steering_rate.prev_heading_deg);
        heading_rate_gap_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.steering_rate.at).count();
        heading_rate_gap_ticks = tick_seq - runtime_state_.steering_rate.at_tick;
        const bool heading_changed = std::abs(heading_rate_raw_delta_deg) > kSteeringHeadingChangeEpsilonDeg;
        // Staleness is counted in ticks, not milliseconds: the reference goes stale after so many missed chances
        // to observe a change, and slow frame capture stretches those chances out without making them fewer. A
        // wall-clock cap threw the rate away on exactly the slow loops that need it most.
        if (heading_changed && heading_rate_gap_ms > 0 && heading_rate_gap_ticks <= kSteeringRateMaxGapTicks) {
            heading_rate_deg =
                heading_rate_raw_delta_deg * static_cast<double>(kSteeringRateReferenceMs) / static_cast<double>(heading_rate_gap_ms);
        }
        if (heading_changed) {
            runtime_state_.steering_rate.prev_heading_deg = current_heading;
            runtime_state_.steering_rate.at = now;
            runtime_state_.steering_rate.at_tick = tick_seq;
        }
    }
    else {
        runtime_state_.steering_rate.prev_heading_deg = current_heading;
        runtime_state_.steering_rate.at = now;
        runtime_state_.steering_rate.at_tick = tick_seq;
        runtime_state_.steering_rate.has_prev = true;
    }

    // Settle the turn already sent against what the heading actually moved, so the controller discounts what is
    // still in flight instead of commanding the same error again. Only motion toward the debt pays it down, and
    // an unpaid debt expires, so a swallowed drag can never leave steering suppressed against a turn never coming.
    // Walking turns at about half rate: a jogging-sized lifetime expires mid-turn and the loop re-commands it.
    const int64_t pending_lifetime_ms =
        walk_mode_.engaged() ? kSteeringPendingLifetimeMs * kWalkModeSlowFactor : kSteeringPendingLifetimeMs;
    SteeringRateState& steering_rate = runtime_state_.steering_rate;
    if (steering_rate.pending_turn_deg != 0.0) {
        const int64_t pending_age_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - steering_rate.cmd_at).count();
        if (pending_age_ms >= pending_lifetime_ms) {
            steering_rate.pending_turn_deg = 0.0;
        }
        else {
            const double landed = NaviMath::NormalizeAngle(current_heading - steering_rate.pending_ref_heading_deg);
            const double owed = std::abs(steering_rate.pending_turn_deg);
            steering_rate.pending_turn_deg = std::clamp(steering_rate.pending_turn_deg - landed, -owed, owed);
        }
    }
    steering_rate.pending_ref_heading_deg = current_heading;

    const double heading_error = NaviMath::NormalizeAngle(effective_route_heading - current_heading);
    const SteeringCommand steering = SteeringController::Update(
        heading_error,
        heading_rate_deg,
        motion_controller_->IsMovingForward(),
        steering_rate.turn_latch_sign,
        steering_rate.pending_turn_deg);

    motion_controller_->SetForwardState(true);

    double issued_delta_deg = 0.0;
    int64_t steer_send_ms = 0;
    if (steering.issued) {
        const TurnCommandResult steering_result = motion_controller_->ApplySteering(steering.yaw_delta_deg, tick_gap_ms);
        steer_send_ms = steering_result.send_ms;
        if (steering_result.issued) {
            issued_delta_deg = steering_result.issued_delta_degrees;
        }
    }
    if (issued_delta_deg != 0.0) {
        steering_rate.cmd_heading_deg = current_heading;
        steering_rate.cmd_delta_deg = issued_delta_deg;
        steering_rate.cmd_at = now;
        steering_rate.has_cmd = true;
        steering_rate.pending_turn_deg += issued_delta_deg;
    }
    // 只有走到这里的拍才记账。自救、绕障、语义转向在上面就返回了，留下的拍号缺口正好标出账不连续。
    sensitivity::RecordTick(maa_context_, tick_seq, current_heading, issued_delta_deg, degraded_fix);

    // Closed the loop on the forward hold: the keydown goes out once on the transition, so a swallowed one
    // strands the agent aimed correctly and walking nowhere until an obstacle recovery notices seconds later.
    // Only counts ticks that steered nowhere, so turning in place and braked arrivals never trigger it. Stands
    // down once recovery is active: it owns the movement keys from then on, its cooldown ticks fall through to
    // here, and a release/re-press dropped between two of its jump pulses would fight it. That leaves this the
    // early window only — kObstacleRecoveryMinTriggerMs away, which is the whole point of it.
    FlowState& flow = runtime_state_.flow;
    const bool moved = flow.has_last_steer_position
                       && std::hypot(position_->x - flow.last_steer_position.x, position_->y - flow.last_steer_position.y)
                              > kMeasurementDefaultPositionQuantum;
    // The re-send counter deliberately outlives recovery going active. Recovery clears the tick counter because it
    // owns the keys from then on, but clearing the evidence that opened the gate would close it again a tick later,
    // before the ladder got past its first jump. Motion or a turn is what makes the hold no longer the suspect.
    const bool hold_progressing = moved || issued_delta_deg != 0.0 || !motion_controller_->IsMovingForward();
    if (hold_progressing) {
        flow.futile_forward_reasserts = 0;
    }
    if (hold_progressing || runtime_state_.recovery.active) {
        flow.motionless_hold_ticks = 0;
    }
    else if (flow.has_last_steer_position) {
        ++flow.motionless_hold_ticks;
    }
    flow.last_steer_position = *position_;
    flow.has_last_steer_position = true;
    // At walking speed one tick's step sits under the position quantum, so a straight walk reads as motionless.
    // Widen the window rather than lower the threshold — the threshold is what rejects quantization noise.
    const int32_t hold_reassert_ticks = walk_mode_.engaged() ? kForwardHoldReassertTicks * kWalkModeSlowFactor : kForwardHoldReassertTicks;
    if (flow.motionless_hold_ticks >= hold_reassert_ticks) {
        ++flow.futile_forward_reasserts;
        LogWarn << "Forward hold produced no motion; re-sending it." << VAR(flow.motionless_hold_ticks) << VAR(heading_error)
                << VAR(flow.futile_forward_reasserts) << VAR(position_->x) << VAR(position_->y);
        motion_controller_->ReassertForward();
        flow.motionless_hold_ticks = 0;
    }

    const double lookahead_x = nav_run_result.lookahead_point.x;
    const double lookahead_y = nav_run_result.lookahead_point.y;
    const double projection_x = nav_run_result.projection_point.x;
    const double projection_y = nav_run_result.projection_point.y;
    const int nav_run_replan_reason = static_cast<int>(nav_run_result.replanned_with);
    LogDebug << "TickNavigate corridor target." << VAR(session_->current_node_idx()) << VAR(waypoint.x) << VAR(waypoint.y)
             << VAR(waypoint.RequiresStrictArrival()) << VAR(lookahead_x) << VAR(lookahead_y) << VAR(projection_x) << VAR(projection_y)
             << VAR(nav_run_result.remaining_to_anchor) << VAR(nav_run_result.straight_to_anchor)
             << VAR(nav_run_result.passed_run_waypoints) << VAR(nav_run_replan_reason) << VAR(route.along_track_remaining)
             << VAR(route.cross_track) << VAR(route.projection_anchor) << VAR(arrival_distance) << VAR(position_->zone_id)
             << VAR(stalled_ms);

    const int64_t tick_compute_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - tick_started_at).count();
    LogDebug << "TickNavigate steering decision." << VAR(tick_seq) << VAR(current_heading) << VAR(route.route_heading)
             << VAR(effective_route_heading) << VAR(nav_run_result.has_corridor_heading) << VAR(nav_run_result.cross_track)
             << VAR(nav_run_result.upcoming_turn_deg) << VAR(heading_rate_deg) << VAR(heading_rate_raw_delta_deg)
             << VAR(heading_rate_gap_ms) << VAR(heading_rate_gap_ticks) << VAR(heading_error) << VAR(steering.yaw_delta_deg)
             << VAR(issued_delta_deg) << VAR(turn_achieved_deg) << VAR(turn_residual_deg) << VAR(turn_elapsed_ms)
             << VAR(route.waypoint_distance) << VAR(route.on_route) << VAR(degraded_fix) << VAR(held_fix_streak) << VAR(capture_ms)
             << VAR(fix_age_ms) << VAR(tick_gap_ms) << VAR(tick_compute_ms);

    // 只有走到这里的拍才是完整的常规导航拍，语义节点、恢复、丢定位在上面就提前 return 了。
    latency::RecordStage(latency::Stage::Other, std::max<int64_t>(0, tick_compute_ms - capture_ms - steer_send_ms));
    latency::RecordTick(maa_context_, runtime_state_.flow.tick_seq, tick_gap_ms);

    // Keep sprint for travel but drop to walking near a prompt-driven point (cancelling an active sprint), so the
    // detection-stop lands before we overrun it. Must precede the sprint gate below.
    UpdatePromptSprintSuppression();

    // Balanced sprint gate: burst only when the agent already points down the corridor (heading aligned)
    // and no sharp turn is imminent within the scan window. No clearance term — it reads near zero on
    // bridges and locked sprint out entirely; alignment + upcoming-turn keep it from charging a corner.
    const bool heading_aligned_for_sprint = std::abs(heading_error) < kAutoSprintMaxHeadingErrorDeg;
    const bool corridor_turn_calm =
        !nav_run_result.has_corridor_heading || nav_run_result.upcoming_turn_deg < kAutoSprintMaxUpcomingTurnDeg;
    const bool turn_calm = heading_aligned_for_sprint && corridor_turn_calm;
    const bool target_requires_strict_arrival = waypoint.RequiresStrictArrival();
    const double sprint_remaining = nav_run_result.has_corridor_heading && std::isfinite(nav_run_result.remaining_to_anchor)
                                        ? nav_run_result.remaining_to_anchor
                                        : route.along_track_remaining;
    // Strict-arrival goals no longer block sprint outright; allow it until a braking buffer before the
    // waypoint so long straight runs into a strict goal still sprint, then brake in time to land.
    const bool has_strict_arrival_braking_room =
        !target_requires_strict_arrival || route.waypoint_distance > arrival_distance + kStrictArrivalSprintBrakeDistance;
    const bool allow_sprint =
        turn_calm && motion_controller_->SupportsSprint() && startup_grace_elapsed && param_.sprint_threshold > 0.0
        && has_strict_arrival_braking_room && sprint_remaining > param_.sprint_threshold
        && (runtime_state_.flow.last_auto_sprint_time.time_since_epoch().count() == 0
            || std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.flow.last_auto_sprint_time).count()
                   >= kAutoSprintCooldownMs);
    LogDebug << "TickNavigate sprint gate." << VAR(allow_sprint) << VAR(heading_aligned_for_sprint) << VAR(corridor_turn_calm)
             << VAR(startup_grace_elapsed) << VAR(has_strict_arrival_braking_room) << VAR(sprint_remaining) << VAR(param_.sprint_threshold);
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

bool NavigationStateMachine::ResumeAfterEscape(const char* reason)
{
    runtime_state_.flow.futile_forward_reasserts = 0;
    runtime_state_.flow.motionless_hold_ticks = 0;
    runtime_state_.recovery.Reset();
    runtime_state_.recovery_escalation.Reset();
    runtime_state_.route.ResetTracking();
    runtime_state_.dynamic_replan_requested = false;
    runtime_state_.nav_run_dirty = true;
    session_->ResetProgress();
    SelectPhaseForCurrentWaypoint(reason);
    return true;
}

double NavigationStateMachine::ObserveNavigationProgress(
    const RouteTrackingState& route,
    double straight_to_anchor,
    const std::optional<size_t>& anchor_index,
    const std::chrono::steady_clock::time_point& now)
{
    // On a RUN anchor the remaining distance to that anchor is the true progress signal — chasing
    // route.progress_distance would fire spurious stalls while the agent legitimately detours around
    // obstacles, and would read a breadcrumb waypoint underfoot as arrival while the anchor is far off.
    const bool from_anchor = std::isfinite(straight_to_anchor);
    const double effective_progress = from_anchor ? straight_to_anchor : route.progress_distance;
    if (!route.valid) {
        return effective_progress;
    }
    ProgressIdentityState& identity = runtime_state_.progress_identity;
    if (identity.from_anchor != from_anchor) {
        identity.from_anchor = from_anchor;
        if (identity.source_reset_idx != session_->current_node_idx()) {
            identity.source_reset_idx = session_->current_node_idx();
            session_->ResetProgress();
            session_->ResetHardProgress();
        }
    }
    session_->ObserveProgress(session_->current_node_idx(), effective_progress, now);
    // Feed the same signal to the hard watchdog, which recovery escapes can never reset (they only clear
    // the ordinary ObserveProgress clock). This is what lets the recovery timeout actually fire.
    const size_t hard_progress_key =
        from_anchor && anchor_index ? *anchor_index : ProgressIdentityState::kSerialKeyBias + session_->current_node_idx();
    session_->ObserveHardProgress(hard_progress_key, effective_progress, now);
    return effective_progress;
}

void NavigationStateMachine::StopMotion()
{
    motion_controller_->SetForwardState(false);
}

NavigationStateMachine::~NavigationStateMachine()
{
    // Backstop: guarantee the PositionProvider's frame observer (it captures `this` and reads the scanners) is
    // torn down before this object dies, even on an early Run() return path.
    StopScanners();
}

std::array<AsyncPromptAction*, 2> NavigationStateMachine::PromptActions()
{
    return { &collect_prompt_, &interact_prompt_ };
}

void NavigationStateMachine::StartScanners()
{
    StartPromptScanners();
    StartDeviceProbe();

    if (position_provider_ == nullptr) {
        return;
    }
    // One observer for every consumer, bound once. Binding it per scanner would let whichever one stops first
    // silently cut the others' frame supply.
    position_provider_->SetFrameObserver([this](const cv::Mat& frame) {
        collect_prompt_.SubmitFrame(frame);
        interact_prompt_.SubmitFrame(frame);
        device_recovery_.SubmitFrame(frame);
        if (zipline_mount_scanner_ != nullptr) {
            zipline_mount_scanner_->SubmitFrame(frame);
        }
    });
}

void NavigationStateMachine::StopScanners()
{
    if (position_provider_ != nullptr) {
        position_provider_->SetFrameObserver(nullptr);
    }
    if (motion_controller_ != nullptr) {
        motion_controller_->SetSprintSuppressed(false);
    }
    collect_prompt_.Stop();
    interact_prompt_.Stop();
    device_recovery_.Stop();
    zipline_mount_scanner_.reset();
}

void NavigationStateMachine::StartPromptScanners()
{
    for (AsyncPromptAction* prompt : PromptActions()) {
        if (prompt->armed() || !prompt->RouteHasPoint()) {
            continue;
        }

        cv::Rect base_roi;
        if (!TryReadNodeRoi(maa_context_, prompt->spec().recognition_node, &base_roi)) {
            LogWarn << "Async prompt scanner not started: could not read its ROI from the pipeline." << VAR(prompt->spec().tag)
                    << VAR(prompt->spec().recognition_node);
            continue;
        }
        prompt->Start(base_roi, action_wrapper_->controller_type());
    }
}

void NavigationStateMachine::StartDeviceProbe()
{
    cv::Rect base_roi;
    if (!TryReadNodeRoi(maa_context_, kObstacleDeviceProbeNode, &base_roi)) {
        LogWarn << "Blocking-device probe not started: could not read its ROI from the pipeline." << VAR(kObstacleDeviceProbeNode);
        return;
    }
    device_recovery_.Start(base_roi);
}

// 导入的架子坐标跟脚下的面对不齐时, 严格到达要么迟迟收不拢要么把这条索直接放弃。提示是游戏
// 自己说的「够得着」, 所以命中就放行到达判定; 判定圈之外的观测照样读掉, 免得跨进带内那一拍
// 拿着旧观测开火。
bool NavigationStateMachine::TryZiplineMountPrompt(const Waypoint& waypoint, const RouteTrackingState& route)
{
    if (waypoint.action != ActionType::ZIPLINE) {
        zipline_mount_scanner_.reset(); // 链走完了, 别让它继续逐帧匹配
        return false;
    }
    // 已经站在架子上时下一跳靠瞄准接上, 不再上索; 预筛这时既没用也该省下来
    if (runtime_state_.semantic.zipline_mounted) {
        return false;
    }
    if (zipline_mount_scanner_ == nullptr && maa_context_ != nullptr) {
        PromptScanProfile profile;
        if (!TryLoadPromptScanProfile(maa_context_, kZiplineMountScanNode, action_wrapper_->controller_type(), &profile)) {
            LogWarn << "Zipline mount pre-filter not started; the chain stops on arrival instead." << VAR(kZiplineMountScanNode);
            return false;
        }
        zipline_mount_scanner_ =
            std::make_unique<RoiTemplateScanner>("zipline", profile.base_roi, profile.templ, profile.mask, profile.threshold);
        LogInfo << "Zipline mount pre-filter started." << VAR(kZiplineMountScanNode) << VAR(profile.base_roi.x) << VAR(profile.base_roi.y)
                << VAR(profile.threshold);
    }
    if (zipline_mount_scanner_ == nullptr) {
        return false;
    }
    const bool flagged = zipline_mount_scanner_->ConsumeDetection();
    return flagged && route.startup_motion_confirmed && route.waypoint_distance <= kPromptTriggerBandWu;
}

double NavigationStateMachine::NearestPromptDistanceSq() const
{
    double nearest_sq = -1.0;
    for (const AsyncPromptAction* prompt : { &collect_prompt_, &interact_prompt_ }) {
        const double distance_sq = prompt->NearestDistanceSq();
        if (distance_sq >= 0.0 && (nearest_sq < 0.0 || distance_sq < nearest_sq)) {
            nearest_sq = distance_sq;
        }
    }
    // 上索点也算提示点: 同一个图标、同一件事 —— 够不够得着只看身位离架子多远。已经站上去了就不算,
    // 剩下的跳靠瞄准接上; 走过的那些也不算, 链尾旁边的旧架子会把走路模式一直摁着
    if (!runtime_state_.semantic.zipline_mounted) {
        const std::vector<Waypoint>& path = session_->current_path();
        for (size_t index = session_->current_node_idx(); index < path.size(); ++index) {
            const Waypoint& waypoint = path[index];
            if (waypoint.action != ActionType::ZIPLINE || !waypoint.HasPosition()) {
                continue;
            }
            const double dx = waypoint.x - position_->x;
            const double dy = waypoint.y - position_->y;
            const double distance_sq = dx * dx + dy * dy;
            if (nearest_sq < 0.0 || distance_sq < nearest_sq) {
                nearest_sq = distance_sq;
            }
        }
    }
    return nearest_sq;
}

void NavigationStateMachine::UpdatePromptSprintSuppression()
{
    if (motion_controller_ == nullptr) {
        return;
    }

    const double nearest_sq = NearestPromptDistanceSq();
    // 这条线上没有提示驱动的点时 nearest_sq < 0, 疾跑行为一点不碰
    const bool approaching_prompt = nearest_sq >= 0.0 && nearest_sq <= kCollectSprintSuppressBandWu * kCollectSprintSuppressBandWu;
    motion_controller_->SetSprintSuppressed(approaching_prompt);
}

// Walk mode's only decision point: engaged on the last few units of an approach to a prompt-driven point,
// released everywhere else (travel legs, recovery, turn-in-place nodes, before motion is confirmed).
void NavigationStateMachine::UpdateWalkMode(NaviPhase phase)
{
    const double nearest_sq = NearestPromptDistanceSq();
    const bool recovering = runtime_state_.recovery.active || runtime_state_.cross_tier_escape.active;
    const ActionType action = session_->HasCurrentWaypoint() ? session_->CurrentWaypoint().action : ActionType::HEADING;
    const bool plain_approach = action == ActionType::COLLECT || action == ActionType::INTERACT || action == ActionType::RUN
                                || action == ActionType::NAVMESH || action == ActionType::ZIPLINE;
    if (phase != NaviPhase::Navigate || nearest_sq < 0.0 || recovering || !plain_approach
        || !runtime_state_.route.startup_motion_confirmed) {
        walk_mode_.Request(false);
        return;
    }

    const bool was_engaged = walk_mode_.engaged();
    const double band = was_engaged ? kCollectWalkExitBandWu : kCollectWalkEnterBandWu;
    walk_mode_.Request(nearest_sq <= band * band);
    const bool walking = walk_mode_.engaged();
    if (walking != was_engaged) {
        const double nearest_prompt_point = std::sqrt(nearest_sq);
        LogInfo << "Walk mode boundary crossed." << VAR(walking) << VAR(nearest_prompt_point) << VAR(position_->x) << VAR(position_->y);
    }
}

void NavigationStateMachine::PreWarmPromptRecognition()
{
    for (AsyncPromptAction* prompt : PromptActions()) {
        prompt->PreWarmRecognition();
    }
}

bool NavigationStateMachine::TryRunPromptSubtaskWhileWalking(const RouteTrackingState& route)
{
    if (!route.startup_motion_confirmed) {
        return false;
    }

    const std::array<AsyncPromptAction*, 2> prompts = PromptActions();
    for (size_t index = 0; index < prompts.size(); ++index) {
        if (!prompts[index]->TryTriggerWhileWalking(motion_controller_, route.waypoint_distance, session_->current_node_idx())) {
            continue;
        }
        // 屏幕上一次只弹一个提示, 一次观测只值一次停车: 清掉另一类的闩, 免得同一个提示被停两次
        for (size_t other = 0; other < prompts.size(); ++other) {
            if (other != index) {
                prompts[other]->ForgetDetection();
            }
        }
        if (prompts[index]->spec().CompletesWaypointOnTrigger()) {
            CompleteWaypointAfterPromptTrigger();
        }
        return true;
    }
    return false;
}

// 提示就是游戏说的「够近了」, 所以命中即算走完, 不再往前挪那几个单位: 交互一开界面角色就不动了、
// 小地图也没了, 剩下那段永远走不完, 一次成功的交互会被拖成到达超时判败。
void NavigationStateMachine::CompleteWaypointAfterPromptTrigger()
{
    if (!session_->HasCurrentWaypoint()) {
        return;
    }

    const ActionType action = session_->CurrentWaypoint().action;
    const std::optional<size_t> consumed_absolute_node_idx = session_->CurrentAbsoluteNodeIndex();
    session_->NoteCanonicalFinalGoalConsumed(consumed_absolute_node_idx, *position_, "async_prompt_completed");
    session_->AdvanceToNextWaypoint(action, "async_prompt_completed");
    runtime_state_.OnWaypointAdvance();
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return;
    }
    SelectPhaseForCurrentWaypoint("async_prompt_completed");
}

// 最后一个点被吃掉的同一拍路线就结束、扫描器随即销毁, 行进中的检测没机会报第二次, 所以收尾单独给一个窗口。
// 放在成功判定之后, 这里失败不该翻掉跑成功的线路。只服务共用表那类: 点名目标的那类每个点必定恰好跑一次。
void NavigationStateMachine::TryRunPromptSubtaskAtRouteTail()
{
    if (maa_context_ == nullptr || should_stop_() || session_->phase() != NaviPhase::Finished) {
        return;
    }

    AsyncPromptAction* tail_prompt = nullptr;
    for (AsyncPromptAction* prompt : PromptActions()) {
        if (prompt->armed() && !prompt->spec().CompletesWaypointOnTrigger() && prompt->RouteEndsWithPoint()) {
            tail_prompt = prompt;
            break;
        }
    }
    if (tail_prompt == nullptr) {
        return;
    }

    StopMotion();
    utils::SleepFor(kStopWaitMs); // 先站定, 还在滑行就动手容易白按一次
    const auto started_at = std::chrono::steady_clock::now();
    while (!should_stop_()) {
        if (tail_prompt->TryTriggerAtRouteTail()) {
            return;
        }

        const int64_t elapsed_ms =
            std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - started_at).count();
        if (elapsed_ms >= kCollectTailGraceMs) {
            LogInfo << "Route tail prompt grace expired with nothing flagged." << VAR(tail_prompt->spec().tag) << VAR(elapsed_ms);
            return;
        }

        CaptureCurrentPosition(false); // 只为把新画面喂给检测器
        utils::SleepFor(kTargetTickMs);
    }
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
