#include <algorithm>
#include <chrono>
#include <string_view>
#include <thread>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../../controller_type_utils.h"
#include "../../navi_config.h"
#include "adb_input_backend.h"

namespace mapnavigator::backend::adb
{

namespace
{

constexpr int32_t kReferenceFrameHeight = 720;

class ScopedImageBuffer
{
public:
    ScopedImageBuffer()
        : buffer_(MaaImageBufferCreate())
    {
    }

    ~ScopedImageBuffer() { MaaImageBufferDestroy(buffer_); }

    ScopedImageBuffer(const ScopedImageBuffer&) = delete;
    ScopedImageBuffer& operator=(const ScopedImageBuffer&) = delete;

    MaaImageBuffer* Get() const { return buffer_; }

private:
    MaaImageBuffer* buffer_ = nullptr;
};

AdbCameraSwipeDriverConfig MakeDefaultCameraSwipeDriverConfig(std::string_view controller_type)
{
    AdbCameraSwipeDriverConfig config;
    config.contact_id = IsPlayCoverControllerType(controller_type) ? 0 : config.contact_id;
    config.pressure = 0;
    config.turn_swipe_duration_ms = kAdbTouchTurnProfile.swipe_duration_ms;
    config.post_swipe_settle_ms = kAdbTouchTurnProfile.post_swipe_settle_ms;
    return config;
}

AdbVirtualJoystickDriverConfig MakeDefaultJoystickDriverConfig(std::string_view controller_type)
{
    AdbVirtualJoystickDriverConfig config;
    if (!IsPlayCoverControllerType(controller_type)) {
        return config;
    }

    config.contact_id = 0;
    return config;
}

AdbActionButtonLayout MakeDefaultActionButtonLayout(std::string_view controller_type)
{
    AdbActionButtonLayout layout;
    if (!IsPlayCoverControllerType(controller_type)) {
        return layout;
    }

    layout.sprint_button.contact_id = 0;
    layout.jump_button.contact_id = 0;
    layout.attack_button.contact_id = 0;
    layout.interact_button.contact_id = 0;
    return layout;
}

bool RequiresSingleTouchSerialization(std::string_view controller_type)
{
    return IsPlayCoverControllerType(controller_type);
}

double ComputeDefaultTurnUnitsPerDegree([[maybe_unused]] MaaController* ctrl)
{
    return kAdbTouchTurnProfile.default_units_per_degree;
}

bool HasUsableBlindActionZoneGate(const maplocator::YoloCoarseResult& coarse)
{
    return coarse.valid && !coarse.is_none && !coarse.zone_id.empty() && coarse.zone_id != "None" && coarse.raw_class != "None"
           && coarse.base_class != "None";
}

} // namespace

AdbInputBackend::AdbInputBackend(MaaController* ctrl, std::string controller_type, std::shared_ptr<maplocator::MapLocator> locator)
    : ctrl_(ctrl)
    , controller_type_(std::move(controller_type))
    , default_turn_units_per_degree_(ComputeDefaultTurnUnitsPerDegree(ctrl))
    , has_locator_(locator != nullptr)
    , camera_swipe_driver_(ctrl, MakeDefaultCameraSwipeDriverConfig(controller_type_))
    , zone_guard_(locator)
    , joystick_driver_(ctrl, locator, MakeDefaultJoystickDriverConfig(controller_type_))
    , action_buttons_(MakeDefaultActionButtonLayout(controller_type_))
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
    if (!ClickBlindTargetSync(
            "jump",
            action_buttons_.jump_button,
            std::max(hold_millis, action_buttons_.default_hold_ms),
            action_buttons_.post_action_delay_ms)) {
        LogWarn << "AdbInputBackend: failed to trigger jump." << VAR(hold_millis);
    }
}

void AdbInputBackend::TriggerInteractSync(int hold_millis)
{
    if (!ClickBlindTargetSync(
            "interact",
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
    if (!ClickBlindTargetSync("sprint", action_buttons_.sprint_button, action_buttons_.default_hold_ms, action_buttons_.post_action_delay_ms)) {
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

    const bool single_touch_mode = RequiresSingleTouchSerialization(controller_type_);
    const bool has_active_movement = single_touch_mode && (forward_down_ || left_down_ || backward_down_ || right_down_);
    if (has_active_movement && !joystick_driver_.Release(0)) {
        LogWarn << "AdbInputBackend: failed to release joystick before single-touch camera swipe.";
    }

    const bool applied = camera_swipe_driver_.SwipeByPixels(dx, dy);
    if (has_active_movement) {
        const bool restored = joystick_driver_.SetMovementState(forward_down_, left_down_, backward_down_, right_down_, 0);
        if (!restored) {
            LogWarn << "AdbInputBackend: failed to restore joystick after single-touch camera swipe.";
        }
    }

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

bool AdbInputBackend::CaptureFrame(cv::Mat* out_image) const
{
    if (out_image == nullptr || ctrl_ == nullptr) {
        return false;
    }

    ScopedImageBuffer buffer;
    const MaaCtrlId screencap_id = MaaControllerPostScreencap(ctrl_);
    MaaControllerWait(ctrl_, screencap_id);
    if (!MaaControllerCachedImage(ctrl_, buffer.Get()) || MaaImageBufferIsEmpty(buffer.Get())) {
        return false;
    }

    *out_image = cv::Mat(
                     MaaImageBufferHeight(buffer.Get()),
                     MaaImageBufferWidth(buffer.Get()),
                     MaaImageBufferType(buffer.Get()),
                     MaaImageBufferGetRawData(buffer.Get()))
                     .clone();
    return !out_image->empty();
}

bool AdbInputBackend::IsBlindActionAllowed(const char* action_name) const
{
    cv::Mat frame;
    if (!CaptureFrame(&frame)) {
        LogWarn << "AdbInputBackend: blind action blocked because screencap failed." << VAR(action_name);
        return false;
    }

    const auto coarse = zone_guard_.ProbeYolo(frame);
    if (HasUsableBlindActionZoneGate(coarse)) {
        return true;
    }

    LogInfo << "AdbInputBackend: blind action blocked by zone gate." << VAR(action_name) << VAR(coarse.valid) << VAR(coarse.is_none)
            << VAR(coarse.raw_class) << VAR(coarse.base_class) << VAR(coarse.zone_id) << VAR(coarse.confidence);
    return false;
}

bool AdbInputBackend::ClickBlindTargetSync(const char* action_name, const AdbTapTarget& target, int hold_millis, int delay_millis)
{
    if (!IsBlindActionAllowed(action_name)) {
        return false;
    }

    const bool clicked = ClickTargetSync(target, hold_millis, delay_millis);
    if (!clicked) {
        LogWarn << "AdbInputBackend: failed to trigger blind action." << VAR(action_name);
    }
    return clicked;
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
    if (!IsBlindActionAllowed("sprint_hold")) {
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
