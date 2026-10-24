#pragma once

#include <chrono>

#include "../Backend/backend.h"
#include "../navi_domain_types.h"

namespace mapnavigator
{

class ActionWrapper;

class MotionController
{
public:
    MotionController(ActionWrapper* action_wrapper, bool enable_local_driver);

    void Stop();
    void SetForwardState(bool forward);
    TurnCommandResult ApplySteering(double yaw_delta_deg, int64_t tick_gap_ms = 0);
    bool TriggerSprint();
    bool SupportsSprint() const;
    void SetSprintSuppressed(bool suppressed);
    void SetAction(LocalDriverAction action, bool force);
    void ReassertForward();

    bool IsMoving() const;
    bool IsMovingForward() const;

    double SteeringBatchCapDeg() const { return steering_profile_.max_batch_delta_deg; }

    bool SteeringDropsTurnSends() const { return steering_profile_.drops_turn_sends; }

private:
    bool SteeringGateOpen(double yaw_delta_deg, std::chrono::steady_clock::time_point now) const;
    int64_t PlanBatchCount(double yaw_delta_deg, int64_t tick_gap_ms) const;
    TurnCommandResult EmitBatches(double yaw_delta_deg, int64_t batches);
    TurnCommandResult SendViewDelta(double delta_degrees);
    void ArmSteeringQuietPeriod();
    void HoldPosition();

    ActionWrapper* action_wrapper_;
    bool enable_local_driver_;
    SteeringTransportProfile steering_profile_ {};
    std::chrono::steady_clock::time_point last_steering_sent_at_ {};
    std::chrono::steady_clock::time_point steering_quiet_until_ {};
    LocalDriverAction applied_action_ = LocalDriverAction::Forward;
    bool has_applied_action_ = false;
    bool is_moving_ = false;
    bool is_moving_forward_ = false;
    bool sprint_active_ = false;
    bool sprint_suppressed_ = false;
};

} // namespace mapnavigator
