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

    virtual MaaController* getCtrl() const = 0;
    virtual const std::string& controllerType() const = 0;
    virtual bool usesTouchBackend() const = 0;
    virtual bool isSupported() const = 0;
    virtual const std::string& unsupportedReason() const = 0;
    virtual double defaultTurnUnitsPerDegree() const = 0;

    virtual double defaultPitchUnitsPerDegree() const { return defaultTurnUnitsPerDegree(); }

    virtual SteeringTransportProfile steeringProfile() const = 0;

    virtual bool supportsSprint() const { return true; }

    // 返回 false 时走路请求不生效, 保持跑步速度
    virtual bool supportsWalkToggle() const { return false; }

    virtual void setMovementStateSync(MovementState state, int delay_millis) = 0;
    virtual void triggerJumpSync(int hold_millis) = 0;
    virtual void triggerInteractSync(int hold_millis) = 0;
    virtual void pulseForwardSync(int hold_millis) = 0;
    virtual void triggerSprintSync() = 0;

    virtual void toggleWalkModeSync() {}

    virtual void resetForwardWalkSync(int release_millis) = 0;
    virtual void clickMouseLeftSync() = 0;
    virtual void triggerZiplineLaunchSync() = 0;
    virtual void triggerZiplineDismountSync(int hold_millis) = 0;
    virtual bool sendViewDeltaSync(int dx, int dy) = 0;
};

std::unique_ptr<IInputBackend> CreateInputBackend(MaaController* ctrl);

} // namespace mapnavigator
