#include <cmath>

#include "navi_config.h"
#include "recovery_manager.h"
#include <MaaUtils/Logger.h>

namespace mapnavigator
{

RecoveryStatus RecoveryManager::Tick(
    MotionController* motion_controller,
    NavigationSession* session,
    NavigationRuntimeState* runtime_state,
    const NaviPosition& position,
    const RouteTrackingState& route,
    int64_t stalled_ms)
{
    (void)session;
    (void)route;

    auto& state = runtime_state->recovery;
    const auto now = std::chrono::steady_clock::now();

    if (!state.IsActive()) {
        if (stalled_ms < kObstacleRecoveryMinTriggerMs) {
            return RecoveryStatus::NotTriggered;
        }
        state.stuck_start_time = now;
        state.stuck_anchor_pos = position;
        LogInfo << "Detected stuck. RecoveryManager taking over.";
        return RecoveryStatus::InProgress;
    }

    const double escape_dist = std::hypot(
        position.x - state.stuck_anchor_pos.x,
        position.y - state.stuck_anchor_pos.y);

    if (escape_dist > 2.0) {
        state.Reset();
        return RecoveryStatus::Recovered;
    }

    const int64_t total_stuck_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        now - state.stuck_start_time).count();

    if (total_stuck_ms >= 60000) {
        return RecoveryStatus::TimeoutFailed;
    }

    if (total_stuck_ms > 10000) {
        motion_controller->SetForwardState(false);
        LogInfo << "Jump recovery ineffective after 10s. Requesting rejoin from previous waypoint."
                << VAR(total_stuck_ms) << VAR(escape_dist);
        return RecoveryStatus::RequestRejoin;
    }

    if (now > state.next_action_time) {
        motion_controller->SetForwardState(false);
        motion_controller->SetAction(LocalDriverAction::JumpForward, true);
        state.next_action_time = now + std::chrono::milliseconds(1500);
    }

    return RecoveryStatus::InProgress;
}

} // namespace mapnavigator
