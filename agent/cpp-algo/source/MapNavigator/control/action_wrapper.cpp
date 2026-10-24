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

MaaController* ActionWrapper::GetCtrl() const
{
    return backend_->GetCtrl();
}

const char* ActionWrapper::controller_type() const
{
    return backend_->ControllerType().c_str();
}

bool ActionWrapper::uses_touch_backend() const
{
    return backend_->UsesTouchBackend();
}

bool ActionWrapper::is_supported() const
{
    return backend_->IsSupported();
}

const char* ActionWrapper::unsupported_reason() const
{
    return backend_->UnsupportedReason().c_str();
}

double ActionWrapper::DefaultTurnUnitsPerDegree() const
{
    // 灵敏度校正系数只在这里和下面的俯仰处乘
    return backend_->DefaultTurnUnitsPerDegree() * sensitivity::TurnUnitsScale();
}

double ActionWrapper::DefaultPitchUnitsPerDegree() const
{
    // 游戏视角灵敏度两轴共用, 偏航测出的倍率俯仰照乘
    return backend_->DefaultPitchUnitsPerDegree() * sensitivity::TurnUnitsScale();
}

SteeringTransportProfile ActionWrapper::SteeringProfile() const
{
    return backend_->SteeringProfile();
}

bool ActionWrapper::SupportsSprint() const
{
    return backend_->SupportsSprint();
}

bool ActionWrapper::SupportsWalkToggle() const
{
    return backend_->SupportsWalkToggle();
}

void ActionWrapper::SetMovementStateSync(MovementState state, int delay_millis)
{
    backend_->SetMovementStateSync(state, delay_millis);
}

void ActionWrapper::TriggerJumpSync(int hold_millis)
{
    backend_->TriggerJumpSync(hold_millis);
}

void ActionWrapper::TriggerInteractSync(int hold_millis)
{
    backend_->TriggerInteractSync(hold_millis);
}

void ActionWrapper::PulseForwardSync(int hold_millis)
{
    backend_->PulseForwardSync(hold_millis);
}

void ActionWrapper::TriggerSprintSync()
{
    backend_->TriggerSprintSync();
}

void ActionWrapper::ToggleWalkModeSync()
{
    backend_->ToggleWalkModeSync();
}

void ActionWrapper::ResetForwardWalkSync(int release_millis)
{
    backend_->ResetForwardWalkSync(release_millis);
}

void ActionWrapper::ClickMouseLeftSync()
{
    backend_->ClickMouseLeftSync();
}

void ActionWrapper::TriggerZiplineLaunchSync()
{
    backend_->TriggerZiplineLaunchSync();
}

void ActionWrapper::TriggerZiplineDismountSync(int hold_millis)
{
    backend_->TriggerZiplineDismountSync(hold_millis);
}

bool ActionWrapper::SendViewDeltaSync(int dx, int dy)
{
    const bool sent = backend_->SendViewDeltaSync(dx, dy);
    const double units_per_degree = DefaultTurnUnitsPerDegree();
    if (sent && dx != 0 && units_per_degree > 0.0) {
        sensitivity::NoteTurnIssued(static_cast<double>(dx) / units_per_degree);
    }
    // 送失败的拖拽也可能已动了视角, 照样记一次
    if (dx != 0) {
        NoteHeadingDisturbed();
    }
    return sent;
}

} // namespace mapnavigator
