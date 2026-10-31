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
    MotionController(ActionWrapper* action_wrapper, bool enable_local_driver, bool enable_sprint);

    void stop();
    void setForwardState(bool forward);
    TurnCommandResult applySteering(double yaw_delta_deg, int64_t tick_gap_ms = 0);
    bool triggerSprint();
    bool supportsSprint() const;
    void setSprintSuppressed(bool suppressed);
    void setAction(LocalDriverAction action, bool force);
    void reassertForward();

    bool isMoving() const;
    bool isMovingForward() const;

    double steeringBatchCapDeg() const { return steering_profile_.max_batch_delta_deg; }

    bool steeringDropsTurnSends() const { return steering_profile_.drops_turn_sends; }

private:
    bool steeringGateOpen(double yaw_delta_deg, std::chrono::steady_clock::time_point now) const;
    int64_t planBatchCount(double yaw_delta_deg, int64_t tick_gap_ms) const;
    TurnCommandResult emitBatches(double yaw_delta_deg, int64_t batches);
    TurnCommandResult sendViewDelta(double delta_degrees);
    void armSteeringQuietPeriod();
    void holdPosition();

    ActionWrapper* action_wrapper_;
    bool enable_local_driver_;
    bool enable_sprint_;
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
