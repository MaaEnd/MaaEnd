#pragma once

#include <cstdint>
#include <string>

#include <MaaFramework/MaaAPI.h>

#include "../backend_types.h"

namespace mapnavigator
{

struct InputBackend;

} // namespace mapnavigator

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

struct DesktopBackend
{
    std::string backend_name;
    DesktopKeyCodes key_codes {};
    bool mouse_lock_follow_enabled = false;
    bool background_managed_keys_enabled = false;
    int hover_x = 0;
    int hover_y = 0;
    bool hover_inited = false;
    bool forward_down = false;
    bool left_down = false;
    bool backward_down = false;
    bool right_down = false;
};

InputBackend MakeDesktopInputBackend(BackendKind kind, MaaController* ctrl, std::string controller_type, std::string backend_name);
InputBackend OpenDesktopInputBackend(MaaController* ctrl, std::string controller_type, std::string backend_name);
void CloseDesktopInputBackend(const InputBackend& backend);

SteeringTransportProfile DesktopSteeringProfile();

void SetDesktopMovementStateSync(InputBackend* backend, MovementState state, int delay_millis);
void TriggerDesktopJumpSync(InputBackend* backend, int hold_millis);
void TriggerDesktopInteractSync(InputBackend* backend, int hold_millis);
void PulseDesktopForwardSync(InputBackend* backend, int hold_millis);
void TriggerDesktopSprintSync(InputBackend* backend);
void ToggleDesktopWalkModeSync(InputBackend* backend);
void ResetDesktopForwardWalkSync(InputBackend* backend, int release_millis);
void ClickDesktopMouseLeftSync(InputBackend* backend);
void TriggerDesktopZiplineLaunchSync(InputBackend* backend);
void TriggerDesktopZiplineDismountSync(InputBackend* backend, int hold_millis);
bool SendDesktopViewDeltaSync(const InputBackend& backend, int dx, int dy);

} // namespace mapnavigator::backend::desktop
