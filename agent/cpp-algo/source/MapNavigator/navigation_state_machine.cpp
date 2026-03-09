#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <thread>

#include <MaaUtils/Logger.h>

#include "action_executor.h"
#include "action_wrapper.h"
#include "local_driver_lite.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "navigation_state_machine.h"
#include "position_provider.h"
#include "route_rejoin.h"
#include "zone_transition_controller.h"

namespace mapnavigator
{

namespace
{

struct NavigationSnapshot
{
    size_t current_node_idx = 0;
    size_t path_origin_index = 0;
    ActionType waypoint_action = ActionType::RUN;
    std::string expected_zone_id;
    std::string actual_zone_id;
    double route_distance = 0.0;
    double actual_distance = 0.0;
    double portal_distance = std::numeric_limits<double>::max();
    double visual_yaw = 0.0;
    double virtual_yaw = 0.0;
    double sensor_yaw_error = 0.0;
    double error_yaw = 0.0;
    int64_t stalled_ms = 0;
    int64_t recovery_cooldown_ms = 0;
    bool advanced_by_pass_through = false;
    bool portal_commit_ready = false;
    bool is_zone_transition_isolated = false;
    bool post_turn_forward_commit_active = false;
};

const char* ActionTypeName(ActionType action)
{
    switch (action) {
    case ActionType::RUN:
        return "RUN";
    case ActionType::SPRINT:
        return "SPRINT";
    case ActionType::JUMP:
        return "JUMP";
    case ActionType::FIGHT:
        return "FIGHT";
    case ActionType::INTERACT:
        return "INTERACT";
    case ActionType::TRANSFER:
        return "TRANSFER";
    case ActionType::PORTAL:
        return "PORTAL";
    case ActionType::HEADING:
        return "HEADING";
    case ActionType::ZONE:
        return "ZONE";
    }
    return "UNKNOWN";
}

void LogNavigationSnapshot(const NavigationSnapshot& snapshot, const char* reason)
{
    const char* waypoint_action = ActionTypeName(snapshot.waypoint_action);
    const size_t current_node_idx = snapshot.current_node_idx;
    const size_t path_origin_index = snapshot.path_origin_index;
    const std::string& expected_zone_id = snapshot.expected_zone_id;
    const std::string& actual_zone_id = snapshot.actual_zone_id;
    const double route_distance = snapshot.route_distance;
    const double actual_distance = snapshot.actual_distance;
    const double portal_distance = snapshot.portal_distance;
    const double visual_yaw = snapshot.visual_yaw;
    const double virtual_yaw = snapshot.virtual_yaw;
    const double sensor_yaw_error = snapshot.sensor_yaw_error;
    const double error_yaw = snapshot.error_yaw;
    const int64_t stalled_ms = snapshot.stalled_ms;
    const int64_t recovery_cooldown_ms = snapshot.recovery_cooldown_ms;
    const bool advanced_by_pass_through = snapshot.advanced_by_pass_through;
    const bool portal_commit_ready = snapshot.portal_commit_ready;
    const bool is_zone_transition_isolated = snapshot.is_zone_transition_isolated;
    const bool post_turn_forward_commit_active = snapshot.post_turn_forward_commit_active;

    LogInfo << "Snapshot." << VAR(reason) << VAR(current_node_idx) << VAR(path_origin_index) << VAR(waypoint_action)
            << VAR(expected_zone_id) << VAR(actual_zone_id) << VAR(route_distance) << VAR(actual_distance) << VAR(portal_distance)
            << VAR(visual_yaw) << VAR(virtual_yaw) << VAR(sensor_yaw_error) << VAR(error_yaw) << VAR(stalled_ms)
            << VAR(recovery_cooldown_ms) << VAR(advanced_by_pass_through) << VAR(portal_commit_ready) << VAR(is_zone_transition_isolated)
            << VAR(post_turn_forward_commit_active);
}

} // namespace

NavigationStateMachine::NavigationStateMachine(
    const NaviParam& param,
    ActionWrapper* action_wrapper,
    PositionProvider* position_provider,
    NavigationSession* session,
    RouteRejoinPlanner* rejoin_planner,
    MotionController* motion_controller,
    ZoneTransitionController* zone_transition_controller,
    LocalDriverLite* local_driver,
    IActionExecutor* action_executor,
    NaviPosition* position,
    std::function<bool()> should_stop)
    : param_(param)
    , action_wrapper_(action_wrapper)
    , position_provider_(position_provider)
    , session_(session)
    , rejoin_planner_(rejoin_planner)
    , motion_controller_(motion_controller)
    , zone_transition_controller_(zone_transition_controller)
    , local_driver_(local_driver)
    , action_executor_(action_executor)
    , position_(position)
    , should_stop_(std::move(should_stop))
{
}

bool NavigationStateMachine::Run()
{
    const auto stop_motion_before_exit = [&]() {
        motion_controller_->Stop();
        motion_controller_->ClearForwardCommit();
        if (local_driver_ != nullptr) {
            local_driver_->CancelCommitment();
        }
    };

    if (!Bootstrap()) {
        stop_motion_before_exit();
        return false;
    }

    while (!should_stop_() && session_->phase() != NaviPhase::Finished && session_->phase() != NaviPhase::Failed) {
        if (!TickPhase(session_->phase())) {
            stop_motion_before_exit();
            return false;
        }
    }

    if (!should_stop_() && param_.is_exact_target && !session_->current_path().empty() && session_->phase() != NaviPhase::Failed) {
        session_->UpdatePhase(NaviPhase::ExactTargetRefine, "exact_target_start");
        const bool exact_target_completed = TickPhase(session_->phase()) && !should_stop_();
        stop_motion_before_exit();
        return exact_target_completed;
    }

    if (!should_stop_() && session_->phase() != NaviPhase::Failed) {
        session_->UpdatePhase(NaviPhase::Finished, "navigation_complete");
    }

    stop_motion_before_exit();
    return !should_stop_();
}

bool NavigationStateMachine::Bootstrap()
{
    if (param_.enable_rejoin || param_.path_trim) {
        const RouteRejoinPlan initial_rejoin_plan = rejoin_planner_->Plan(*position_, position_->angle, session_->original_path(), 0);
        if (initial_rejoin_plan.abort || initial_rejoin_plan.candidates.empty()) {
            session_->UpdatePhase(NaviPhase::Failed, "initial_rejoin_failed");
            const bool rejoin_abort = initial_rejoin_plan.abort;
            const double nearest_route_distance = initial_rejoin_plan.nearest_route_distance;
            LogError << "Initial route rejoin failed." << VAR(rejoin_abort) << VAR(nearest_route_distance);
            return false;
        }
        if (!ApplyRejoinCandidate(initial_rejoin_plan.candidates.front(), "initial_rejoin")) {
            session_->UpdatePhase(NaviPhase::Failed, "initial_rejoin_apply_failed");
            return false;
        }
    }
    else {
        SelectPhaseForCurrentWaypoint("bootstrap_ready");
    }

    LogInfo << "Mathematical Odometry Engine Start.";
    return true;
}

bool NavigationStateMachine::TickPhase(NaviPhase phase)
{
    switch (phase) {
    case NaviPhase::Bootstrap:
        SelectPhaseForCurrentWaypoint("bootstrap_dispatch");
        return true;

    case NaviPhase::AlignHeading:
        return TickAlignHeading();

    case NaviPhase::AdvanceOnRoute:
        return TickAdvanceOnRoute();

    case NaviPhase::WaitZoneTransition:
        return TickWaitZoneTransition();

    case NaviPhase::WaitRelocation:
        return TickWaitRelocation();

    case NaviPhase::RecoverRejoin:
        SelectPhaseForCurrentWaypoint("recover_rejoin_dispatch");
        return true;

    case NaviPhase::ExactTargetRefine:
        return TickExactTargetRefine();

    case NaviPhase::Finished:
    case NaviPhase::Failed:
        return true;
    }

    return false;
}

bool NavigationStateMachine::TickWaitZoneTransition()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->UpdatePhase(NaviPhase::Finished, "zone_tail_consumed");
        LogInfo << "Zone declaration consumed at path tail.";
        return true;
    }

    if (session_->CurrentWaypoint().IsZoneDeclaration()) {
        const bool landing_from_portal = session_->is_waiting_for_zone_switch();
        zone_transition_controller_->ConsumeZoneNodes(landing_from_portal);

        if (landing_from_portal) {
            session_->SetWaitingForZoneSwitch(false, "landing_zone_nodes_consumed");
            zone_transition_controller_->ConsumeLandingPortalNode();
        }

        if (!session_->HasCurrentWaypoint()) {
            session_->UpdatePhase(NaviPhase::Finished, "zone_tail_consumed");
            LogInfo << "Zone declaration consumed at path tail.";
            return true;
        }

        SelectPhaseForCurrentWaypoint("zone_nodes_consumed");
        return true;
    }

    if (!session_->is_waiting_for_zone_switch()) {
        SelectPhaseForCurrentWaypoint("zone_phase_exit");
        return true;
    }

    if (!position_provider_->Capture(position_, false, session_->CurrentExpectedZone())) {
        SleepFor(kLocatorRetryIntervalMs);
        return true;
    }

    const std::string expected_zone_id = session_->CurrentExpectedZone();
    if (expected_zone_id.empty() && position_->zone_id != session_->current_zone_id()) {
        if (!HandleImplicitZoneTransition(expected_zone_id)) {
            return true;
        }
        return true;
    }

    if (session_->is_waiting_for_zone_switch()) {
        SleepFor(kLocatorRetryIntervalMs);
        return true;
    }

    SelectPhaseForCurrentWaypoint("zone_switch_complete");
    return true;
}

bool NavigationStateMachine::TickAlignHeading()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->UpdatePhase(NaviPhase::Finished, "heading_only_completed");
        LogInfo << "Heading-only path completed.";
        return true;
    }

    if (!session_->CurrentWaypoint().IsHeadingOnly()) {
        SelectPhaseForCurrentWaypoint("align_phase_exit");
        return true;
    }

    ConsumeHeadingNodes(true);

    if (!session_->HasCurrentWaypoint()) {
        session_->UpdatePhase(NaviPhase::Finished, "heading_only_completed");
        LogInfo << "Heading-only path completed.";
        return true;
    }

    SelectPhaseForCurrentWaypoint("heading_nodes_consumed");
    return true;
}

bool NavigationStateMachine::TickWaitRelocation()
{
    const auto now = std::chrono::steady_clock::now();
    const auto waited_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - relocation_wait_started_).count();
    if (waited_ms < relocation_min_pause_ms_) {
        SleepFor(kRelocationRetryIntervalMs);
        return true;
    }

    if (!position_provider_->Capture(position_, false, session_->CurrentExpectedZone())) {
        if (waited_ms > kRelocationWaitTimeoutMs) {
            session_->UpdatePhase(NaviPhase::Failed, "relocation_wait_timeout");
            const size_t current_node_idx = session_->current_node_idx();
            LogError << "Relocation wait timed out before position stabilized." << VAR(current_node_idx) << VAR(waited_ms);
            return true;
        }
        SleepFor(kRelocationRetryIntervalMs);
        return true;
    }

    const bool held_fix = position_provider_->LastCaptureWasHeld();
    if (held_fix) {
        relocation_stable_hits_ = 0;
        if (waited_ms > kRelocationWaitTimeoutMs) {
            session_->UpdatePhase(NaviPhase::Failed, "relocation_wait_timeout");
            const size_t current_node_idx = session_->current_node_idx();
            const int held_fix_streak = position_provider_->HeldFixStreak();
            LogError << "Relocation wait timed out while locator fix stayed held." << VAR(current_node_idx) << VAR(waited_ms)
                     << VAR(held_fix_streak);
            return true;
        }
        SleepFor(kRelocationRetryIntervalMs);
        return true;
    }

    const double moved_from_anchor = std::hypot(position_->x - relocation_anchor_pos_.x, position_->y - relocation_anchor_pos_.y);
    const bool relocation_complete = !relocation_requires_movement_ || position_->zone_id != relocation_anchor_pos_.zone_id
                                     || moved_from_anchor >= kRelocationResumeMinDistance;

    if (!relocation_complete) {
        relocation_stable_hits_ = 0;
        if (waited_ms > kRelocationWaitTimeoutMs) {
            session_->UpdatePhase(NaviPhase::Failed, "relocation_wait_timeout");
            const size_t current_node_idx = session_->current_node_idx();
            LogError << "Relocation wait timed out without movement." << VAR(current_node_idx) << VAR(waited_ms) << VAR(moved_from_anchor);
            return true;
        }
        SleepFor(kRelocationRetryIntervalMs);
        return true;
    }

    relocation_stable_hits_++;
    if (relocation_stable_hits_ < kRelocationStableFixes) {
        SleepFor(kRelocationRetryIntervalMs);
        return true;
    }

    const bool relocation_finish_when_complete = relocation_finish_when_complete_;
    const char* relocation_reason = relocation_reason_;
    const RelocationCompletionPolicy relocation_completion_policy = relocation_completion_policy_;
    relocation_finish_when_complete_ = false;
    relocation_reason_ = "";
    relocation_completion_policy_ = RelocationCompletionPolicy::ResumeRoute;
    pending_transfer_relocation_wait_ = false;
    if (relocation_finish_when_complete || !session_->HasCurrentWaypoint()) {
        session_->UpdatePhase(NaviPhase::Finished, "relocation_complete");
        return true;
    }

    if (relocation_completion_policy == RelocationCompletionPolicy::RejoinOrFail) {
        const size_t current_node_idx = session_->current_node_idx();
        const double position_x = position_->x;
        const double position_y = position_->y;
        LogInfo << "Relocation recovery completed, attempting route rejoin." << VAR(relocation_reason) << VAR(current_node_idx)
                << VAR(position_x) << VAR(position_y);
        if (AttemptRouteRejoin(relocation_reason, false)) {
            return true;
        }

        session_->UpdatePhase(NaviPhase::Failed, "relocation_rejoin_failed");
        LogError << "Relocation recovery could not rejoin route; aborting blind resume." << VAR(relocation_reason) << VAR(current_node_idx)
                 << VAR(position_x) << VAR(position_y);
        return true;
    }

    SelectPhaseForCurrentWaypoint("relocation_complete");
    return true;
}

bool NavigationStateMachine::TickAdvanceOnRoute()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->UpdatePhase(NaviPhase::Finished, "advance_completed");
        return true;
    }

    if (session_->CurrentWaypoint().IsControlNode()) {
        SelectPhaseForCurrentWaypoint("advance_control_node");
        return true;
    }

    const Waypoint& current_waypoint = session_->CurrentWaypoint();
    if (!motion_controller_->IsMoving()) {
        const double stationary_portal_distance =
            session_->DistanceToAdjacentPortal(session_->current_node_idx(), position_->x, position_->y);
        const bool stationary_portal_commit_ready =
            current_waypoint.action == ActionType::PORTAL
            && std::hypot(current_waypoint.x - position_->x, current_waypoint.y - position_->y) <= kPortalCommitDistance;
        if (CanArriveSamePointAction(current_waypoint)) {
            const double stationary_distance = std::hypot(current_waypoint.x - position_->x, current_waypoint.y - position_->y);
            return HandleWaypointArrival(
                position_->x,
                position_->y,
                stationary_distance,
                stationary_distance,
                stationary_portal_distance,
                false,
                stationary_portal_commit_ready);
        }

        ResumeMotionTowardsCurrentWaypoint(position_->x, position_->y, "advance_resume", kWaitAfterFirstTurnMs);
        return true;
    }

    const auto frame_start_time = std::chrono::steady_clock::now();
    const std::string expected_zone_id = session_->CurrentExpectedZone();

    const bool allow_blind_recovery = current_waypoint.action == ActionType::PORTAL || session_->is_waiting_for_zone_switch();
    const auto enter_black_screen_recovery = [&](const char* capture_stage) {
        if (!motion_controller_->IsMoving() || allow_blind_recovery || !position_provider_->LastCaptureWasBlackScreen()) {
            return false;
        }

        const size_t current_node_idx = session_->current_node_idx();
        const bool held_fix = position_provider_->LastCaptureWasHeld();
        const std::string zone_id = position_->valid ? position_->zone_id : session_->current_zone_id();
        LogWarn << "Black screen detected while moving, delaying motion recovery." << VAR(current_node_idx) << VAR(capture_stage)
                << VAR(held_fix) << VAR(zone_id);
        EnterRelocationWait("black_screen_recovery", kRespawnRecoveryPauseMs, false, RelocationCompletionPolicy::RejoinOrFail);
        return true;
    };

    if (!position_provider_->Capture(position_, false, expected_zone_id)) {
        if (enter_black_screen_recovery("capture_failed")) {
            return true;
        }
        SleepFor(kLocatorRetryIntervalMs);
        return true;
    }

    if (enter_black_screen_recovery("capture_succeeded")) {
        return true;
    }

    if (expected_zone_id.empty() && position_->zone_id != session_->current_zone_id()) {
        if (!HandleImplicitZoneTransition(expected_zone_id)) {
            return true;
        }
        return true;
    }

    if (session_->phase() != NaviPhase::AdvanceOnRoute) {
        return true;
    }

    if (!session_->HasCurrentWaypoint() || session_->CurrentWaypoint().IsControlNode()) {
        SelectPhaseForCurrentWaypoint("advance_transitioned");
        return true;
    }

    const double latency_sec =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - frame_start_time).count() / 1000.0;
    const int latency_ms = static_cast<int>(latency_sec * 1000);

    double real_pos_x = position_->x;
    double real_pos_y = position_->y;
    const double visual_yaw = position_->angle;

    if (motion_controller_->IsMoving()) {
        const double yaw_rad = session_->virtual_yaw() * (kPi / 180.0);
        real_pos_x += kRunSpeedMps * latency_sec * std::cos(yaw_rad);
        real_pos_y += kRunSpeedMps * latency_sec * std::sin(yaw_rad);
    }

    const double target_x = current_waypoint.x;
    const double target_y = current_waypoint.y;
    const double distance = std::hypot(target_x - real_pos_x, target_y - real_pos_y);
    const double actual_distance = std::hypot(target_x - position_->x, target_y - position_->y);
    if (current_waypoint.RequiresStrictArrival() && strict_arrival_walk_reset_node_idx_ != session_->current_node_idx()
        && actual_distance <= kStrictArrivalWalkResetDistance && motion_controller_->IsMoving()) {
        motion_controller_->ResetForwardWalk(kWalkResetReleaseMs);
        strict_arrival_walk_reset_node_idx_ = session_->current_node_idx();
        last_auto_sprint_time_ = {};
        session_->ResetStraightStableFrames();
        SleepFor(kWalkResetSettleMs);
        return true;
    }

    const double yaw_mismatch = std::abs(NaviMath::NormalizeAngle(visual_yaw - session_->virtual_yaw()));
    const bool advanced_by_pass_through = session_->ShouldAdvanceByPassThrough(session_->current_node_idx(), position_->x, position_->y);
    const double portal_distance = session_->DistanceToAdjacentPortal(session_->current_node_idx(), position_->x, position_->y);
    const bool is_zone_transition_isolated = portal_distance <= kZoneTransitionIsolationDistance;
    const bool portal_commit_ready = current_waypoint.action == ActionType::PORTAL && actual_distance <= kPortalCommitDistance;

    const auto progress_now = std::chrono::steady_clock::now();
    session_->ObserveProgress(session_->current_node_idx(), actual_distance, progress_now);
    const auto stalled_ms = session_->StalledMs(progress_now);
    const auto recovery_cooldown_ms = session_->RecoveryCooldownMs(progress_now);
    const auto make_snapshot = [&]() {
        NavigationSnapshot snapshot;
        snapshot.current_node_idx = session_->current_node_idx();
        snapshot.path_origin_index = session_->path_origin_index();
        snapshot.waypoint_action = session_->CurrentWaypoint().action;
        snapshot.expected_zone_id = expected_zone_id;
        snapshot.actual_zone_id = position_->zone_id;
        snapshot.route_distance = distance;
        snapshot.actual_distance = actual_distance;
        snapshot.portal_distance = portal_distance;
        snapshot.visual_yaw = visual_yaw;
        snapshot.virtual_yaw = session_->virtual_yaw();
        snapshot.stalled_ms = stalled_ms;
        snapshot.recovery_cooldown_ms = recovery_cooldown_ms;
        snapshot.advanced_by_pass_through = advanced_by_pass_through;
        snapshot.portal_commit_ready = portal_commit_ready;
        snapshot.is_zone_transition_isolated = is_zone_transition_isolated;
        return snapshot;
    };

    if (!is_zone_transition_isolated && param_.arrival_timeout > 0 && actual_distance > kNoProgressMinDistance
        && stalled_ms > param_.arrival_timeout && session_->no_progress_recovery_attempts() >= kArrivalTimeoutMinRecoveryAttempts) {
        session_->UpdatePhase(NaviPhase::Failed, "arrival_watchdog_exhausted");
        NavigationSnapshot snapshot = make_snapshot();
        const size_t current_node_idx = session_->current_node_idx();
        const double best_actual_distance = session_->best_actual_distance();
        const int no_progress_recovery_attempts = session_->no_progress_recovery_attempts();
        LogError << "Arrival watchdog exhausted after repeated recovery attempts." << VAR(current_node_idx) << VAR(actual_distance)
                 << VAR(best_actual_distance) << VAR(stalled_ms) << VAR(no_progress_recovery_attempts);
        LogNavigationSnapshot(snapshot, "arrival_watchdog_exhausted");
        return true;
    }

    if (distance < current_waypoint.GetLookahead() || advanced_by_pass_through || portal_commit_ready) {
        return HandleWaypointArrival(
            real_pos_x,
            real_pos_y,
            distance,
            actual_distance,
            portal_distance,
            advanced_by_pass_through,
            portal_commit_ready);
    }

    const auto loop_now = std::chrono::steady_clock::now();
    const bool held_fix = position_provider_->LastCaptureWasHeld();
    const double moved_distance =
        session_->has_previous_driver_pos() && session_->previous_driver_pos().zone_id == position_->zone_id
            ? std::hypot(position_->x - session_->previous_driver_pos().x, position_->y - session_->previous_driver_pos().y)
            : 0.0;
    const double distance_delta =
        std::isnan(session_->previous_driver_distance()) ? 0.0 : session_->previous_driver_distance() - actual_distance;
    const double distance_increase =
        std::isnan(session_->previous_driver_distance()) ? 0.0 : actual_distance - session_->previous_driver_distance();
    const bool suspected_respawn = session_->has_previous_driver_pos() && position_->zone_id == session_->previous_driver_pos().zone_id
                                   && moved_distance >= kRespawnTeleportDistance && distance_increase >= kRespawnDistanceIncreaseThreshold
                                   && current_waypoint.action != ActionType::PORTAL && !current_waypoint.WaitsForRelocation()
                                   && !session_->is_waiting_for_zone_switch();
    const bool partial_respawn_signal =
        session_->has_previous_driver_pos() && position_->zone_id == session_->previous_driver_pos().zone_id && !suspected_respawn
        && (moved_distance >= kRespawnTeleportDistance || distance_increase >= kRespawnDistanceIncreaseThreshold);
    if (partial_respawn_signal) {
        const size_t current_node_idx = session_->current_node_idx();
        LogWarn << "Respawn-like relocation signal observed, but thresholds not fully met." << VAR(current_node_idx) << VAR(moved_distance)
                << VAR(distance_increase) << VAR(actual_distance) << VAR(held_fix);
    }
    if (suspected_respawn) {
        const size_t current_node_idx = session_->current_node_idx();
        LogWarn << "Respawn-like relocation detected, delaying motion recovery." << VAR(current_node_idx) << VAR(moved_distance)
                << VAR(distance_increase) << VAR(actual_distance);
        EnterRelocationWait("respawn_recovery", kRespawnRecoveryPauseMs, false, RelocationCompletionPolicy::RejoinOrFail);
        return true;
    }

    const double ideal_yaw_now = NaviMath::CalcTargetRotation(real_pos_x, real_pos_y, target_x, target_y);
    const double error_yaw = NaviMath::NormalizeAngle(ideal_yaw_now - session_->virtual_yaw());
    const double sensor_target_yaw = NaviMath::CalcTargetRotation(position_->x, position_->y, target_x, target_y);
    const double sensor_yaw_error = NaviMath::NormalizeAngle(sensor_target_yaw - visual_yaw);
    const bool post_turn_forward_commit_active = motion_controller_->IsForwardCommitActive(loop_now);
    const size_t next_position_idx = session_->FindNextPositionNode(session_->current_node_idx());

    LocalDriverDecision driver_decision;
    driver_decision.state = LocalDriverState::Cruise;
    driver_decision.action = LocalDriverAction::Forward;
    if (param_.enable_local_driver && !held_fix) {
        LocalDriverObservation observation;
        observation.now = loop_now;
        observation.yaw_error = sensor_yaw_error;
        observation.distance_delta = distance_delta;
        observation.moved_distance = moved_distance;
        observation.actual_distance = actual_distance;
        observation.prefer_jump_recovery = is_zone_transition_isolated;
        driver_decision = local_driver_->Evaluate(observation);
    }
    else {
        local_driver_->Reset();
    }

    if (post_turn_forward_commit_active && !is_zone_transition_isolated) {
        motion_controller_->EnsureForwardMotion(false);
    }

    if (!is_zone_transition_isolated && driver_decision.request_rejoin && recovery_cooldown_ms > param_.rejoin_retry_timeout
        && actual_distance > kNoProgressMinDistance) {
        session_->UpdatePhase(NaviPhase::RecoverRejoin, "local_driver_escalation");
        NavigationSnapshot snapshot = make_snapshot();
        snapshot.sensor_yaw_error = sensor_yaw_error;
        snapshot.error_yaw = error_yaw;
        snapshot.post_turn_forward_commit_active = post_turn_forward_commit_active;
        const size_t current_node_idx = session_->current_node_idx();
        const double best_actual_distance = session_->best_actual_distance();
        const int no_progress_recovery_attempts = session_->no_progress_recovery_attempts();
        LogWarn << "LocalDriverLite escalated to route rejoin." << VAR(current_node_idx) << VAR(actual_distance)
                << VAR(best_actual_distance) << VAR(stalled_ms) << VAR(no_progress_recovery_attempts);
        LogNavigationSnapshot(snapshot, "local_driver_escalation");
        if (AttemptRouteRejoin("recover_exhausted", true)) {
            session_->MarkRecoveryAttempt(actual_distance, std::chrono::steady_clock::now());
            return true;
        }
        session_->UpdatePhase(NaviPhase::Failed, "route_rejoin_failed_after_escalation");
        const size_t failed_current_node_idx = session_->current_node_idx();
        LogError << "Route rejoin failed after local recover exhaustion." << VAR(failed_current_node_idx) << VAR(actual_distance)
                 << VAR(stalled_ms);
        LogNavigationSnapshot(snapshot, "route_rejoin_failed_after_escalation");
        return true;
    }

    const bool allow_turn_in_place = !held_fix && !post_turn_forward_commit_active && !is_zone_transition_isolated
                                     && std::abs(sensor_yaw_error) > kLocalDriverTurnInPlaceYawDegrees
                                     && (!motion_controller_->IsMoving() || stalled_ms >= kTurnInPlaceStallMs);

    if (allow_turn_in_place) {
        const bool severe_turn_drift = HasSevereTurnDrift(yaw_mismatch, actual_distance, stalled_ms);
        const bool moving_now = motion_controller_->IsMoving();
        const size_t current_node_idx = session_->current_node_idx();
        const double position_x = position_->x;
        const double position_y = position_->y;
        const double virtual_yaw = session_->virtual_yaw();
        const double best_actual_distance = session_->best_actual_distance();
        LogWarn << "Turn-in-place correction triggered." << VAR(current_node_idx) << VAR(position_x) << VAR(position_y) << VAR(target_x)
                << VAR(target_y) << VAR(next_position_idx) << VAR(sensor_target_yaw) << VAR(visual_yaw) << VAR(virtual_yaw)
                << VAR(advanced_by_pass_through) << VAR(sensor_yaw_error) << VAR(actual_distance) << VAR(best_actual_distance)
                << VAR(stalled_ms) << VAR(moving_now);
        if (next_position_idx < session_->current_path().size()) {
            const Waypoint& next_waypoint = session_->CurrentPathAt(next_position_idx);
            const double next_waypoint_x = next_waypoint.x;
            const double next_waypoint_y = next_waypoint.y;
            const std::string& next_waypoint_zone_id = next_waypoint.zone_id;
            LogWarn << "Turn-in-place next waypoint." << VAR(next_waypoint_x) << VAR(next_waypoint_y) << VAR(next_waypoint_zone_id);
        }
        motion_controller_->Stop();
        local_driver_->CancelCommitment();
        session_->SyncVirtualYaw(visual_yaw, "turn_in_place_sync");
        if (!motion_controller_->adaptive_mode_enabled() && severe_turn_drift) {
            motion_controller_->EnableAdaptiveMode("severe_turn_drift", yaw_mismatch);
        }

        const double turn_in_place_delta = NaviMath::NormalizeAngle(sensor_target_yaw - session_->virtual_yaw());
        motion_controller_
            ->InjectMouseAndTrack(turn_in_place_delta, severe_turn_drift, session_->CurrentExpectedZone(), kWaitAfterFirstTurnMs);
        motion_controller_->ArmForwardCommit(turn_in_place_delta, "turn_in_place");
        motion_controller_->EnsureForwardMotion(true);

        session_->RecordDriverObservation(actual_distance, *position_);
        session_->ResetStraightStableFrames();
        return true;
    }

    const bool suppress_micro_turn =
        held_fix || position_provider_->HeldFixStreak() > 0 || post_turn_forward_commit_active
        || (param_.enable_local_driver && driver_decision.commitment_active && driver_decision.state != LocalDriverState::Cruise);

    if (held_fix && position_provider_->HeldFixStreak() == 1) {
        const size_t current_node_idx = session_->current_node_idx();
        const std::string& zone_id = position_->zone_id;
        LogWarn << "Held locator fix detected, suppress steering corrections." << VAR(current_node_idx) << VAR(zone_id)
                << VAR(actual_distance) << VAR(sensor_yaw_error);
    }

    if (is_zone_transition_isolated) {
        session_->ResetStraightStableFrames();
    }
    else if (!suppress_micro_turn && std::abs(error_yaw) > kMicroThreshold) {
        motion_controller_->InjectMouseAndTrack(error_yaw, false, session_->CurrentExpectedZone(), 0);
        session_->ResetStraightStableFrames();

        if (std::abs(error_yaw) > kMaxErrorYawWithoutStop) {
            SleepFor(kStopWaitMs);
        }
    }
    else {
        session_->IncrementStraightStableFrames();
    }

    if (session_->straight_stable_frames() > kStableFramesThreshold) {
        if (latency_ms < kMaxLatencyForCorrectionMs
            && std::abs(NaviMath::NormalizeAngle(visual_yaw - session_->virtual_yaw())) < kMaxYawDeviationForCorrection) {
            session_->SyncVirtualYaw(visual_yaw, "straight_run_correction");
        }
        session_->ResetStraightStableFrames();
    }

    if (param_.enable_local_driver) {
        const LocalDriverAction target_drive_action = post_turn_forward_commit_active ? LocalDriverAction::Forward : driver_decision.action;
        const bool force_drive_action = post_turn_forward_commit_active
                                            ? !motion_controller_->HasAppliedAction()
                                            : (driver_decision.action_changed || !motion_controller_->HasAppliedAction());
        motion_controller_->SetAction(target_drive_action, force_drive_action);
    }
    else if (!motion_controller_->IsMoving()) {
        motion_controller_->EnsureForwardMotion(false);
    }

    const bool auto_sprint_ready = param_.sprint_threshold > 0.0 && actual_distance > param_.sprint_threshold
                                   && std::abs(sensor_yaw_error) <= kLocalDriverSideAvoidYawDegrees && !held_fix
                                   && !is_zone_transition_isolated && !post_turn_forward_commit_active
                                   && !current_waypoint.RequiresStrictArrival() && motion_controller_->IsMoving()
                                   && (!param_.enable_local_driver
                                       || (driver_decision.state == LocalDriverState::Cruise
                                           && driver_decision.action == LocalDriverAction::Forward && !driver_decision.commitment_active));
    if (auto_sprint_ready) {
        MaybeTriggerAutoSprint(actual_distance, sensor_yaw_error, loop_now);
    }

    session_->RecordDriverObservation(actual_distance, *position_);

    int sleep_time = kTargetTickMs - latency_ms;
    if (sleep_time < kMinSleepMs) {
        sleep_time = kMinSleepMs;
    }
    SleepFor(sleep_time);
    return true;
}

void NavigationStateMachine::MaybeTriggerAutoSprint(
    double actual_distance,
    double sensor_yaw_error,
    const std::chrono::steady_clock::time_point& now)
{
    if (last_auto_sprint_time_.time_since_epoch().count() > 0
        && std::chrono::duration_cast<std::chrono::milliseconds>(now - last_auto_sprint_time_).count() < kAutoSprintCooldownMs) {
        return;
    }

    action_wrapper_->TriggerSprintSync();
    last_auto_sprint_time_ = now;

    const size_t current_node_idx = session_->current_node_idx();
    LogInfo << "Auto sprint triggered." << VAR(current_node_idx) << VAR(actual_distance) << VAR(sensor_yaw_error);
}

bool NavigationStateMachine::TickExactTargetRefine()
{
    const auto exact_target_it =
        std::find_if(session_->current_path().rbegin(), session_->current_path().rend(), [](const Waypoint& waypoint) {
            return waypoint.HasPosition();
        });
    if (exact_target_it == session_->current_path().rend()) {
        session_->UpdatePhase(NaviPhase::Finished, "no_exact_target_waypoint");
        return true;
    }

    const double target_x = exact_target_it->x;
    const double target_y = exact_target_it->y;
    const std::string target_zone_id = exact_target_it->zone_id;
    LogInfo << "Starting exact target approach...";
    const auto exact_start_time = std::chrono::steady_clock::now();

    while (!should_stop_()) {
        if (!position_provider_->Capture(position_, false, target_zone_id)) {
            SleepFor(kExactTargetLocatorRetryIntervalMs);
            continue;
        }

        const double dist = std::hypot(target_x - position_->x, target_y - position_->y);
        if (dist < kExactTargetDistanceThreshold) {
            session_->UpdatePhase(NaviPhase::Finished, "exact_target_reached");
            LogInfo << "Exact target reached!!!";
            return true;
        }

        const auto elapsed =
            std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - exact_start_time).count();
        if (elapsed > kExactTargetTimeoutMs) {
            session_->UpdatePhase(NaviPhase::Failed, "exact_target_timeout");
            LogWarn << "Exact target approach timeout";
            return true;
        }

        const double exact_rot = NaviMath::CalcTargetRotation(position_->x, position_->y, target_x, target_y);
        const double delta_rot = NaviMath::CalcDeltaRotation(position_->angle, exact_rot);
        if (std::abs(delta_rot) > kExactTargetRotationDeviationThreshold) {
            motion_controller_->InjectMouseAndTrack(delta_rot, false, target_zone_id, kExactTargetRotationWaitMs);
        }

        action_wrapper_->KeyDownSync(kKeyW, 0);
        SleepFor(kExactTargetMoveWaitMs);
        action_wrapper_->KeyUpSync(kKeyW, 0);
        SleepFor(kExactTargetStopWaitMs);
    }

    return false;
}

bool NavigationStateMachine::ConsumeHeadingNodes(bool sync_with_sensor_yaw)
{
    if (sync_with_sensor_yaw && session_->HasCurrentWaypoint() && session_->CurrentWaypoint().IsHeadingOnly()) {
        session_->SyncVirtualYaw(position_->angle, "heading_sync_sensor");
    }

    bool consumed = false;
    while (session_->HasCurrentWaypoint() && session_->CurrentWaypoint().IsHeadingOnly()) {
        const Waypoint& heading_node = session_->CurrentWaypoint();
        double target_heading = heading_node.heading_angle;
        target_heading = std::fmod(target_heading, 360.0);
        if (target_heading < 0.0) {
            target_heading += 360.0;
        }
        const double required_turn = NaviMath::NormalizeAngle(target_heading - session_->virtual_yaw());
        const size_t current_node_idx = session_->current_node_idx();
        const double virtual_yaw = session_->virtual_yaw();

        LogInfo << "HEADING node triggered." << VAR(current_node_idx) << VAR(target_heading) << VAR(virtual_yaw) << VAR(required_turn);

        if (motion_controller_->IsMoving()) {
            motion_controller_->Stop();
            SleepFor(kStopWaitMs);
        }

        motion_controller_->InjectMouseAndTrack(required_turn, false, heading_node.zone_id, kWaitAfterFirstTurnMs);
        motion_controller_->ArmForwardCommit(required_turn, "heading");

        session_->AdvanceToNextWaypoint(ActionType::HEADING, "heading_consumed");
        last_auto_sprint_time_ = {};
        consumed = true;
        session_->ResetStraightStableFrames();
        session_->ResetDriverProgressTracking(local_driver_);

        if (session_->HasCurrentWaypoint()) {
            motion_controller_->EnsureForwardMotion(true);
        }
        else {
            action_wrapper_->ClickKeySync(kKeyW, kExactTargetMoveWaitMs);
        }
    }

    return consumed;
}

void NavigationStateMachine::EnterRelocationWait(
    const char* reason,
    int min_pause_ms,
    bool require_movement,
    RelocationCompletionPolicy completion_policy)
{
    motion_controller_->Stop();
    motion_controller_->ClearForwardCommit();
    if (local_driver_ != nullptr) {
        local_driver_->CancelCommitment();
    }
    position_provider_->ResetTracking();
    relocation_wait_started_ = std::chrono::steady_clock::now();
    relocation_anchor_pos_ = *position_;
    relocation_min_pause_ms_ = min_pause_ms;
    relocation_stable_hits_ = 0;
    relocation_requires_movement_ = require_movement;
    relocation_reason_ = reason;
    relocation_completion_policy_ = completion_policy;
    pending_transfer_relocation_wait_ = false;
    session_->UpdatePhase(NaviPhase::WaitRelocation, reason);
}

bool NavigationStateMachine::HandleImplicitZoneTransition(const std::string& expected_zone_id)
{
    if (!expected_zone_id.empty() || position_->zone_id == session_->current_zone_id()) {
        return false;
    }

    bool valid_transition = false;
    for (size_t index = session_->current_node_idx();
         index <= std::min(session_->current_node_idx() + 1, session_->current_path().size() - 1);
         ++index) {
        if (session_->CurrentPathAt(index).action != ActionType::PORTAL) {
            continue;
        }

        session_->SkipPastWaypoint(index, "portal_zone_transition");
        session_->UpdateCurrentZone(position_->zone_id, "portal_zone_transition");
        session_->SetWaitingForZoneSwitch(false, "portal_zone_transition");
        valid_transition = true;
        zone_transition_controller_->ConsumeLandingPortalNode();
        break;
    }

    if (!valid_transition) {
        return false;
    }

    if (!session_->HasCurrentWaypoint()) {
        session_->UpdatePhase(NaviPhase::Finished, "portal_transition_finished");
        LogInfo << "Reached final destination after portal transition.";
        return true;
    }

    SelectPhaseForCurrentWaypoint("portal_transition_complete");
    if (session_->phase() == NaviPhase::AdvanceOnRoute) {
        session_->SyncVirtualYaw(position_->angle, "portal_resume_sync");
        ResumeMotionTowardsCurrentWaypoint(position_->x, position_->y, "portal_resume", 0);
    }

    return true;
}

bool NavigationStateMachine::HandleWaypointArrival(
    double real_pos_x,
    double real_pos_y,
    [[maybe_unused]] double distance,
    double actual_distance,
    double portal_distance,
    bool advanced_by_pass_through,
    bool portal_commit_ready)
{
    const Waypoint arrived_waypoint = session_->CurrentWaypoint();
    const ActionType current_action = arrived_waypoint.action;

    if (advanced_by_pass_through) {
        const size_t current_node_idx = session_->current_node_idx();
        const double position_x = position_->x;
        const double position_y = position_->y;
        LogInfo << "Advance waypoint by pass-through." << VAR(current_node_idx) << VAR(actual_distance) << VAR(position_x)
                << VAR(position_y);
    }
    if (portal_commit_ready) {
        const size_t current_node_idx = session_->current_node_idx();
        LogInfo << "Commit PORTAL approach early." << VAR(current_node_idx) << VAR(actual_distance) << VAR(portal_distance);
    }

    if (arrived_waypoint.RequiresStrictArrival() && motion_controller_->IsMoving()) {
        motion_controller_->Stop();
        motion_controller_->ClearForwardCommit();
        if (local_driver_ != nullptr) {
            local_driver_->CancelCommitment();
        }
        SleepFor(kStopWaitMs);
    }

    if (arrived_waypoint.WaitsForRelocation()) {
        session_->AdvanceToNextWaypoint(current_action, "transfer_wait_started");
        last_auto_sprint_time_ = {};
        session_->ResetDriverProgressTracking(local_driver_);
        pending_transfer_relocation_wait_ = true;
        relocation_finish_when_complete_ = !session_->HasCurrentWaypoint();
        if (session_->HasCurrentWaypoint() && session_->CurrentWaypoint().HasPosition()
            && CanChainSamePointAction(arrived_waypoint, session_->CurrentWaypoint())) {
            const size_t current_node_idx = session_->current_node_idx();
            LogInfo << "TRANSFER chained with same-point follow-up action." << VAR(current_node_idx);
            SelectPhaseForCurrentWaypoint("transfer_chain_deferred");
            session_->ResetStraightStableFrames();
            return true;
        }

        EnterRelocationWait("transfer_wait_started", 0, true, RelocationCompletionPolicy::ResumeRoute);
        return true;
    }

    const ActionExecutionResult execution = action_executor_->Execute(current_action);
    if (execution.entered_portal_mode) {
        if (session_->current_node_idx() + 1 < session_->current_path().size()
            && session_->CurrentPathAt(session_->current_node_idx() + 1).IsZoneDeclaration()) {
            session_->AdvanceToNextWaypoint(ActionType::PORTAL, "portal_skip_zone_declaration");
        }
        session_->SetWaitingForZoneSwitch(true, "portal_wait_zone_switch");
        position_provider_->ResetTracking();
        session_->UpdatePhase(NaviPhase::WaitZoneTransition, "portal_wait_zone_switch");
        return true;
    }

    session_->AdvanceToNextWaypoint(current_action, "waypoint_action_completed");
    last_auto_sprint_time_ = {};
    session_->ResetDriverProgressTracking(local_driver_);
    const bool should_enter_pending_transfer_wait = pending_transfer_relocation_wait_;
    relocation_finish_when_complete_ = should_enter_pending_transfer_wait && !session_->HasCurrentWaypoint();
    if (should_enter_pending_transfer_wait) {
        const bool can_chain_same_point_action = session_->HasCurrentWaypoint() && session_->CurrentWaypoint().HasPosition()
                                                 && CanChainSamePointAction(arrived_waypoint, session_->CurrentWaypoint());
        if (can_chain_same_point_action) {
            SelectPhaseForCurrentWaypoint("waypoint_action_completed");
            session_->ResetStraightStableFrames();
            return true;
        }

        EnterRelocationWait("transfer_wait_started", 0, true, RelocationCompletionPolicy::ResumeRoute);
        return true;
    }

    if (!session_->HasCurrentWaypoint()) {
        session_->UpdatePhase(NaviPhase::Finished, "reached_final_destination");
        LogInfo << "Reached final destination.";
        return true;
    }

    SelectPhaseForCurrentWaypoint("waypoint_action_completed");
    if (session_->phase() == NaviPhase::AdvanceOnRoute) {
        const Waypoint& next_waypoint = session_->CurrentWaypoint();
        const bool can_chain_same_point_action =
            !motion_controller_->IsMoving() && CanChainSamePointAction(arrived_waypoint, next_waypoint);
        if (can_chain_same_point_action) {
            session_->ResetStraightStableFrames();
            return true;
        }

        ResumeMotionTowardsCurrentWaypoint(real_pos_x, real_pos_y, "corner", 0);
        session_->ResetStraightStableFrames();
    }

    return true;
}

bool NavigationStateMachine::CanArriveSamePointAction(const Waypoint& waypoint) const
{
    if (!waypoint.HasPosition()) {
        return false;
    }

    return (waypoint.zone_id.empty() || waypoint.zone_id == position_->zone_id)
           && std::hypot(waypoint.x - position_->x, waypoint.y - position_->y) <= kSamePointActionChainDistance;
}

bool NavigationStateMachine::CanChainSamePointAction(const Waypoint& from_waypoint, const Waypoint& to_waypoint) const
{
    if (!from_waypoint.HasPosition() || !to_waypoint.HasPosition()) {
        return false;
    }

    return (to_waypoint.zone_id.empty() || to_waypoint.zone_id == position_->zone_id)
           && std::hypot(to_waypoint.x - from_waypoint.x, to_waypoint.y - from_waypoint.y) <= kSamePointActionChainDistance;
}

bool NavigationStateMachine::ApplyRejoinCandidate(const RouteRejoinCandidate& candidate, const char* reason)
{
    if (candidate.continue_index >= session_->original_path().size()) {
        return false;
    }

    const size_t slice_start = session_->FindRejoinSliceStart(candidate.continue_index);
    if (slice_start >= session_->original_path().size()) {
        return false;
    }

    session_->UpdatePhase(NaviPhase::RecoverRejoin, reason);
    motion_controller_->Stop();
    last_auto_sprint_time_ = {};
    session_->ApplyRejoinSlice(slice_start, *position_);
    motion_controller_->ClearForwardCommit();
    session_->ResetProgress();
    session_->ResetDriverProgressTracking(local_driver_);
    const size_t candidate_continue_index = candidate.continue_index;
    const double candidate_route_distance = candidate.route_distance;
    const double candidate_score = candidate.score;

    LogInfo << "Route rejoin applied." << VAR(reason) << VAR(candidate_continue_index) << VAR(slice_start) << VAR(candidate_route_distance)
            << VAR(candidate_score);

    SelectPhaseForCurrentWaypoint("rejoin_applied");
    return !session_->current_path().empty();
}

bool NavigationStateMachine::AttemptRouteRejoin(const char* reason, bool require_forward_candidate)
{
    if (!param_.enable_rejoin || session_->original_path().empty()) {
        return false;
    }

    const size_t preferred_index = session_->CurrentAbsoluteNodeIndex();
    const RouteRejoinPlan plan = rejoin_planner_->Plan(*position_, position_->angle, session_->original_path(), preferred_index);
    if (plan.abort || plan.candidates.empty()) {
        const bool plan_abort = plan.abort;
        const double nearest_route_distance = plan.nearest_route_distance;
        LogWarn << "Route rejoin plan rejected." << VAR(reason) << VAR(plan_abort) << VAR(nearest_route_distance) << VAR(preferred_index);
        return false;
    }

    const RouteRejoinCandidate* selected = nullptr;
    for (const auto& candidate : plan.candidates) {
        if (!require_forward_candidate) {
            selected = &candidate;
            break;
        }
        if (candidate.continue_index > preferred_index || candidate.decision == RejoinDecisionType::Segment) {
            selected = &candidate;
            break;
        }
    }
    if (selected == nullptr) {
        selected = &plan.candidates.front();
    }

    if (require_forward_candidate && selected->continue_index <= preferred_index && selected->decision == RejoinDecisionType::Waypoint) {
        const size_t selected_continue_index = selected->continue_index;
        LogWarn << "Route rejoin found no better forward candidate." << VAR(reason) << VAR(preferred_index) << VAR(selected_continue_index);
        return false;
    }

    return ApplyRejoinCandidate(*selected, reason);
}

bool NavigationStateMachine::HasSevereTurnDrift(double yaw_mismatch, double actual_distance, int64_t stalled_ms) const
{
    return actual_distance > kAdaptiveActivationMinDistance
           && actual_distance + kAdaptiveActivationDistanceSlack >= session_->best_actual_distance()
           && stalled_ms >= kAdaptiveActivationStallMs && yaw_mismatch >= kAdaptiveActivationSevereYawMismatchDegrees;
}

void NavigationStateMachine::ResumeMotionTowardsCurrentWaypoint(double origin_x, double origin_y, const char* reason, int settle_wait_ms)
{
    if (!session_->HasCurrentWaypoint() || session_->CurrentWaypoint().IsControlNode()) {
        return;
    }

    const Waypoint& waypoint = session_->CurrentWaypoint();
    const double target_yaw = NaviMath::CalcTargetRotation(origin_x, origin_y, waypoint.x, waypoint.y);
    const double required_turn = NaviMath::NormalizeAngle(target_yaw - session_->virtual_yaw());

    motion_controller_->InjectMouseAndTrack(required_turn, false, session_->CurrentExpectedZone(), settle_wait_ms);
    motion_controller_->ArmForwardCommit(required_turn, reason);
    motion_controller_->EnsureForwardMotion(true);
}

void NavigationStateMachine::SelectPhaseForCurrentWaypoint(const char* reason)
{
    if (!session_->HasCurrentWaypoint()) {
        session_->UpdatePhase(NaviPhase::Finished, reason);
        return;
    }

    if (session_->is_waiting_for_zone_switch() || session_->CurrentWaypoint().IsZoneDeclaration()) {
        session_->UpdatePhase(NaviPhase::WaitZoneTransition, reason);
        return;
    }

    if (session_->CurrentWaypoint().IsHeadingOnly()) {
        session_->UpdatePhase(NaviPhase::AlignHeading, reason);
        return;
    }

    session_->UpdatePhase(NaviPhase::AdvanceOnRoute, reason);
}

void NavigationStateMachine::SleepFor(int millis) const
{
    if (millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(millis));
}

} // namespace mapnavigator
