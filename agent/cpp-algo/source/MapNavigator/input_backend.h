#pragma once

#include <memory>
#include <string>

#include "MaaFramework/MaaAPI.h"

namespace mapnavigator
{

class IInputBackend
{
public:
    virtual ~IInputBackend() = default;

    virtual MaaController* GetCtrl() const = 0;
    virtual const std::string& controller_type() const = 0;
    virtual bool uses_touch_backend() const = 0;
    virtual bool is_supported() const = 0;
    virtual const std::string& unsupported_reason() const = 0;
    virtual double default_turn_units_per_degree() const = 0;

    virtual void KeyDownSync(int key_code, int delay_millis) = 0;
    virtual void KeyUpSync(int key_code, int delay_millis) = 0;
    virtual void ClickKeySync(int key_code, int hold_millis) = 0;
    virtual void TriggerSprintSync() = 0;
    virtual void ResetForwardWalkSync(int release_millis) = 0;
    virtual void ClickMouseLeftSync() = 0;
    virtual void MouseRightDownSync(int delay_millis) = 0;
    virtual void MouseRightUpSync(int delay_millis) = 0;
    virtual void SendRelativeMoveSync(int dx, int dy) = 0;
};

std::unique_ptr<IInputBackend> CreateInputBackend(MaaContext* context, MaaController* ctrl);

} // namespace mapnavigator
