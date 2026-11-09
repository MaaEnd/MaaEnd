#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../../control/controller_info_utils.h"
#include "../../navi_config.h"
#include "../../navi_time.h"
#include "../backend.h"
#include "desktop_input_backend.h"

namespace mapnavigator::backend::desktop
{

namespace
{

constexpr int32_t kDesktopDefaultHoverAnchorX = kWorkWidth / 2;
constexpr int32_t kDesktopDefaultHoverAnchorY = kWorkHeight / 2;
constexpr int32_t kDesktopHoverTouchContactId = 0;
constexpr int32_t kDesktopPrimaryTouchContactId = 1;
constexpr int32_t kDesktopDefaultTouchPressure = 0;

bool is_mouse_lock_follow_supported(MaaController* ctrl)
{
#ifdef _WIN32
    MaaWin32InputMethod method = MaaWin32InputMethod_None;
    return TryGetWin32MouseInputMethod(ctrl, &method) && IsMessageInputMethod(method);
#else
    (void)ctrl;
    return false;
#endif
}

bool try_set_mouse_lock_follow(MaaController* ctrl, bool enabled)
{
    return ctrl != nullptr && MaaControllerSetOption(ctrl, MaaCtrlOption_MouseLockFollow, &enabled, sizeof(enabled));
}

bool try_set_background_managed_keys(MaaController* ctrl, const std::vector<int32_t>& keycodes)
{
    return ctrl != nullptr
           && MaaControllerSetOption(
               ctrl,
               MaaCtrlOption_BackgroundManagedKeys,
               keycodes.empty() ? nullptr : const_cast<int32_t*>(keycodes.data()),
               static_cast<MaaOptionValueSize>(sizeof(int32_t) * keycodes.size()));
}

struct ViewScale
{
    double yaw_units_per_degree = 0.0;
    double pitch_range_units = 0.0;
};

ViewScale compute_view_scale(MaaController* ctrl, const std::string& backend_name)
{
    int32_t screen_width = 0;
    int32_t screen_height = 0;
    if (!MaaControllerGetResolution(ctrl, &screen_width, &screen_height) || screen_width <= 0 || screen_height <= 0) {
        LogWarn << backend_name << " backend: failed to get controller resolution, fallback to default turn scale." << VAR(screen_width)
                << VAR(screen_height);
        return ViewScale { ComputeDefaultUnitsPerDegree(), ComputeDefaultPitchRangeUnits() };
    }

    const int turn360 = ComputeTurn360Units(screen_width);
    const double units_per_degree = ComputeUnitsPerDegreeForWidth(screen_width);
    const double pitch_range_units = ComputePitchRangeUnitsForHeight(screen_height);
    LogInfo << "Computed turn scale from raw controller resolution." << VAR(backend_name) << VAR(screen_width) << VAR(screen_height)
            << VAR(turn360) << VAR(units_per_degree) << VAR(pitch_range_units);
    return ViewScale { units_per_degree, pitch_range_units };
}

// Win32 虚拟键码, Linux 控制器开 use_win32_vk_code 后由 MaaFramework 转成 evdev 码
DesktopKeyCodes make_desktop_key_codes()
{
    return DesktopKeyCodes {
        .move_forward = 'W',
        .move_left = 'A',
        .move_backward = 'S',
        .move_right = 'D',
        .interact = 'F',
        .jump = 0x20,        // VK_SPACE
        .walk_toggle = 0x11, // VK_CONTROL
    };
}

bool* find_movement_key_state(DesktopBackend* desktop, int key_code)
{
    if (key_code == desktop->key_codes.move_forward) {
        return &desktop->forward_down;
    }
    if (key_code == desktop->key_codes.move_left) {
        return &desktop->left_down;
    }
    if (key_code == desktop->key_codes.move_backward) {
        return &desktop->backward_down;
    }
    if (key_code == desktop->key_codes.move_right) {
        return &desktop->right_down;
    }
    return nullptr;
}

void ensure_hover_anchor_sync(InputBackend* backend)
{
    if (backend->desktop.hover_inited) {
        return;
    }

    backend->desktop.hover_inited = true;
    const MaaCtrlId ctrl_id = MaaControllerPostTouchMove(
        backend->ctrl,
        kDesktopHoverTouchContactId,
        backend->desktop.hover_x,
        backend->desktop.hover_y,
        kDesktopDefaultTouchPressure);
    MaaControllerWait(backend->ctrl, ctrl_id);
}

void mouse_right_down_sync(InputBackend* backend, int delay_millis)
{
    ensure_hover_anchor_sync(backend);
    const MaaCtrlId ctrl_id = MaaControllerPostTouchDown(
        backend->ctrl,
        kDesktopPrimaryTouchContactId,
        backend->desktop.hover_x,
        backend->desktop.hover_y,
        kDesktopDefaultTouchPressure);
    MaaControllerWait(backend->ctrl, ctrl_id);
    SleepFor(delay_millis);
}

void mouse_right_up_sync(const InputBackend& backend, int delay_millis)
{
    const MaaCtrlId ctrl_id = MaaControllerPostTouchUp(backend.ctrl, kDesktopPrimaryTouchContactId);
    MaaControllerWait(backend.ctrl, ctrl_id);
    SleepFor(delay_millis);
}

void post_key_down_sync(InputBackend* backend, int key_code, int delay_millis)
{
    if (bool* movement_state = find_movement_key_state(&backend->desktop, key_code)) {
        *movement_state = true;
    }

    // 键盘输入法为 Interception 时绕开 MaaFramework 直连驱动：框架的
    // InterceptionInput::key_down 会 ensure_foreground()，每次按键都把游戏窗口拉到前台，
    // 后台挂机无法成立。其它输入法保持上游路径不变。
    // MaaWin32InputMethod 只在 Windows 资源里定义，与上游 is_mouse_lock_follow_supported
    // 等调用点一致，必须整段包在 _WIN32 内，否则 Linux/macOS/Android 编译失败。
#ifdef _WIN32
    MaaWin32InputMethod kb_method {};
    if (TryGetWin32KeyboardInputMethod(backend->ctrl, &kb_method)
        && kb_method == MaaWin32InputMethod_Interception
        && InterceptionKeyboard::Instance().KeyDown(key_code)) {
        SleepFor(delay_millis);
        return;
    }
#endif

    const MaaCtrlId ctrl_id = MaaControllerPostKeyDown(backend->ctrl, key_code);
    MaaControllerWait(backend->ctrl, ctrl_id);
    SleepFor(delay_millis);
}

void post_key_up_sync(InputBackend* backend, int key_code, int delay_millis)
{
    if (bool* movement_state = find_movement_key_state(&backend->desktop, key_code)) {
        *movement_state = false;
    }

#ifdef _WIN32
    MaaWin32InputMethod kb_method {};
    if (TryGetWin32KeyboardInputMethod(backend->ctrl, &kb_method)
        && kb_method == MaaWin32InputMethod_Interception
        && InterceptionKeyboard::Instance().KeyUp(key_code)) {
        SleepFor(delay_millis);
        return;
    }
#endif

    const MaaCtrlId ctrl_id = MaaControllerPostKeyUp(backend->ctrl, key_code);
    MaaControllerWait(backend->ctrl, ctrl_id);
    SleepFor(delay_millis);
}

void click_key_sync(InputBackend* backend, int key_code, int hold_millis)
{
    post_key_down_sync(backend, key_code, 0);
    SleepFor(hold_millis);
    post_key_up_sync(backend, key_code, 0);
}

void apply_movement_key_state(InputBackend* backend, int key_code, bool pressed)
{
    bool* current_state = find_movement_key_state(&backend->desktop, key_code);
    if (current_state == nullptr || *current_state == pressed) {
        return;
    }

    if (pressed) {
        post_key_down_sync(backend, key_code, 0);
        return;
    }
    post_key_up_sync(backend, key_code, 0);
}

} // namespace

InputBackend MakeDesktopInputBackend(BackendKind kind, MaaController* ctrl, std::string controller_type, std::string backend_name)
{
    InputBackend backend;
    backend.kind = kind;
    backend.ctrl = ctrl;
    backend.controller_type = std::move(controller_type);
    DesktopBackend& desktop = backend.desktop;
    desktop.backend_name = std::move(backend_name);
    desktop.key_codes = make_desktop_key_codes();
    desktop.hover_x = kDesktopDefaultHoverAnchorX;
    desktop.hover_y = kDesktopDefaultHoverAnchorY;

    const ViewScale view_scale = compute_view_scale(ctrl, desktop.backend_name);
    backend.default_turn_units_per_degree = view_scale.yaw_units_per_degree;
    backend.default_pitch_range_units = view_scale.pitch_range_units;

    if (backend.ctrl == nullptr) {
        backend.unsupported_reason = "controller handle is null";
        return backend;
    }

    if (is_mouse_lock_follow_supported(backend.ctrl)) {
        desktop.mouse_lock_follow_enabled = try_set_mouse_lock_follow(backend.ctrl, true);
        LogInfo << desktop.backend_name << " backend mouse lock follow." << VAR(backend.controller_type)
                << VAR(desktop.mouse_lock_follow_enabled);
    }
    else {
        LogInfo << desktop.backend_name << " backend mouse lock follow skipped." << VAR(backend.controller_type);
    }

    const std::vector<int32_t> managed_keys {
        desktop.key_codes.move_forward, desktop.key_codes.move_left, desktop.key_codes.move_backward, desktop.key_codes.move_right,
        desktop.key_codes.interact,     desktop.key_codes.jump,      desktop.key_codes.walk_toggle,
    };
    desktop.background_managed_keys_enabled = try_set_background_managed_keys(backend.ctrl, managed_keys);
    LogInfo << desktop.backend_name << " backend background managed keys." << VAR(backend.controller_type)
            << VAR(desktop.background_managed_keys_enabled);
    return backend;
}

InputBackend OpenDesktopInputBackend(MaaController* ctrl, std::string controller_type, std::string backend_name)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << VAR(backend_name);
    return MakeDesktopInputBackend(BackendKind::Desktop, ctrl, std::move(controller_type), std::move(backend_name));
}

void CloseDesktopInputBackend(const InputBackend& backend)
{
    if (backend.desktop.mouse_lock_follow_enabled && !try_set_mouse_lock_follow(backend.ctrl, false)) {
        LogWarn << backend.desktop.backend_name << " backend: failed to disable mouse lock follow." << VAR(backend.controller_type);
    }
    if (backend.desktop.background_managed_keys_enabled && !try_set_background_managed_keys(backend.ctrl, {})) {
        LogWarn << backend.desktop.backend_name << " backend: failed to clear background managed keys." << VAR(backend.controller_type);
    }
}

SteeringTransportProfile DesktopSteeringProfile()
{
    return SteeringTransportProfile {
        .supports_concurrent_move_and_look = true,
        .min_send_interval_ms = 0,
        .min_emit_delta_deg = 1.0,
        .max_batch_delta_deg = 30.0,
        .action_quiet_period_ms = 0,
    };
}

void SetDesktopMovementStateSync(InputBackend* backend, MovementState state, int delay_millis)
{
    apply_movement_key_state(backend, backend->desktop.key_codes.move_forward, state.forward);
    apply_movement_key_state(backend, backend->desktop.key_codes.move_left, state.left);
    apply_movement_key_state(backend, backend->desktop.key_codes.move_backward, state.backward);
    apply_movement_key_state(backend, backend->desktop.key_codes.move_right, state.right);
    SleepFor(delay_millis);
}

void TriggerDesktopJumpSync(InputBackend* backend, int hold_millis)
{
    click_key_sync(backend, backend->desktop.key_codes.jump, hold_millis);
}

void TriggerDesktopInteractSync(InputBackend* backend, int hold_millis)
{
    click_key_sync(backend, backend->desktop.key_codes.interact, hold_millis);
}

void PulseDesktopForwardSync(InputBackend* backend, int hold_millis)
{
    post_key_down_sync(backend, backend->desktop.key_codes.move_forward, 0);
    SleepFor(hold_millis);
    post_key_up_sync(backend, backend->desktop.key_codes.move_forward, 0);
}

void TriggerDesktopSprintSync(InputBackend* backend)
{
    mouse_right_down_sync(backend, 0);
    SleepFor(kActionSprintPressMs);
    mouse_right_up_sync(*backend, 0);
}

void ToggleDesktopWalkModeSync(InputBackend* backend)
{
    click_key_sync(backend, backend->desktop.key_codes.walk_toggle, kActionWalkTogglePressMs);
}

void ResetDesktopForwardWalkSync(InputBackend* backend, int release_millis)
{
    post_key_up_sync(backend, backend->desktop.key_codes.move_forward, 0);
    SleepFor(release_millis);
    post_key_down_sync(backend, backend->desktop.key_codes.move_forward, 0);
}

void ClickDesktopMouseLeftSync(InputBackend* backend)
{
    ensure_hover_anchor_sync(backend);
    const MaaCtrlId ctrl_id = MaaControllerPostClick(backend->ctrl, backend->desktop.hover_x, backend->desktop.hover_y);
    MaaControllerWait(backend->ctrl, ctrl_id);
}

void TriggerDesktopZiplineLaunchSync(InputBackend* backend)
{
    ClickDesktopMouseLeftSync(backend);
}

void TriggerDesktopZiplineDismountSync(InputBackend* backend, int hold_millis)
{
    mouse_right_down_sync(backend, hold_millis);
    mouse_right_up_sync(*backend, 0);
}

bool SendDesktopViewDeltaSync(const InputBackend& backend, int dx, int dy)
{
    if (dx == 0 && dy == 0) {
        return true;
    }

    if (backend.ctrl == nullptr) {
        return false;
    }

    // 鼠标输入法为 Interception 时直连驱动：MaaFramework 的 InterceptionInput 至今没有实现
    // RelativeMoveInput，MaaControllerPostRelativeMove 会直接失败，转视角随之失效
    // （表现是角色朝错误方向走、甚至走进水里）。
#ifdef _WIN32
    MaaWin32InputMethod mouse_method {};
    if (TryGetWin32MouseInputMethod(backend.ctrl, &mouse_method)
        && mouse_method == MaaWin32InputMethod_Interception
        && InterceptionKeyboard::Instance().MouseRelativeMove(dx, dy)) {
        LogInfo << "SendRelativeMoveInterception" << VAR(dx) << VAR(dy);
        return true;
    }
#endif

    LogInfo << "SendRelativeMoveNative" << VAR(dx) << VAR(dy);
    const MaaCtrlId ctrl_id = MaaControllerPostRelativeMove(backend.ctrl, dx, dy);
    if (ctrl_id == MaaInvalidId) {
        return false;
    }

    return MaaControllerWait(backend.ctrl, ctrl_id) == MaaStatus_Succeeded;
}

} // namespace mapnavigator::backend::desktop
