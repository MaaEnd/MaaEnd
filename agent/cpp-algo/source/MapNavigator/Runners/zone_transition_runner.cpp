#include <algorithm>
#include <chrono>
#include <limits>
#include <thread>

#include <MaaUtils/Logger.h>

#include "../motion_controller.h"
#include "../navi_config.h"
#include "../navigation_session.h"
#include "../position_provider.h"
#include "common/runner_phase_utils.h"
#include "zone_transition_runner.h"

namespace mapnavigator
{

namespace
{

struct ImplicitZoneTransitionAssessment
{
    bool valid_transition = false;
    size_t portal_index = std::numeric_limits<size_t>::max();
};

bool ShouldProbePortalTransition(const NavigationSession& session, bool waiting_for_zone_switch, bool require_nearby_portal)
{
    bool has_portal_transition_ahead = false;
    bool has_nearby_portal = waiting_for_zone_switch;
    const size_t current_node_idx = session.current_node_idx();
    const size_t nearby_end_idx = std::min(current_node_idx + 1, session.current_path().size() - 1);
    for (size_t index = current_node_idx; index < session.current_path().size(); ++index) {
        if (session.CurrentPathAt(index).action != ActionType::PORTAL) {
            continue;
        }
        if (index + 1 >= session.current_path().size() || !session.CurrentPathAt(index + 1).IsZoneDeclaration()) {
            continue;
        }
        has_portal_transition_ahead = true;
        if (index <= nearby_end_idx) {
            has_nearby_portal = true;
        }
        if (has_nearby_portal && require_nearby_portal) {
            break;
        }
    }
    return has_portal_transition_ahead && (!require_nearby_portal || has_nearby_portal);
}

bool IsValidPortalProbeCandidate(const NaviPosition& candidate_position, const std::string& current_zone_id)
{
    return candidate_position.valid && !candidate_position.zone_id.empty() && candidate_position.zone_id != current_zone_id;
}

ImplicitZoneTransitionAssessment AssessImplicitZoneTransition(const NavigationSession& session, const NaviPosition& position)
{
    if (position.zone_id.empty() || position.zone_id == session.current_zone_id()) {
        return {};
    }
    for (size_t index = session.current_node_idx(); index <= std::min(session.current_node_idx() + 1, session.current_path().size() - 1);
         ++index) {
        if (session.CurrentPathAt(index).action != ActionType::PORTAL) {
            continue;
        }
        const size_t landing_zone_idx = index + 1;
        if (landing_zone_idx >= session.current_path().size() || !session.CurrentPathAt(landing_zone_idx).IsZoneDeclaration()) {
            continue;
        }
        const std::string& landing_zone_id = session.CurrentPathAt(landing_zone_idx).zone_id;
        if (!landing_zone_id.empty() && landing_zone_id != position.zone_id) {
            continue;
        }
        return {
            .valid_transition = true,
            .portal_index = index,
        };
    }
    return {};
}

} // namespace

ZoneTransitionRunner::ZoneTransitionRunner(
    PositionProvider* position_provider,
    NavigationSession* session,
    MotionController* motion_controller,
    NaviPosition* position,
    NavigationRuntimeState* runtime_state,
    std::function<bool()> should_stop)
    : position_provider_(position_provider)
    , session_(session)
    , motion_controller_(motion_controller)
    , position_(position)
    , runtime_state_(runtime_state)
    , should_stop_(std::move(should_stop))
{
}

bool ZoneTransitionRunner::Tick()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        LogInfo << "Zone declaration consumed at current route tail.";
        return true;
    }

    const bool waiting_for_zone_switch = session_->is_waiting_for_zone_switch();
    if (session_->CurrentWaypoint().IsZoneDeclaration()) {
        const bool landing_from_portal = waiting_for_zone_switch;
        ConsumeZoneNodes(landing_from_portal);
        if (landing_from_portal) {
            session_->SetWaitingForZoneSwitch(false);
            ConsumeLandingPortalNode();
        }

        if (!session_->HasCurrentWaypoint()) {
            session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
            LogInfo << "Zone declaration consumed at current route tail.";
            return true;
        }

        if (landing_from_portal) {
            StopMotionAndCommitment(motion_controller_);
            session_->ResetDriverProgressTracking();
            runtime_state_->post_zone_transition_reacquire_pending_ = true;
            if (session_->phase() == NaviPhase::AdvanceOnRoute) {
                const size_t current_node_idx = session_->current_node_idx();
                const std::string& zone_id = position_->zone_id;
                LogInfo << "Landing zone confirmed; holding motion until a fresh capture in the new zone." << VAR(current_node_idx)
                        << VAR(zone_id);
            }
        }

        SelectPhaseForCurrentWaypoint(session_, position_, "zone_nodes_consumed");
        return true;
    }

    if (!waiting_for_zone_switch) {
        SelectPhaseForCurrentWaypoint(session_, position_, "zone_phase_exit");
        return true;
    }

    if (TryFastPortalZoneTransition("wait_zone_transition_probe")) {
        return true;
    }

    if (!position_provider_->Capture(position_, false, session_->CurrentExpectedZone())) {
        SleepFor(kLocatorRetryIntervalMs);
        return true;
    }

    const std::string expected_zone_id = session_->CurrentExpectedZone();
    if (expected_zone_id.empty() && position_->zone_id != session_->current_zone_id()) {
        HandleImplicitZoneTransition(expected_zone_id);
        return true;
    }
    if (session_->is_waiting_for_zone_switch()) {
        SleepFor(kLocatorRetryIntervalMs);
        return true;
    }

    SelectPhaseForCurrentWaypoint(session_, position_, "zone_switch_complete");
    return true;
}

bool ZoneTransitionRunner::TryFastPortalZoneTransition(const char* reason, bool require_nearby_portal)
{
    if (!session_->HasCurrentWaypoint()) {
        return false;
    }
    if (!ShouldProbePortalTransition(*session_, session_->is_waiting_for_zone_switch(), require_nearby_portal)) {
        return false;
    }

    NaviPosition candidate_pos;
    if (!position_provider_->Capture(&candidate_pos, true, {})) {
        return false;
    }
    if (!IsValidPortalProbeCandidate(candidate_pos, session_->current_zone_id())) {
        return false;
    }

    *position_ = candidate_pos;
    if (HandleImplicitZoneTransition({})) {
        const std::string& candidate_zone_id = candidate_pos.zone_id;
        LogInfo << "Fast portal zone probe accepted zone switch." << VAR(reason) << VAR(candidate_zone_id);
        return true;
    }

    const std::string& candidate_zone_id = candidate_pos.zone_id;
    const std::string& current_zone_id = session_->current_zone_id();
    LogInfo << "Fast portal zone probe observed unmatched zone switch; pausing old motion for reprobe." << VAR(reason)
            << VAR(current_zone_id) << VAR(candidate_zone_id);
    StopMotionAndCommitment(motion_controller_);
    position_provider_->ResetTracking();
    session_->ResetDriverProgressTracking();
    SleepFor(kLocatorRetryIntervalMs);
    return true;
}

bool ZoneTransitionRunner::HandleImplicitZoneTransition(const std::string& expected_zone_id)
{
    const ImplicitZoneTransitionAssessment transition_assessment = AssessImplicitZoneTransition(*session_, *position_);
    if (!transition_assessment.valid_transition) {
        return false;
    }

    const size_t portal_index = transition_assessment.portal_index;
    const size_t landing_zone_idx = portal_index + 1;
    const std::string& landing_zone_id = session_->CurrentPathAt(landing_zone_idx).zone_id;

    session_->NoteCanonicalFinalGoalConsumed(session_->path_origin_index() + portal_index, *position_, "portal_zone_transition");
    session_->SkipPastWaypoint(portal_index, "portal_zone_transition");
    session_->ConfirmZone(position_->zone_id, *position_, "portal_zone_transition");
    session_->SetWaitingForZoneSwitch(false);
    while (session_->HasCurrentWaypoint() && session_->CurrentWaypoint().IsZoneDeclaration()) {
        const std::string& zone_id = session_->CurrentWaypoint().zone_id;
        if (!zone_id.empty() && zone_id != position_->zone_id) {
            break;
        }
        session_->AdvanceToNextWaypoint(ActionType::ZONE, "portal_zone_transition_confirmed");
    }
    ConsumeLandingPortalNode();
    session_->ResetDriverProgressTracking();

    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        LogInfo << "Portal transition consumed the current route tail; final success will use stable session evidence.";
        return true;
    }

    StopMotionAndCommitment(motion_controller_);
    runtime_state_->post_zone_transition_reacquire_pending_ = true;
    SelectPhaseForCurrentWaypoint(session_, position_, "portal_transition_complete");

    if (session_->phase() == NaviPhase::AdvanceOnRoute) {
        const size_t current_node_idx = session_->current_node_idx();
        const std::string& zone_id = position_->zone_id;
        (void)expected_zone_id;
        (void)landing_zone_id;
        LogInfo << "Portal zone switch accepted; holding motion until a fresh capture in the new zone." << VAR(current_node_idx)
                << VAR(zone_id);
    }
    return true;
}

bool ZoneTransitionRunner::ConsumeZoneNodes(bool keep_moving_until_first_fix)
{
    bool consumed = false;
    while (session_->HasCurrentWaypoint() && session_->CurrentWaypoint().IsZoneDeclaration()) {
        const std::string expected_zone_id = session_->CurrentWaypoint().zone_id;
        if (!WaitForExpectedZone(expected_zone_id, keep_moving_until_first_fix)) {
            const size_t current_node_idx = session_->current_node_idx();
            LogWarn << "Skip strict zone gate for this declaration." << VAR(expected_zone_id) << VAR(current_node_idx);
        }
        keep_moving_until_first_fix = false;
        session_->AdvanceToNextWaypoint(ActionType::ZONE, "zone_declaration_consumed");
        consumed = true;
    }
    if (consumed) {
        session_->ResetDriverProgressTracking();
    }
    return consumed || session_->HasCurrentWaypoint();
}

bool ZoneTransitionRunner::ConsumeLandingPortalNode()
{
    if (!session_->HasCurrentWaypoint() || !session_->CurrentWaypoint().HasPosition()
        || session_->CurrentWaypoint().action != ActionType::PORTAL) {
        return false;
    }

    const size_t current_node_idx = session_->current_node_idx();
    LogInfo << "Skip landing PORTAL waypoint after zone transition." << VAR(current_node_idx);
    session_->NoteCanonicalFinalGoalConsumed(session_->CurrentAbsoluteNodeIndex(), *position_, "landing_portal_consumed");
    session_->AdvanceToNextWaypoint(ActionType::PORTAL, "landing_portal_consumed");
    session_->ResetStraightStableFrames();
    session_->ResetDriverProgressTracking();
    return true;
}

bool ZoneTransitionRunner::WaitForExpectedZone(const std::string& expected_zone_id, bool keep_moving_until_first_fix)
{
    if (expected_zone_id.empty()) {
        return true;
    }

    LogInfo << "Waiting for expected zone." << VAR(expected_zone_id) << VAR(keep_moving_until_first_fix);
    position_provider_->ResetTracking();

    int stable_hits = 0;
    bool first_fix_seen = false;
    const auto wait_start = std::chrono::steady_clock::now();
    auto last_blind_recovery_time = wait_start;
    int blind_recovery_attempts = 0;
    bool blind_strafe_left = false;

    while (!should_stop_()) {
        NaviPosition candidate_pos;
        const bool force_global_search = !first_fix_seen;
        const bool updated = position_provider_->Capture(&candidate_pos, force_global_search, expected_zone_id);
        if (!updated || candidate_pos.zone_id != expected_zone_id) {
            stable_hits = 0;
        }
        else {
            *position_ = candidate_pos;
            first_fix_seen = true;
            const bool held_fix = position_provider_->LastCaptureWasHeld();
            if (held_fix && position_provider_->HeldFixStreak() < kZoneConfirmStableFrames) {
                const double candidate_x = candidate_pos.x;
                const double candidate_y = candidate_pos.y;
                const int held_fix_streak = position_provider_->HeldFixStreak();
                stable_hits = 0;
                LogInfo << "Ignore held locator fix while confirming zone." << VAR(expected_zone_id) << VAR(candidate_x)
                        << VAR(candidate_y) << VAR(held_fix_streak);
                SleepFor(kZoneConfirmRetryIntervalMs);
                continue;
            }

            if (keep_moving_until_first_fix && motion_controller_->IsMoving()) {
                motion_controller_->Stop();
                SleepFor(kStopWaitMs);
                position_provider_->ResetTracking();
                keep_moving_until_first_fix = false;
                stable_hits = 0;
                continue;
            }

            ++stable_hits;
            if (stable_hits >= kZoneConfirmStableFrames) {
                session_->ConfirmZone(expected_zone_id, *position_, "zone_confirmed");
                const double position_x = position_->x;
                const double position_y = position_->y;
                LogInfo << "Zone confirmed." << VAR(expected_zone_id) << VAR(position_x) << VAR(position_y);
                return true;
            }
        }

        const auto now = std::chrono::steady_clock::now();
        const auto waited_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - wait_start).count();
        if (keep_moving_until_first_fix && motion_controller_->IsMoving() && waited_ms >= kZoneBlindRecoveryStartMs
            && std::chrono::duration_cast<std::chrono::milliseconds>(now - last_blind_recovery_time).count()
                   >= kZoneBlindRecoveryIntervalMs) {
            const int blind_recovery_attempt = blind_recovery_attempts + 1;
            const bool use_strafe_pulse = blind_recovery_attempt % 2 == 0;
            const char* blind_action_name = use_strafe_pulse ? (blind_strafe_left ? "ForwardLeft" : "ForwardRight") : "JumpForward";

            if (use_strafe_pulse) {
                const LocalDriverAction blind_action = blind_strafe_left ? LocalDriverAction::ForwardLeft : LocalDriverAction::ForwardRight;
                motion_controller_->SetAction(blind_action, true);
                SleepFor(kZoneBlindStrafePulseMs);
                motion_controller_->SetAction(LocalDriverAction::Forward, true);
                blind_strafe_left = !blind_strafe_left;
            }
            else {
                motion_controller_->SetAction(LocalDriverAction::JumpForward, true);
            }

            ++blind_recovery_attempts;
            last_blind_recovery_time = std::chrono::steady_clock::now();
            LogWarn << "Zone blind-walk recovery triggered." << VAR(expected_zone_id) << VAR(blind_recovery_attempt)
                    << VAR(blind_action_name);
        }

        if (waited_ms > kZoneConfirmTimeoutMs) {
            LogWarn << "Zone confirm timeout, continue without strict validation." << VAR(expected_zone_id);
            return false;
        }

        SleepFor(kZoneConfirmRetryIntervalMs);
    }

    return false;
}

void ZoneTransitionRunner::SleepFor(int millis) const
{
    if (millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(millis));
}

} // namespace mapnavigator
