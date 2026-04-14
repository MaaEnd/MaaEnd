#pragma once

#include <memory>
#include <string>

#include "../backend.h"

namespace mapnavigator::backend::win32
{

class Win32InputBackend final : public IInputBackend
{
public:
    Win32InputBackend(MaaController* ctrl, std::string controller_type);

    MaaController* GetCtrl() const override;
    const std::string& controller_type() const override;
    bool uses_touch_backend() const override;
    bool is_supported() const override;
    const std::string& unsupported_reason() const override;
    double default_turn_units_per_degree() const override;
    SteeringTransportProfile steering_transport_profile() const override;

    void SetMovementStateSync(bool forward, bool left, bool backward, bool right, int delay_millis) override;
    void TriggerJumpSync(int hold_millis) override;
    void TriggerInteractSync(int hold_millis) override;
    void PulseForwardSync(int hold_millis) override;
    void TriggerSprintSync() override;
    void ResetForwardWalkSync(int release_millis) override;
    void ClickMouseLeftSync() override;
    void MouseRightDownSync(int delay_millis) override;
    void MouseRightUpSync(int delay_millis) override;
    bool SendViewDeltaSync(int dx, int dy) override;

private:
    void PostKeyDownSync(int key_code, int delay_millis);
    void PostKeyUpSync(int key_code, int delay_millis);
    void ClickKeySync(int key_code, int hold_millis);
    void ApplyMovementKeyState(int key_code, bool pressed);
    bool* FindMovementKeyState(int key_code);
    void EnsureHoverAnchorSync();

    MaaController* ctrl_ = nullptr;
    std::string controller_type_;
    std::string unsupported_reason_;
    double default_turn_units_per_degree_ = 0.0;
    int hover_x_ = 0;
    int hover_y_ = 0;
    bool hover_inited_ = false;
    bool forward_down_ = false;
    bool left_down_ = false;
    bool backward_down_ = false;
    bool right_down_ = false;
};

std::unique_ptr<IInputBackend> CreateWin32InputBackend(MaaController* ctrl, std::string controller_type);

} // namespace mapnavigator::backend::win32
