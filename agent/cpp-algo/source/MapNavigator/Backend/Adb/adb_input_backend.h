#pragma once

#include <memory>
#include <string>

#include <MaaUtils/NoWarningCV.hpp>

#include "../backend_types.h"
#include "adb_camera_swipe_driver.h"
#include "adb_virtual_joystick_driver.h"

namespace maplocator
{

class MapLocator;

} // namespace maplocator

namespace mapnavigator
{

struct InputBackend;

} // namespace mapnavigator

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

struct AdbBackend
{
    bool has_locator = false;
    AdbCameraSwipeDriver camera_swipe;
    AdbVirtualJoystickDriver joystick;
    AdbActionButtonLayout action_buttons {};
};

InputBackend OpenAdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator);
void CloseAdbInputBackend(InputBackend* backend);

SteeringTransportProfile AdbSteeringProfile();

void SetAdbMovementStateSync(InputBackend* backend, MovementState state, int delay_millis);
void TriggerAdbJumpSync(const InputBackend& backend, int hold_millis);
void TriggerAdbInteractSync(const InputBackend& backend, int hold_millis);
void PulseAdbForwardSync(InputBackend* backend, int hold_millis);
void TriggerAdbSprintSync(InputBackend* backend);
void ToggleAdbWalkModeSync(InputBackend* backend);
void ResetAdbForwardWalkSync(InputBackend* backend, int release_millis);
void ClickAdbMouseLeftSync(const InputBackend& backend);
void TriggerAdbZiplineLaunchSync(const InputBackend& backend);
void TriggerAdbZiplineDismountSync(const InputBackend& backend, int hold_millis);
bool SendAdbViewDeltaSync(const InputBackend& backend, int dx, int dy);

} // namespace mapnavigator::backend::adb
