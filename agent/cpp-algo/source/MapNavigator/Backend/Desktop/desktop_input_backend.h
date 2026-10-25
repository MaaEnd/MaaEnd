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

    MaaController* getCtrl() const override;
    const std::string& controllerType() const override;
    bool usesTouchBackend() const override;
    bool isSupported() const override;
    const std::string& unsupportedReason() const override;
    double defaultTurnUnitsPerDegree() const override;
    double defaultPitchUnitsPerDegree() const override;
    SteeringTransportProfile steeringProfile() const override;

    bool supportsWalkToggle() const override { return true; }

    void setMovementStateSync(MovementState state, int delay_millis) override;
    void triggerJumpSync(int hold_millis) override;
    void triggerInteractSync(int hold_millis) override;
    void pulseForwardSync(int hold_millis) override;
    void triggerSprintSync() override;
    void toggleWalkModeSync() override;
    void resetForwardWalkSync(int release_millis) override;
    void clickMouseLeftSync() override;
    void triggerZiplineLaunchSync() override;
    void triggerZiplineDismountSync(int hold_millis) override;
    bool sendViewDeltaSync(int dx, int dy) override;

private:
    void ensureHoverAnchorSync();
    void mouseRightDownSync(int delay_millis);
    void mouseRightUpSync(int delay_millis);
    void postKeyDownSync(int key_code, int delay_millis);
    void postKeyUpSync(int key_code, int delay_millis);
    void clickKeySync(int key_code, int hold_millis);
    void applyMovementKeyState(int key_code, bool pressed);
    bool* findMovementKeyState(int key_code);

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
