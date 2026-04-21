#include <chrono>
#include <thread>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../../navi_config.h"
#include "desktop_input_backend.h"

namespace mapnavigator::backend::desktop
{

namespace
{

constexpr int32_t kDesktopReferenceFrameHeight = 720;
constexpr int32_t kDesktopDefaultHoverAnchorX = kWorkWidth / 2;
constexpr int32_t kDesktopDefaultHoverAnchorY = kDesktopReferenceFrameHeight / 2;
constexpr int32_t kDesktopHoverTouchContactId = 0;
constexpr int32_t kDesktopPrimaryTouchContactId = 1;
constexpr int32_t kDesktopDefaultTouchPressure = 0;

double ComputeDefaultTurnUnitsPerDegree(MaaController* ctrl, const std::string& backend_name)
{
    int32_t screen_width = 0;
    int32_t screen_height = 0;
    if (!MaaControllerGetResolution(ctrl, &screen_width, &screen_height) || screen_width <= 0) {
        LogWarn << backend_name << " backend: failed to get controller resolution, fallback to default turn scale." << VAR(screen_width)
                << VAR(screen_height);
        return ComputeDefaultUnitsPerDegree();
    }

    const int turn360 = ComputeTurn360Units(screen_width);
    const double units_per_degree = ComputeUnitsPerDegreeForWidth(screen_width);
    LogInfo << "Computed turn scale from raw controller resolution." << VAR(backend_name) << VAR(screen_width) << VAR(screen_height)
            << VAR(turn360) << VAR(units_per_degree);
    return units_per_degree;
}

} // namespace

void DesktopInputBackend::SleepIfNeeded(int delay_millis)
{
    if (delay_millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(delay_millis));
}

DesktopInputBackend::DesktopInputBackend(
    MaaController* ctrl,
    std::string controller_type,
    std::string backend_name,
    DesktopKeyCodes key_codes)
    : ctrl_(ctrl)
    , controller_type_(std::move(controller_type))
    , backend_name_(std::move(backend_name))
    , key_codes_(key_codes)
    , default_turn_units_per_degree_(ComputeDefaultTurnUnitsPerDegree(ctrl, backend_name_))
    , hover_x_(kDesktopDefaultHoverAnchorX)
    , hover_y_(kDesktopDefaultHoverAnchorY)
{
    if (ctrl_ == nullptr) {
        unsupported_reason_ = "controller handle is null";
    }
}

MaaController* DesktopInputBackend::GetCtrl() const
{
    return ctrl_;
}

const std::string& DesktopInputBackend::controller_type() const
{
    return controller_type_;
}

bool DesktopInputBackend::uses_touch_backend() const
{
    return false;
}

bool DesktopInputBackend::is_supported() const
{
    return ctrl_ != nullptr;
}

const std::string& DesktopInputBackend::unsupported_reason() const
{
    return unsupported_reason_;
}

double DesktopInputBackend::default_turn_units_per_degree() const
{
    return default_turn_units_per_degree_;
}

SteeringTransportProfile DesktopInputBackend::steering_transport_profile() const
{
    return SteeringTransportProfile {
        .supports_concurrent_move_and_look = true,
        .min_send_interval_ms = 0,
        .min_emit_delta_deg = 1.0,
        .max_batch_delta_deg = 18.0,
        .action_quiet_period_ms = 0,
    };
}

void DesktopInputBackend::SetMovementStateSync(bool forward, bool left, bool backward, bool right, int delay_millis)
{
    ApplyMovementKeyState(key_codes_.move_forward, forward);
    ApplyMovementKeyState(key_codes_.move_left, left);
    ApplyMovementKeyState(key_codes_.move_backward, backward);
    ApplyMovementKeyState(key_codes_.move_right, right);
    SleepIfNeeded(delay_millis);
}

void DesktopInputBackend::TriggerJumpSync(int hold_millis)
{
    ClickKeySync(key_codes_.jump, hold_millis);
}

void DesktopInputBackend::TriggerInteractSync(int hold_millis)
{
    ClickKeySync(key_codes_.interact, hold_millis);
}

void DesktopInputBackend::PulseForwardSync(int hold_millis)
{
    PostKeyDownSync(key_codes_.move_forward, 0);
    SleepIfNeeded(hold_millis);
    PostKeyUpSync(key_codes_.move_forward, 0);
}

void DesktopInputBackend::TriggerSprintSync()
{
    MouseRightDownSync(0);
    SleepIfNeeded(kActionSprintPressMs);
    MouseRightUpSync(0);
}

void DesktopInputBackend::ResetForwardWalkSync(int release_millis)
{
    PostKeyUpSync(key_codes_.move_forward, 0);
    SleepIfNeeded(release_millis);
    PostKeyDownSync(key_codes_.move_forward, 0);
}

void DesktopInputBackend::ClickMouseLeftSync()
{
    EnsureHoverAnchorSync();
    const MaaCtrlId ctrl_id = MaaControllerPostClick(ctrl_, hover_x_, hover_y_);
    MaaControllerWait(ctrl_, ctrl_id);
}

void DesktopInputBackend::MouseRightDownSync(int delay_millis)
{
    EnsureHoverAnchorSync();
    const MaaCtrlId ctrl_id =
        MaaControllerPostTouchDown(ctrl_, kDesktopPrimaryTouchContactId, hover_x_, hover_y_, kDesktopDefaultTouchPressure);
    MaaControllerWait(ctrl_, ctrl_id);
    SleepIfNeeded(delay_millis);
}

void DesktopInputBackend::MouseRightUpSync(int delay_millis)
{
    const MaaCtrlId ctrl_id = MaaControllerPostTouchUp(ctrl_, kDesktopPrimaryTouchContactId);
    MaaControllerWait(ctrl_, ctrl_id);
    SleepIfNeeded(delay_millis);
}

bool DesktopInputBackend::SendViewDeltaSync(int dx, int dy)
{
    if (dx == 0 && dy == 0) {
        return true;
    }

    if (ctrl_ == nullptr) {
        return false;
    }

    LogInfo << "SendRelativeMoveNative" << VAR(dx) << VAR(dy);
    const MaaCtrlId ctrl_id = MaaControllerPostRelativeMove(ctrl_, dx, dy);
    if (ctrl_id == MaaInvalidId) {
        return false;
    }

    return MaaControllerWait(ctrl_, ctrl_id) == MaaStatus_Succeeded;
}

void DesktopInputBackend::PostKeyDownSync(int key_code, int delay_millis)
{
    if (bool* movement_state = FindMovementKeyState(key_code)) {
        *movement_state = true;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostKeyDown(ctrl_, key_code);
    MaaControllerWait(ctrl_, ctrl_id);
    SleepIfNeeded(delay_millis);
}

void DesktopInputBackend::PostKeyUpSync(int key_code, int delay_millis)
{
    if (bool* movement_state = FindMovementKeyState(key_code)) {
        *movement_state = false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostKeyUp(ctrl_, key_code);
    MaaControllerWait(ctrl_, ctrl_id);
    SleepIfNeeded(delay_millis);
}

void DesktopInputBackend::ClickKeySync(int key_code, int hold_millis)
{
    PostKeyDownSync(key_code, 0);
    SleepIfNeeded(hold_millis);
    PostKeyUpSync(key_code, 0);
}

void DesktopInputBackend::ApplyMovementKeyState(int key_code, bool pressed)
{
    bool* current_state = FindMovementKeyState(key_code);
    if (current_state == nullptr || *current_state == pressed) {
        return;
    }

    if (pressed) {
        PostKeyDownSync(key_code, 0);
        return;
    }
    PostKeyUpSync(key_code, 0);
}

bool* DesktopInputBackend::FindMovementKeyState(int key_code)
{
    if (key_code == key_codes_.move_forward) {
        return &forward_down_;
    }
    if (key_code == key_codes_.move_left) {
        return &left_down_;
    }
    if (key_code == key_codes_.move_backward) {
        return &backward_down_;
    }
    if (key_code == key_codes_.move_right) {
        return &right_down_;
    }
    return nullptr;
}

void DesktopInputBackend::EnsureHoverAnchorSync()
{
    if (hover_inited_) {
        return;
    }

    hover_inited_ = true;
    const MaaCtrlId ctrl_id =
        MaaControllerPostTouchMove(ctrl_, kDesktopHoverTouchContactId, hover_x_, hover_y_, kDesktopDefaultTouchPressure);
    MaaControllerWait(ctrl_, ctrl_id);
}

// Win32 Virtual-Key codes。WlRoots 控制器启用 use_win32_vk_code 后，
// MaaFramework 会将这些码翻译为 Linux evdev 码，行为与 Win32 控制器一致。
DesktopKeyCodes MakeDesktopKeyCodes()
{
    return DesktopKeyCodes {
        .move_forward = 'W',  // 0x57
        .move_left = 'A',     // 0x41
        .move_backward = 'S', // 0x53
        .move_right = 'D',    // 0x44
        .interact = 'F',      // 0x46
        .jump = 0x20,         // VK_SPACE
    };
}

std::unique_ptr<IInputBackend>
    CreateDesktopInputBackend(MaaController* ctrl, std::string controller_type, std::string backend_name)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << VAR(backend_name);
    return std::make_unique<DesktopInputBackend>(ctrl, std::move(controller_type), std::move(backend_name), MakeDesktopKeyCodes());
}

} // namespace mapnavigator::backend::desktop
