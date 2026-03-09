#include <MaaUtils/Logger.h>

#include "action_wrapper.h"
#include "input_backend.h"

namespace mapnavigator
{

ActionWrapper::ActionWrapper(MaaContext* context)
    : backend_(CreateInputBackend(context, MaaTaskerGetController(MaaContextGetTasker(context))))
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

void ActionWrapper::KeyDownSync(int key_code, int delay_millis)
{
    backend_->KeyDownSync(key_code, delay_millis);
}

void ActionWrapper::KeyUpSync(int key_code, int delay_millis)
{
    backend_->KeyUpSync(key_code, delay_millis);
}

void ActionWrapper::ClickKeySync(int key_code, int hold_millis)
{
    backend_->ClickKeySync(key_code, hold_millis);
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

void ActionWrapper::SendRelativeMoveNative(int dx, int dy)
{
    backend_->SendRelativeMoveSync(dx, dy);
}

NativeMouseTurnActuator::NativeMouseTurnActuator(ActionWrapper& action_wrapper)
    : action_wrapper_(action_wrapper)
{
}

TurnActuationResult NativeMouseTurnActuator::TurnByUnits(int units, int duration_millis)
{
    (void)duration_millis;
    action_wrapper_.SendRelativeMoveNative(units, 0);
    return { units };
}

} // namespace mapnavigator
