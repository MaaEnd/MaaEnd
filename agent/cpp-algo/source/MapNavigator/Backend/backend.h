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
    bool drops_turn_sends = false;
};

struct MovementState
{
    bool forward = false;
    bool left = false;
    bool backward = false;
    bool right = false;
};

class IInputBackend
{
public:
    virtual ~IInputBackend() = default;

    virtual MaaController* GetCtrl() const = 0;
    virtual const std::string& ControllerType() const = 0;
    virtual bool UsesTouchBackend() const = 0;
    virtual bool IsSupported() const = 0;
    virtual const std::string& UnsupportedReason() const = 0;
    virtual double DefaultTurnUnitsPerDegree() const = 0;

    virtual double DefaultPitchUnitsPerDegree() const { return DefaultTurnUnitsPerDegree(); }

    virtual SteeringTransportProfile SteeringProfile() const = 0;

    virtual bool SupportsSprint() const { return true; }

    // 返回 false 时走路请求不生效, 保持跑步速度
    virtual bool SupportsWalkToggle() const { return false; }

    virtual void SetMovementStateSync(MovementState state, int delay_millis) = 0;
    virtual void TriggerJumpSync(int hold_millis) = 0;
    virtual void TriggerInteractSync(int hold_millis) = 0;
    virtual void PulseForwardSync(int hold_millis) = 0;
    virtual void TriggerSprintSync() = 0;

    virtual void ToggleWalkModeSync() {}

    virtual void ResetForwardWalkSync(int release_millis) = 0;
    virtual void ClickMouseLeftSync() = 0;
    virtual void TriggerZiplineLaunchSync() = 0;
    virtual void TriggerZiplineDismountSync(int hold_millis) = 0;
    virtual bool SendViewDeltaSync(int dx, int dy) = 0;
};

std::unique_ptr<IInputBackend> CreateInputBackend(MaaController* ctrl);

} // namespace mapnavigator
