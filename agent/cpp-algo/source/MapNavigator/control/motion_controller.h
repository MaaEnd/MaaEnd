#pragma once

#include <chrono>
#include <cstdint>

#include "../Backend/backend.h"
#include "../navi_domain_types.h"

namespace mapnavigator
{

struct Motion
{
    InputBackend* input_backend = nullptr;
    bool enable_local_driver = false;
    bool enable_sprint = false;
    SteeringTransportProfile steering_profile {};
    std::chrono::steady_clock::time_point last_steering_sent_at {};
    std::chrono::steady_clock::time_point steering_quiet_until {};
    LocalDriverAction applied_action = LocalDriverAction::Forward;
    bool has_applied_action = false;
    bool is_moving = false;
    bool is_moving_forward = false;
    bool sprint_active = false;
    bool sprint_suppressed = false;
};

Motion MakeMotion(InputBackend* input_backend);

void HoldPosition(Motion* motion);
void MoveForward(Motion* motion);
void ReassertForward(Motion* motion);
TurnCommandResult ApplySteering(Motion* motion, double yaw_delta_deg, int64_t tick_gap_ms);
void ArmSteeringQuietPeriod(Motion* motion);
bool CanSprint(const Motion& motion);
bool SprintAllowed(const Motion& motion);
void MarkSprintActive(Motion* motion);
bool SprintCancellable(const Motion& motion);
void CancelSprint(Motion* motion);
void ApplyLocalAction(Motion* motion, LocalDriverAction action);

} // namespace mapnavigator
