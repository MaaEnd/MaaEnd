#include "interception_keyboard.h"

// winsock2.h 必须先于 windows.h：windows.h 会拉入旧版 winsock.h，
// 与 MaaUtils 依赖链里的 winsock2.h 产生 struct 重定义冲突。
#include <winsock2.h>
#include <windows.h>

#include <cstring>

#include <MaaUtils/Logger.h>

namespace mapnavigator::backend::desktop
{

namespace
{

// interception.dll 的最小动态绑定。不引入官方 SDK 头/库，避免给构建增加依赖；
// 需要的只是 4 个函数，签名直接从拦截库文档抄录。
void* g_icpt_dll = nullptr;

using PFN_create_context = void* (*)();
using PFN_destroy_context = void (*)(void*);
using PFN_is_keyboard = int (*)(int);
using PFN_is_mouse = int (*)(int);
using PFN_set_filter = void (*)(void*, unsigned short (*)(int), unsigned short);
using PFN_send = unsigned int (*)(void*, int, const void*, unsigned int);

PFN_create_context p_create_context = nullptr;
PFN_destroy_context p_destroy_context = nullptr;
PFN_is_keyboard p_is_keyboard = nullptr;
PFN_is_mouse p_is_mouse = nullptr;
PFN_set_filter p_set_filter = nullptr;
PFN_send p_send = nullptr;

constexpr unsigned short kFilterKeyAll = 0xFFFF;
constexpr unsigned short kFilterKeyDown = 0x00;
constexpr unsigned short kFilterKeyUp = 0x01;

// InterceptionKeyStroke: { unsigned short code; unsigned short state; unsigned int information; }
#pragma pack(push, 1)
struct InterceptionKeyStroke
{
    unsigned short code;
    unsigned short state;
    unsigned int information;
};

// InterceptionMouseStroke: { unsigned short state; unsigned short flags; short rolling;
//                            int x; int y; unsigned int information; }
struct InterceptionMouseStroke
{
    unsigned short state;
    unsigned short flags;
    short rolling;
    int x;
    int y;
    unsigned int information;
};
#pragma pack(pop)

// MouseStroke 标志位（来自 interception.h）
constexpr unsigned short kMouseMoveRelative = 0x0000;

bool IsKeyboardDevice(int device)
{
    return p_is_keyboard != nullptr && p_is_keyboard(device) != 0;
}

bool IsMouseDevice(int device)
{
    return p_is_mouse != nullptr && p_is_mouse(device) != 0;
}

/**
 * VK 码 → Set-1 扫描码。
 *
 * Interception 要求的是硬件扫描码而不是 VK 码，这里用 MapVirtualKeyA
 * 做转换，并保留扩展键标志（0xE0 前缀由驱动内部处理，我们只标记 extended）。
 */
bool VkToScanCode(int vk, unsigned short& scan, bool& extended)
{
    extended = false;
    switch (vk) {
    case VK_SHIFT:
    case VK_LSHIFT:
        scan = 0x2A;
        return true;
    case VK_RSHIFT:
        scan = 0x36;
        return true;
    case VK_CONTROL:
    case VK_LCONTROL:
        scan = 0x1D;
        return true;
    case VK_RCONTROL:
        scan = 0x1D;
        extended = true;
        return true;
    case VK_MENU:
    case VK_LMENU:
        scan = 0x38;
        return true;
    case VK_RMENU:
        scan = 0x38;
        extended = true;
        return true;
    default:
        break;
    }

    UINT mapped = MapVirtualKeyA(static_cast<UINT>(vk), MAPVK_VK_TO_VSC);
    if (mapped == 0) {
        return false;
    }
    // 方向键/删除/Home 等扩展键的扫描码最高位会被置 0xE0，
    // MapVirtualKey 对这部分返回的是带 0xE0 的值，需要拆出来。
    if (mapped & 0xE000) {
        extended = true;
        mapped &= 0xFF;
    }
    scan = static_cast<unsigned short>(mapped);
    return true;
}

} // namespace

InterceptionKeyboard& InterceptionKeyboard::Instance()
{
    static InterceptionKeyboard instance;
    return instance;
}

InterceptionKeyboard::InterceptionKeyboard()
{
    Initialize();
}

InterceptionKeyboard::~InterceptionKeyboard()
{
    Destroy();
}

bool InterceptionKeyboard::Initialize()
{
    g_icpt_dll = static_cast<void*>(LoadLibraryA("interception.dll"));
    if (g_icpt_dll == nullptr) {
        unavailable_reason_ = "interception.dll 未找到（需放在进程搜索路径）";
        LogWarn << "[InterceptionKeyboard] " << unavailable_reason_;
        return false;
    }

    p_create_context = reinterpret_cast<PFN_create_context>(
        reinterpret_cast<void*>(GetProcAddress(static_cast<HMODULE>(g_icpt_dll), "interception_create_context")));
    p_destroy_context = reinterpret_cast<PFN_destroy_context>(
        reinterpret_cast<void*>(GetProcAddress(static_cast<HMODULE>(g_icpt_dll), "interception_destroy_context")));
    p_is_keyboard = reinterpret_cast<PFN_is_keyboard>(
        reinterpret_cast<void*>(GetProcAddress(static_cast<HMODULE>(g_icpt_dll), "interception_is_keyboard")));
    p_is_mouse = reinterpret_cast<PFN_is_mouse>(
        reinterpret_cast<void*>(GetProcAddress(static_cast<HMODULE>(g_icpt_dll), "interception_is_mouse")));
    p_set_filter = reinterpret_cast<PFN_set_filter>(
        reinterpret_cast<void*>(GetProcAddress(static_cast<HMODULE>(g_icpt_dll), "interception_set_filter")));
    p_send = reinterpret_cast<PFN_send>(
        reinterpret_cast<void*>(reinterpret_cast<void*>(GetProcAddress(static_cast<HMODULE>(g_icpt_dll), "interception_send"))));

    if (!p_create_context || !p_destroy_context || !p_is_keyboard || !p_set_filter || !p_send) {
        unavailable_reason_ = "interception.dll 缺少必需导出函数";
        LogWarn << "[InterceptionKeyboard] " << unavailable_reason_;
        return false;
    }

    context_ = p_create_context();
    if (context_ == nullptr) {
        unavailable_reason_ = "interception_create_context 失败（驱动未加载，需重启系统）";
        LogWarn << "[InterceptionKeyboard] " << unavailable_reason_;
        return false;
    }

    for (int device = 1; device <= 20; ++device) {
        if (IsKeyboardDevice(device)) {
            keyboard_device_ = device;
            break;
        }
    }
    if (keyboard_device_ == 0) {
        unavailable_reason_ = "未找到可注入的键盘设备";
        LogWarn << "[InterceptionKeyboard] " << unavailable_reason_;
        return false;
    }

    // 鼠标设备可选：没有鼠标设备时键盘功能仍可用，转向会回退到框架通路。
    if (p_is_mouse != nullptr) {
        for (int device = 1; device <= 20; ++device) {
            if (p_is_mouse(device) != 0) {
                mouse_device_ = device;
                break;
            }
        }
    }

    // 只接管键盘：鼠标留给 MaaController（AnchoredTouch / 消息注入），
    // 避免与框架的鼠标通路互相抢占。
    // interception_set_filter 需要 int(*)(int) 形式的判定函数；
    // IsKeyboardDevice 内部读的是函数指针，这里用无捕获 lambda 包一层保证签名匹配。
    // 同时接管键盘与鼠标：驱动相对移动报文是实现「后台视角转向」的前提。
    unsigned short device_mask = 0;
    if (keyboard_device_ > 0 && keyboard_device_ <= 16) {
        device_mask |= static_cast<unsigned short>(1 << (keyboard_device_ - 1));
    }
    if (mouse_device_ > 0 && mouse_device_ <= 16) {
        device_mask |= static_cast<unsigned short>(1 << (mouse_device_ - 1));
    }
    p_set_filter(
        context_,
        [](int device) -> unsigned short {
            return (IsKeyboardDevice(device) || IsMouseDevice(device)) ? 1 : 0;
        },
        device_mask);

    available_ = true;
    LogInfo << "[InterceptionKeyboard] 驱动就绪, keyboard_device=" << keyboard_device_;
    return true;
}

void InterceptionKeyboard::Destroy()
{
    ReleaseAll();
    if (context_ != nullptr && p_destroy_context != nullptr) {
        p_destroy_context(context_);
        context_ = nullptr;
    }
    if (g_icpt_dll != nullptr) {
        FreeLibrary(static_cast<HMODULE>(g_icpt_dll));
        g_icpt_dll = nullptr;
    }
    available_ = false;
}

bool InterceptionKeyboard::SendKey(int vk_code, bool up)
{
    if (!available_ || context_ == nullptr || keyboard_device_ == 0) {
        return false;
    }
    if (vk_code < 0 || vk_code > 255) {
        return false;
    }

    unsigned short scan = 0;
    bool extended = false;
    if (!VkToScanCode(vk_code, scan, extended)) {
        LogWarn << "[InterceptionKeyboard] 无法转换按键" << VAR(vk_code);
        return false;
    }

    InterceptionKeyStroke stroke {};
    stroke.code = scan;
    stroke.state = up ? kFilterKeyUp : kFilterKeyDown;
    // 扩展键标志：Interception 用 information 字段的高位携带 0xE0 前缀信息
    stroke.information = extended ? 0xE000 : 0;

    const unsigned int sent = p_send(context_, keyboard_device_, &stroke, 1);
    if (sent != 1) {
        LogWarn << "[InterceptionKeyboard] 注入失败" << VAR(vk_code) << VAR(scan) << VAR(sent);
        return false;
    }
    return true;
}

bool InterceptionKeyboard::KeyDown(int vk_code)
{
    if (!SendKey(vk_code, false)) {
        return false;
    }
    if (vk_code >= 0 && vk_code <= 255) {
        held_keys_[vk_code] = true;
    }
    return true;
}

bool InterceptionKeyboard::KeyUp(int vk_code)
{
    if (!SendKey(vk_code, true)) {
        return false;
    }
    if (vk_code >= 0 && vk_code <= 255) {
        held_keys_[vk_code] = false;
    }
    return true;
}

bool InterceptionKeyboard::MouseRelativeMove(int dx, int dy)
{
    if (!available_ || context_ == nullptr || mouse_device_ == 0 || p_send == nullptr) {
        return false;
    }
    if (dx == 0 && dy == 0) {
        return true;
    }

    InterceptionMouseStroke stroke {};
    stroke.state = 0;
    stroke.flags = kMouseMoveRelative;
    stroke.rolling = 0;
    stroke.x = dx;
    stroke.y = dy;
    stroke.information = 0;

    const unsigned int sent = p_send(context_, mouse_device_, &stroke, 1);
    if (sent != 1) {
        LogWarn << "[InterceptionKeyboard] 鼠标相对移动失败" << VAR(dx) << VAR(dy) << VAR(sent);
        return false;
    }
    return true;
}

void InterceptionKeyboard::ReleaseAll()
{
    if (!available_) {
        return;
    }
    for (int vk = 0; vk < 256; ++vk) {
        if (held_keys_[vk]) {
            SendKey(vk, true);
            held_keys_[vk] = false;
        }
    }
}

} // namespace mapnavigator::backend::desktop
