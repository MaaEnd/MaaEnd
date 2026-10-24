#pragma once

#include <cstdint>
#include <memory>

#include "MaaFramework/MaaAPI.h"

#include "../Backend/backend.h"
#include "../navi_domain_types.h"

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
    double DefaultPitchUnitsPerDegree() const;
    SteeringTransportProfile SteeringProfile() const;
    bool SupportsSprint() const;
    bool SupportsWalkToggle() const;

    void SetMovementStateSync(bool forward, bool left, bool backward, bool right, int delay_millis);
    void TriggerJumpSync(int hold_millis);
    void TriggerInteractSync(int hold_millis);
    void PulseForwardSync(int hold_millis);
    void TriggerSprintSync();
    void ToggleWalkModeSync();
    void ResetForwardWalkSync(int release_millis);
    void ClickMouseLeftSync();

    void MouseRightDownSync(int delay_millis);
    void MouseRightUpSync(int delay_millis);

    void TriggerZiplineLaunchSync();
    void TriggerZiplineDismountSync(int hold_millis);

    bool SendViewDeltaSync(int dx, int dy);

    // 每次可能动过镜头就加一, 取位时记下的值仍等于当前值, 那次的镜头朝向才可当参考
    uint64_t heading_epoch() const { return heading_epoch_; }

    void NoteHeadingDisturbed() { ++heading_epoch_; }

private:
    std::unique_ptr<IInputBackend> backend_;
    uint64_t heading_epoch_ = 0;
};

} // namespace mapnavigator
