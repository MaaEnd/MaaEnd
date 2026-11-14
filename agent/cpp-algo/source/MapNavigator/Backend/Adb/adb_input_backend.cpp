#include <algorithm>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../../navi_config.h"
#include "../../navi_log.h"
#include "../../navi_time.h"
#include "../backend.h"
#include "adb_input_backend.h"

namespace mapnavigator::backend::adb
{

namespace
{

bool wait_for_controller_action(const InputBackend& backend, MaaCtrlId ctrl_id, const char* action_name)
{
    if (ctrl_id == MaaInvalidId) {
        LogWarn << "AdbInputBackend: failed to post controller action." << VAR(action_name);
        return false;
    }

    const MaaStatus status = MaaControllerWait(backend.ctrl, ctrl_id);
    if (status == MaaStatus_Succeeded) {
        return true;
    }

    LogWarn << "AdbInputBackend: controller action did not succeed." << VAR(action_name) << VAR(ctrl_id) << VAR(status);
    return false;
}

bool touch_down_target_sync(const InputBackend& backend, const AdbTapTarget& target)
{
    const cv::Point point(std::clamp(target.point.x, 0, kWorkWidth - 1), std::clamp(target.point.y, 0, kWorkHeight - 1));
    const MaaCtrlId move_id = MaaControllerPostTouchMove(backend.ctrl, target.contact_id, point.x, point.y, 0);
    if (!wait_for_controller_action(backend, move_id, "touch_move")) {
        return false;
    }

    const MaaCtrlId down_id = MaaControllerPostTouchDown(backend.ctrl, target.contact_id, point.x, point.y, 0);
    if (!wait_for_controller_action(backend, down_id, "touch_down")) {
        return false;
    }

    return true;
}

bool click_target_sync(const InputBackend& backend, const AdbTapTarget& target, int hold_millis, int delay_millis)
{
    if (!touch_down_target_sync(backend, target)) {
        return false;
    }

    SleepFor(hold_millis);

    const MaaCtrlId up_id = MaaControllerPostTouchUp(backend.ctrl, target.contact_id);
    if (!wait_for_controller_action(backend, up_id, "touch_up")) {
        return false;
    }

    SleepFor(delay_millis);
    return true;
}

void apply_movement_state(InputBackend* backend, MovementState state, int delay_millis)
{
    const bool applied = SetJoystickMovementState(&backend->adb.joystick, state, delay_millis);
    if (!applied) {
        LogWarn << "AdbInputBackend: failed to apply movement state." << VAR_AS(forward_down_, state.forward)
                << VAR_AS(left_down_, state.left) << VAR_AS(backward_down_, state.backward) << VAR_AS(right_down_, state.right);
    }
}

} // namespace

InputBackend OpenAdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=adb";
    InputBackend backend;
    backend.kind = BackendKind::Adb;
    backend.ctrl = ctrl;
    backend.controller_type = std::move(controller_type);
    backend.default_turn_units_per_degree = kAdbTouchTurnProfile.default_units_per_degree;
    backend.default_pitch_range_units = kAdbTouchTurnProfile.default_pitch_range_units;
    backend.adb.has_locator = locator != nullptr;
    backend.adb.camera_swipe.controller = ctrl;
    backend.adb.joystick.controller = ctrl;

    if (backend.ctrl == nullptr) {
        backend.unsupported_reason = "controller handle is null";
        return backend;
    }

    if (!backend.adb.has_locator) {
        backend.unsupported_reason = "map locator is unavailable";
    }

    LogInfo << "Adb touch turn profile initialized." << VAR(backend.default_turn_units_per_degree)
            << VAR(kAdbTouchTurnProfile.swipe_duration_ms) << VAR(kAdbTouchTurnProfile.post_swipe_settle_ms);
    return backend;
}

void CloseAdbInputBackend(InputBackend* backend)
{
    const bool released = ReleaseJoystick(&backend->adb.joystick, 0);
    (void)released;
}

SteeringTransportProfile AdbSteeringProfile()
{
    return SteeringTransportProfile {
        .supports_concurrent_move_and_look = true,
        .min_send_interval_ms = 0,
        .min_emit_delta_deg = 1.5,
        .max_batch_delta_deg = 90.0,
        .action_quiet_period_ms = kAdbTouchTurnProfile.action_quiet_period_ms,
        .drops_turn_sends = true,
    };
}

void SetAdbMovementStateSync(InputBackend* backend, MovementState state, int delay_millis)
{
    apply_movement_state(backend, state, delay_millis);
}

void TriggerAdbJumpSync(const InputBackend& backend, int hold_millis)
{
    const AdbActionButtonLayout& buttons = backend.adb.action_buttons;
    if (!click_target_sync(backend, buttons.jump_button, std::max(hold_millis, buttons.default_hold_ms), buttons.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger jump." << VAR(hold_millis);
    }
}

void TriggerAdbInteractSync(const InputBackend& backend, int hold_millis)
{
    const AdbActionButtonLayout& buttons = backend.adb.action_buttons;
    if (!click_target_sync(
            backend,
            buttons.interact_button,
            std::max(hold_millis, buttons.default_hold_ms),
            buttons.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger interact." << VAR(hold_millis);
    }
}

void PulseAdbForwardSync(InputBackend* backend, int hold_millis)
{
    const bool pulsed = PulseJoystickForward(&backend->adb.joystick, hold_millis);
    if (!pulsed) {
        LogWarn << "AdbInputBackend: failed to pulse forward." << VAR(hold_millis);
    }
}

void TriggerAdbSprintSync(InputBackend* backend)
{
    const AdbActionButtonLayout& buttons = backend->adb.action_buttons;
    if (!click_target_sync(*backend, buttons.sprint_button, buttons.default_hold_ms, buttons.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger sprint.";
    }
    // 冲刺会退出走路, 摇杆推回满行程
    SetJoystickWalking(&backend->adb.joystick, false);
}

void ToggleAdbWalkModeSync(InputBackend* backend)
{
    SetJoystickWalking(&backend->adb.joystick, !backend->adb.joystick.walking);
}

void ResetAdbForwardWalkSync(InputBackend* backend, int release_millis)
{
    ReleaseJoystick(&backend->adb.joystick, release_millis);
    apply_movement_state(backend, { .forward = true }, 0);
}

void ClickAdbMouseLeftSync(const InputBackend& backend)
{
    const AdbActionButtonLayout& buttons = backend.adb.action_buttons;
    if (!click_target_sync(backend, buttons.attack_button, buttons.default_hold_ms, buttons.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click attack button.";
    }
}

void TriggerAdbZiplineLaunchSync(const InputBackend& backend)
{
    const AdbActionButtonLayout& buttons = backend.adb.action_buttons;
    if (!click_target_sync(backend, buttons.zipline_launch_button, buttons.default_hold_ms, buttons.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click zipline launch button.";
    }
}

void TriggerAdbZiplineDismountSync(const InputBackend& backend, int hold_millis)
{
    const AdbActionButtonLayout& buttons = backend.adb.action_buttons;
    if (!click_target_sync(backend, buttons.zipline_dismount_button, hold_millis, buttons.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click zipline dismount button.";
    }
}

bool SendAdbViewDeltaSync(const InputBackend& backend, int dx, int dy)
{
    if (backend.ctrl == nullptr || (dx == 0 && dy == 0)) {
        return backend.ctrl != nullptr;
    }

    const bool applied = SwipeCameraByPixels(backend.adb.camera_swipe, dx, dy);
    if (!applied) {
        LogWarn << "AdbInputBackend: failed to apply camera swipe." << VAR(dx) << VAR(dy);
        return false;
    }
    return true;
}

} // namespace mapnavigator::backend::adb
