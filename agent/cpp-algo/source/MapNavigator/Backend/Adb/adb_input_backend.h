#pragma once

#include <memory>
#include <string>

#include <MaaUtils/NoWarningCV.hpp>

#include "../backend.h"
#include "adb_camera_swipe_driver.h"
#include "adb_virtual_joystick_driver.h"
#include "adb_zone_guard.h"

namespace maplocator
{

class MapLocator;

} // namespace maplocator

namespace mapnavigator::backend::adb
{

struct AdbTapTarget
{
    cv::Point point {};
    int contact_id = 0;
};

struct AdbActionButtonLayout
{
    AdbTapTarget sprint_button { { 1166, 620 }, 2 };
    AdbTapTarget jump_button { { 1166, 475 }, 3 };
    AdbTapTarget attack_button { { 1030, 551 }, 4 };
    AdbTapTarget interact_button { { 1080, 390 }, 5 };
    int default_hold_ms = 50;
    int post_action_delay_ms = 0;
};

class AdbInputBackend final : public IInputBackend
{
public:
    AdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator);

    MaaController* GetCtrl() const override;
    const std::string& controller_type() const override;
    bool uses_touch_backend() const override;
    bool is_supported() const override;
    const std::string& unsupported_reason() const override;
    double default_turn_units_per_degree() const override;
    SteeringTransportProfile steering_transport_profile() const override;
    bool supports_sprint() const override;

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
    void ApplyMovementState(int delay_millis);
    bool CaptureFrame(cv::Mat* out_image) const;
    bool IsBlindActionAllowed(const char* action_name) const;
    bool ClickBlindTargetSync(const char* action_name, const AdbTapTarget& target, int hold_millis, int delay_millis);
    bool ClickTargetSync(const AdbTapTarget& target, int hold_millis, int delay_millis);
    bool TouchDownTargetSync(const AdbTapTarget& target) const;
    void MouseRightDownOnTargetSync(const AdbTapTarget& target, int delay_millis);
    void MouseRightUpOnTargetSync(int contact_id, int delay_millis);
    bool WaitForControllerAction(MaaCtrlId ctrl_id, const char* action_name) const;
    static void SleepIfNeeded(int delay_millis);

    MaaController* ctrl_ = nullptr;
    std::string controller_type_;
    std::string unsupported_reason_;
    double default_turn_units_per_degree_ = 0.0;
    bool has_locator_ = false;
    AdbCameraSwipeDriver camera_swipe_driver_;
    AdbZoneGuard zone_guard_;
    AdbVirtualJoystickDriver joystick_driver_;
    AdbActionButtonLayout action_buttons_ {};
    bool forward_down_ = false;
    bool left_down_ = false;
    bool backward_down_ = false;
    bool right_down_ = false;
    bool sprint_button_down_ = false;
};

std::unique_ptr<IInputBackend>
    CreateAdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator);

} // namespace mapnavigator::backend::adb
