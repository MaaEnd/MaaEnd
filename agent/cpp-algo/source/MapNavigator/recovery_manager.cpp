#include "navi_config.h"
#include "recovery_manager.h"

namespace mapnavigator
{

bool RecoveryManager::Step(
    MotionController* motion_controller,
    NavigationSession* session,
    NavigationRuntimeState* runtime_state,
    const PoseEstimate&,
    const RouteTrackingState& route,
    int64_t stalled_ms)
{
    (void)route;
    if (motion_controller == nullptr || session == nullptr || runtime_state == nullptr) {
        return false;
    }

    if (stalled_ms < kObstacleRecoveryMinTriggerMs) {
        return false;
    }

    if (runtime_state->recovery.armed) {
        return false;
    }

    motion_controller->SetForwardState(false);
    motion_controller->SetAction(LocalDriverAction::JumpForward, true);

    runtime_state->recovery.armed = true;
    session->ResetProgress();
    return true;
}

} // namespace mapnavigator
