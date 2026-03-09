#pragma once

#include <memory>

#include "MaaFramework/MaaAPI.h"

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

    void KeyDownSync(int key_code, int delay_millis);
    void KeyUpSync(int key_code, int delay_millis);
    void ClickKeySync(int key_code, int hold_millis);
    void TriggerSprintSync();
    void ResetForwardWalkSync(int release_millis);
    void ClickMouseLeftSync();

    void MouseRightDownSync(int delay_millis);
    void MouseRightUpSync(int delay_millis);

    void SendRelativeMoveNative(int dx, int dy);

private:
    std::unique_ptr<IInputBackend> backend_;
};

class NativeMouseTurnActuator : public ITurnActuator
{
public:
    explicit NativeMouseTurnActuator(ActionWrapper& action_wrapper);
    TurnActuationResult TurnByUnits(int units, int duration_millis) override;

private:
    ActionWrapper& action_wrapper_;
};

} // namespace mapnavigator
