#pragma once

#include <memory>
#include <string>

#include "../backend.h"

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
};

class DesktopInputBackend : public IInputBackend
{
public:
    DesktopInputBackend(MaaController* ctrl, std::string controller_type, std::string backend_name, DesktopKeyCodes key_codes);

    MaaController* GetCtrl() const override;
    const std::string& controller_type() const override;
    bool uses_touch_backend() const override;
    bool is_supported() const override;
    const std::string& unsupported_reason() const override;
    double default_turn_units_per_degree() const override;
    SteeringTransportProfile steering_transport_profile() const override;

    bool supports_sprint() const override { return true; }

    void SetMovementStateSync(bool forward, bool left, bool backward, bool right, int delay_millis) override;
    void TriggerJumpSync(int hold_millis) override;
    void TriggerInteractSync(int hold_millis) override;
    void PulseForwardSync(int hold_millis) override;
    void TriggerSprintSync() override;
    void ResetForwardWalkSync(int release_millis) override;
    void ClickMouseLeftSync() override;
    void MouseRightDownSync(int delay_millis) override;
    void MouseRightUpSync(int delay_millis) override;
    virtual bool SendViewDeltaSync(int dx, int dy) override;

protected:
    static void SleepIfNeeded(int delay_millis);

private:
    void EnsureHoverAnchorSync();
    void PostKeyDownSync(int key_code, int delay_millis);
    void PostKeyUpSync(int key_code, int delay_millis);
    void ClickKeySync(int key_code, int hold_millis);
    void ApplyMovementKeyState(int key_code, bool pressed);
    bool* FindMovementKeyState(int key_code);

    MaaController* ctrl_ = nullptr;
    std::string controller_type_;
    std::string backend_name_;
    std::string unsupported_reason_;
    DesktopKeyCodes key_codes_ {};
    double default_turn_units_per_degree_ = 0.0;
    int hover_x_ = 0;
    int hover_y_ = 0;
    bool hover_inited_ = false;
    bool forward_down_ = false;
    bool left_down_ = false;
    bool backward_down_ = false;
    bool right_down_ = false;
};

// 返回终末地默认绑定下的 Win32 Virtual-Key 键码表。
// Win32 控制器原生使用 VK 码；WlRoots 控制器在 interface.json 中开启
// use_win32_vk_code，MaaFramework 会将这些码翻译为 Linux evdev 码。
DesktopKeyCodes MakeDesktopKeyCodes();

// 构造以 VK 码为语义的桌面端（Win32 / WlRoots）输入后端。
// backend_name 仅用于日志区分（"win32" / "wlroots"）。
std::unique_ptr<IInputBackend>
    CreateDesktopInputBackend(MaaController* ctrl, std::string controller_type, std::string backend_name);

} // namespace mapnavigator::backend::desktop
