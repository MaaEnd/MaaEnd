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

    MaaController* getCtrl() const;
    const char* controllerType() const;
    bool usesTouchBackend() const;
    bool isSupported() const;
    const char* unsupportedReason() const;
    double defaultTurnUnitsPerDegree() const;
    double defaultPitchUnitsPerDegree() const;
    SteeringTransportProfile steeringProfile() const;
    bool supportsSprint() const;
    bool supportsWalkToggle() const;

    void setMovementStateSync(MovementState state, int delay_millis);
    void triggerJumpSync(int hold_millis);
    void triggerInteractSync(int hold_millis);
    void pulseForwardSync(int hold_millis);
    void triggerSprintSync();
    void toggleWalkModeSync();
    void resetForwardWalkSync(int release_millis);
    void clickMouseLeftSync();

    void triggerZiplineLaunchSync();
    void triggerZiplineDismountSync(int hold_millis);

    bool sendViewDeltaSync(int dx, int dy);

    // 每次可能动过镜头就加一, 取位时记下的值仍等于当前值, 那次的镜头朝向才可当参考
    uint64_t headingEpoch() const { return heading_epoch_; }

    void noteHeadingDisturbed() { ++heading_epoch_; }

private:
    std::unique_ptr<IInputBackend> backend_;
    uint64_t heading_epoch_ = 0;
};

} // namespace mapnavigator
