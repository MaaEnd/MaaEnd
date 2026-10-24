#pragma once

#include <memory>
#include <string>

#include "../backend.h"

namespace mapnavigator::backend::desktop
{

struct DesktopKeyCodes
{
    int32_t move_forward = 0;
    int32_t move_left = 0;
    int32_t move_backward = 0;
    int32_t move_right = 0;
    int32_t interact = 0;
    int32_t jump = 0;
    int32_t walk_toggle = 0;
};

class DesktopInputBackend : public IInputBackend
{
public:
    DesktopInputBackend(MaaController* ctrl, std::string controller_type, std::string backend_name, DesktopKeyCodes key_codes);
    ~DesktopInputBackend() override;

    MaaController* GetCtrl() const override;
    const std::string& ControllerType() const override;
    bool UsesTouchBackend() const override;
    bool IsSupported() const override;
    const std::string& UnsupportedReason() const override;
    double DefaultTurnUnitsPerDegree() const override;
    double DefaultPitchUnitsPerDegree() const override;
    SteeringTransportProfile SteeringProfile() const override;

    bool SupportsWalkToggle() const override { return true; }

    void SetMovementStateSync(MovementState state, int delay_millis) override;
    void TriggerJumpSync(int hold_millis) override;
    void TriggerInteractSync(int hold_millis) override;
    void PulseForwardSync(int hold_millis) override;
    void TriggerSprintSync() override;
    void ToggleWalkModeSync() override;
    void ResetForwardWalkSync(int release_millis) override;
    void ClickMouseLeftSync() override;
    void TriggerZiplineLaunchSync() override;
    void TriggerZiplineDismountSync(int hold_millis) override;
    bool SendViewDeltaSync(int dx, int dy) override;

private:
    void EnsureHoverAnchorSync();
    void MouseRightDownSync(int delay_millis);
    void MouseRightUpSync(int delay_millis);
    void PostKeyDownSync(int key_code, int delay_millis);
    void PostKeyUpSync(int key_code, int delay_millis);
    void ClickKeySync(int key_code, int hold_millis);
    void ApplyMovementKeyState(int key_code, bool pressed);
    bool* FindMovementKeyState(int key_code);

    MaaController* ctrl_ = nullptr;
    std::string controller_type_;
    std::string backend_name_;
    std::string unsupported_reason_;
    DesktopKeyCodes key_codes_ {};
    double default_turn_units_per_degree_ = 0.0;
    double default_pitch_units_per_degree_ = 0.0;
    bool mouse_lock_follow_enabled_ = false;
    bool background_managed_keys_enabled_ = false;
    int hover_x_ = 0;
    int hover_y_ = 0;
    bool hover_inited_ = false;
    bool forward_down_ = false;
    bool left_down_ = false;
    bool backward_down_ = false;
    bool right_down_ = false;
};

DesktopKeyCodes MakeDesktopKeyCodes();

std::unique_ptr<IInputBackend> CreateDesktopInputBackend(MaaController* ctrl, std::string controller_type, std::string backend_name);

} // namespace mapnavigator::backend::desktop
