#include <chrono>
#include <thread>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../../navi_config.h"
#include "win32_input_backend.h"
#include "win32_key_codes.h"

namespace mapnavigator::backend::win32
{

namespace
{

constexpr int32_t kWin32ReferenceFrameHeight = 720;
constexpr int32_t kWin32DefaultHoverAnchorX = kWorkWidth / 2;
constexpr int32_t kWin32DefaultHoverAnchorY = kWin32ReferenceFrameHeight / 2;
constexpr int32_t kWin32HoverTouchContactId = 0;
constexpr int32_t kWin32PrimaryTouchContactId = 1;
constexpr int32_t kWin32DefaultTouchPressure = 0;

void SleepIfNeeded(int delay_millis)
{
    if (delay_millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(delay_millis));
}

double ComputeDefaultTurnUnitsPerDegree(MaaController* ctrl)
{
    int32_t screen_width = 0;
    int32_t screen_height = 0;
    if (!MaaControllerGetResolution(ctrl, &screen_width, &screen_height) || screen_width <= 0) {
        LogWarn << "Win32InputBackend: failed to get controller resolution, fallback to default turn scale." << VAR(screen_width)
                << VAR(screen_height);
        return ComputeDefaultUnitsPerDegree();
    }

    const int turn360 = ComputeTurn360Units(screen_width);
    const double units_per_degree = ComputeUnitsPerDegreeForWidth(screen_width);
    LogInfo << "Computed turn scale from raw controller resolution." << VAR(screen_width) << VAR(screen_height) << VAR(turn360)
            << VAR(units_per_degree);
    return units_per_degree;
}

} // namespace

Win32InputBackend::Win32InputBackend(MaaController* ctrl, std::string controller_type)
    : ctrl_(ctrl)
    , controller_type_(std::move(controller_type))
    , default_turn_units_per_degree_(ComputeDefaultTurnUnitsPerDegree(ctrl))
    , hover_x_(kWin32DefaultHoverAnchorX)
    , hover_y_(kWin32DefaultHoverAnchorY)
{
    if (ctrl_ == nullptr) {
        unsupported_reason_ = "controller handle is null";
    }
}

MaaController* Win32InputBackend::GetCtrl() const
{
    return ctrl_;
}

const std::string& Win32InputBackend::controller_type() const
{
    return controller_type_;
}

bool Win32InputBackend::uses_touch_backend() const
{
    return false;
}

bool Win32InputBackend::is_supported() const
{
    return ctrl_ != nullptr;
}

const std::string& Win32InputBackend::unsupported_reason() const
{
    return unsupported_reason_;
}

double Win32InputBackend::default_turn_units_per_degree() const
{
    return default_turn_units_per_degree_;
}

void Win32InputBackend::SetMovementStateSync(bool forward, bool left, bool backward, bool right, int delay_millis)
{
    ApplyMovementKeyState(kMoveForwardKey, forward);
    ApplyMovementKeyState(kMoveLeftKey, left);
    ApplyMovementKeyState(kMoveBackwardKey, backward);
    ApplyMovementKeyState(kMoveRightKey, right);
    SleepIfNeeded(delay_millis);
}

void Win32InputBackend::TriggerJumpSync(int hold_millis)
{
    ClickKeySync(kJumpKey, hold_millis);
}

void Win32InputBackend::TriggerInteractSync(int hold_millis)
{
    ClickKeySync(kInteractKey, hold_millis);
}

void Win32InputBackend::PulseForwardSync(int hold_millis)
{
    PostKeyDownSync(kMoveForwardKey, 0);
    SleepIfNeeded(hold_millis);
    PostKeyUpSync(kMoveForwardKey, 0);
}

void Win32InputBackend::TriggerSprintSync()
{
    MouseRightDownSync(0);
    SleepIfNeeded(kActionSprintPressMs);
    MouseRightUpSync(0);
}

void Win32InputBackend::ResetForwardWalkSync(int release_millis)
{
    PostKeyUpSync(kMoveForwardKey, 0);
    SleepIfNeeded(release_millis);
    PostKeyDownSync(kMoveForwardKey, 0);
}

void Win32InputBackend::ClickMouseLeftSync()
{
    EnsureHoverAnchorSync();
    const MaaCtrlId ctrl_id = MaaControllerPostClick(ctrl_, hover_x_, hover_y_);
    MaaControllerWait(ctrl_, ctrl_id);
}

void Win32InputBackend::MouseRightDownSync(int delay_millis)
{
    EnsureHoverAnchorSync();
    const MaaCtrlId ctrl_id =
        MaaControllerPostTouchDown(ctrl_, kWin32PrimaryTouchContactId, hover_x_, hover_y_, kWin32DefaultTouchPressure);
    MaaControllerWait(ctrl_, ctrl_id);
    SleepIfNeeded(delay_millis);
}

void Win32InputBackend::MouseRightUpSync(int delay_millis)
{
    const MaaCtrlId ctrl_id = MaaControllerPostTouchUp(ctrl_, kWin32PrimaryTouchContactId);
    MaaControllerWait(ctrl_, ctrl_id);
    SleepIfNeeded(delay_millis);
}

bool Win32InputBackend::SendViewDeltaSync(int dx, int dy)
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

void Win32InputBackend::PostKeyDownSync(int key_code, int delay_millis)
{
    if (bool* movement_state = FindMovementKeyState(key_code)) {
        *movement_state = true;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostKeyDown(ctrl_, key_code);
    MaaControllerWait(ctrl_, ctrl_id);
    SleepIfNeeded(delay_millis);
}

void Win32InputBackend::PostKeyUpSync(int key_code, int delay_millis)
{
    if (bool* movement_state = FindMovementKeyState(key_code)) {
        *movement_state = false;
    }

    const MaaCtrlId ctrl_id = MaaControllerPostKeyUp(ctrl_, key_code);
    MaaControllerWait(ctrl_, ctrl_id);
    SleepIfNeeded(delay_millis);
}

void Win32InputBackend::ClickKeySync(int key_code, int hold_millis)
{
    PostKeyDownSync(key_code, 0);
    SleepIfNeeded(hold_millis);
    PostKeyUpSync(key_code, 0);
}

void Win32InputBackend::ApplyMovementKeyState(int key_code, bool pressed)
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

bool* Win32InputBackend::FindMovementKeyState(int key_code)
{
    switch (key_code) {
    case kMoveForwardKey:
        return &forward_down_;
    case kMoveLeftKey:
        return &left_down_;
    case kMoveBackwardKey:
        return &backward_down_;
    case kMoveRightKey:
        return &right_down_;
    default:
        return nullptr;
    }
}

void Win32InputBackend::EnsureHoverAnchorSync()
{
    if (hover_inited_) {
        return;
    }

    hover_inited_ = true;
    const MaaCtrlId ctrl_id = MaaControllerPostTouchMove(ctrl_, kWin32HoverTouchContactId, hover_x_, hover_y_, kWin32DefaultTouchPressure);
    MaaControllerWait(ctrl_, ctrl_id);
}

std::unique_ptr<IInputBackend> CreateWin32InputBackend(MaaController* ctrl, std::string controller_type)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=win32";
    return std::make_unique<Win32InputBackend>(ctrl, std::move(controller_type));
}

} // namespace mapnavigator::backend::win32
