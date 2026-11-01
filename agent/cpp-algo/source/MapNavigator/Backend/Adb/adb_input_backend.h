#pragma once

#include <memory>
#include <string>

#include <MaaUtils/NoWarningCV.hpp>

#include "../backend.h"
#include "adb_camera_swipe_driver.h"
#include "adb_virtual_joystick_driver.h"

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
    AdbTapTarget zipline_launch_button { { 1099, 526 }, 6 };
    AdbTapTarget zipline_dismount_button { { 1166, 411 }, 7 };
    int default_hold_ms = 50;
    int post_action_delay_ms = 0;
};

class AdbInputBackend final : public IInputBackend
{
public:
    AdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator);

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

    void toggleWalkModeSync() override { joystick_driver_.setWalking(!joystick_driver_.walking()); }

    void resetForwardWalkSync(int release_millis) override;
    void clickMouseLeftSync() override;
    void triggerZiplineLaunchSync() override;
    void triggerZiplineDismountSync(int hold_millis) override;
    bool sendViewDeltaSync(int dx, int dy) override;

private:
    void applyMovementState(MovementState state, int delay_millis);
    bool clickTargetSync(const AdbTapTarget& target, int hold_millis, int delay_millis);
    bool touchDownTargetSync(const AdbTapTarget& target) const;
    bool waitForControllerAction(MaaCtrlId ctrl_id, const char* action_name) const;

    MaaController* ctrl_ = nullptr;
    std::string controller_type_;
    std::string unsupported_reason_;
    double default_turn_units_per_degree_ = 0.0;
    bool has_locator_ = false;
    AdbCameraSwipeDriver camera_swipe_driver_;
    AdbVirtualJoystickDriver joystick_driver_;
    AdbActionButtonLayout action_buttons_ {};
};

std::unique_ptr<IInputBackend>
    CreateAdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator);

} // namespace mapnavigator::backend::adb
