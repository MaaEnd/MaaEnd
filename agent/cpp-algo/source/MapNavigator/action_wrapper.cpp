#include <MaaUtils/Logger.h>

#include "Backend/backend.h"
#include "action_wrapper.h"
#include "sensitivity_observer.h"

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
    return backend_->controller_type().c_str();
}

bool ActionWrapper::uses_touch_backend() const
{
    return backend_->uses_touch_backend();
}

bool ActionWrapper::is_supported() const
{
    return backend_->is_supported();
}

const char* ActionWrapper::unsupported_reason() const
{
    return backend_->unsupported_reason().c_str();
}

double ActionWrapper::DefaultTurnUnitsPerDegree() const
{
    // 偏航度→单位只从这里过，校正系数只乘这一处。
    return backend_->default_turn_units_per_degree() * sensitivity::TurnUnitsScale();
}

double ActionWrapper::DefaultPitchUnitsPerDegree() const
{
    return backend_->default_pitch_units_per_degree();
}

SteeringTransportProfile ActionWrapper::SteeringProfile() const
{
    return backend_->steering_transport_profile();
}

bool ActionWrapper::SupportsSprint() const
{
    return backend_->supports_sprint();
}

bool ActionWrapper::SupportsWalkToggle() const
{
    return backend_->supports_walk_toggle();
}

void ActionWrapper::SetMovementStateSync(bool forward, bool left, bool backward, bool right, int delay_millis)
{
    backend_->SetMovementStateSync(forward, left, backward, right, delay_millis);
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

void ActionWrapper::MouseRightDownSync(int delay_millis)
{
    backend_->MouseRightDownSync(delay_millis);
}

void ActionWrapper::MouseRightUpSync(int delay_millis)
{
    backend_->MouseRightUpSync(delay_millis);
}

bool ActionWrapper::SendViewDeltaSync(int dx, int dy)
{
    const bool sent = backend_->SendViewDeltaSync(dx, dy);
    // 所有偏航输入都从这里出去，按当下的度→单位系数折回度数报给灵敏度估计器。
    const double units_per_degree = DefaultTurnUnitsPerDegree();
    if (sent && dx != 0 && units_per_degree > 0.0) {
        sensitivity::NoteTurnIssued(static_cast<double>(dx) / units_per_degree);
    }
    return sent;
}

} // namespace mapnavigator
