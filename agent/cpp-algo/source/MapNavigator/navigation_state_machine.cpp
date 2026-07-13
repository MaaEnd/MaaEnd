#include <algorithm>
#include <chrono>
#include <cmath>
#include <filesystem>
#include <limits>
#include <memory>
#include <optional>
#include <string>
#include <thread>
#include <utility>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "action_executor.h"
#include "action_wrapper.h"
#include "collectible_scanner.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "navigation_state_machine.h"
#include "navmesh_path_expander.h"
#include "position_provider.h"
#include "route_tracker.h"
#include "semantic_nodes.h"
#include "steering_controller.h"

#include "../utils.h"

namespace mapnavigator
{

namespace
{

// Pull the collect-label ROI from the pipeline node (single source of truth) rather than hardcoding it:
// MaaContextGetNodeData returns the node's resolved JSON, so editing AutoCollectClick.json's roi
// automatically retargets the async scanner. The roi is authored in the 1280x720 base frame.
bool ReadRoiArray(const json::value& holder, cv::Rect* out)
{
    if (!holder.is_array()) {
        return false;
    }
    const auto& arr = holder.as_array();
    if (arr.size() < 4) {
        return false;
    }
    for (size_t i = 0; i < 4; ++i) {
        if (!arr.at(i).is_number()) {
            return false;
        }
    }
    out->x = static_cast<int>(std::lround(arr.at(0).as_double()));
    out->y = static_cast<int>(std::lround(arr.at(1).as_double()));
    out->width = static_cast<int>(std::lround(arr.at(2).as_double()));
    out->height = static_cast<int>(std::lround(arr.at(3).as_double()));
    return out->width > 0 && out->height > 0;
}

bool ParseCollectRoiFromNode(MaaContext* context, const char* node_name, cv::Rect* out)
{
    if (context == nullptr || node_name == nullptr) {
        return false;
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaContextGetNodeData(context, node_name, buffer.Get())) {
        LogWarn << "Collect ROI: MaaContextGetNodeData failed." << VAR(node_name);
        return false;
    }
    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || raw[0] == '\0') {
        LogWarn << "Collect ROI: empty node data." << VAR(node_name);
        return false;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        LogWarn << "Collect ROI: node JSON is not an object." << VAR(node_name);
        return false;
    }
    const auto& node = parsed->as_object();

    // Canonical shape: { "recognition": { "param": { "roi": [x,y,w,h] } } }. Fall back to flatter shapes in
    // case the framework serializes the loaded node differently.
    if (node.contains("recognition") && node.at("recognition").is_object()) {
        const auto& reco = node.at("recognition").as_object();
        if (reco.contains("param") && reco.at("param").is_object()) {
            const auto& param = reco.at("param").as_object();
            if (param.contains("roi") && ReadRoiArray(param.at("roi"), out)) {
                return true;
            }
        }
        if (reco.contains("roi") && ReadRoiArray(reco.at("roi"), out)) {
            return true;
        }
    }
    if (node.contains("roi") && ReadRoiArray(node.at("roi"), out)) {
        return true;
    }

    LogWarn << "Collect ROI: no usable roi array in node data." << VAR(node_name);
    return false;
}

bool RouteHasCollectWaypoint(const std::vector<Waypoint>& path)
{
    return std::any_of(path.begin(), path.end(), [](const Waypoint& wp) { return wp.action == ActionType::COLLECT; });
}

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

    // Absorb the collect-OCR cold start here, while the avatar is still stopped after Bootstrap and before
    // the first forward press, so it can never land on a while-walking scan tick and freeze the thread.
    PreWarmCollectOcr();

    // Spin up the background collectible detector (no-op unless the route has a COLLECT waypoint). It runs
    // off the nav thread on pure OpenCV; the nav loop only reacts to its flag, never recognizes inline.
    StartCollectScanner();

    while (!should_stop_() && session_->phase() != NaviPhase::Finished && session_->phase() != NaviPhase::Failed) {
        if (!TickPhase(session_->phase())) {
            StopCollectScanner();
            StopMotion();
            return false;
        }
    }

    if (!should_stop_() && session_->phase() != NaviPhase::Failed) {
        session_->HasSatisfiedFinalSuccess(*position_, "navigation_complete");
    }

    StopCollectScanner();
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

    const bool relocalize_cooling =
        last_global_relocalize_at_ != std::chrono::steady_clock::time_point {}
        && std::chrono::duration_cast<std::chrono::milliseconds>(now - last_global_relocalize_at_)
               < std::chrono::milliseconds(kRelocationRetryIntervalMs);
    if (!relocalize_cooling) {
        last_global_relocalize_at_ = now;
        const std::string prior_zone = session_->current_zone_id();
        if (position_provider_->Capture(position_, /*force_global_search=*/true, /*expected_zone_id=*/std::string())) {
            const bool zone_changed = !position_->zone_id.empty() && position_->zone_id != prior_zone;

            if (runtime_state_.cross_tier_escape.active) {
                if (NavmeshZonesShareGeometry(param_, runtime_state_.cross_tier_escape.anchor_zone, position_->zone_id)) {
                    LogInfo << "Cross-tier escape rode a zone flip; preserving the corridor." << VAR(prior_zone)
                            << VAR(position_->zone_id) << VAR(position_->x) << VAR(position_->y);
                    if (zone_changed) {
                        session_->UpdateCurrentZone(position_->zone_id);
                    }
                    loss.Reset();
                    return true;
                }
                LogInfo << "Cross-tier escape: re-acquired zone left the pit span; reverting to loss handling."
                        << VAR(prior_zone) << VAR(position_->zone_id);
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
            LogInfo << "Localization recovered via global re-acquire; resuming navigation." << VAR(loss_elapsed.count())
                    << VAR(prior_zone) << VAR(position_->zone_id) << VAR(position_->x) << VAR(position_->y)
                    << VAR(runtime_state_.global_reacquire_streak);
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
    runtime_state_.river_fall.pending = true;
    runtime_state_.river_fall.anchor_pos = *position_;
    // Post-fall facing points at the water (the infinite-jump invariant); recovery turns to water_heading + 180.
    runtime_state_.river_fall.water_heading = NaviMath::NormalizeAngle(position_->angle);
    // River-fall owns the recovery: the pre-fall dynamic-recovery anchor is stale after the teleport, and a live
    // recovery's escaped-obstacle check (runs before the river-fall block) would otherwise pre-empt the about-face.
    runtime_state_.recovery.Reset();
    LogInfo << "River-fall recovery armed (black-screen loss recovered)." << VAR(via) << VAR(position_->x)
            << VAR(position_->y) << VAR(runtime_state_.river_fall.water_heading);
    return true;
}

bool NavigationStateMachine::TryApplyDynamicOverlayToAnchor(
    const char* reason,
    size_t continue_index,
    const Waypoint& anchor,
    bool use_detour,
    double route_heading,
    bool emit_interior_corners,
    bool reset_hard_progress)
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
    else if (!AppendGeneratedNavmeshWaypoints(param_, position_->zone_id, *route, generated_prefix,
                                              /*include_goal=*/false, emit_interior_corners,
                                              /*strict_segment_breaks=*/false)) {
        LogWarn << "Dynamic navmesh overlay skipped: generated path is unusable." << VAR(reason) << VAR(continue_index)
                << VAR(route->path.points.size());
        return false;
    }
    if (generated_prefix.empty()
        && std::hypot(anchor.x - position_->x, anchor.y - position_->y) > ArrivalBandForStartupBypass(anchor)) {
        generated_prefix.emplace_back(position_->x, position_->y, ActionType::RUN);
        LogInfo << "Dynamic overlay seeded leading node to avoid single-point path." << VAR(reason)
                << VAR(continue_index) << VAR(position_->x) << VAR(position_->y);
    }
    const size_t generated_count = generated_prefix.size();
    session_->ApplyDynamicOverlay(std::move(generated_prefix), continue_index, *position_, reset_hard_progress);
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

bool NavigationStateMachine::TryApplyDynamicOverlayToNextAnchor(const char* reason, bool use_detour, double route_heading,
                                                               bool reset_hard_progress)
{
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    if (!anchor) {
        runtime_state_.dynamic_replan_requested = false;
        LogInfo << "Dynamic navmesh overlay skipped: no future anchor." << VAR(reason) << VAR(position_->x) << VAR(position_->y)
                << VAR(position_->zone_id);
        return false;
    }
    return TryApplyDynamicOverlayToAnchor(reason, anchor->first, anchor->second, use_detour, route_heading,
                                          /*emit_interior_corners=*/false, reset_hard_progress);
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
            continue;  // a generated overlay waypoint (no canonical index) is not a rejoin target
        }
        if (TryApplyDynamicOverlayToAnchor(reason, *continue_index, candidate, /*use_detour=*/false, heading,
                                           /*emit_interior_corners=*/true)) {
            runtime_state_.cross_tier_escape.goal_x = candidate.x;
            runtime_state_.cross_tier_escape.goal_y = candidate.y;
            LogInfo << "Cross-tier escape corridor planned." << VAR(reason) << VAR(position_->zone_id)
                    << VAR(position_->x) << VAR(position_->y) << VAR(*continue_index) << VAR(candidate.x)
                    << VAR(candidate.y);
            return true;
        }
    }
    LogInfo << "Cross-tier escape: on a wrong tier but no reachable authored waypoint." << VAR(reason)
            << VAR(position_->zone_id) << VAR(position_->x) << VAR(position_->y);
    return false;
}

bool NavigationStateMachine::TryEnterCrossTierEscape()
{
    // Positive-ID: the fresh fix must sit on a real FLOORED tier (not a geometry / "…_Base" overview zone).
    const float tier_floor = NavmeshFloorYForZone(param_, position_->zone_id);
    if (tier_floor <= navmesh::kBaseNavFloorYValidMin) {
        LogInfo << "Cross-tier escape declined: zone is not a floored tier." << VAR(position_->zone_id)
                << VAR(tier_floor) << VAR(position_->x) << VAR(position_->y);
        return false;
    }
    if (ResolveCurrentAnchor(session_, *position_)) {
        LogInfo << "Cross-tier escape declined: route has a zone-compatible anchor here (normal travel)."
                << VAR(position_->zone_id) << VAR(position_->x) << VAR(position_->y);
        return false;
    }

    if (!PlanCrossTierEscapeCorridorFromHere("crosstier_escape")) {
        return false;  // on a wrong tier but no reachable authored waypoint; defer to loss handling
    }
    runtime_state_.cross_tier_escape.active = true;
    runtime_state_.cross_tier_escape.anchor_zone = position_->zone_id;
    LogInfo << "Cross-tier escape engaged: routing out of a wrong tier via navmesh." << VAR(position_->zone_id)
            << VAR(position_->x) << VAR(position_->y) << VAR(runtime_state_.cross_tier_escape.goal_x)
            << VAR(runtime_state_.cross_tier_escape.goal_y);
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
    const std::optional<navmesh::WorldPoint> target =
        PlanUnstickTarget(param_, *position_, stuck_heading, unstick.count, &distance);
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
        if (!CaptureCurrentPosition(false) || position_provider_->LastCaptureWasHeld()
            || position_provider_->LastCaptureWasBlackScreen() || !position_->valid) {
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

    // Refresh the route from the new spot but keep the hard-progress clock: this replan re-fires every recovery
    // retry while stuck, and resetting the clock each time would defeat the 30s recovery timeout (livelock).
    if (TryApplyDynamicOverlayToNextAnchor("recovery_unstick_replan", false, 0.0, /*reset_hard_progress=*/false)) {
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
        return HandleLocalizationLoss();
    }
    {
        LocalizationLossState& loss = runtime_state_.localization_loss;
        if (loss.started_at != std::chrono::steady_clock::time_point {}) {
            const auto loss_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
                                     std::chrono::steady_clock::now() - loss.started_at)
                                     .count();
            const bool armed = ArmRiverFallRecoveryIfBlackScreenLoss("normal_reacquire");
            LogInfo << "Localization recovered via normal tracking." << VAR(loss_ms) << VAR(loss.saw_black_screen)
                    << VAR(armed) << VAR(position_->x) << VAR(position_->y);
            loss.Reset();
            if (armed) {
                return true;
            }
        } else {
            loss.Reset();
        }
    }

    if (runtime_state_.cross_tier_escape.active) {
        const double distance_to_goal = std::hypot(position_->x - runtime_state_.cross_tier_escape.goal_x,
                                                    position_->y - runtime_state_.cross_tier_escape.goal_y);
        const bool on_floorless_zone =
            NavmeshFloorYForZone(param_, position_->zone_id) <= navmesh::kBaseNavFloorYValidMin;
        if (distance_to_goal <= kCrossTierEscapeArrivalM && on_floorless_zone) {
            LogInfo << "Cross-tier escape reached the rejoin point on the base floor; resuming authored route."
                    << VAR(distance_to_goal) << VAR(position_->zone_id) << VAR(position_->x) << VAR(position_->y);
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
                nav_run_anchor = DynamicAnchor { session_->CanonicalIndexAtCurrent().value_or(kUnaddressableAnchorIndex),
                                                 current_waypoint };
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
            // Passing corridor waypoints is discrete forward progress the thrash fast-fail must honour: a
            // long leg with several transient losses would otherwise reach the re-acquire cap and wrongly
            // fail. A stationary recover<->re-lose storm passes none, so it stays storm-proof.
            runtime_state_.global_reacquire_streak = 0;
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
        // Feed the same signal to the hard watchdog, which recovery escapes can never reset (they only clear
        // the ordinary ObserveProgress clock). This is what lets the recovery timeout below actually fire.
        session_->ObserveHardProgress(session_->current_node_idx(), effective_progress, now);
    }
    // Cross-tier escape: follow the ONE planned corridor (arrival above is the success exit). Fast-fail when the
    // corridor makes no genuine progress for too long. Keys on the hard-progress clock, which the escape's own
    // overlay re-applies can't reset, so it trips only on a continuously stuck (walled/unfollowable) escape, never
    // a slow-but-advancing one. The orthogonal recover<->re-lose thrash is caught by the re-acquire streak above.
    if (runtime_state_.cross_tier_escape.active && session_->HardStalledMs(now) >= kCrossTierEscapeHardStallMs) {
        const double goal_dist = std::hypot(position_->x - runtime_state_.cross_tier_escape.goal_x,
                                            position_->y - runtime_state_.cross_tier_escape.goal_y);
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
    if (TryScanApproachCollect(route, waypoint)) {
        return true;
    }

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
        const double rf_target_heading = NaviMath::NormalizeAngle(rf.water_heading + 180.0);
        const double rf_heading_error = NaviMath::NormalizeAngle(rf_target_heading - current_heading);
        if (rf_displacement >= kRiverFallRecoveryClearDistance) {
            rf.Reset();
            runtime_state_.recovery.Reset();
            runtime_state_.route.ResetTracking();
            runtime_state_.dynamic_replan_requested = false;
            runtime_state_.nav_run_dirty = true;
            session_->ResetProgress();
            LogInfo << "River-fall recovery cleared; resuming navigation." << VAR(rf_displacement) << VAR(rf_heading_error);
            SelectPhaseForCurrentWaypoint("river_fall_recovered");
            return true;
        }
        motion_controller_->SetForwardState(false);
        utils::SleepFor(kStopWaitMs);
        int turn_units = static_cast<int>(std::lround(rf_heading_error * action_wrapper_->DefaultTurnUnitsPerDegree()));
        if (turn_units == 0) {
            turn_units = rf_heading_error > 0.0 ? 1 : -1;
        }
        action_wrapper_->SendViewDeltaSync(turn_units, 0);
        action_wrapper_->PulseForwardSync(kRiverFallRecoveryPulseMs);
        motion_controller_->SetForwardState(false);
        LogInfo << "River-fall recovery turn+pulse." << VAR(rf_heading_error) << VAR(turn_units) << VAR(rf_displacement)
                << VAR(position_->x) << VAR(position_->y);
        return true;
    }

    // Off-route wedge watchdog (see kOffRouteWedge* in navi_config.h). Only corridor (non-strict RUN) waypoints,
    // where on_route is meaningful; the stall clocks above are fed corridor progress and miss a pinned-off-route
    // cursor. Fed straight-line distance, so the timer only grows while genuinely off-route with no inward gain.
    if (session_->phase() == NaviPhase::Navigate && waypoint.action == ActionType::RUN && !waypoint.RequiresStrictArrival()
        && !route.on_route && !runtime_state_.cross_tier_escape.active) {
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
        const bool replan_cooling = wedge.last_replan_at.time_since_epoch().count() > 0
                                    && std::chrono::duration_cast<std::chrono::milliseconds>(now - wedge.last_replan_at).count()
                                           < kOffRouteWedgeReplanCooldownMs;
        if (wedge_ms >= kOffRouteWedgeReplanMs && !replan_cooling) {
            wedge.last_replan_at = now;
            LogWarn << "Off-route wedge detected; replanning from current position." << VAR(wedge_ms)
                    << VAR(route.waypoint_distance) << VAR(route.cross_track) << VAR(session_->current_node_idx());
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
    const bool near_strict_goal = waypoint.RequiresStrictArrival()
        && route.waypoint_distance <= arrival_distance + kCloseGoalDetourSuppressSlack;
    const bool should_try_recovery = session_->phase() == NaviPhase::Navigate && stalled_ms >= kObstacleRecoveryMinTriggerMs
                                     && (route.progress_distance > kNoProgressMinDistance || waypoint.RequiresStrictArrival())
                                     && !runtime_state_.cross_tier_escape.active;
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

            // Measure the recovery timeout from the session hard-progress clock, not this episode's
            // recovery.started_at. Every small jump "escape" calls recovery.Reset() + ResetProgress(), which
            // re-stamps started_at and the ordinary stall clock — so a started_at-based elapsed never grows and
            // the agent can thrash in place indefinitely (observed: ~6 min, 1094 jumps, 0 timeouts). The hard
            // clock only advances on genuine net progress toward the waypoint, so a livelock now trips the
            // emergency stop after kDynamicRecoveryTotalTimeoutMs of real no-progress.
            const int64_t recovery_elapsed_ms = session_->HardStalledMs(now);
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
                recovery.last_replan_at = now;

                if (!near_strict_goal && recovery.detour_attempt_count >= kRecoveryDetourAttemptsBeforeUnstick) {
                    if (ExecutePhysicalUnstick(route.route_heading)) {
                        return true;
                    }
                }

                ++recovery.jump_attempt_count;
                const NaviPosition jump_start = *position_;
                LogInfo << "Dynamic recovery jump pulse issued." << VAR(recovery.jump_attempt_count)
                        << VAR(recovery.detour_attempt_count);
                motion_controller_->SetAction(LocalDriverAction::JumpForward, true);
                utils::SleepFor(kActionJumpSettleMs);
                motion_controller_->SetForwardState(false);
                if (!CaptureCurrentPosition(false) || position_provider_->LastCaptureWasHeld()
                    || position_provider_->LastCaptureWasBlackScreen() || !position_->valid) {
                    LogWarn << "Dynamic recovery waiting for post-jump local tracking fix." << VAR(stalled_ms)
                            << VAR(recovery.jump_attempt_count);
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
                    recovery.jump_attempt_count = 1;
                }

                // The jump pulse above runs every recovery tick — so even once we are detouring, a
                // fresh stall keeps trying to hop free first ("jump during detour"). The detour is the
                // fallback: it only kicks in after the jump has failed kRecoveryJumpAttemptsBeforeDetour
                // times for this anchor, and is suppressed next to a strict goal where it would route
                // away from the exact point (there we keep jumping instead).
                const bool detour_allowed =
                    !near_strict_goal && recovery.jump_attempt_count >= kRecoveryJumpAttemptsBeforeDetour;
                if (detour_allowed) {
                    ++recovery.detour_attempt_count;
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
                            << VAR(recovery.detour_attempt_count) << VAR(recovery.jump_attempt_count)
                            << VAR(post_jump_anchor->first) << VAR(route.progress_distance) << VAR(stalled_ms);
                }
                utils::SleepFor(kTargetTickMs);
                return true;
            }
        }
    }

    const double effective_route_heading =
        nav_run_result.has_corridor_heading ? nav_run_result.corridor_heading : route.route_heading;

    double heading_rate_deg = 0.0;
    double heading_rate_raw_delta_deg = 0.0;
    int64_t heading_rate_gap_ms = 0;
    if (runtime_state_.steering_rate.has_prev) {
        heading_rate_raw_delta_deg =
            NaviMath::NormalizeAngle(current_heading - runtime_state_.steering_rate.prev_heading_deg);
        heading_rate_gap_ms =
            std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.steering_rate.at).count();
        const bool heading_changed = std::abs(heading_rate_raw_delta_deg) > kSteeringHeadingChangeEpsilonDeg;
        if (heading_changed && heading_rate_gap_ms > 0 && heading_rate_gap_ms <= kSteeringRateMaxGapMs) {
            heading_rate_deg = heading_rate_raw_delta_deg * static_cast<double>(kSteeringRateReferenceMs)
                               / static_cast<double>(heading_rate_gap_ms);
        }
        if (heading_changed) {
            runtime_state_.steering_rate.prev_heading_deg = current_heading;
            runtime_state_.steering_rate.at = now;
        }
    } else {
        runtime_state_.steering_rate.prev_heading_deg = current_heading;
        runtime_state_.steering_rate.at = now;
        runtime_state_.steering_rate.has_prev = true;
    }

    const double heading_error = NaviMath::NormalizeAngle(effective_route_heading - current_heading);
    const SteeringCommand steering = SteeringController::Update(
        heading_error, heading_rate_deg, motion_controller_->IsMovingForward());

    motion_controller_->SetForwardState(true);

    double issued_delta_deg = 0.0;
    if (steering.issued) {
        const TurnCommandResult steering_result = motion_controller_->ApplySteering(steering.yaw_delta_deg);
        if (steering_result.issued) {
            issued_delta_deg = steering_result.issued_delta_degrees;
        }
    }

    LogDebug << "TickNavigate steering decision." << VAR(current_heading)
             << VAR(route.route_heading) << VAR(effective_route_heading)
             << VAR(nav_run_result.has_corridor_heading) << VAR(nav_run_result.cross_track)
             << VAR(nav_run_result.upcoming_turn_deg) << VAR(heading_rate_deg)
             << VAR(heading_rate_raw_delta_deg) << VAR(heading_rate_gap_ms)
             << VAR(heading_error) << VAR(steering.yaw_delta_deg)
             << VAR(issued_delta_deg) << VAR(route.waypoint_distance) << VAR(route.on_route);

    // Collect routes: keep sprint for travel but drop to walking speed once near a COLLECT point (cancels any
    // active sprint), so the detection-stop can land before we overrun the collectible. No-op off collect
    // routes. Must run before the sprint gate below so a freshly-entered zone suppresses this tick's sprint.
    UpdateCollectSprintSuppression();

    // Balanced sprint gate: burst only when the agent already points down the corridor (heading aligned)
    // and no sharp turn is imminent within the scan window. No clearance term — it reads near zero on
    // bridges and locked sprint out entirely; alignment + upcoming-turn keep it from charging a corner.
    const bool heading_aligned_for_sprint = std::abs(heading_error) < kAutoSprintMaxHeadingErrorDeg;
    const bool corridor_turn_calm =
        !nav_run_result.has_corridor_heading || nav_run_result.upcoming_turn_deg < kAutoSprintMaxUpcomingTurnDeg;
    const bool turn_calm = heading_aligned_for_sprint && corridor_turn_calm;
    const bool target_requires_strict_arrival = waypoint.RequiresStrictArrival();
    const double sprint_remaining =
        nav_run_result.has_corridor_heading && std::isfinite(nav_run_result.remaining_to_anchor)
            ? nav_run_result.remaining_to_anchor
            : route.along_track_remaining;
    // Strict-arrival goals no longer block sprint outright; allow it until a braking buffer before the
    // waypoint so long straight runs into a strict goal still sprint, then brake in time to land.
    const bool has_strict_arrival_braking_room = !target_requires_strict_arrival
        || route.waypoint_distance > arrival_distance + kStrictArrivalSprintBrakeDistance;
    const bool allow_sprint =
        turn_calm && motion_controller_->SupportsSprint() && startup_grace_elapsed && param_.sprint_threshold > 0.0
        && has_strict_arrival_braking_room && sprint_remaining > param_.sprint_threshold
        && (runtime_state_.flow.last_auto_sprint_time.time_since_epoch().count() == 0
            || std::chrono::duration_cast<std::chrono::milliseconds>(now - runtime_state_.flow.last_auto_sprint_time).count()
                   >= kAutoSprintCooldownMs);
    LogDebug << "TickNavigate sprint gate." << VAR(allow_sprint) << VAR(heading_aligned_for_sprint)
             << VAR(corridor_turn_calm) << VAR(startup_grace_elapsed)
             << VAR(has_strict_arrival_braking_room) << VAR(sprint_remaining) << VAR(param_.sprint_threshold);
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

NavigationStateMachine::~NavigationStateMachine()
{
    // Backstop: guarantee the PositionProvider's frame observer (it captures `this` and reads
    // collect_scanner_) is torn down before this object dies, even on an early Run() return path.
    StopCollectScanner();
}

void NavigationStateMachine::StartCollectScanner()
{
    if (collect_scanner_ != nullptr) {
        return;
    }

    if (!RouteHasCollectWaypoint(session_->original_path())) {
        return;
    }

    cv::Rect base_roi;
    if (!ParseCollectRoiFromNode(maa_context_, kCollectRoiNode, &base_roi)) {
        LogWarn << "Async collectible scanner not started: could not read collect ROI from pipeline."
                << VAR(kCollectRoiNode);
        return;
    }

    const std::filesystem::path icon_path =
        std::filesystem::absolute(get_exe_dir() / ".." / kCollectIconRelativePath);
    const cv::Mat icon_template = MAA_NS::imread(icon_path, cv::IMREAD_GRAYSCALE);
    if (icon_template.empty()) {
        LogWarn << "Collect icon template not loaded; falling back to bright-text heuristic."
                << VAR(MAA_NS::path_to_utf8_string(icon_path));
    }
    else {
        LogInfo << "Collect icon template loaded." << VAR(MAA_NS::path_to_utf8_string(icon_path))
                << VAR(icon_template.cols) << VAR(icon_template.rows);
    }

    collect_scanner_ = std::make_unique<CollectibleScanner>(base_roi, icon_template);
    position_provider_->SetFrameObserver([this](const cv::Mat& frame) {
        if (collect_scanner_ != nullptr) {
            collect_scanner_->SubmitFrame(frame);
        }
    });
    LogInfo << "Async collectible scanner started." << VAR(base_roi.x) << VAR(base_roi.y) << VAR(base_roi.width)
            << VAR(base_roi.height);
    // NOTE: sprint is NOT suppressed for the whole route — that killed fast travel. Suppression is driven per
    // tick in TickNavigate (UpdateCollectSprintSuppression), enabled only when the avatar is within
    // kCollectSprintSuppressBandWu of a COLLECT waypoint, so travel between collect points still sprints.
}

void NavigationStateMachine::StopCollectScanner()
{
    if (position_provider_ != nullptr) {
        position_provider_->SetFrameObserver(nullptr);
    }
    if (motion_controller_ != nullptr) {
        motion_controller_->SetSprintSuppressed(false);
    }
    collect_scanner_.reset();
}

void NavigationStateMachine::UpdateCollectSprintSuppression()
{
    if (collect_scanner_ == nullptr || motion_controller_ == nullptr) {
        return;  // not a collect route — leave sprint behaviour entirely untouched
    }

    bool near_collect = false;
    if (position_ != nullptr && position_->valid && session_ != nullptr) {
        const double band_sq = kCollectSprintSuppressBandWu * kCollectSprintSuppressBandWu;
        for (const Waypoint& waypoint : session_->current_path()) {
            if (waypoint.action != ActionType::COLLECT || !waypoint.HasPosition()) {
                continue;
            }
            const double dx = waypoint.x - position_->x;
            const double dy = waypoint.y - position_->y;
            if (dx * dx + dy * dy <= band_sq) {
                near_collect = true;
                break;
            }
        }
    }
    motion_controller_->SetSprintSuppressed(near_collect);
}

void NavigationStateMachine::PreWarmCollectOcr()
{
    if (maa_context_ == nullptr) {
        return;
    }

    if (!RouteHasCollectWaypoint(session_->original_path())) {
        return;
    }

    LogInfo << "Pre-warming collect OCR model before navigation (absorbs one-time cold start while stopped).";
    MaaContextRunTask(maa_context_, kDefaultCollectEntry, kCollectPrewarmOverride);
}

bool NavigationStateMachine::TryScanApproachCollect(const RouteTrackingState& route, const Waypoint& waypoint)
{
    (void)waypoint;
    if (maa_context_ == nullptr || collect_scanner_ == nullptr || !route.startup_motion_confirmed) {
        return false;
    }

    const auto now = std::chrono::steady_clock::now();
    if (collect_scan_last_at_.time_since_epoch().count() != 0
        && now - collect_scan_last_at_ < std::chrono::milliseconds(kCollectScanIntervalMs)) {
        return false;
    }

    if (!collect_scanner_->ConsumeDetection()) {
        return false;  // background worker has not flagged a collectible — keep walking, zero cost this tick
    }

    if (collect_attempt_pos_valid_ && position_ != nullptr && position_->valid) {
        const bool zone_changed = !collect_attempt_pos_.zone_id.empty() && !position_->zone_id.empty()
                                  && collect_attempt_pos_.zone_id != position_->zone_id;
        const double moved = std::hypot(position_->x - collect_attempt_pos_.x, position_->y - collect_attempt_pos_.y);
        if (!zone_changed && moved < kCollectRetryMinMoveWu) {
            LogDebug << "Collect detection suppressed (anti-stuck): not past the last attempt yet." << VAR(moved);
            return false;
        }
    }

    collect_scan_last_at_ = now;
    if (position_ != nullptr && position_->valid) {
        collect_attempt_pos_ = *position_;
        collect_attempt_pos_valid_ = true;
    }

    LogInfo << "Async collectible flagged — stopping for authoritative collect." << VAR(route.waypoint_distance)
            << VAR(session_->current_node_idx());
    motion_controller_->SetForwardState(false);
    utils::SleepFor(kStopWaitMs);
    MaaContextRunTask(maa_context_, kDefaultCollectEntry, kCollectPipelineOverride);
    utils::SleepFor(kCollectPostSleepMs);
    return true;
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
