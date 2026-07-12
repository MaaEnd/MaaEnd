#pragma once

#include <memory>
#include <string>

#include <MaaFramework/MaaAPI.h>

namespace mapnavigator
{

struct SteeringTransportProfile
{
    bool supports_concurrent_move_and_look = true;
    int min_send_interval_ms = 0;
    double min_emit_delta_deg = 1.0;
    double max_batch_delta_deg = 18.0;
    int action_quiet_period_ms = 0;
};

class IInputBackend
{
public:
    virtual ~IInputBackend() = default;

    virtual MaaController* GetCtrl() const = 0;
    virtual const std::string& controller_type() const = 0;
    virtual bool uses_touch_backend() const = 0;
    virtual bool is_supported() const = 0;
    virtual const std::string& unsupported_reason() const = 0;
    virtual double default_turn_units_per_degree() const = 0;
    virtual SteeringTransportProfile steering_transport_profile() const = 0;

    virtual bool supports_sprint() const { return true; }

    virtual void SetMovementStateSync(bool forward, bool left, bool backward, bool right, int delay_millis) = 0;
    virtual void TriggerJumpSync(int hold_millis) = 0;
    virtual void TriggerInteractSync(int hold_millis) = 0;
    virtual void PulseForwardSync(int hold_millis) = 0;
    virtual void TriggerSprintSync() = 0;
    virtual void ResetForwardWalkSync(int release_millis) = 0;
    virtual void ClickMouseLeftSync() = 0;
    virtual void MouseRightDownSync(int delay_millis) = 0;
    virtual void MouseRightUpSync(int delay_millis) = 0;
    virtual bool SendViewDeltaSync(int dx, int dy) = 0;
};

std::unique_ptr<IInputBackend> CreateInputBackend(MaaController* ctrl);

} // namespace mapnavigator
