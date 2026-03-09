#include <chrono>
#include <thread>

#include <MaaUtils/Logger.h>

#include "local_driver_lite.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navigation_session.h"
#include "position_provider.h"
#include "zone_transition_controller.h"

namespace mapnavigator
{

ZoneTransitionController::ZoneTransitionController(
    MotionController* motion_controller,
    PositionProvider* position_provider,
    NavigationSession* session,
    NaviPosition* position,
    LocalDriverLite* local_driver,
    std::function<bool()> should_stop)
    : motion_controller_(motion_controller)
    , position_provider_(position_provider)
    , session_(session)
    , position_(position)
    , local_driver_(local_driver)
    , should_stop_(std::move(should_stop))
{
}

bool ZoneTransitionController::ConsumeZoneNodes(bool keep_moving_until_first_fix)
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
        session_->ResetDriverProgressTracking(local_driver_);
    }
    return consumed || session_->HasCurrentWaypoint();
}

bool ZoneTransitionController::ConsumeLandingPortalNode()
{
    if (!session_->HasCurrentWaypoint() || !session_->CurrentWaypoint().HasPosition()
        || session_->CurrentWaypoint().action != ActionType::PORTAL) {
        return false;
    }

    const size_t current_node_idx = session_->current_node_idx();
    LogInfo << "Skip landing PORTAL waypoint after zone transition." << VAR(current_node_idx);
    session_->AdvanceToNextWaypoint(ActionType::PORTAL, "landing_portal_consumed");
    session_->ResetStraightStableFrames();
    session_->ResetDriverProgressTracking(local_driver_);
    return true;
}

bool ZoneTransitionController::WaitForExpectedZone(const std::string& expected_zone_id, bool keep_moving_until_first_fix)
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
                LogInfo << "Ignore held locator fix while confirming zone." << VAR(expected_zone_id) << VAR(candidate_x) << VAR(candidate_y)
                        << VAR(held_fix_streak);
                std::this_thread::sleep_for(std::chrono::milliseconds(kZoneConfirmRetryIntervalMs));
                continue;
            }

            if (held_fix) {
                const double candidate_x = candidate_pos.x;
                const double candidate_y = candidate_pos.y;
                const int held_fix_streak = position_provider_->HeldFixStreak();
                LogInfo << "Accept held locator fix for zone confirmation." << VAR(expected_zone_id) << VAR(candidate_x) << VAR(candidate_y)
                        << VAR(held_fix_streak);
            }

            if (keep_moving_until_first_fix && motion_controller_->IsMoving()) {
                motion_controller_->Stop();
                std::this_thread::sleep_for(std::chrono::milliseconds(kStopWaitMs));
                position_provider_->ResetTracking();
                keep_moving_until_first_fix = false;
                stable_hits = 0;
                continue;
            }

            stable_hits++;
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
                std::this_thread::sleep_for(std::chrono::milliseconds(kZoneBlindStrafePulseMs));
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

        std::this_thread::sleep_for(std::chrono::milliseconds(kZoneConfirmRetryIntervalMs));
    }

    return false;
}

} // namespace mapnavigator
