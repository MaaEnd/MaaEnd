#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
#include "../sensing/sensitivity_observer.h"
#include "action_wrapper.h"

namespace mapnavigator
{

ActionWrapper::ActionWrapper(MaaContext* context)
    : backend_(CreateInputBackend(MaaTaskerGetController(MaaContextGetTasker(context))))
{
}

ActionWrapper::~ActionWrapper() = default;

MaaController* ActionWrapper::getCtrl() const
{
    return backend_->getCtrl();
}

const char* ActionWrapper::controllerType() const
{
    return backend_->controllerType().c_str();
}

bool ActionWrapper::usesTouchBackend() const
{
    return backend_->usesTouchBackend();
}

bool ActionWrapper::isSupported() const
{
    return backend_->isSupported();
}

const char* ActionWrapper::unsupportedReason() const
{
    return backend_->unsupportedReason().c_str();
}

double ActionWrapper::defaultTurnUnitsPerDegree() const
{
    // 灵敏度校正系数只在这里和下面的俯仰处乘
    return backend_->defaultTurnUnitsPerDegree() * sensitivity::TurnUnitsScale();
}

double ActionWrapper::defaultPitchUnitsPerDegree() const
{
    // 游戏视角灵敏度两轴共用, 偏航测出的倍率俯仰照乘
    return backend_->defaultPitchUnitsPerDegree() * sensitivity::TurnUnitsScale();
}

SteeringTransportProfile ActionWrapper::steeringProfile() const
{
    return backend_->steeringProfile();
}

bool ActionWrapper::supportsSprint() const
{
    return backend_->supportsSprint();
}

bool ActionWrapper::supportsWalkToggle() const
{
    return backend_->supportsWalkToggle();
}

void ActionWrapper::setMovementStateSync(MovementState state, int delay_millis)
{
    backend_->setMovementStateSync(state, delay_millis);
}

void ActionWrapper::triggerJumpSync(int hold_millis)
{
    backend_->triggerJumpSync(hold_millis);
}

void ActionWrapper::triggerInteractSync(int hold_millis)
{
    backend_->triggerInteractSync(hold_millis);
}

void ActionWrapper::pulseForwardSync(int hold_millis)
{
    backend_->pulseForwardSync(hold_millis);
}

void ActionWrapper::triggerSprintSync()
{
    backend_->triggerSprintSync();
}

void ActionWrapper::toggleWalkModeSync()
{
    backend_->toggleWalkModeSync();
}

void ActionWrapper::resetForwardWalkSync(int release_millis)
{
    backend_->resetForwardWalkSync(release_millis);
}

void ActionWrapper::clickMouseLeftSync()
{
    backend_->clickMouseLeftSync();
}

void ActionWrapper::triggerZiplineLaunchSync()
{
    backend_->triggerZiplineLaunchSync();
}

void ActionWrapper::triggerZiplineDismountSync(int hold_millis)
{
    backend_->triggerZiplineDismountSync(hold_millis);
}

bool ActionWrapper::sendViewDeltaSync(int dx, int dy)
{
    const bool sent = backend_->sendViewDeltaSync(dx, dy);
    const double units_per_degree = defaultTurnUnitsPerDegree();
    if (sent && dx != 0 && units_per_degree > 0.0) {
        sensitivity::NoteTurnIssued(static_cast<double>(dx) / units_per_degree);
    }
    // 送失败的拖拽也可能已动了视角, 照样记一次
    if (dx != 0) {
        noteHeadingDisturbed();
    }
    return sent;
}

} // namespace mapnavigator
