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

MaaController* AdbInputBackend::getCtrl() const
{
    return ctrl_;
}

const std::string& AdbInputBackend::controllerType() const
{
    return controller_type_;
}

bool AdbInputBackend::usesTouchBackend() const
{
    return true;
}

bool AdbInputBackend::isSupported() const
{
    return ctrl_ != nullptr && has_locator_;
}

const std::string& AdbInputBackend::unsupportedReason() const
{
    return unsupported_reason_;
}

double AdbInputBackend::defaultTurnUnitsPerDegree() const
{
    return default_turn_units_per_degree_;
}

double AdbInputBackend::defaultPitchUnitsPerDegree() const
{
    return kAdbTouchTurnProfile.default_pitch_units_per_degree;
}

SteeringTransportProfile AdbInputBackend::steeringProfile() const
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

void AdbInputBackend::setMovementStateSync(MovementState state, int delay_millis)
{
    applyMovementState(state, delay_millis);
}

void AdbInputBackend::triggerJumpSync(int hold_millis)
{
    if (!clickTargetSync(
            action_buttons_.jump_button,
            std::max(hold_millis, action_buttons_.default_hold_ms),
            action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger jump." << VAR(hold_millis);
    }
}

void AdbInputBackend::triggerInteractSync(int hold_millis)
{
    if (!clickTargetSync(
            action_buttons_.interact_button,
            std::max(hold_millis, action_buttons_.default_hold_ms),
            action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger interact." << VAR(hold_millis);
    }
}

void AdbInputBackend::pulseForwardSync(int hold_millis)
{
    const bool pulsed = joystick_driver_.pulseForward(hold_millis);
    if (!pulsed) {
        LogWarn << "AdbInputBackend: failed to pulse forward." << VAR(hold_millis);
    }
}

void AdbInputBackend::triggerSprintSync()
{
    if (!clickTargetSync(action_buttons_.sprint_button, action_buttons_.default_hold_ms, action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger sprint.";
    }
    // 冲刺会退出走路, 摇杆推回满行程
    joystick_driver_.setWalking(false);
}

void AdbInputBackend::resetForwardWalkSync(int release_millis)
{
    joystick_driver_.release(release_millis);
    applyMovementState({ .forward = true }, 0);
}

void AdbInputBackend::clickMouseLeftSync()
{
    if (!clickTargetSync(action_buttons_.attack_button, action_buttons_.default_hold_ms, action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click attack button.";
    }
}

void AdbInputBackend::triggerZiplineLaunchSync()
{
    if (!clickTargetSync(action_buttons_.zipline_launch_button, action_buttons_.default_hold_ms, action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click zipline launch button.";
    }
}

void AdbInputBackend::triggerZiplineDismountSync(int hold_millis)
{
    if (!clickTargetSync(action_buttons_.zipline_dismount_button, hold_millis, action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click zipline dismount button.";
    }
}

bool AdbInputBackend::sendViewDeltaSync(int dx, int dy)
{
    if (ctrl_ == nullptr || (dx == 0 && dy == 0)) {
        return ctrl_ != nullptr;
    }

    const bool applied = camera_swipe_driver_.swipeByPixels(dx, dy);
    if (!applied) {
        LogWarn << "AdbInputBackend: failed to apply camera swipe." << VAR(dx) << VAR(dy);
        return false;
    }
    return true;
}

void AdbInputBackend::applyMovementState(MovementState state, int delay_millis)
{
    const bool applied = joystick_driver_.setMovementState(state, delay_millis);
    if (!applied) {
        LogWarn << "AdbInputBackend: failed to apply movement state." << VAR_AS(forward_down_, state.forward)
                << VAR_AS(left_down_, state.left) << VAR_AS(backward_down_, state.backward) << VAR_AS(right_down_, state.right);
    }
}

bool AdbInputBackend::clickTargetSync(const AdbTapTarget& target, int hold_millis, int delay_millis)
{
    if (!touchDownTargetSync(target)) {
        return false;
    }

    SleepFor(hold_millis);

    const MaaCtrlId up_id = MaaControllerPostTouchUp(ctrl_, target.contact_id);
    if (!waitForControllerAction(up_id, "touch_up")) {
        return false;
    }

    SleepFor(delay_millis);
    return true;
}

bool AdbInputBackend::touchDownTargetSync(const AdbTapTarget& target) const
{
    const cv::Point point(std::clamp(target.point.x, 0, kWorkWidth - 1), std::clamp(target.point.y, 0, kWorkHeight - 1));
    const MaaCtrlId move_id = MaaControllerPostTouchMove(ctrl_, target.contact_id, point.x, point.y, 0);
    if (!waitForControllerAction(move_id, "touch_move")) {
        return false;
    }

    const MaaCtrlId down_id = MaaControllerPostTouchDown(ctrl_, target.contact_id, point.x, point.y, 0);
    if (!waitForControllerAction(down_id, "touch_down")) {
        return false;
    }

    return true;
}

bool AdbInputBackend::waitForControllerAction(MaaCtrlId ctrl_id, const char* action_name) const
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
