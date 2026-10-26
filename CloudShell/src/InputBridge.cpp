#include "InputBridge.h"

#include <format>

#include <windowsx.h>
#include <wrl.h>

#include "KeyMap.h"
#include "Log.h"
#include "Resources.h"

namespace cloudshell
{

namespace
{

constexpr int kModifierAlt = 1;
constexpr int kModifierCtrl = 2;
constexpr int kModifierMeta = 4;
constexpr int kModifierShift = 8;

constexpr int kButtonLeft = 1;
constexpr int kButtonRight = 2;
constexpr int kButtonMiddle = 4;
constexpr int kButtonBack = 8;
constexpr int kButtonForward = 16;

constexpr UINT_PTR kWatchdogTimer = 1;
// 页面对话框或渲染进程卡死时 CDP 命令可能永远不返回，超时就丢弃，后面的输入才能继续
constexpr UINT kCommandTimeoutMs = 3000;

std::string jsonEscape(std::string_view src)
{
    std::string out;
    out.reserve(src.size() + 2);
    for (const char ch : src) {
        if (ch == '"' || ch == '\\') {
            out += '\\';
            out += ch;
        }
        else if (static_cast<unsigned char>(ch) < 0x20) {
            out += std::format("\\u{:04x}", static_cast<unsigned char>(ch));
        }
        else {
            out += ch;
        }
    }
    return out;
}

const char* buttonName(int button)
{
    switch (button) {
    case kButtonLeft:
        return "left";
    case kButtonRight:
        return "right";
    case kButtonMiddle:
        return "middle";
    case kButtonBack:
        return "back";
    case kButtonForward:
        return "forward";
    default:
        return "none";
    }
}

// 系统交换了左右键时 MaaFramework 把 contact 0 投递成右键消息，换回来
int unswapButton(int button)
{
    if (GetSystemMetrics(SM_SWAPBUTTON) == 0) {
        return button;
    }
    if (button == kButtonLeft) {
        return kButtonRight;
    }
    if (button == kButtonRight) {
        return kButtonLeft;
    }
    return button;
}

// 页面里的 KeyboardEvent.keyCode 对左右修饰键报的是泛指键码
unsigned genericVirtualKey(unsigned vk)
{
    switch (vk) {
    case VK_LSHIFT:
    case VK_RSHIFT:
        return VK_SHIFT;
    case VK_LCONTROL:
    case VK_RCONTROL:
        return VK_CONTROL;
    case VK_LMENU:
    case VK_RMENU:
        return VK_MENU;
    default:
        return vk;
    }
}

int modifierBit(unsigned vk)
{
    switch (genericVirtualKey(vk)) {
    case VK_SHIFT:
        return kModifierShift;
    case VK_CONTROL:
        return kModifierCtrl;
    case VK_MENU:
        return kModifierAlt;
    case VK_LWIN:
    case VK_RWIN:
        return kModifierMeta;
    default:
        return 0;
    }
}

} // namespace

InputBridge::InputBridge(HWND hwnd)
    : hwnd_(hwnd)
{
}

void InputBridge::Attach(ICoreWebView2* webview)
{
    webview_ = webview;
}

void InputBridge::Detach()
{
    KillTimer(hwnd_, kWatchdogTimer);
    webview_.Reset();
    queue_.clear();
}

std::optional<LRESULT> InputBridge::HandleMessage(UINT msg, WPARAM wparam, LPARAM lparam)
{
    switch (msg) {
    case WM_MOUSEMOVE:
        // 控制器投递的移动总带着按键标志；不带的是偶尔落到顶层窗口上的真实鼠标移动
        if (wparam & (MK_LBUTTON | MK_RBUTTON | MK_MBUTTON | MK_XBUTTON1 | MK_XBUTTON2)) {
            onMouseMove(lparam);
        }
        return 0;

    case WM_TIMER:
        if (wparam != kWatchdogTimer) {
            return std::nullopt;
        }
        onWatchdog();
        return 0;

    case WM_LBUTTONDOWN:
    case WM_LBUTTONUP:
        onMouseButton(kButtonLeft, msg == WM_LBUTTONDOWN, lparam);
        return 0;
    case WM_RBUTTONDOWN:
    case WM_RBUTTONUP:
        onMouseButton(kButtonRight, msg == WM_RBUTTONDOWN, lparam);
        return 0;
    case WM_MBUTTONDOWN:
    case WM_MBUTTONUP:
        onMouseButton(kButtonMiddle, msg == WM_MBUTTONDOWN, lparam);
        return 0;
    case WM_XBUTTONDOWN:
    case WM_XBUTTONUP:
        onMouseButton(GET_XBUTTON_WPARAM(wparam) == XBUTTON1 ? kButtonBack : kButtonForward, msg == WM_XBUTTONDOWN, lparam);
        return TRUE;

    case WM_MOUSEWHEEL:
        onMouseWheel(false, wparam, lparam);
        return 0;
    case WM_MOUSEHWHEEL:
        onMouseWheel(true, wparam, lparam);
        return 0;

    case WM_KEYDOWN:
    case WM_KEYUP:
        onKey(msg == WM_KEYDOWN, wparam, lparam);
        return 0;
    case WM_CHAR:
        onChar(wparam, lparam);
        return 0;

    default:
        return std::nullopt;
    }
}

void InputBridge::SetForeground(bool foreground)
{
    if (foreground_ == foreground) {
        return;
    }
    foreground_ = foreground;
    log::Info("foreground={}", foreground_);
    SyncPageState();
}

void InputBridge::SyncPageState()
{
    evaluate(std::format("window.__maaShellSetForeground&&window.__maaShellSetForeground({})", foreground_));
}

void InputBridge::onMouseButton(int raw_button, bool down, LPARAM lparam)
{
    const int button = unswapButton(raw_button);

    // 抬起消息带的是控制器全局最后一次的坐标，不一定属于这个按键，沿用当前指针位置
    if (down) {
        pointer_ = { GET_X_LPARAM(lparam), GET_Y_LPARAM(lparam) };
        buttons_ |= button;
    }
    else {
        buttons_ &= ~button;
    }

    log::Info("mouse {} {} ({}, {})", buttonName(button), down ? "down" : "up", pointer_.x, pointer_.y);
    dispatchMouse(down ? "mousePressed" : "mouseReleased", buttonName(button), 1);
}

void InputBridge::onMouseMove(LPARAM lparam)
{
    pointer_ = { GET_X_LPARAM(lparam), GET_Y_LPARAM(lparam) };
    // button 取按住的第一个键，即最低位
    dispatchMouse("mouseMoved", buttonName(buttons_ & -buttons_), 0);
}

void InputBridge::onMouseWheel(bool horizontal, WPARAM wparam, LPARAM lparam)
{
    // 滚轮消息的坐标是屏幕坐标
    POINT point { GET_X_LPARAM(lparam), GET_Y_LPARAM(lparam) };
    if (ScreenToClient(hwnd_, &point)) {
        pointer_ = point;
    }

    // 滚轮正值向上，CDP 的 deltaY 正值向下；水平方向一致
    const int delta = GET_WHEEL_DELTA_WPARAM(wparam);
    log::Info("wheel horizontal={} delta={} ({}, {})", horizontal, delta, pointer_.x, pointer_.y);
    dispatchMouse("mouseWheel", "none", 0, horizontal ? delta : 0, horizontal ? 0 : -delta);
}

void InputBridge::onKey(bool down, WPARAM wparam, LPARAM lparam)
{
    const auto vk = static_cast<unsigned>(wparam);
    const bool extended = ((lparam >> 24) & 1) != 0;
    const auto info = LookupKey(vk, extended);
    if (!info) {
        log::Warn("unsupported key vk={}", vk);
        return;
    }

    if (const int bit = modifierBit(vk); bit != 0) {
        modifiers_ = down ? (modifiers_ | bit) : (modifiers_ & ~bit);
    }

    const std::string& key = (modifiers_ & kModifierShift) ? info->shifted_key : info->key;

    // 文本统一走 WM_CHAR → Input.insertText，按键用不产生文本的 rawKeyDown；
    // 回车例外，表单提交依赖带文本的 keypress
    std::string type = down ? "rawKeyDown" : "keyUp";
    std::string text_fields;
    if (down && vk == VK_RETURN) {
        type = "keyDown";
        text_fields = R"(,"text":"\r","unmodifiedText":"\r")";
    }

    const unsigned js_vk = genericVirtualKey(vk);
    std::string params = std::format(
        R"({{"type":"{}","modifiers":{},"windowsVirtualKeyCode":{},"nativeVirtualKeyCode":{},"code":"{}","key":"{}","location":{}{}{}}})",
        type,
        modifiers_,
        js_vk,
        js_vk,
        jsonEscape(info->code),
        jsonEscape(key),
        info->location,
        info->location == 3 ? R"(,"isKeypad":true)" : "",
        text_fields);

    log::Info("key vk={} {} {}", vk, down ? "down" : "up", info->code);
    // 页面策略会在 keydown 里补发 mouseenter，SDK 可能随之申请指针锁定，先记为控制器输入
    if (down) {
        evaluate("window.__maaShellExpect&&window.__maaShellExpect()");
    }
    enqueue("Input.dispatchKeyEvent", std::move(params));
}

void InputBridge::onChar(WPARAM wparam, LPARAM lparam)
{
    // TranslateMessage 会把投递的 WM_KEYDOWN 再翻译出带扫描码的 WM_CHAR，与 keydown 重复；
    // 控制器 input_text 投递的 WM_CHAR lParam 为 0
    if (lparam != 0) {
        return;
    }

    const auto ch = static_cast<wchar_t>(wparam);
    std::wstring text;
    if (IS_HIGH_SURROGATE(ch)) {
        pending_high_surrogate_ = ch;
        return;
    }
    if (IS_LOW_SURROGATE(ch)) {
        if (pending_high_surrogate_ == 0) {
            return;
        }
        text = { pending_high_surrogate_, ch };
    }
    else if (ch >= 0x20 && ch != 0x7f) {
        text = ch;
    }
    pending_high_surrogate_ = 0;

    if (text.empty()) {
        return;
    }

    enqueue("Input.insertText", std::format(R"({{"text":"{}"}})", jsonEscape(WideToUtf8(text))));
}

void InputBridge::dispatchMouse(std::string_view type, std::string_view button, int click_count, int delta_x, int delta_y)
{
    std::string params = std::format(
        R"({{"type":"{}","x":{},"y":{},"button":"{}","buttons":{},"clickCount":{},"modifiers":{})",
        type,
        pointer_.x,
        pointer_.y,
        button,
        buttons_,
        click_count,
        modifiers_);
    if (type == "mouseWheel") {
        params += std::format(R"(,"deltaX":{},"deltaY":{})", delta_x, delta_y);
    }
    params += '}';

    // 拖动时控制器每 10ms 投递一次移动，渲染进程按帧确认，命令会越积越多；还没发出的移动只保留最新的
    const bool move = type == "mouseMoved";
    if (move && queue_.size() >= 2 && queue_.back().move && queue_[queue_.size() - 2].move) {
        queue_.pop_back();
        queue_.pop_back();
    }

    // 页面策略按坐标认出控制器的事件
    const std::string expect = std::format("window.__maaShellExpect&&window.__maaShellExpect({},{})", pointer_.x, pointer_.y);
    queue_.push_back(Command { .method = "Runtime.evaluate", .params = std::format(R"({{"expression":"{}"}})", jsonEscape(expect)), .move = move });
    queue_.push_back(Command { .method = "Input.dispatchMouseEvent", .params = std::move(params), .move = move });
    pump();
}

void InputBridge::evaluate(std::string_view expression)
{
    enqueue("Runtime.evaluate", std::format(R"({{"expression":"{}"}})", jsonEscape(expression)));
}

void InputBridge::enqueue(std::string method, std::string params)
{
    queue_.push_back(Command { .method = std::move(method), .params = std::move(params) });
    pump();
}

void InputBridge::pump()
{
    using Microsoft::WRL::Callback;
    using Handler = ICoreWebView2CallDevToolsProtocolMethodCompletedHandler;

    while (!busy_ && !queue_.empty()) {
        if (!webview_) {
            queue_.clear();
            return;
        }

        Command command = std::move(queue_.front());
        queue_.pop_front();

        const std::wstring method = Utf8ToWide(command.method);
        const std::wstring params = Utf8ToWide(command.params);
        const uint64_t generation = ++generation_;
        busy_ = true;
        SetTimer(hwnd_, kWatchdogTimer, kCommandTimeoutMs, nullptr);
        const HRESULT hr = webview_->CallDevToolsProtocolMethod(
            method.c_str(),
            params.c_str(),
            Callback<Handler>([this, generation, name = command.method](HRESULT error, LPCWSTR) -> HRESULT {
                if (generation != generation_) {
                    return S_OK;
                }
                if (FAILED(error)) {
                    log::Warn("CDP {} failed: 0x{:08x}", name, static_cast<unsigned>(error));
                }
                busy_ = false;
                pump();
                if (!busy_) {
                    KillTimer(hwnd_, kWatchdogTimer);
                }
                return S_OK;
            }).Get());

        if (FAILED(hr)) {
            log::Error("CDP {} dispatch failed: 0x{:08x}", command.method, static_cast<unsigned>(hr));
            busy_ = false;
        }
    }
}

void InputBridge::onWatchdog()
{
    KillTimer(hwnd_, kWatchdogTimer);
    if (!busy_) {
        return;
    }
    log::Warn("CDP command timed out after {}ms, skipped", kCommandTimeoutMs);
    ++generation_;
    busy_ = false;
    pump();
}

} // namespace cloudshell
