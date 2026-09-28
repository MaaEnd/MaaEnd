#include <algorithm>
#include <chrono>
#include <thread>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../../navi_config.h"
#include "adb_input_backend.h"

namespace mapnavigator::backend::adb
{

namespace
{

constexpr int32_t kReferenceFrameHeight = 720;

AdbCameraSwipeDriverConfig MakeDefaultCameraSwipeDriverConfig()
{
    AdbCameraSwipeDriverConfig config;
    config.pressure = 0;
    config.turn_swipe_duration_ms = kAdbTouchTurnProfile.swipe_duration_ms;
    config.post_swipe_settle_ms = kAdbTouchTurnProfile.post_swipe_settle_ms;
    return config;
}

double ComputeDefaultTurnUnitsPerDegree([[maybe_unused]] MaaController* ctrl)
{
    return kAdbTouchTurnProfile.default_units_per_degree;
}

} // namespace

AdbInputBackend::AdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator)
    : ctrl_(ctrl)
    , controller_type_(std::move(controller_type))
    , default_turn_units_per_degree_(ComputeDefaultTurnUnitsPerDegree(ctrl))
    , has_locator_(locator != nullptr)
    , camera_swipe_driver_(ctrl, MakeDefaultCameraSwipeDriverConfig())
    , joystick_driver_(ctrl, locator, AdbVirtualJoystickDriverConfig {})
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

const std::string& AdbInputBackend::controller_type() const
{
    return controller_type_;
}

bool AdbInputBackend::uses_touch_backend() const
{
    return true;
}

bool AdbInputBackend::is_supported() const
{
    return ctrl_ != nullptr && has_locator_;
}

const std::string& AdbInputBackend::unsupported_reason() const
{
    return unsupported_reason_;
}

double AdbInputBackend::default_turn_units_per_degree() const
{
    return default_turn_units_per_degree_;
}

SteeringTransportProfile AdbInputBackend::steering_transport_profile() const
{
    return SteeringTransportProfile {
        .supports_concurrent_move_and_look = true,
        .min_send_interval_ms = 0,
        .min_emit_delta_deg = 1.5,
        .max_batch_delta_deg = 20.0,
        .action_quiet_period_ms = kAdbTouchTurnProfile.action_quiet_period_ms,
    };
}

bool AdbInputBackend::supports_sprint() const
{
    return true;
}

void AdbInputBackend::SetMovementStateSync(bool forward, bool left, bool backward, bool right, int delay_millis)
{
    forward_down_ = forward;
    left_down_ = left;
    backward_down_ = backward;
    right_down_ = right;
    ApplyMovementState(delay_millis);
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
    sprint_button_down_ = false;
}

void AdbInputBackend::ResetForwardWalkSync(int release_millis)
{
    joystick_driver_.Release(release_millis);
    forward_down_ = true;
    left_down_ = false;
    backward_down_ = false;
    right_down_ = false;
    ApplyMovementState(0);
}

void AdbInputBackend::ClickMouseLeftSync()
{
    if (!ClickTargetSync(action_buttons_.attack_button, action_buttons_.default_hold_ms, action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to click attack button.";
    }
}

void AdbInputBackend::MouseRightDownSync(int delay_millis)
{
    MouseRightDownOnTargetSync(action_buttons_.sprint_button, delay_millis);
}

void AdbInputBackend::MouseRightUpSync(int delay_millis)
{
    MouseRightUpOnTargetSync(action_buttons_.sprint_button.contact_id, delay_millis);
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

void AdbInputBackend::ApplyMovementState(int delay_millis)
{
    const bool applied = joystick_driver_.SetMovementState(forward_down_, left_down_, backward_down_, right_down_, delay_millis);
    if (!applied) {
        LogWarn << "AdbInputBackend: failed to apply movement state." << VAR(forward_down_) << VAR(left_down_) << VAR(backward_down_)
                << VAR(right_down_);
    }
}

bool AdbInputBackend::ClickTargetSync(const AdbTapTarget& target, int hold_millis, int delay_millis)
{
    if (!TouchDownTargetSync(target)) {
        return false;
    }

    SleepIfNeeded(hold_millis);

    const MaaCtrlId up_id = MaaControllerPostTouchUp(ctrl_, target.contact_id);
    if (!WaitForControllerAction(up_id, "touch_up")) {
        return false;
    }

    SleepIfNeeded(delay_millis);
    return true;
}

bool AdbInputBackend::TouchDownTargetSync(const AdbTapTarget& target) const
{
    const cv::Point point(std::clamp(target.point.x, 0, kWorkWidth - 1), std::clamp(target.point.y, 0, kReferenceFrameHeight - 1));
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

void AdbInputBackend::MouseRightDownOnTargetSync(const AdbTapTarget& target, int delay_millis)
{
    if (sprint_button_down_) {
        SleepIfNeeded(delay_millis);
        return;
    }
    if (!TouchDownTargetSync(target)) {
        return;
    }

    sprint_button_down_ = true;
    SleepIfNeeded(delay_millis);
}

void AdbInputBackend::MouseRightUpOnTargetSync(int contact_id, int delay_millis)
{
    if (!sprint_button_down_) {
        SleepIfNeeded(delay_millis);
        return;
    }

    const MaaCtrlId up_id = MaaControllerPostTouchUp(ctrl_, contact_id);
    if (WaitForControllerAction(up_id, "touch_up")) {
        sprint_button_down_ = false;
    }
    SleepIfNeeded(delay_millis);
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

void AdbInputBackend::SleepIfNeeded(int delay_millis)
{
    if (delay_millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(delay_millis));
}

std::unique_ptr<IInputBackend>
    CreateAdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=adb";
    return std::make_unique<AdbInputBackend>(ctrl, std::move(controller_type), std::move(locator));
}

} // namespace mapnavigator::backend::adb
