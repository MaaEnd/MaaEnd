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
#include "semantic_helpers.h"
#include "semantic_nodes.h"
#include "sensitivity_observer.h"
#include "steering_controller.h"
#include "trigger_action.h"
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

bool ZonesDiffer(const std::string& a, const std::string& b)
{
    return !a.empty() && !b.empty() && a != b;
}

bool IsZoneCompatible(const Waypoint& waypoint, const std::string& current_zone_id)
{
    return waypoint.HasPosition() && !ZonesDiffer(waypoint.zone_id, current_zone_id);
}

constexpr size_t kUnaddressableAnchorIndex = std::numeric_limits<size_t>::max();

bool IsRequiredSemanticAnchor(const Waypoint& waypoint)
{
    if (!waypoint.HasPosition()) {
        return waypoint.IsHeadingOnly() || waypoint.IsZoneDeclaration();
    }
    return !waypoint.IsContinuousRun();
}

double ArrivalBandForStartupBypass(const Waypoint& waypoint)
{
    return std::max(waypoint.ArrivalBand(kMeasurementDefaultPositionQuantum), waypoint.Traits().commit_distance);
}

template <typename Plan>
auto WithPlanningPause(MotionController* motion, NavigationRuntimeState* runtime, NavigationSession* session, Plan&& plan)
{
    const auto started_at = std::chrono::steady_clock::now();
    motion->SetForwardState(false);
    runtime->offroute.PauseAt(started_at);
    auto result = plan();
    session->ExcludeFromStallClocks(std::chrono::steady_clock::now() - started_at);
    return result;
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
            if (ZonesDiffer(waypoint.zone_id, position.zone_id)) {
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
        if (ZonesDiffer(from.zone_id, to.zone_id)) {
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

// Distances are measured from the anchor so the walk-back stays bounded; a positionless marker ends it.
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

    // Skipping ahead needs evidence; without it the route resumes further back.
    const std::vector<BootstrapWaypointCandidate> reachable = CollectReachableWaypoints(path, position);
    if (reachable.empty()) {
        return std::nullopt;
    }

    // The earliest standing waypoint wins so a loop route's coincident head is read as its head.
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

std::optional<DynamicAnchor> ResolveReachableNavmeshAnchor(
    const NaviParam& param,
    NavigationSession* session,
    const NaviPosition& position,
    size_t start_index,
    const char* reason)
{
    const size_t path_size = session->current_path().size();
    const std::optional<double> walked_floor_y = session->WalkedFloorY(position);
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
            if (ZonesDiffer(waypoint.zone_id, position.zone_id)) {
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
        // 第一个规划得通的候选即为入口
        ++plan_attempts;
        const auto route = PlanNavmeshRoute(
            param,
            position.zone_id,
            start,
            goal,
            waypoint.target_deck_y,
            walked_floor_y,
            nullptr,
            nullptr,
            session->LandedTowerDeckY(position));
        if (route) {
            anchor_cost = route->cost;
            anchor = { *canonical_index, waypoint };
            break;
        }
        if (IsRequiredSemanticAnchor(waypoint)) {
            break;
        }
        // 每次不可达都跑满一次秒级 A*, 连败到预算即放弃本轮扫描
        if (plan_attempts >= kReachableAnchorPlanAttemptsMax) {
            LogWarn << "Reachable navmesh anchor scan exhausted its plan budget." << VAR(reason) << VAR(plan_attempts) << VAR(index)
                    << VAR(path_size);
            break;
        }
    }

    // plan_attempts - 1 waypoints ahead of the anchor turned out unreachable.
    if (anchor) {
        LogInfo << "Reachable navmesh anchor selected." << VAR(reason) << VAR(anchor->first) << VAR(anchor_cost) << VAR(plan_attempts)
                << VAR(start_index);
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
    if (std::optional<DynamicAnchor> navmesh_anchor = ResolveReachableNavmeshAnchor(param, session, position, start_index, "bootstrap")) {
        return navmesh_anchor;
    }
    return ResolveCurrentAnchorFrom(session, position, start_index);
}

std::optional<DynamicAnchor> ResolveNavRunAnchor(NavigationSession* session, const NaviPosition& position, const RouteTrackingState& route)
{
    if (!route.valid || !session->HasCurrentWaypoint()) {
        return std::nullopt;
    }
    const Waypoint& current_waypoint = session->CurrentWaypoint();
    if (!current_waypoint.HasPosition() || current_waypoint.action != ActionType::RUN) {
        return std::nullopt;
    }
    if (current_waypoint.RequiresStrictArrival()) {
        return DynamicAnchor { session->CanonicalIndexAtCurrent().value_or(kUnaddressableAnchorIndex), current_waypoint };
    }
    return ResolveCurrentAnchor(session, position);
}

// 台沿下落的落点: 进圈时人可能还在台上, 沿下落方向越过它才算到
bool ShortOfDrop(const Waypoint& waypoint, const NaviPosition& position)
{
    if (!waypoint.drop_from) {
        return false;
    }
    const double dx = waypoint.x - (*waypoint.drop_from)[0];
    const double dy = waypoint.y - (*waypoint.drop_from)[1];
    return (position.x - waypoint.x) * dx + (position.y - waypoint.y) * dy < 0.0;
}

double EffectiveRouteHeading(const NavRunTickResult& nav_run_result, const RouteTrackingState& route)
{
    return nav_run_result.has_corridor_heading ? nav_run_result.corridor_heading : route.route_heading;
}

bool InsidePushThroughDisc(const std::vector<VirtualNoGoDisc>& discs, const NaviPosition& position)
{
    return std::any_of(discs.begin(), discs.end(), [&](const VirtualNoGoDisc& disc) {
        return disc.push_through && disc.zone_id == position.zone_id
               && std::hypot(disc.x - position.x, disc.y - position.y) <= disc.radius + kVirtualNoGoStandoff + kVirtualNoGoRadiusStep;
    });
}

void RestartRecovery(
    DynamicRecoveryState& recovery,
    const NaviPosition& position,
    std::chrono::steady_clock::time_point now,
    size_t anchor_index)
{
    recovery.Reset();
    recovery.active = true;
    recovery.anchor_pos = position;
    recovery.started_at = now;
    recovery.anchor_index = anchor_index;
}

struct TurnObservation
{
    double achieved_deg = 0.0;
    double residual_deg = 0.0;
    int64_t elapsed_ms = 0;
};

// Covers the whole heading change since the last sent turn, forward motion and camera follow included.
TurnObservation ObserveLastTurn(const SteeringRateState& steering_rate, double steer_heading, std::chrono::steady_clock::time_point now)
{
    if (!steering_rate.has_cmd) {
        return {};
    }
    const double achieved_deg = NaviMath::NormalizeAngle(steer_heading - steering_rate.cmd_heading_deg);
    return {
        .achieved_deg = achieved_deg,
        .residual_deg = NaviMath::NormalizeAngle(achieved_deg - steering_rate.cmd_delta_deg),
        .elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - steering_rate.cmd_at).count(),
    };
}

struct HeadingRate
{
    double rate_deg = 0.0;
    double raw_delta_deg = 0.0;
    int64_t gap_ms = 0;
    uint64_t gap_ticks = 0;
};

HeadingRate
    TrackHeadingRate(SteeringRateState& steering_rate, double steer_heading, std::chrono::steady_clock::time_point now, uint64_t tick_seq)
{
    if (!steering_rate.has_prev) {
        steering_rate.prev_heading_deg = steer_heading;
        steering_rate.at = now;
        steering_rate.at_tick = tick_seq;
        steering_rate.has_prev = true;
        return {};
    }
    HeadingRate rate;
    rate.raw_delta_deg = NaviMath::NormalizeAngle(steer_heading - steering_rate.prev_heading_deg);
    rate.gap_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - steering_rate.at).count();
    rate.gap_ticks = tick_seq - steering_rate.at_tick;
    const bool heading_changed = std::abs(rate.raw_delta_deg) > kSteeringHeadingChangeEpsilonDeg;
    // Staleness is counted in ticks because slow frame capture stretches the gaps without making them fewer.
    if (heading_changed && rate.gap_ms > 0 && rate.gap_ticks <= kSteeringRateMaxGapTicks) {
        rate.rate_deg = rate.raw_delta_deg * static_cast<double>(kSteeringRateReferenceMs) / static_cast<double>(rate.gap_ms);
    }
    if (heading_changed) {
        steering_rate.prev_heading_deg = steer_heading;
        steering_rate.at = now;
        steering_rate.at_tick = tick_seq;
    }
    return rate;
}

std::optional<int64_t> SampleSteerPeriod(FlowState& flow, std::chrono::steady_clock::time_point now)
{
    if (flow.last_steer_tick_at.time_since_epoch().count() > 0) {
        const int64_t period_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - flow.last_steer_tick_at).count();
        flow.steer_periods_ms.push_back(std::min(period_ms, kSteerPeriodMaxSampleMs));
        if (flow.steer_periods_ms.size() > kSteerPeriodWindow) {
            flow.steer_periods_ms.erase(flow.steer_periods_ms.begin());
        }
    }
    flow.last_steer_tick_at = now;
    return flow.steer_periods_ms.empty() ? std::nullopt : std::optional<int64_t>(latency::Median(flow.steer_periods_ms));
}

} // namespace

std::optional<size_t> ResolveRouteResumeIndex(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    const std::optional<BootstrapContinueCandidate> candidate = ResolveBootstrapContinueCandidate(path, position);
    if (!candidate || candidate->continue_index == 0 || candidate->continue_index >= path.size()) {
        return std::nullopt;
    }
    LogInfo << "Route resume index resolved." << VAR(candidate->continue_index) << VAR(candidate->route_distance) << VAR(candidate->reason)
            << VAR(position.x) << VAR(position.y) << VAR(position.zone_id);
    return candidate->continue_index;
}

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

semantic_nodes::Context NavigationStateMachine::SemanticContext()
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

bool NavigationStateMachine::Run()
{
    latency::BeginRun();

    if (!Bootstrap()) {
        StopMotion();
        sensitivity::EndRun(maa_context_);
        return false;
    }

    // Pay the recognition cold start while still stopped, so it can never land on a walking tick.
    PreWarmPromptRecognition();

    StartScanners();

    while (!should_stop_() && session_->phase() != NaviPhase::Finished && session_->phase() != NaviPhase::Failed) {
        if (!TickPhase(session_->phase())) {
            StopScanners();
            StopMotion();
            sensitivity::EndRun(maa_context_);
            return false;
        }
    }

    if (!should_stop_() && session_->phase() != NaviPhase::Failed) {
        session_->HasSatisfiedFinalSuccess(*position_, "navigation_complete");
    }

    TryRunPromptSubtaskAtRouteTail();

    StopScanners();
    StopMotion();

    const bool succeeded = !should_stop_() && session_->success();
    sensitivity::EndRun(maa_context_);
    return succeeded;
}

bool NavigationStateMachine::Bootstrap()
{
    runtime_state_.BeginNavigation(std::chrono::steady_clock::now());
    sensitivity::BeginRun();

    // 起步 A* 是否跳过只看线路声明, 跳过时落到下面两档兜底
    std::optional<DynamicAnchor> anchor;
    if (param_.enable_bootstrap_navmesh) {
        anchor = ResolveBootstrapAnchor(param_, session_, *position_);
    }
    else {
        LogInfo << "Bootstrap navmesh planning disabled by route param." << VAR(position_->x) << VAR(position_->y)
                << VAR(position_->zone_id);
    }
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
    // 相位切换后镜头可能被别的流程转过, 包括绕过 ActionWrapper 的转向, 所以在这里作废先验观测
    if (!last_tick_phase_ || *last_tick_phase_ != phase) {
        last_tick_phase_ = phase;
        action_wrapper_->NoteHeadingDisturbed();
    }
    // Ahead of every early return, so no branch or phase can strand the game in walking mode.
    UpdateWalkMode(phase);

    switch (phase) {
    case NaviPhase::Bootstrap:
        SelectPhaseForCurrentWaypoint("bootstrap_dispatch");
        return true;
    case NaviPhase::Navigate:
        return TickNavigate();
    case NaviPhase::WaitTransfer:
    case NaviPhase::WaitZipline:
    case NaviPhase::WaitFind:
    case NaviPhase::WaitJumpPad:
    case NaviPhase::WaitRope: {
        // 顶墙计数和设备命中只在 Navigate 里攒, 语义动作做完人可能已换了地方
        runtime_state_.flow.motionless_hold_ticks = 0;
        runtime_state_.flow.futile_forward_reasserts = 0;
        device_recovery_.ForgetObservation();
        const semantic_nodes::Result semantic_result = semantic_nodes::TickSemanticFlow(SemanticContext(), phase);
        if (semantic_result.request_failure) {
            return FailNavigation(semantic_result.failure_reason, semantic_result.failure_log_message, 0.0, 0.0, 0);
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

bool NavigationStateMachine::CaptureCurrentPosition(bool force_global_search)
{
    // 纪元未变说明自上次取位以来没有发过任何 yaw 指令, 先验才可用
    const uint64_t heading_epoch = action_wrapper_->heading_epoch();
    const bool prior_usable = !force_global_search && param_.heading_source == HeadingSource::Camera && position_->valid
                              && prior_observation_epoch_ && *prior_observation_epoch_ == heading_epoch
                              && session_->phase() == NaviPhase::Navigate;
    const std::optional<double> camera_heading_prior = prior_usable ? std::optional<double>(position_->angle) : std::nullopt;
    const bool captured =
        position_provider_->Capture(position_, force_global_search, session_->current_zone_id(), {}, camera_heading_prior);
    if (captured) {
        // 记取位前读到的纪元, 本拍随后发的转向由下一拍挡住
        prior_observation_epoch_ = heading_epoch;
        consecutive_prior_uses_ = camera_heading_prior ? consecutive_prior_uses_ + 1 : 0;
        LogDebug << "MapNavigator camera heading prior" << VAR(camera_heading_prior.value_or(-1.0)) << VAR(consecutive_prior_uses_);
    }
    else {
        consecutive_prior_uses_ = 0;
    }
    UpdateDwellWatchdog(captured);
    return captured;
}

// Fed from captures because unstick pulses and replans burn wall time inside ticks that never finish.
void NavigationStateMachine::UpdateDwellWatchdog(bool captured)
{
    DwellWatchdogState& dwell = runtime_state_.dwell;
    const bool usable = captured && position_->valid && !position_provider_->LastCaptureWasBlackScreen();
    // Only walking with a usable fix counts; other phases stand still by design.
    if (!usable || session_->phase() != NaviPhase::Navigate) {
        dwell.last_usable = {};
        return;
    }

    const auto now = std::chrono::steady_clock::now();
    const bool zone_changed = ZonesDiffer(dwell.center_zone, position_->zone_id);
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

bool NavigationStateMachine::HandleLocalizationLoss()
{
    const auto now = std::chrono::steady_clock::now();
    LocalizationLossState& loss = runtime_state_.localization_loss;
    if (loss.started_at == std::chrono::steady_clock::time_point {}) {
        loss.started_at = now;
        loss.last_fix = *position_;
    }
    runtime_state_.offroute.PauseAt(now);
    // A black capture during a loss means the agent fell into water.
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
            return ResumeAfterGlobalReacquire(prior_zone, loss_elapsed);
        }
    }

    const bool unstick_cooling = loss.last_unstick_at != std::chrono::steady_clock::time_point {}
                                 && std::chrono::duration_cast<std::chrono::milliseconds>(now - loss.last_unstick_at)
                                        < std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs);
    // TRIGGER 未命中时不盲跳: 小地图可能正被待命中的画面盖住, 按键会干扰它
    if (loss_elapsed >= std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs) && !unstick_cooling
        && !semantic_nodes::IsTriggerPending(*session_)) {
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

bool NavigationStateMachine::ResumeAfterGlobalReacquire(const std::string& prior_zone, std::chrono::milliseconds loss_elapsed)
{
    LocalizationLossState& loss = runtime_state_.localization_loss;
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
        // Without this the next capture re-imposes the stale expected_zone and fails again.
        session_->UpdateCurrentZone(position_->zone_id);
    }
    // The streak counts recover/re-lose cycles; a short on-the-spot drop in the same zone is just lost frames.
    const bool dropped_frames = !zone_changed && !loss.saw_black_screen
                                && loss_elapsed < std::chrono::milliseconds(kLocalizationLossUnstickIntervalMs)
                                && std::hypot(position_->x - loss.last_fix.x, position_->y - loss.last_fix.y) < kDwellWatchdogRadius;
    if (!dropped_frames && ++runtime_state_.global_reacquire_streak >= kLocalizationThrashFailCount) {
        return FailNavigation(
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
    ArmRiverFallRecoveryIfBlackScreenLoss("global_reacquire");
    loss.Reset();
    runtime_state_.route.ResetTracking();
    runtime_state_.nav_run_dirty = true;
    session_->ResetProgress();
    return true;
}

bool NavigationStateMachine::ArmRiverFallRecoveryIfBlackScreenLoss(const char* via)
{
    if (!runtime_state_.localization_loss.saw_black_screen) {
        return false;
    }
    // A re-fall must redo the settle and the about-face.
    runtime_state_.river_fall.Reset();
    runtime_state_.river_fall.pending = true;
    runtime_state_.river_fall.anchor_pos = *position_;
    runtime_state_.river_fall.water_heading = NaviMath::NormalizeAngle(position_->angle);
    // The pre-fall recovery anchor is stale after the teleport and would pre-empt the escape.
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
    bool emit_interior_corners)
{
    if (!anchor.HasPosition()) {
        LogWarn << "Dynamic navmesh overlay skipped: anchor has no position." << VAR(reason) << VAR(continue_index);
        return false;
    }

    const navmesh::WorldPoint start { .x = position_->x, .y = position_->y };
    const navmesh::WorldPoint goal { .x = anchor.x, .y = anchor.y };
    // 被挤出网格(搬设备、卡墙脱困之后)时路线从网格上的入口点规划, 先直着走上入口点
    std::optional<navmesh::WorldPoint> start_recovery_entry;
    const auto route = WithPlanningPause(motion_controller_, &runtime_state_, session_, [&] {
        return PlanNavmeshRoute(
            param_,
            position_->zone_id,
            start,
            goal,
            anchor.target_deck_y,
            session_->WalkedFloorY(*position_),
            nullptr,
            &runtime_state_.virtual_no_go,
            session_->LandedTowerDeckY(*position_),
            &start_recovery_entry);
    });
    if (!route) {
        return false;
    }

    std::vector<Waypoint> generated_prefix;
    if (start_recovery_entry) {
        generated_prefix.emplace_back(start_recovery_entry->x, start_recovery_entry->y, ActionType::RUN);
        generated_prefix.back().strict_arrival = false;
    }
    if (!AppendGeneratedNavmeshWaypoints(
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
    runtime_state_.offroute.InvalidateBaseline();
    runtime_state_.nav_run_dirty = true;
    if (generated_count == 0 && std::hypot(anchor.x - position_->x, anchor.y - position_->y) <= ArrivalBandForStartupBypass(anchor)) {
        runtime_state_.route.startup_anchor_pos = *position_;
        runtime_state_.route.startup_anchor_initialized = true;
        runtime_state_.route.startup_motion_confirmed = true;
    }
    runtime_state_.dynamic_replan_requested = false;
    const size_t planned_points = route->path.points.size();
    LogInfo << "Dynamic navmesh overlay selected." << VAR(reason) << VAR(continue_index) << VAR(generated_count) << VAR(planned_points)
            << VAR(runtime_state_.virtual_no_go.size()) << VAR(anchor.x) << VAR(anchor.y);
    return true;
}

bool NavigationStateMachine::TryApplyDynamicOverlayToNextAnchor(const char* reason)
{
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    if (!anchor) {
        runtime_state_.dynamic_replan_requested = false;
        LogInfo << "Dynamic navmesh overlay skipped: no future anchor." << VAR(reason) << VAR(position_->x) << VAR(position_->y)
                << VAR(position_->zone_id);
        return false;
    }
    return TryApplyDynamicOverlayToAnchor(reason, anchor->first, anchor->second, /*emit_interior_corners=*/false);
}

bool NavigationStateMachine::TryVirtualNoGoReplan(
    const char* reason,
    size_t continue_index,
    const Waypoint& anchor,
    const NaviPosition& origin,
    double stuck_heading)
{
    std::vector<VirtualNoGoDisc>& discs = runtime_state_.virtual_no_go;
    // 朝向是罗盘角(0 朝上, 顺时针), 与 PlanUnstickTarget 同一口径
    const double rad = NaviMath::NormalizeHeading(stuck_heading) * kPi / 180.0;
    const double dir_x = std::sin(rad);
    const double dir_y = -std::cos(rad);
    // 圆心取在卡死点前方, 留出 standoff 以免把卡死点本身圈进禁区
    const auto center_x = [&](double radius) {
        return origin.x + dir_x * (radius + kVirtualNoGoStandoff);
    };
    const auto center_y = [&](double radius) {
        return origin.y + dir_y * (radius + kVirtualNoGoStandoff);
    };
    // 逐次折半收缩, 直到当前位置落在禁区之外
    const auto fit_outside = [&](double radius) {
        while (radius >= kVirtualNoGoRadiusMin && std::hypot(position_->x - center_x(radius), position_->y - center_y(radius)) < radius) {
            radius *= 0.5;
        }
        return radius;
    };

    // 在既有禁区边缘再次卡死说明障碍更大, 新禁区半径加一档
    double radius = kVirtualNoGoRadius;
    bool grown = false;
    for (const VirtualNoGoDisc& disc : discs) {
        if (disc.zone_id != origin.zone_id || disc.push_through) {
            continue;
        }
        if (std::hypot(center_x(kVirtualNoGoRadius) - disc.x, center_y(kVirtualNoGoRadius) - disc.y)
            <= disc.radius + kVirtualNoGoRadiusStep) {
            radius = std::max(radius, std::min(disc.radius + kVirtualNoGoRadiusStep, kVirtualNoGoRadiusMax));
            grown = true;
        }
    }
    // 半径上界按锚点正处前方的最坏情形取: 距离 - standoff - 半径 ≥ 半径 + 净空
    const double anchor_distance = std::hypot(anchor.x - origin.x, anchor.y - origin.y);
    const double radius_cap = (anchor_distance - kVirtualNoGoStandoff - kVirtualNoGoGoalClearance) * 0.5;
    radius = fit_outside(std::min(radius, radius_cap));
    if (radius < kVirtualNoGoRadiusMin) {
        LogInfo << "Virtual no-go skipped: no room for a disc between here and the anchor." << VAR(reason) << VAR(anchor_distance)
                << VAR(radius);
        return false;
    }

    discs.push_back({ .zone_id = origin.zone_id, .x = center_x(radius), .y = center_y(radius), .radius = radius });
    VirtualNoGoDisc* disc = &discs.back();
    LogInfo << "Virtual no-go stamped." << VAR(reason) << VAR(grown) << VAR(disc->x) << VAR(disc->y) << VAR(disc->radius)
            << VAR(stuck_heading) << VAR(discs.size());

    if (TryApplyDynamicOverlayToAnchor(reason, continue_index, anchor)) {
        return true;
    }
    // 禁区可能封住了整条窄路, 半径折半再规划一次
    const double shrunk = fit_outside(disc->radius * 0.5);
    if (shrunk >= kVirtualNoGoRadiusMin) {
        disc->radius = shrunk;
        disc->x = center_x(shrunk);
        disc->y = center_y(shrunk);
        LogInfo << "Virtual no-go shrunk after replan failure." << VAR(reason) << VAR(disc->radius);
        if (TryApplyDynamicOverlayToAnchor(reason, continue_index, anchor)) {
            return true;
        }
    }
    // 收缩后仍失败说明此处是唯一通路, 禁区只留作物理脱困的标记
    disc->push_through = true;
    LogWarn << "Virtual no-go blocks the only passage; marked push-through." << VAR(reason) << VAR(disc->x) << VAR(disc->y)
            << VAR(disc->radius);
    return false;
}

// 同一个上索点的重规划次数用满就当它够不着, 丢链改走路
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
        approach.spot_index = 0;
    }
    if (++approach.replans <= kZiplineApproachReplanBudget) {
        return false;
    }

    LogWarn << "Zipline mount tower unreachable after repeated replans; walking instead." << VAR(reason) << VAR(approach.replans)
            << VAR(anchor->second.x) << VAR(anchor->second.y) << VAR(position_->x) << VAR(position_->y);
    runtime_state_.dynamic_replan_requested = false;
    semantic_nodes::AbandonZipline(SemanticContext(), "zipline_unreachable", reason);
    return true;
}

bool NavigationStateMachine::HandleZiplineRecoveryReplan()
{
    ZiplineRecoveryState& recovery = runtime_state_.zipline_recovery;
    StopMotion();
    const auto now = std::chrono::steady_clock::now();
    const int64_t elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - recovery.started_at).count();
    if (elapsed_ms >= kZiplineRecoveryTimeoutMs) {
        return FailNavigation(
            "zipline_recovery_localization_timeout",
            "Zipline recovery could not obtain a stable on-mesh position; refusing to follow the stale departure route.",
            0.0,
            0.0,
            elapsed_ms);
    }

    const bool force_global_search = recovery.stable_hits == 0;
    if (!CaptureCurrentPosition(force_global_search)) {
        utils::SleepFor(kZiplineRecoveryRetryIntervalMs);
        return true;
    }

    // 人留在架子上等重规划时脚下是架子, 不要求贴回 navmesh
    const bool on_tower = runtime_state_.IsZiplineMounted();
    const navmesh::WorldPoint fix { .x = position_->x, .y = position_->y };
    const auto snap = NavmeshSnapAt(param_, position_->zone_id, fix, param_.navmesh_snap_radius);
    const bool on_mesh = on_tower || (snap && snap->distance <= param_.navmesh_snap_radius);
    // 贴不回可走面的定位照样接受, 重展开时会吸附回最近的可走面
    const bool same_fix =
        recovery.stable_hits > 0 && recovery.stable_pos.zone_id == position_->zone_id
        && std::hypot(recovery.stable_pos.x - position_->x, recovery.stable_pos.y - position_->y) <= kZiplineRecoveryStableRadiusWu;
    recovery.stable_pos = *position_;
    recovery.stable_hits = same_fix ? recovery.stable_hits + 1 : 1;
    if (recovery.stable_hits < kZiplineRecoveryStableFixes) {
        utils::SleepFor(kZiplineRecoveryRetryIntervalMs);
        return true;
    }

    if (!position_->zone_id.empty()) {
        session_->UpdateCurrentZone(position_->zone_id);
    }
    LogInfo << "Zipline recovery position stabilized." << VAR(elapsed_ms) << VAR(recovery.stable_hits) << VAR(position_->x)
            << VAR(position_->y) << VAR(position_->zone_id);

    // 剩余展开从链尾落点算起, 人没到那里时先回作者路线从当前位置重新展开
    bool rejoined = TryReplanRemainingAuthoredRoute("zipline_recovery_reexpand");
    if (!rejoined && on_tower) {
        LogWarn << "Zipline recovery could not re-expand from the tower; stepping down to retry from the ground." << VAR(elapsed_ms)
                << VAR(position_->x) << VAR(position_->y);
        semantic_nodes::LeaveZiplineTower(SemanticContext());
        recovery.Begin(std::chrono::steady_clock::now());
        return true;
    }
    if (!rejoined && on_mesh) {
        const std::optional<DynamicAnchor> anchor = WithPlanningPause(motion_controller_, &runtime_state_, session_, [&] {
            return ResolveReachableNavmeshAnchor(param_, session_, *position_, session_->current_node_idx(), "zipline_recovery");
        });
        rejoined = anchor && TryApplyDynamicOverlayToAnchor("zipline_recovery", anchor->first, anchor->second);
    }
    if (!rejoined) {
        return FailNavigation(
            "zipline_recovery_route_unavailable",
            "Zipline recovery found no reachable point in the remaining route and could not re-expand the authored route; "
            "refusing to follow the stale departure route.",
            0.0,
            0.0,
            elapsed_ms);
    }

    recovery.Reset();
    // 人还在架子上即新路线从脚下起滑, 直接开跳
    if (runtime_state_.IsZiplineMounted()) {
        semantic_nodes::StartZiplineHop(SemanticContext(), session_->CurrentWaypoint(), 0.0);
        return true;
    }
    SelectPhaseForCurrentWaypoint("zipline_recovery");
    return true;
}

bool NavigationStateMachine::TryReplanRemainingAuthoredRoute(const char* reason)
{
    const std::vector<Waypoint>& authored = param_.authored_path;
    if (authored.empty()) {
        return false;
    }

    size_t slice_begin = authored.size();
    for (size_t index = session_->current_node_idx(); index < session_->current_path().size(); ++index) {
        const std::optional<size_t> canonical_index = session_->CanonicalIndexAtCurrentPath(index);
        if (!canonical_index) {
            continue;
        }
        const size_t stamp = session_->original_path()[*canonical_index].authored_group_begin;
        if (stamp < authored.size()) {
            slice_begin = stamp;
            break;
        }
    }
    if (slice_begin >= authored.size()) {
        LogWarn << "Authored route replan unavailable: no authored origin recorded past the current node." << VAR(reason)
                << VAR(session_->current_node_idx()) << VAR(session_->current_path().size());
        return false;
    }

    NaviParam replan_param = param_;
    // 只封禁判死过的跳, 弃索次数过多才整段退回走路
    const HopLedger& ledger = runtime_state_.zipline_ride.ledger();
    const int32_t rope_failures = CountZiplineRopeFailures(ledger);
    if (rope_failures >= kZiplineAbandonWalkFallbackCount) {
        replan_param.zipline_enabled = false;
        LogWarn << "Authored route replan disables ziplines: too many rope failures this run." << VAR(rope_failures);
    }
    else {
        replan_param.zipline_ledger = ledger;
    }
    replan_param.rope_off.insert(replan_param.rope_off.end(), rope_off_.begin(), rope_off_.end());
    replan_param.path.assign(authored.begin() + static_cast<std::ptrdiff_t>(slice_begin), authored.end());
    std::vector<Waypoint> replanned;
    const bool expanded = WithPlanningPause(motion_controller_, &runtime_state_, session_, [&] {
        return ExpandNavmeshWaypoints(
            replan_param,
            *position_,
            should_stop_,
            replanned,
            nullptr,
            &runtime_state_.virtual_no_go,
            session_->LandedTowerDeckY(*position_));
    });
    if (!expanded || replanned.empty()) {
        LogWarn << "Authored route replan failed to expand the remaining route." << VAR(reason) << VAR(slice_begin)
                << VAR(replan_param.path.size());
        return false;
    }
    // 组首下标是切片内偏移, 折回完整作者路线的下标
    for (Waypoint& waypoint : replanned) {
        if (waypoint.authored_group_begin != std::numeric_limits<size_t>::max()) {
            waypoint.authored_group_begin += slice_begin;
        }
    }

    session_->ReplaceRoute(std::move(replanned), *position_, reason);
    runtime_state_.route.Reset();
    runtime_state_.offroute.InvalidateBaseline();
    runtime_state_.nav_run_dirty = true;
    runtime_state_.dynamic_replan_requested = false;

    // 新路线仍从脚下这根架子起滑就留在上面且不铺接入段, 用不上就先下来
    if (runtime_state_.IsZiplineMounted()) {
        const semantic_nodes::Context ctx = SemanticContext();
        if (semantic_nodes::SkipToHopUnderfoot(ctx, reason)) {
            LogInfo << "Zipline recovery re-expanded the remaining authored route; the next hop leaves from this tower." << VAR(reason)
                    << VAR(slice_begin) << VAR(session_->current_path().size());
            return true;
        }
        semantic_nodes::LeaveZiplineTower(ctx);
    }

    const std::optional<DynamicAnchor> anchor = WithPlanningPause(motion_controller_, &runtime_state_, session_, [&] {
        return ResolveReachableNavmeshAnchor(param_, session_, *position_, 0, reason);
    });
    if (anchor) {
        TryApplyDynamicOverlayToAnchor(reason, anchor->first, anchor->second);
    }
    LogInfo << "Zipline recovery re-expanded the remaining authored route." << VAR(reason) << VAR(slice_begin)
            << VAR(session_->current_path().size()) << VAR(position_->x) << VAR(position_->y) << VAR(position_->zone_id);
    return true;
}

bool NavigationStateMachine::AbandonRope(uint32_t uid)
{
    rope_off_.push_back(uid);
    LogWarn << "Rope unavailable at the bottom; re-expanding the remaining route without it." << VAR(uid) << VAR(position_->x)
            << VAR(position_->y) << VAR(rope_off_.size());
    if (!TryReplanRemainingAuthoredRoute("rope_unavailable")) {
        return FailNavigation(
            "rope_unavailable",
            "Rope prompt not recognized at the bottom and the remaining route could not be re-expanded without it.",
            0.0,
            0.0,
            0);
    }
    SelectPhaseForCurrentWaypoint("rope_unavailable");
    return true;
}

bool NavigationStateMachine::HandleDynamicReplanRequest(const char* reason)
{
    if (GiveUpUnreachableZipline(reason)) {
        return true;
    }
    if (TryApplyDynamicOverlayToNextAnchor(reason)) {
        return true;
    }

    // 重规划失败时沿当前路线继续, 只清跟随进度, 起步门保留
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
        if (TryApplyDynamicOverlayToAnchor(reason, *continue_index, candidate, /*emit_interior_corners=*/true)) {
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
        // A blind fix likely means a river fall, so stop and leave it to loss handling.
        if (!CaptureCurrentPosition(false) || position_provider_->LastCaptureWasBlackScreen() || !position_->valid) {
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

    // 禁区以位移前的位置为原点, 障碍在那个位置的前方
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    const bool replanned =
        (anchor && TryVirtualNoGoReplan("recovery_unstick_replan", anchor->first, anchor->second, step_start, stuck_heading))
        || TryApplyDynamicOverlayToNextAnchor("recovery_unstick_replan");
    if (replanned) {
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

struct NavigationStateMachine::NavigateTick
{
    std::chrono::steady_clock::time_point tick_started_at {};
    int64_t tick_gap_ms = 0;
    int64_t capture_ms = 0;
    std::chrono::steady_clock::time_point now {};
    bool startup_grace_elapsed = false;
    double current_heading = 0.0;
    bool drops_turn_sends = false;
    bool arrow_lags_camera = false;
    bool steering_on_camera = false;
    double steer_heading = 0.0;
    bool degraded_fix = false;
    int64_t fix_age_ms = 0;
    RouteTrackingState route {};
    NavRunTickResult nav_run_result {};
    std::optional<size_t> nav_run_anchor_index;
    double effective_progress = 0.0;
    int64_t stalled_ms = 0;
    Waypoint waypoint {};
    double arrival_distance = 0.0;
    double heading_error = 0.0;
};

bool NavigationStateMachine::TickNavigate()
{
    NavigateTick tick = BeginNavigateTick();
    if (const TickExit exit = RunSemanticFlow()) {
        return *exit;
    }
    if (const TickExit exit = CaptureNavigateFix(tick)) {
        return *exit;
    }
    if (const TickExit exit = CheckDwellWatchdog()) {
        return *exit;
    }
    if (const TickExit exit = RunInlineSemantics()) {
        return *exit;
    }
    if (const TickExit exit = IdleWhileNotWalking()) {
        return *exit;
    }
    TrackRoute(tick);
    if (const TickExit exit = CheckRouteProgress(tick)) {
        return *exit;
    }
    if (const TickExit exit = HandlePrompts(tick)) {
        return *exit;
    }
    if (const TickExit exit = HandleWaypointArrival(tick)) {
        return *exit;
    }
    if (const TickExit exit = RunRiverFallRecovery(tick)) {
        return *exit;
    }
    if (const TickExit exit = CheckOffRouteWedge(tick)) {
        return *exit;
    }
    if (const TickExit exit = RunObstacleRecovery(tick)) {
        return *exit;
    }
    SteerAlongCorridor(tick);
    return FinishNavigateTick(tick);
}

NavigationStateMachine::NavigateTick NavigationStateMachine::BeginNavigateTick()
{
    NavigateTick tick;
    tick.tick_started_at = std::chrono::steady_clock::now();
    tick.tick_gap_ms =
        runtime_state_.flow.last_tick_started_at.time_since_epoch().count() > 0
            ? std::chrono::duration_cast<std::chrono::milliseconds>(tick.tick_started_at - runtime_state_.flow.last_tick_started_at).count()
            : 0;
    runtime_state_.flow.last_tick_started_at = tick.tick_started_at;
    ++runtime_state_.flow.tick_seq;
    return tick;
}

NavigationStateMachine::TickExit NavigationStateMachine::ConcludeSemanticStep(const semantic_nodes::Result& result)
{
    if (result.request_failure) {
        return FailNavigation(result.failure_reason, result.failure_log_message, 0.0, 0.0, 0);
    }
    // Must precede the replan gate, which returns first when a node holds the tick and asks for a replan.
    if (result.stay_in_current_tick) {
        runtime_state_.dwell.last_usable = {};
    }
    if (runtime_state_.dynamic_replan_requested) {
        return runtime_state_.zipline_recovery.pending ? HandleZiplineRecoveryReplan() : HandleDynamicReplanRequest("dynamic_replan");
    }
    if (result.stay_in_current_tick) {
        return true;
    }
    return std::nullopt;
}

NavigationStateMachine::TickExit NavigationStateMachine::RunSemanticFlow()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }
    return ConcludeSemanticStep(semantic_nodes::TickSemanticFlow(SemanticContext(), NaviPhase::Navigate));
}

NavigationStateMachine::TickExit NavigationStateMachine::CaptureNavigateFix(NavigateTick& tick)
{
    const auto capture_started_at = std::chrono::steady_clock::now();
    const bool position_captured = CaptureCurrentPosition(false);
    tick.capture_ms = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - capture_started_at).count();
    // 须在定位丢失分支之前: 待命中的画面可能盖住小地图
    const semantic_nodes::Result trigger_result = semantic_nodes::ProbeTriggerWhileNavigating(SemanticContext());
    if (trigger_result.request_failure) {
        return FailNavigation(trigger_result.failure_reason, trigger_result.failure_log_message, 0.0, 0.0, 0);
    }
    if (trigger_result.consumed) {
        return true;
    }
    if (!position_captured) {
        return HandleLocalizationLoss();
    }
    if (EndLocalizationLoss()) {
        return true;
    }
    return std::nullopt;
}

bool NavigationStateMachine::EndLocalizationLoss()
{
    LocalizationLossState& loss = runtime_state_.localization_loss;
    if (loss.started_at == std::chrono::steady_clock::time_point {}) {
        loss.Reset();
        return false;
    }
    const auto loss_ms = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - loss.started_at).count();
    const bool armed = ArmRiverFallRecoveryIfBlackScreenLoss("normal_reacquire");
    LogInfo << "Localization recovered via normal tracking." << VAR(loss_ms) << VAR(loss.saw_black_screen) << VAR(armed)
            << VAR(position_->x) << VAR(position_->y);
    loss.Reset();
    return armed;
}

// Checked as early as a fix allows so no later step of this tick can outrun it.
NavigationStateMachine::TickExit NavigationStateMachine::CheckDwellWatchdog()
{
    if (runtime_state_.dwell.dwell_ms < kDwellWatchdogFailMs || !session_->HasCurrentWaypoint()) {
        return std::nullopt;
    }
    const Waypoint& pinned_at = session_->CurrentWaypoint();
    // 索边卡死先退索改走路, 退链后再超时才判导航失败
    if (pinned_at.action == ActionType::ZIPLINE) {
        LogWarn << "Dwell watchdog tripped at a zipline tower; dropping the chain and walking." << VAR(runtime_state_.dwell.dwell_ms)
                << VAR(position_->x) << VAR(position_->y);
        semantic_nodes::AbandonZipline(SemanticContext(), "zipline_dwell_watchdog", "never left the tower's dwell radius");
        runtime_state_.dwell.Reset();
        return true;
    }
    LogError << "Dwell watchdog tripped; the agent never left its dwell radius." << VAR(runtime_state_.dwell.dwell_ms)
             << VAR(runtime_state_.dwell.center_x) << VAR(runtime_state_.dwell.center_y) << VAR(position_->x) << VAR(position_->y)
             << VAR(session_->current_node_idx());
    return FailNavigation(
        "dwell_watchdog",
        "Agent stayed inside the dwell radius past the watchdog budget; terminating so the pipeline can retry.",
        std::hypot(position_->x - pinned_at.x, position_->y - pinned_at.y),
        0.0,
        runtime_state_.dwell.dwell_ms);
}

NavigationStateMachine::TickExit NavigationStateMachine::RunInlineSemantics()
{
    // 排在内联语义之前, 让随后的 HEADING 转身从对齐后的镜头起算
    if (runtime_state_.camera_align_pending) {
        runtime_state_.camera_align_pending = false;
        semantic_nodes::AlignCameraToHeadingOnce(SemanticContext());
        if (!position_->valid) {
            return HandleLocalizationLoss();
        }
    }
    EndCrossTierEscapeAtRejoin();
    if (const TickExit exit = ConcludeSemanticStep(semantic_nodes::ConsumeInlineSemantics(SemanticContext()))) {
        return exit;
    }
    if (!position_->valid) {
        return HandleLocalizationLoss();
    }
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }
    return std::nullopt;
}

void NavigationStateMachine::EndCrossTierEscapeAtRejoin()
{
    if (!runtime_state_.cross_tier_escape.active) {
        return;
    }
    const double distance_to_goal =
        std::hypot(position_->x - runtime_state_.cross_tier_escape.goal_x, position_->y - runtime_state_.cross_tier_escape.goal_y);
    const bool on_floorless_zone = NavmeshFloorYForZone(param_, position_->zone_id) <= navmesh::kBaseNavFloorYValidMin;
    if (distance_to_goal <= kCrossTierEscapeArrivalM && on_floorless_zone) {
        LogInfo << "Cross-tier escape reached the rejoin point on the base floor; resuming authored route." << VAR(distance_to_goal)
                << VAR(position_->zone_id) << VAR(position_->x) << VAR(position_->y);
        runtime_state_.cross_tier_escape.Reset();
    }
}

NavigationStateMachine::TickExit NavigationStateMachine::IdleWhileNotWalking()
{
    if (runtime_state_.semantic.portal_transit_active || session_->phase() != NaviPhase::Navigate) {
        utils::SleepFor(kTargetTickMs);
        return true;
    }
    if (session_->CurrentWaypoint().IsZoneDeclaration()) {
        motion_controller_->SetForwardState(true);
        utils::SleepFor(kTargetTickMs);
        return true;
    }
    return std::nullopt;
}

void NavigationStateMachine::TrackRoute(NavigateTick& tick)
{
    ObserveFix(tick);
    const size_t node_idx_before_tracking = session_->current_node_idx();
    tick.route = RouteTracker::Update(session_, &runtime_state_.route, *position_);
    if (session_->current_node_idx() != node_idx_before_tracking) {
        runtime_state_.recovery.Reset();
    }
    TickNavRun(tick);
    ConsumeCorridorPassedWaypoints(tick);
}

void NavigationStateMachine::ObserveFix(NavigateTick& tick)
{
    tick.now = std::chrono::steady_clock::now();
    tick.startup_grace_elapsed =
        runtime_state_.flow.navigate_started_at.time_since_epoch().count() > 0
        && std::chrono::duration_cast<std::chrono::milliseconds>(tick.now - runtime_state_.flow.navigate_started_at).count() >= 3000;
    tick.current_heading = NaviMath::NormalizeAngle(position_->angle);
    // Slow-loop handling below applies only to backends that drop turn sends.
    tick.drops_turn_sends = motion_controller_->SteeringDropsTurnSends();
    // The arrow trails the camera while standing or just after forward is pressed, so steer on the camera then.
    tick.arrow_lags_camera = position_->camera_angle.has_value()
                             && std::abs(NaviMath::NormalizeAngle(*position_->camera_angle - position_->angle)) > kSteerArrowLagCameraDeg;
    tick.steering_on_camera =
        tick.drops_turn_sends
        && (tick.arrow_lags_camera || (position_->camera_angle.has_value() && !motion_controller_->IsMovingForward()));
    tick.steer_heading = tick.steering_on_camera ? NaviMath::NormalizeAngle(*position_->camera_angle) : tick.current_heading;
    tick.degraded_fix = position_provider_->LastCaptureWasBlackScreen() || !position_->valid;
    // Time from the screencap behind this tick's fix to the decision below.
    tick.fix_age_ms = position_->timestamp.time_since_epoch().count() > 0
                          ? std::chrono::duration_cast<std::chrono::milliseconds>(tick.now - position_->timestamp).count()
                          : 0;
}

void NavigationStateMachine::TickNavRun(NavigateTick& tick)
{
    if (const std::optional<DynamicAnchor> nav_run_anchor = ResolveNavRunAnchor(session_, *position_, tick.route)) {
        tick.nav_run_anchor_index = nav_run_anchor->first;
        tick.nav_run_result = nav_run_controller_.tick(
            session_,
            &runtime_state_,
            *position_,
            tick.route,
            param_,
            nav_run_anchor->first,
            nav_run_anchor->second,
            tick.now,
            [this] { motion_controller_->SetForwardState(false); });
    }
    if (tick.nav_run_result.planning > std::chrono::steady_clock::duration::zero()) {
        runtime_state_.offroute.PauseAt(tick.now);
        session_->ExcludeFromStallClocks(tick.nav_run_result.planning);
        tick.now += tick.nav_run_result.planning;
    }
}

// Corridor steering can carry the agent past waypoints the serial tracker never reaches; consume them here.
void NavigationStateMachine::ConsumeCorridorPassedWaypoints(NavigateTick& tick)
{
    size_t consumed = 0;
    while (consumed < tick.nav_run_result.passed_run_waypoints && session_->HasCurrentWaypoint()
           && session_->CurrentWaypoint().IsContinuousRun()) {
        session_->AdvanceToNextWaypoint(ActionType::RUN, "navmesh_corridor_passed_run_waypoint");
        ++consumed;
    }
    if (consumed == 0) {
        return;
    }
    // Same anchor, so the corridor stays valid and only the serial projection is refreshed.
    runtime_state_.recovery.Reset();
    // Passed waypoints count as progress for the re-acquire cap; a stationary recover/re-lose storm passes none.
    runtime_state_.global_reacquire_streak = 0;
    tick.route = RouteTracker::Update(session_, &runtime_state_.route, *position_);
}

NavigationStateMachine::TickExit NavigationStateMachine::CheckRouteProgress(NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    const NavRunTickResult& nav_run_result = tick.nav_run_result;
    const auto now = tick.now;
    tick.effective_progress = ObserveNavigationProgress(route, nav_run_result.straight_to_anchor, tick.nav_run_anchor_index, now);
    // Keyed on the hard-progress clock, which the escape's own overlay re-applies cannot reset.
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
    // Only an OffCorridor replan builds a different corridor; a ProgressRegression replan keeps the stall clock running.
    if (nav_run_result.replanned_with == NavRunReplanReason::OffCorridor) {
        session_->ResetProgress();
    }
    if (runtime_state_.recovery.active) {
        const bool recovery_zone_changed = ZonesDiffer(runtime_state_.recovery.anchor_pos.zone_id, position_->zone_id);
        const double recovery_displacement =
            std::hypot(position_->x - runtime_state_.recovery.anchor_pos.x, position_->y - runtime_state_.recovery.anchor_pos.y);
        if (recovery_zone_changed || recovery_displacement >= kDynamicRecoveryResetDistance) {
            LogInfo << "Dynamic recovery escaped obstacle." << VAR(recovery_zone_changed) << VAR(recovery_displacement);
            return ResumeAfterEscape("recovery_escape");
        }
    }
    tick.stalled_ms = session_->StalledMs(now);
    // Warmed from the first stalled tick so the recovery ladder reads an already latched answer.
    device_recovery_.UpdateFeeding(tick.stalled_ms, runtime_state_.recovery_escalation.device_attempt_count < kRecoveryDeviceAttempts);

    if (!route.valid) {
        if (tick.degraded_fix) {
            motion_controller_->SetForwardState(false);
        }
        utils::SleepFor(kTargetTickMs);
        return true;
    }
    return std::nullopt;
}

NavigationStateMachine::TickExit NavigationStateMachine::HandlePrompts(NavigateTick& tick)
{
    tick.waypoint = session_->CurrentWaypoint();
    const Waypoint& waypoint = tick.waypoint;
    const RouteTrackingState& route = tick.route;
    if (TryRunPromptSubtaskWhileWalking(route)) {
        return true;
    }

    if (TryZiplineMountPrompt(waypoint, route)) {
        runtime_state_.semantic.zipline_prompt_probe = true;
        const semantic_nodes::Result prompt_result = semantic_nodes::HandleArrival(SemanticContext(), waypoint, route.waypoint_distance);
        runtime_state_.semantic.zipline_prompt_probe = false;
        if (prompt_result.request_failure) {
            return FailNavigation(
                prompt_result.failure_reason,
                prompt_result.failure_log_message,
                route.waypoint_distance,
                0.0,
                tick.stalled_ms);
        }
        if (prompt_result.consumed) {
            return true;
        }
    }

    // 顶在设备上走不动时也换站位, 只限站位跟前这一小片
    if (waypoint.action == ActionType::ZIPLINE && !runtime_state_.IsZiplineMounted() && waypoint.zipline_hop) {
        const size_t cursor = runtime_state_.zipline_approach.MountSpotCursor(waypoint.zipline_hop->mount);
        if (cursor + 1 < waypoint.zipline_hop->mount_spots.size() && route.waypoint_distance <= kZiplineWalkEnterBandWu
            && session_->HardStalledMs(tick.now) >= kZiplineMountSpotStallMs) {
            const semantic_nodes::Result advanced =
                semantic_nodes::AdvanceMountSpot(SemanticContext(), waypoint, "zipline_mount_spot_stalled");
            if (advanced.consumed) {
                return true;
            }
        }
    }
    return std::nullopt;
}

double NavigationStateMachine::ResolveArrivalDistance(const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteTrackingState& route = tick.route;
    const auto now = tick.now;
    double arrival_distance = std::max(route.arrival_band, waypoint.Traits().commit_distance);
    // 收窄的判定圈长时间站不上去就放回常规值
    if (waypoint.StopsOnPromptDetection() && session_->HardStalledMs(now) > kCollectArrivalRelaxMs) {
        const double relaxed = waypoint.ArrivalBand(kMeasurementDefaultPositionQuantum, /*relax_tight_band=*/true);
        if (relaxed > arrival_distance && route.waypoint_distance <= relaxed) {
            LogInfo << "Prompt-point arrival band relaxed after no progress." << VAR(session_->current_node_idx())
                    << VAR(route.waypoint_distance) << VAR(arrival_distance) << VAR(relaxed);
        }
        arrival_distance = std::max(arrival_distance, relaxed);
    }
    // 上索按采集同一套收紧判定圈, 只收链首那一次
    if (waypoint.action == ActionType::ZIPLINE && !runtime_state_.IsZiplineMounted()
        && session_->HardStalledMs(now) <= kCollectArrivalRelaxMs) {
        arrival_distance = std::min(arrival_distance, kCollectArrivalBandWu);
        // 按空一次就收紧接着走, 收不拢由上索点自己的重规划预算收口
        if (runtime_state_.zipline_approach.press_missed) {
            arrival_distance = std::min(arrival_distance, kZiplineRestandBandWu);
        }
    }
    // 交互点认空过一次就收紧到站上点再认, 收不拢同样放回常规值
    if (waypoint.IsAsyncInteract() && runtime_state_.interact_approach.PromptMissedAt(session_->CurrentAbsoluteNodeIndex())
        && now - runtime_state_.interact_approach.missed_at <= std::chrono::milliseconds(kCollectArrivalRelaxMs)) {
        arrival_distance = std::min(arrival_distance, kStrictSettleAcceptBandWu);
    }
    return arrival_distance;
}

NavigationStateMachine::TickExit NavigationStateMachine::HandleWaypointArrival(NavigateTick& tick)
{
    tick.arrival_distance = ResolveArrivalDistance(tick);
    const RouteTrackingState& route = tick.route;
    const double arrival_distance = tick.arrival_distance;
    const bool reached = route.waypoint_distance <= arrival_distance && !ShortOfDrop(tick.waypoint, *position_);
    if (!reached) {
        return std::nullopt;
    }
    if (!route.startup_motion_confirmed) {
        LogDebug << "Arrival advance blocked before startup movement confirmed." << VAR(session_->current_node_idx())
                 << VAR(route.waypoint_distance) << VAR(arrival_distance) << VAR(route.progress_distance) << VAR(route.cross_track)
                 << VAR(route.projection_anchor);
        return std::nullopt;
    }
    if (const TickExit exit = SettleAtArrival(tick)) {
        return exit;
    }
    // 恢复步行模式可能同步等待控制器，期间收到取消也不得再派发到点动作。
    if (should_stop_()) {
        return true;
    }
    return PassArrivedWaypoints(tick);
}

NavigationStateMachine::TickExit NavigationStateMachine::SettleAtArrival(const NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    const Waypoint& waypoint = tick.waypoint;
    if (!waypoint.SettlesAtArrival()) {
        return std::nullopt;
    }
    if (route.waypoint_distance <= route.arrival_band) {
        const semantic_nodes::Context semantic_ctx = SemanticContext();
        // 这一拍可能已跨过步行带, 所以先停车
        semantic_nodes::StopMotionAndCommitment(semantic_ctx);
        walk_mode_.Request(true);
        const bool settled = semantic_nodes::SettleAtStrictGoal(semantic_ctx, waypoint, should_stop_);
        // 静止转镜头没有走操舵路径，清除在途转角记账。
        runtime_state_.steering_rate.Reset();
        if (should_stop_()) {
            walk_mode_.Request(false);
            return true;
        }
        if (!settled) {
            // 定位失效时人可能没到点, 交回定位恢复; 定位有效只是没停稳, 到点动作照旧非致命
            if (!position_->valid) {
                walk_mode_.Request(false);
                return HandleLocalizationLoss();
            }
            LogWarn << "Strict arrival correction incomplete; arrival action remains non-fatal." << VAR(session_->current_node_idx())
                    << VAR(position_->valid);
        }
    }
    // 到点即退出步行, 后续动作在慢跑态下执行
    walk_mode_.Request(false);
    return std::nullopt;
}

// A plain corridor waypoint keeps steering on this same fix; one that needs more gets a tick of its own.
NavigationStateMachine::TickExit NavigationStateMachine::PassArrivedWaypoints(NavigateTick& tick)
{
    RouteTrackingState& route = tick.route;
    Waypoint& waypoint = tick.waypoint;
    double& arrival_distance = tick.arrival_distance;
    do {
        const semantic_nodes::Result arrival_result = semantic_nodes::HandleArrival(SemanticContext(), waypoint, route.waypoint_distance);
        if (arrival_result.rope_unavailable != 0) {
            return AbandonRope(arrival_result.rope_unavailable);
        }
        if (arrival_result.request_failure) {
            return FailNavigation(
                arrival_result.failure_reason,
                arrival_result.failure_log_message,
                route.waypoint_distance,
                0.0,
                tick.stalled_ms);
        }
        if (!tick.drops_turn_sends || !waypoint.IsContinuousRun() || session_->phase() != NaviPhase::Navigate
            || !session_->HasCurrentWaypoint()) {
            return true;
        }
        route = RouteTracker::Update(session_, &runtime_state_.route, *position_);
        waypoint = session_->CurrentWaypoint();
        if (!route.valid || !waypoint.IsContinuousRun() || waypoint.drop_from) {
            return true;
        }
        arrival_distance = std::max(route.arrival_band, waypoint.Traits().commit_distance);
    } while (route.waypoint_distance <= arrival_distance && route.startup_motion_confirmed);
    LogDebug << "Passed corridor waypoints; steering on this fix." << VAR(session_->current_node_idx()) << VAR(route.waypoint_distance)
             << VAR(arrival_distance);
    return std::nullopt;
}

NavigationStateMachine::TickExit NavigationStateMachine::RunRiverFallRecovery(NavigateTick& tick)
{
    if (!runtime_state_.river_fall.pending) {
        return std::nullopt;
    }
    const RouteTrackingState& route = tick.route;
    const double current_heading = tick.current_heading;
    RiverFallRecoveryState& rf = runtime_state_.river_fall;
    runtime_state_.offroute.PauseAt(tick.now);
    if (session_->HardStalledMs(tick.now) > kRiverFallRecoveryTimeoutMs) {
        return FailNavigation(
            "river_fall_recovery_timeout",
            "River-fall recovery made no net progress past the timeout; terminating navigation.",
            route.progress_distance,
            NaviMath::NormalizeAngle(route.route_heading - current_heading),
            tick.stalled_ms);
    }
    const double rf_displacement = std::hypot(position_->x - rf.anchor_pos.x, position_->y - rf.anchor_pos.y);
    if (rf_displacement >= kRiverFallRecoveryClearDistance) {
        rf.Reset();
        LogInfo << "River-fall recovery cleared; resuming navigation." << VAR(rf_displacement) << VAR(current_heading);
        return ResumeAfterEscape("river_fall_recovered");
    }
    motion_controller_->SetForwardState(false);
    // 上岸瞬间的箭头读数不可信, 先站定再算转身
    if (!rf.settled) {
        rf.settled = true;
        utils::SleepFor(kRiverFallRecoverySettleMs);
        LogInfo << "River-fall recovery settling before the about-face." << VAR(rf_displacement) << VAR(position_->x) << VAR(position_->y);
        return true;
    }
    if (!rf.turned) {
        rf.turned = true;
        rf.water_heading = current_heading;
        const int turn_units = static_cast<int>(std::lround(180.0 * action_wrapper_->DefaultTurnUnitsPerDegree()));
        action_wrapper_->SendViewDeltaSync(turn_units, 0);
        utils::SleepFor(kWaitAfterFirstTurnMs);
        LogInfo << "River-fall recovery about-face issued." << VAR(rf.water_heading) << VAR(turn_units) << VAR(rf_displacement);
    }
    // 角色朝向要迈一步才跟上镜头, 这一步沿相机方向离水
    action_wrapper_->PulseForwardSync(kRiverFallRecoveryPulseMs);
    motion_controller_->SetForwardState(false);
    LogInfo << "River-fall recovery inland pulse." << VAR(current_heading) << VAR(rf_displacement) << VAR(position_->x)
            << VAR(position_->y);
    return true;
}

// A non-finite cross_track means the projection failed, so it never arms this watchdog.
NavigationStateMachine::TickExit NavigationStateMachine::CheckOffRouteWedge(NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    const auto now = tick.now;
    const bool watched = session_->phase() == NaviPhase::Navigate && tick.waypoint.IsContinuousRun() && !route.on_route
                         && std::isfinite(route.cross_track) && !runtime_state_.cross_tier_escape.active;
    if (!watched) {
        runtime_state_.offroute.Reset();
        return std::nullopt;
    }
    OffRouteWedgeState& wedge = runtime_state_.offroute;
    if (const int64_t blind_ms = wedge.ResumeAt(now); blind_ms > 0) {
        LogInfo << "Off-route wedge clock resumed after a blind stretch." << VAR(blind_ms) << VAR(route.progress_distance)
                << VAR(wedge.best_distance);
    }
    const double progress_epsilon = std::max(kNoProgressDistanceEpsilon, kMeasurementDefaultPositionQuantum);
    const bool retaking_baseline = wedge.active && !wedge.HasBaseline();
    if (!wedge.active || retaking_baseline || route.progress_distance + progress_epsilon < wedge.best_distance) {
        wedge.active = true;
        wedge.best_distance = route.progress_distance;
        if (!retaking_baseline) {
            wedge.since = now;
        }
    }
    const int64_t wedge_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - wedge.since).count();
    if (wedge_ms >= kOffRouteWedgeFailMs) {
        return FailNavigation(
            "offroute_wedge_timeout",
            "Off-route with no route progress past the wedge timeout; terminating so the pipeline can retry.",
            route.progress_distance,
            NaviMath::NormalizeAngle(route.route_heading - tick.current_heading),
            tick.stalled_ms);
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
    return std::nullopt;
}

NavigationStateMachine::TickExit NavigationStateMachine::RunObstacleRecovery(NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteTrackingState& route = tick.route;
    const int64_t stalled_ms = tick.stalled_ms;
    const auto now = tick.now;
    // A forward re-sent without the fix leaving its quantum opens recovery before the stall clock does.
    const bool forward_hold_futile = runtime_state_.flow.futile_forward_reasserts >= kForwardHoldFutileReassertsBeforeRecovery;
    // Near the anchor a pinned forward waits for the stall clock, since the interactable ahead is usually the destination.
    const bool pinned_without_own_fallback = forward_hold_futile && stalled_ms >= kObstacleRecoveryMinTriggerMs
                                             && !waypoint.StopsOnPromptDetection() && waypoint.action != ActionType::ZIPLINE;
    const bool far_or_strict = tick.effective_progress > kNoProgressMinDistance || waypoint.RequiresStrictArrival();
    const bool should_try_recovery = session_->phase() == NaviPhase::Navigate
                                     && (stalled_ms >= kObstacleRecoveryMinTriggerMs || forward_hold_futile)
                                     && (far_or_strict || pinned_without_own_fallback) && !runtime_state_.cross_tier_escape.active;
    if (!should_try_recovery) {
        return std::nullopt;
    }
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    if (!anchor) {
        return std::nullopt;
    }
    DynamicRecoveryState& recovery = runtime_state_.recovery;
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    if (!recovery.active || recovery.anchor_index != anchor->first) {
        RestartRecovery(recovery, *position_, now, anchor->first);
    }
    if (escalation.anchor_index != anchor->first) {
        // A hit latched under the previous anchor predates the renumbered path, so the new anchor needs a fresh look.
        const bool fought_another_anchor = escalation.anchor_index != std::numeric_limits<size_t>::max();
        escalation.Reset();
        escalation.anchor_index = anchor->first;
        if (fought_another_anchor) {
            device_recovery_.ForgetObservation();
        }
    }

    // Timed on the hard-progress clock because every small escape re-stamps recovery.started_at.
    const int64_t recovery_elapsed_ms = session_->HardStalledMs(now);
    if (recovery_elapsed_ms > kDynamicRecoveryTotalTimeoutMs) {
        // 索边卡死先退索改走路, 退链后再超时才判导航失败
        if (waypoint.action == ActionType::ZIPLINE) {
            LogWarn << "Recovery timed out at a zipline tower; dropping the chain and walking." << VAR(recovery_elapsed_ms)
                    << VAR(waypoint.x) << VAR(waypoint.y) << VAR(position_->x) << VAR(position_->y);
            semantic_nodes::AbandonZipline(SemanticContext(), "zipline_recovery_timeout", "stuck at the tower after recovery ran out");
            return true;
        }
        return FailNavigation(
            "dynamic_recovery_timeout",
            "Dynamic recovery timeout reached and navigation was terminated.",
            route.progress_distance,
            NaviMath::NormalizeAngle(route.route_heading - tick.current_heading),
            stalled_ms);
    }

    const bool retry_cooling_down =
        recovery.last_replan_at.time_since_epoch().count() > 0
        && std::chrono::duration_cast<std::chrono::milliseconds>(now - recovery.last_replan_at).count() < kDynamicRecoveryRetryIntervalMs;
    if (retry_cooling_down) {
        return std::nullopt;
    }
    recovery.last_replan_at = now;
    return ClimbRecoveryLadder(tick, far_or_strict);
}

bool NavigationStateMachine::ClimbRecoveryLadder(const NavigateTick& tick, bool far_or_strict)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteTrackingState& route = tick.route;
    const int64_t stalled_ms = tick.stalled_ms;
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    // Device removal precedes the jump in the same tick; every outcome but NotAttempted spends the attempt.
    const DeviceRemovalOutcome device_outcome =
        device_recovery_.TryRemove(route, waypoint, escalation.device_attempt_count < kRecoveryDeviceAttempts && far_or_strict);
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
    LogInfo << "Dynamic recovery jump pulse issued." << VAR(escalation.jump_attempt_count) << VAR(escalation.detour_attempt_count);
    motion_controller_->SetAction(LocalDriverAction::JumpForward, true);
    utils::SleepFor(kActionJumpSettleMs);
    motion_controller_->SetForwardState(false);
    if (!CaptureCurrentPosition(false) || position_provider_->LastCaptureWasBlackScreen() || !position_->valid) {
        LogWarn << "Dynamic recovery waiting for post-jump local tracking fix." << VAR(stalled_ms) << VAR(escalation.jump_attempt_count);
        utils::SleepFor(kTargetTickMs);
        return true;
    }

    const bool jump_zone_changed = ZonesDiffer(jump_start.zone_id, position_->zone_id);
    const double jump_displacement = std::hypot(position_->x - jump_start.x, position_->y - jump_start.y);
    const double jump_waypoint_distance = std::hypot(waypoint.x - position_->x, waypoint.y - position_->y);
    const bool jump_made_progress = jump_waypoint_distance + kNoProgressDistanceEpsilon < route.waypoint_distance;
    const bool jump_moved_forward = jump_displacement >= kDynamicRecoveryResetDistance * 0.5 && jump_made_progress;
    if (jump_zone_changed || jump_displacement >= kDynamicRecoveryResetDistance || jump_moved_forward) {
        LogInfo << "Dynamic recovery jump escaped obstacle." << VAR(jump_zone_changed) << VAR(jump_displacement) << VAR(jump_moved_forward);
        return ResumeAfterEscape("recovery_jump_escape");
    }
    return EscalatePastJump(tick);
}

bool NavigationStateMachine::EscalatePastJump(const NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    const int64_t stalled_ms = tick.stalled_ms;
    DynamicRecoveryState& recovery = runtime_state_.recovery;
    RecoveryEscalationState& escalation = runtime_state_.recovery_escalation;
    const std::optional<DynamicAnchor> post_jump_anchor = ResolveCurrentAnchor(session_, *position_);
    if (!post_jump_anchor) {
        LogWarn << "Dynamic recovery skipped: no future anchor after post-jump local tracking." << VAR(position_->x) << VAR(position_->y)
                << VAR(position_->zone_id);
        utils::SleepFor(kTargetTickMs);
        return true;
    }
    if (post_jump_anchor->first != recovery.anchor_index) {
        RestartRecovery(recovery, *position_, tick.now, post_jump_anchor->first);
        recovery.last_replan_at = tick.now;
    }
    if (post_jump_anchor->first != escalation.anchor_index) {
        escalation.Reset();
        escalation.anchor_index = post_jump_anchor->first;
        escalation.jump_attempt_count = 1;
    }

    if (escalation.jump_attempt_count < kRecoveryJumpAttemptsBeforeDetour) {
        utils::SleepFor(kTargetTickMs);
        return true;
    }
    const double stuck_heading = EffectiveRouteHeading(tick.nav_run_result, route);
    if (!InsidePushThroughDisc(runtime_state_.virtual_no_go, *position_)
        && escalation.detour_attempt_count < kRecoveryDetourAttemptsBeforeUnstick) {
        ++escalation.detour_attempt_count;
        if (TryVirtualNoGoReplan("recovery_navmesh_detour", post_jump_anchor->first, post_jump_anchor->second, *position_, stuck_heading)) {
            SelectPhaseForCurrentWaypoint("recovery_navmesh_detour");
            return true;
        }
        LogWarn << "Dynamic recovery detour attempt failed; switching to physical unstick." << VAR(escalation.detour_attempt_count)
                << VAR(escalation.jump_attempt_count) << VAR(post_jump_anchor->first) << VAR(route.progress_distance) << VAR(stalled_ms);
    }
    if (ExecutePhysicalUnstick(stuck_heading)) {
        return true;
    }
    utils::SleepFor(kTargetTickMs);
    return true;
}

void NavigationStateMachine::SteerAlongCorridor(NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    const NavRunTickResult& nav_run_result = tick.nav_run_result;
    const Waypoint& waypoint = tick.waypoint;
    const auto now = tick.now;
    const double current_heading = tick.current_heading;
    const double steer_heading = tick.steer_heading;
    const bool drops_turn_sends = tick.drops_turn_sends;
    const bool steering_on_camera = tick.steering_on_camera;
    const bool arrow_lags_camera = tick.arrow_lags_camera;
    const bool degraded_fix = tick.degraded_fix;
    const int64_t capture_ms = tick.capture_ms;
    const int64_t fix_age_ms = tick.fix_age_ms;
    const int64_t tick_gap_ms = tick.tick_gap_ms;
    const double arrival_distance = tick.arrival_distance;
    const int64_t stalled_ms = tick.stalled_ms;
    const double effective_route_heading = EffectiveRouteHeading(nav_run_result, route);
    const auto [turn_achieved_deg, turn_residual_deg, turn_elapsed_ms] = ObserveLastTurn(runtime_state_.steering_rate, steer_heading, now);
    const uint64_t tick_seq = runtime_state_.flow.tick_seq;
    const auto [heading_rate_deg, heading_rate_raw_delta_deg, heading_rate_gap_ms, heading_rate_gap_ticks] =
        TrackHeadingRate(runtime_state_.steering_rate, steer_heading, now, tick_seq);
    const std::optional<int64_t> steer_period_ms = SampleSteerPeriod(runtime_state_.flow, now);
    const bool slow_loop = drops_turn_sends && steer_period_ms.has_value() && *steer_period_ms >= kSlowLoopPivotPeriodMs;
    SettlePendingTurn(steer_heading, now, drops_turn_sends, steer_period_ms);

    const double heading_error = NaviMath::NormalizeAngle(effective_route_heading - steer_heading);
    const SteeringCommand steering = SteeringController::Update(
        heading_error,
        heading_rate_deg,
        motion_controller_->IsMovingForward(),
        runtime_state_.steering_rate.turn_latch_sign,
        runtime_state_.steering_rate.pending_turn_deg);

    // On a slow loop a big turn lands a whole tick late, so turn it in place to avoid overrunning the corner.
    const double turn_abs_deg = std::abs(steering.yaw_delta_deg);
    const bool pivot_in_place =
        drops_turn_sends && steering.issued
        && (steer_period_ms.has_value() ? slow_loop && turn_abs_deg >= kSlowLoopPivotTurnDeg : turn_abs_deg >= kStartupPivotTurnDeg);
    const bool corner_brake = BrakeForSharpCorner(nav_run_result, slow_loop);
    motion_controller_->SetForwardState(!pivot_in_place && !corner_brake);

    const auto [issued_delta_deg, steer_send_ms] = SendSteering(steering, tick_gap_ms, steer_heading, now, drops_turn_sends);
    // 提前返回的拍留下拍号缺口, 估计器据此作废跨缺口的样本
    sensitivity::RecordTick(maa_context_, tick_seq, position_->camera_angle, issued_delta_deg, degraded_fix);
    UpdateForwardHold(issued_delta_deg, heading_error);

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
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - tick.tick_started_at).count();
    // -1 when there is no bend in reach or no speed estimate yet.
    const std::optional<SharpCorner>& corner = nav_run_result.corner;
    const double corner_distance = corner ? corner->distance : -1.0;
    const double corner_turn_deg = corner ? corner->turn_deg : -1.0;
    const double corner_lead_distance = corner ? corner->lead_distance : -1.0;
    const double step_per_tick = nav_run_result.step_per_tick.value_or(-1.0);
    LogDebug << "TickNavigate steering decision." << VAR(tick_seq) << VAR(current_heading) << VAR(steer_heading) << VAR(steering_on_camera)
             << VAR(arrow_lags_camera) << VAR(route.route_heading) << VAR(effective_route_heading)
             << VAR(nav_run_result.has_corridor_heading) << VAR(nav_run_result.cross_track) << VAR(nav_run_result.upcoming_turn_deg)
             << VAR(corner_distance) << VAR(corner_turn_deg) << VAR(corner_lead_distance) << VAR(step_per_tick) << VAR(corner_brake)
             << VAR(heading_rate_deg) << VAR(heading_rate_raw_delta_deg) << VAR(heading_rate_gap_ms) << VAR(heading_rate_gap_ticks)
             << VAR(heading_error) << VAR(steering.yaw_delta_deg) << VAR(issued_delta_deg) << VAR(turn_achieved_deg)
             << VAR(turn_residual_deg) << VAR(turn_elapsed_ms) << VAR(route.waypoint_distance) << VAR(route.on_route) << VAR(degraded_fix)
             << VAR(capture_ms) << VAR(fix_age_ms) << VAR(tick_gap_ms) << VAR(tick_compute_ms);

    latency::RecordStage(latency::Stage::Other, std::max<int64_t>(0, tick_compute_ms - capture_ms - steer_send_ms));
    latency::RecordTick(maa_context_, runtime_state_.flow.tick_seq, tick_gap_ms);
    tick.heading_error = heading_error;
}

// Discounts the turn still in flight: only motion toward it pays the debt down, and an unpaid debt expires.
void NavigationStateMachine::SettlePendingTurn(
    double steer_heading,
    std::chrono::steady_clock::time_point now,
    bool drops_turn_sends,
    std::optional<int64_t> steer_period_ms)
{
    SteeringRateState& steering_rate = runtime_state_.steering_rate;
    const double batch_cap_deg = motion_controller_->SteeringBatchCapDeg();
    const bool walk_engaged = walk_mode_.engaged();
    const auto pending_lifetime_ms = [&](double delta_deg) {
        // Sweeps beyond the first batch get extra lifetime so a debt is never written off mid-sweep.
        const double extra_sweep_deg = std::max(0.0, std::abs(delta_deg) - batch_cap_deg);
        const int64_t floor_ms = drops_turn_sends
                                     ? std::max(kSteeringPendingLifetimeMs, kSteeringPendingLifetimeTicks * steer_period_ms.value_or(0))
                                     : kSteeringPendingLifetimeMs;
        const int64_t base_ms = floor_ms + static_cast<int64_t>(extra_sweep_deg / kYawRateDegPerSec * 1000.0);
        return walk_engaged ? base_ms * kWalkModeSlowFactor : base_ms;
    };
    if (drops_turn_sends) {
        std::erase_if(steering_rate.in_flight, [&](const SteeringRateState::InFlightTurn& turn) {
            return std::chrono::duration_cast<std::chrono::milliseconds>(now - turn.sent_at).count() >= pending_lifetime_ms(turn.delta_deg);
        });
        if (steering_rate.pending_turn_deg != 0.0) {
            if (steering_rate.in_flight.empty()) {
                steering_rate.pending_turn_deg = 0.0;
            }
            else {
                const double landed = NaviMath::NormalizeAngle(steer_heading - steering_rate.pending_ref_heading_deg);
                const double owed = std::abs(steering_rate.pending_turn_deg);
                double negative_sent_deg = 0.0;
                double positive_sent_deg = 0.0;
                for (const SteeringRateState::InFlightTurn& turn : steering_rate.in_flight) {
                    (turn.delta_deg < 0.0 ? negative_sent_deg : positive_sent_deg) += turn.delta_deg;
                }
                steering_rate.pending_turn_deg =
                    std::clamp(std::clamp(steering_rate.pending_turn_deg - landed, -owed, owed), negative_sent_deg, positive_sent_deg);
            }
        }
    }
    else if (steering_rate.pending_turn_deg != 0.0) {
        const int64_t pending_age_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - steering_rate.cmd_at).count();
        if (pending_age_ms >= pending_lifetime_ms(steering_rate.cmd_delta_deg)) {
            steering_rate.pending_turn_deg = 0.0;
        }
        else {
            const double landed = NaviMath::NormalizeAngle(steer_heading - steering_rate.pending_ref_heading_deg);
            const double owed = std::abs(steering_rate.pending_turn_deg);
            steering_rate.pending_turn_deg = std::clamp(steering_rate.pending_turn_deg - landed, -owed, owed);
        }
    }
    steering_rate.pending_ref_heading_deg = steer_heading;
}

// On a slow loop, stop short of a narrow bend once, then walk up to the vertex and turn there in place.
bool NavigationStateMachine::BrakeForSharpCorner(const NavRunTickResult& nav_run_result, bool slow_loop)
{
    FlowState& flow = runtime_state_.flow;
    bool corner_brake = false;
    const std::optional<SharpCorner>& corner = nav_run_result.corner;
    const auto ahead_is_braked_corner = [&] {
        return corner && flow.braked_corner
               && std::hypot(corner->point.x - flow.braked_corner->x, corner->point.y - flow.braked_corner->y) <= kCornerBrakeLatchRadiusM;
    };
    if (slow_loop && corner && nav_run_result.step_per_tick) {
        const double stop_distance = *nav_run_result.step_per_tick * kCornerBrakeLeadTicks;
        corner_brake = !ahead_is_braked_corner() && corner->distance <= stop_distance && corner->lead_distance < stop_distance;
        if (corner_brake) {
            flow.braked_corner = corner->point;
            LogInfo << "Braking before a sharp corner." << VAR(corner->distance) << VAR(corner->turn_deg) << VAR(corner->lead_distance)
                    << VAR(stop_distance) << VAR(corner->point.x) << VAR(corner->point.y);
        }
    }
    // Walk until the bend is behind so the pivot at the vertex starts slow.
    if (const bool corner_walk = ahead_is_braked_corner(); corner_walk != flow.corner_walk) {
        flow.corner_walk = corner_walk;
        LogInfo << (corner_walk ? "Walking into a braked corner." : "Braked corner behind; walking ends.") << VAR(position_->x)
                << VAR(position_->y);
    }
    return corner_brake;
}

std::pair<double, int64_t> NavigationStateMachine::SendSteering(
    const SteeringCommand& steering,
    int64_t tick_gap_ms,
    double steer_heading,
    std::chrono::steady_clock::time_point now,
    bool drops_turn_sends)
{
    SteeringRateState& steering_rate = runtime_state_.steering_rate;
    double issued_delta_deg = 0.0;
    int64_t steer_send_ms = 0;
    // Lifetimes start at the send: a tick that replans before getting here can outlast a whole lifetime.
    const auto steer_sent_at = std::chrono::steady_clock::now();
    if (steering.issued) {
        const TurnCommandResult steering_result = motion_controller_->ApplySteering(steering.yaw_delta_deg, tick_gap_ms);
        steer_send_ms = steering_result.send_ms;
        if (steering_result.issued) {
            issued_delta_deg = steering_result.issued_delta_degrees;
        }
    }
    if (issued_delta_deg != 0.0) {
        steering_rate.cmd_heading_deg = steer_heading;
        steering_rate.cmd_delta_deg = issued_delta_deg;
        steering_rate.cmd_at = now;
        steering_rate.has_cmd = true;
        steering_rate.pending_turn_deg += issued_delta_deg;
        if (drops_turn_sends) {
            steering_rate.in_flight.push_back({ .delta_deg = issued_delta_deg, .sent_at = steer_sent_at });
        }
    }
    return { issued_delta_deg, steer_send_ms };
}

// Re-presses forward after a swallowed keydown; stands down while recovery owns the movement keys.
void NavigationStateMachine::UpdateForwardHold(double issued_delta_deg, double heading_error)
{
    FlowState& flow = runtime_state_.flow;
    const bool moved = flow.has_last_steer_position
                       && std::hypot(position_->x - flow.last_steer_position.x, position_->y - flow.last_steer_position.y)
                              > kMeasurementDefaultPositionQuantum;
    // The re-send counter survives recovery going active; only motion or a turn clears it.
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
    // At walking speed a tick's step sits under the position quantum, so the window widens.
    const int32_t hold_reassert_ticks = walk_mode_.engaged() ? kForwardHoldReassertTicks * kWalkModeSlowFactor : kForwardHoldReassertTicks;
    if (flow.motionless_hold_ticks >= hold_reassert_ticks) {
        ++flow.futile_forward_reasserts;
        LogWarn << "Forward hold produced no motion; re-sending it." << VAR(flow.motionless_hold_ticks) << VAR(heading_error)
                << VAR(flow.futile_forward_reasserts) << VAR(position_->x) << VAR(position_->y);
        motion_controller_->ReassertForward();
        flow.motionless_hold_ticks = 0;
    }
}

bool NavigationStateMachine::FinishNavigateTick(const NavigateTick& tick)
{
    // Must precede the sprint gate below.
    UpdatePromptSprintSuppression();
    UpdateAutoSprint(tick);

    if (param_.arrival_timeout > 0 && tick.stalled_ms > param_.arrival_timeout) {
        return FailNavigation(
            "no_progress_timeout",
            "No progress timeout reached and navigation was terminated.",
            tick.route.progress_distance,
            NaviMath::NormalizeAngle(tick.route.route_heading - tick.current_heading),
            tick.stalled_ms);
    }

    utils::SleepFor(kTargetTickMs);
    return true;
}

// No clearance term: it reads near zero on bridges and would lock sprint out.
void NavigationStateMachine::UpdateAutoSprint(const NavigateTick& tick)
{
    const RouteTrackingState& route = tick.route;
    const NavRunTickResult& nav_run_result = tick.nav_run_result;
    const Waypoint& waypoint = tick.waypoint;
    const double heading_error = tick.heading_error;
    const double arrival_distance = tick.arrival_distance;
    const bool startup_grace_elapsed = tick.startup_grace_elapsed;
    const auto now = tick.now;
    const bool heading_aligned_for_sprint = std::abs(heading_error) < kAutoSprintMaxHeadingErrorDeg;
    const bool corridor_turn_calm =
        !nav_run_result.has_corridor_heading || nav_run_result.upcoming_turn_deg < kAutoSprintMaxUpcomingTurnDeg;
    const bool turn_calm = heading_aligned_for_sprint && corridor_turn_calm;
    const bool target_requires_strict_arrival = waypoint.RequiresStrictArrival();
    const double sprint_remaining = nav_run_result.has_corridor_heading && std::isfinite(nav_run_result.remaining_to_anchor)
                                        ? nav_run_result.remaining_to_anchor
                                        : route.along_track_remaining;
    // Sprint toward a strict goal until its braking buffer.
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
    // On a RUN anchor the distance left to the anchor is the progress signal.
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
    // Recovery escapes can never reset the hard watchdog, which is what lets the recovery timeout fire.
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
    // The frame observer captures this, so it must be torn down before destruction.
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
    // One observer feeds every scanner, so stopping one never cuts the others' frames.
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

// 判定圈之外的观测也读掉, 免得进带那一拍拿旧观测开火
bool NavigationStateMachine::TryZiplineMountPrompt(const Waypoint& waypoint, const RouteTrackingState& route)
{
    if (waypoint.action != ActionType::ZIPLINE) {
        zipline_mount_scanner_.reset(); // 链走完了, 别让它继续逐帧匹配
        return false;
    }
    // 已站在架子上时下一跳靠瞄准接上, 用不着预筛
    if (runtime_state_.IsZiplineMounted()) {
        return false;
    }
    if (zipline_mount_scanner_ == nullptr && maa_context_ != nullptr) {
        PromptScanProfile profile;
        if (!TryLoadPromptScanProfile(maa_context_, kZiplineMountScanNode, action_wrapper_->controller_type(), &profile)) {
            LogWarn << "Zipline mount pre-filter not started; the chain stops on arrival instead." << VAR(kZiplineMountScanNode);
            return false;
        }
        LogInfo << "Zipline mount pre-filter started." << VAR(kZiplineMountScanNode) << VAR(profile.base_roi.x) << VAR(profile.base_roi.y)
                << VAR(profile.icons.size());
        zipline_mount_scanner_ = std::make_unique<RoiTemplateScanner>("zipline", profile.base_roi, std::move(profile.icons));
    }
    if (zipline_mount_scanner_ == nullptr) {
        return false;
    }
    const bool flagged = zipline_mount_scanner_->ConsumeDetection();
    return flagged && route.startup_motion_confirmed && route.waypoint_distance <= kPromptTriggerBandWu;
}

NavigationStateMachine::PromptDistance NavigationStateMachine::NearestPromptDistance() const
{
    PromptDistance nearest;
    for (const AsyncPromptAction* prompt : { &collect_prompt_, &interact_prompt_ }) {
        const double distance_sq = prompt->NearestDistanceSq();
        if (distance_sq >= 0.0 && (nearest.distance_sq < 0.0 || distance_sq < nearest.distance_sq)) {
            nearest.distance_sq = distance_sq;
            nearest.is_zipline = false;
        }
    }
    // 只统计未经过的点, 已站上滑索架时上索点也不统计
    const std::vector<Waypoint>& path = session_->current_path();
    for (size_t index = session_->current_node_idx(); index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        const bool is_zipline = waypoint.action == ActionType::ZIPLINE;
        if (!waypoint.HasPosition() || (waypoint.action != ActionType::DIG && !is_zipline)
            || (is_zipline && runtime_state_.IsZiplineMounted())) {
            continue;
        }
        const double dx = waypoint.x - position_->x;
        const double dy = waypoint.y - position_->y;
        const double distance_sq = dx * dx + dy * dy;
        if (waypoint.action == ActionType::DIG && (nearest.dig_distance_sq < 0.0 || distance_sq < nearest.dig_distance_sq)) {
            nearest.dig_distance_sq = distance_sq;
        }
        if (nearest.distance_sq < 0.0 || distance_sq < nearest.distance_sq) {
            nearest.distance_sq = distance_sq;
            nearest.is_zipline = is_zipline;
        }
    }
    return nearest;
}

void NavigationStateMachine::UpdatePromptSprintSuppression()
{
    if (motion_controller_ == nullptr) {
        return;
    }

    const double nearest_sq = NearestPromptDistance().distance_sq;
    // 没有需要减速接近的点时 nearest_sq < 0
    const bool approaching_prompt = nearest_sq >= 0.0 && nearest_sq <= kCollectSprintSuppressBandWu * kCollectSprintSuppressBandWu;
    motion_controller_->SetSprintSuppressed(approaching_prompt);
}

// DIG may start walking before motion is confirmed; its arrival gate still requires movement before digging.
void NavigationStateMachine::UpdateWalkMode(NaviPhase phase)
{
    // FIND 的接近段一律走路: 移动连续、修正只跟着每拍的框来, 跑起来容易冲过头
    if (phase == NaviPhase::WaitFind || phase == NaviPhase::WaitJumpPad) {
        walk_mode_.Request(true);
        return;
    }

    const bool recovering = runtime_state_.recovery.active || runtime_state_.cross_tier_escape.active;
    // Walk into a braked bend whatever the prompt approach says.
    if (phase == NaviPhase::Navigate && position_->valid && !recovering && !runtime_state_.river_fall.pending
        && runtime_state_.flow.corner_walk) {
        walk_mode_.Request(true);
        return;
    }

    PromptDistance nearest = NearestPromptDistance();
    const bool has_waypoint = session_->HasCurrentWaypoint();
    const ActionTraits traits = has_waypoint ? session_->CurrentWaypoint().Traits() : ActionTraits {};
    const bool plain_approach = traits.walk_approach;
    // 末端要纠正的点按同一套来: 走路让滑行距离减半, 到点后要走回去的那段也就短一半
    bool settling_approach = false;
    if (has_waypoint && session_->CurrentWaypoint().SettlesAtArrival()) {
        const Waypoint& goal = session_->CurrentWaypoint();
        const double dx = goal.x - position_->x;
        const double dy = goal.y - position_->y;
        const double distance_sq = dx * dx + dy * dy;
        if (nearest.distance_sq < 0.0 || distance_sq < nearest.distance_sq) {
            nearest.distance_sq = distance_sq;
            nearest.is_zipline = goal.action == ActionType::ZIPLINE;
        }
        settling_approach = true;
    }
    // 连续挖掘会重置起步确认, 短腿从起步就走路
    const bool startup_blocks_walk = !runtime_state_.route.startup_motion_confirmed && !traits.walk_at_startup;
    if (phase != NaviPhase::Navigate || !position_->valid || nearest.distance_sq < 0.0 || recovering
        || !(plain_approach || settling_approach) || startup_blocks_walk) {
        walk_mode_.Request(false);
        return;
    }

    const bool was_engaged = walk_mode_.engaged();
    const double enter_band = nearest.is_zipline ? kZiplineWalkEnterBandWu : kCollectWalkEnterBandWu;
    const double exit_band = nearest.is_zipline ? kZiplineWalkExitBandWu : kCollectWalkExitBandWu;
    const double band = was_engaged ? exit_band : enter_band;
    // 更近的采集点可能还在自己的步行带外, 不能挡掉稍远但已进入较大步行带的挖掘点。
    const double dig_band = was_engaged ? kDigWalkExitBandWu : kDigWalkEnterBandWu;
    const bool approaching_dig = nearest.dig_distance_sq >= 0.0 && nearest.dig_distance_sq <= dig_band * dig_band;
    walk_mode_.Request(nearest.distance_sq <= band * band || approaching_dig);
    const bool walking = walk_mode_.engaged();
    if (walking != was_engaged) {
        const double nearest_stop_point = std::sqrt(nearest.distance_sq);
        LogInfo << "Walk mode boundary crossed." << VAR(walking) << VAR(nearest.is_zipline) << VAR(nearest_stop_point) << VAR(position_->x)
                << VAR(position_->y);
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
        const PromptTriggerOutcome outcome =
            prompts[index]->TryTriggerWhileWalking(motion_controller_, route.waypoint_distance, session_->current_node_idx());
        if (outcome == PromptTriggerOutcome::NotTriggered) {
            continue;
        }
        // 屏幕上一次只弹一个提示, 一次观测只值一次停车: 清掉另一类的闩, 免得同一个提示被停两次
        for (size_t other = 0; other < prompts.size(); ++other) {
            if (other != index) {
                prompts[other]->ForgetDetection();
            }
        }
        if (outcome == PromptTriggerOutcome::Recognized && prompts[index]->spec().CompletesWaypointOnTrigger()) {
            CompleteWaypointAfterPromptTrigger();
        }
        return true;
    }
    return false;
}

// 交互一开角色就不动、小地图也没了, 所以提示命中即算走完
void NavigationStateMachine::CompleteWaypointAfterPromptTrigger()
{
    if (!session_->HasCurrentWaypoint()) {
        return;
    }

    const ActionType action = session_->CurrentWaypoint().action;
    const std::optional<RopeRideInfo> rope_ride = session_->CurrentWaypoint().rope_ride;
    const std::optional<size_t> consumed_absolute_node_idx = session_->CurrentAbsoluteNodeIndex();
    session_->NoteCanonicalFinalGoalConsumed(consumed_absolute_node_idx, *position_, "async_prompt_completed");
    session_->AdvanceToNextWaypoint(action, "async_prompt_completed");
    runtime_state_.OnWaypointAdvance();
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return;
    }
    if (rope_ride) {
        semantic_nodes::StartRopeRide(SemanticContext(), *rope_ride);
        return;
    }
    SelectPhaseForCurrentWaypoint("async_prompt_completed");
}

// 最后一个点被吃掉的同一拍扫描器就销毁, 所以收尾单独给一次检测窗口
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
