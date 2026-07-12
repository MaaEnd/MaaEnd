#pragma once

#include <memory>

#include "MaaFramework/MaaAPI.h"

#include "Backend/backend.h"
#include "navi_domain_types.h"

namespace mapnavigator
{

class IInputBackend;

class ActionWrapper
{
public:
    explicit ActionWrapper(MaaContext* context);
    ~ActionWrapper();

    MaaController* GetCtrl() const;
    const char* controller_type() const;
    bool uses_touch_backend() const;
    bool is_supported() const;
    const char* unsupported_reason() const;
    double DefaultTurnUnitsPerDegree() const;
    SteeringTransportProfile SteeringProfile() const;
    bool SupportsSprint() const;

    void SetMovementStateSync(bool forward, bool left, bool backward, bool right, int delay_millis);
    void TriggerJumpSync(int hold_millis);
    void TriggerInteractSync(int hold_millis);
    void PulseForwardSync(int hold_millis);
    void TriggerSprintSync();
    void ResetForwardWalkSync(int release_millis);
    void ClickMouseLeftSync();

    void MouseRightDownSync(int delay_millis);
    void MouseRightUpSync(int delay_millis);

    bool SendViewDeltaSync(int dx, int dy);

private:
    std::unique_ptr<IInputBackend> backend_;
};

} // namespace mapnavigator
