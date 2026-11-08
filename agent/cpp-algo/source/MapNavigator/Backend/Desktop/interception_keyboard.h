#pragma once

#include <cstdint>
#include <string>

namespace mapnavigator::backend::desktop
{

/**
 * 通过 Interception 驱动直接注入硬件级键盘扫描码。
 *
 * 为什么需要它：MaaFramework 的 InterceptionInput::key_down/key_up 会调用
 * ensure_foreground_and_topmost()，导致每次按键都把游戏窗口拉到最前，
 * 无法实现「真后台挂机」。在 cpp-algo 侧直接对接驱动，可以得到
 *  - 零窗口置顶
 *  - 零焦点抢占
 *  - 零物理光标干预
 * 的键盘注入通路。
 *
 * 生命周期：进程内单例，首次使用时惰性初始化，驱动不可用时优雅降级
 * （调用方回退到 MaaController 的常规按键接口）。
 */
class InterceptionKeyboard
{
public:
    /// 获取单例。首次调用会尝试加载 interception.dll 并创建上下文。
    static InterceptionKeyboard& Instance();

    ~InterceptionKeyboard();

    InterceptionKeyboard(const InterceptionKeyboard&) = delete;
    InterceptionKeyboard& operator=(const InterceptionKeyboard&) = delete;

    /// 驱动是否可用（dll 已加载 + 上下文已创建 + 键盘设备已找到）。
    bool Available() const { return available_; }

    /// 不可用原因，供日志诊断。
    const std::string& UnavailableReason() const { return unavailable_reason_; }

    /**
     * 按下/抬起一个虚拟键码（VK_*）。
     *
     * 内部会转换为 Set-1 扫描码再交给驱动。修饰键（Shift/Ctrl/Alt）
     * 会自动带上 extended 标志。
     *
     * @return true 表示驱动成功接收；false 表示失败，调用方应回退。
     */
    bool KeyDown(int vk_code);
    bool KeyUp(int vk_code);

    /// 释放本实例按下过但尚未抬起的全部按键（析构/异常路径的兜底）。
    void ReleaseAll();

    /**
     * 发送鼠标相对位移（视角/镜头转动）。
     *
     * 走 Interception 驱动的相对移动报文，不触碰系统光标位置。
     * InterceptionInput 在 MaaFramework 里没有实现 RelativeMoveInput 接口，
     * 导致 cpp-algo 的视角转向在 Interception 下必然失败（角色走错方向）。
     * 这里直接对接驱动补齐这个缺口。
     *
     * @return true 表示驱动成功接收；false 表示失败，调用方应回退。
     */
    bool MouseRelativeMove(int dx, int dy);

private:
    InterceptionKeyboard();

    bool Initialize();
    bool SendKey(int vk_code, bool up);
    void Destroy();

    bool available_ = false;
    std::string unavailable_reason_;
    void* context_ = nullptr;
    int keyboard_device_ = 0;
    int mouse_device_ = 0;
    bool held_keys_[256] = {false};
};

} // namespace mapnavigator::backend::desktop
