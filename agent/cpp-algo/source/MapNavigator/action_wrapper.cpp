#include <MaaUtils/Logger.h>

#include "Backend/backend.h"
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
    return backend_->default_turn_units_per_degree();
}

SteeringTransportProfile ActionWrapper::SteeringProfile() const
{
    return backend_->steering_transport_profile();
}

bool ActionWrapper::SupportsSprint() const
{
    return backend_->supports_sprint();
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
    return backend_->SendViewDeltaSync(dx, dy);
}

} // namespace mapnavigator
