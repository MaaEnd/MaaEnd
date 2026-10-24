#include <algorithm>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../../navi_config.h"
#include "../../navi_log.h"
#include "../../navi_time.h"
#include "adb_input_backend.h"

namespace mapnavigator::backend::adb
{

AdbInputBackend::AdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator)
    : ctrl_(ctrl)
    , controller_type_(std::move(controller_type))
    , default_turn_units_per_degree_(kAdbTouchTurnProfile.default_units_per_degree)
    , has_locator_(locator != nullptr)
    , camera_swipe_driver_(ctrl)
    , joystick_driver_(ctrl)
{
    if (ctrl_ == nullptr) {
        unsupported_reason_ = "controller handle is null";
        return;
    }

    if (!has_locator_) {
        unsupported_reason_ = "map locator is unavailable";
    }

    LogInfo << "Adb touch turn profile initialized." << VAR(default_turn_units_per_degree_) << VAR(kAdbTouchTurnProfile.swipe_duration_ms)
            << VAR(kAdbTouchTurnProfile.post_swipe_settle_ms);
}

MaaController* AdbInputBackend::GetCtrl() const
{
    return ctrl_;
}

const std::string& AdbInputBackend::ControllerType() const
{
    return controller_type_;
}

bool AdbInputBackend::UsesTouchBackend() const
{
    return true;
}

bool AdbInputBackend::IsSupported() const
{
    return ctrl_ != nullptr && has_locator_;
}

const std::string& AdbInputBackend::UnsupportedReason() const
{
    return unsupported_reason_;
}

double AdbInputBackend::DefaultTurnUnitsPerDegree() const
{
    return default_turn_units_per_degree_;
}

double AdbInputBackend::DefaultPitchUnitsPerDegree() const
{
    return kAdbTouchTurnProfile.default_pitch_units_per_degree;
}

SteeringTransportProfile AdbInputBackend::SteeringProfile() const
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

void AdbInputBackend::SetMovementStateSync(MovementState state, int delay_millis)
{
    ApplyMovementState(state, delay_millis);
}

void AdbInputBackend::TriggerJumpSync(int hold_millis)
{
    if (!ClickTargetSync(
            action_buttons_.jump_button,
            std::max(hold_millis, action_buttons_.default_hold_ms),
            action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger jump." << VAR(hold_millis);
    }
}

void AdbInputBackend::TriggerInteractSync(int hold_millis)
{
    if (!ClickTargetSync(
            action_buttons_.interact_button,
            std::max(hold_millis, action_buttons_.default_hold_ms),
            action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger interact." << VAR(hold_millis);
    }
}

void AdbInputBackend::PulseForwardSync(int hold_millis)
{
    const bool pulsed = joystick_driver_.PulseForward(hold_millis);
    if (!pulsed) {
        LogWarn << "AdbInputBackend: failed to pulse forward." << VAR(hold_millis);
    }
}

void AdbInputBackend::TriggerSprintSync()
{
    if (!ClickTargetSync(action_buttons_.sprint_button, action_buttons_.default_hold_ms, action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger sprint.";
    }
    // 冲刺会退出走路, 摇杆推回满行程
    joystick_driver_.SetWalking(false);
}

void AdbInputBackend::ResetForwardWalkSync(int release_millis)
{
    joystick_driver_.Release(release_millis);
    ApplyMovementState({ .forward = true }, 0);
}

void AdbInputBackend::ClickMouseLeftSync()
{
    if (!ClickTargetSync(action_buttons_.attack_button, action_buttons_.default_hold_ms, action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click attack button.";
    }
}

void AdbInputBackend::TriggerZiplineLaunchSync()
{
    if (!ClickTargetSync(action_buttons_.zipline_launch_button, action_buttons_.default_hold_ms, action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click zipline launch button.";
    }
}

void AdbInputBackend::TriggerZiplineDismountSync(int hold_millis)
{
    if (!ClickTargetSync(action_buttons_.zipline_dismount_button, hold_millis, action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click zipline dismount button.";
    }
}

bool AdbInputBackend::SendViewDeltaSync(int dx, int dy)
{
    if (ctrl_ == nullptr || (dx == 0 && dy == 0)) {
        return ctrl_ != nullptr;
    }

    const bool applied = camera_swipe_driver_.SwipeByPixels(dx, dy);
    if (!applied) {
        LogWarn << "AdbInputBackend: failed to apply camera swipe." << VAR(dx) << VAR(dy);
        return false;
    }
    return true;
}

void AdbInputBackend::ApplyMovementState(MovementState state, int delay_millis)
{
    const bool applied = joystick_driver_.SetMovementState(state, delay_millis);
    if (!applied) {
        LogWarn << "AdbInputBackend: failed to apply movement state." << VAR_AS(forward_down_, state.forward)
                << VAR_AS(left_down_, state.left) << VAR_AS(backward_down_, state.backward) << VAR_AS(right_down_, state.right);
    }
}

bool AdbInputBackend::ClickTargetSync(const AdbTapTarget& target, int hold_millis, int delay_millis)
{
    if (!TouchDownTargetSync(target)) {
        return false;
    }

    SleepFor(hold_millis);

    const MaaCtrlId up_id = MaaControllerPostTouchUp(ctrl_, target.contact_id);
    if (!WaitForControllerAction(up_id, "touch_up")) {
        return false;
    }

    SleepFor(delay_millis);
    return true;
}

bool AdbInputBackend::TouchDownTargetSync(const AdbTapTarget& target) const
{
    const cv::Point point(std::clamp(target.point.x, 0, kWorkWidth - 1), std::clamp(target.point.y, 0, kWorkHeight - 1));
    const MaaCtrlId move_id = MaaControllerPostTouchMove(ctrl_, target.contact_id, point.x, point.y, 0);
    if (!WaitForControllerAction(move_id, "touch_move")) {
        return false;
    }

    const MaaCtrlId down_id = MaaControllerPostTouchDown(ctrl_, target.contact_id, point.x, point.y, 0);
    if (!WaitForControllerAction(down_id, "touch_down")) {
        return false;
    }

    return true;
}

bool AdbInputBackend::WaitForControllerAction(MaaCtrlId ctrl_id, const char* action_name) const
{
    if (ctrl_id == MaaInvalidId) {
        LogWarn << "AdbInputBackend: failed to post controller action." << VAR(action_name);
        return false;
    }

    const MaaStatus status = MaaControllerWait(ctrl_, ctrl_id);
    if (status == MaaStatus_Succeeded) {
        return true;
    }

    LogWarn << "AdbInputBackend: controller action did not succeed." << VAR(action_name) << VAR(ctrl_id) << VAR(status);
    return false;
}

std::unique_ptr<IInputBackend>
    CreateAdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=adb";
    return std::make_unique<AdbInputBackend>(ctrl, std::move(controller_type), std::move(locator));
}

} // namespace mapnavigator::backend::adb
