#pragma once

#include <cstdint>
#include <deque>
#include <optional>
#include <string>
#include <string_view>

#include <windows.h>
#include <wrl/client.h>

#include <WebView2.h>

namespace cloudshell
{

// 把控制器投递到顶层窗口的鼠标、键盘消息转成 CDP 的 Input 事件
class InputBridge
{
public:
    explicit InputBridge(HWND hwnd);

    void Attach(ICoreWebView2* webview);
    void Detach();

    std::optional<LRESULT> HandleMessage(UINT msg, WPARAM wparam, LPARAM lparam);

    void SetForeground(bool foreground);
    void SyncPageState();

private:
    struct Command
    {
        std::string method;
        std::string params;
        bool move = false;
    };

    void onMouseButton(int raw_button, bool down, LPARAM lparam);
    void onMouseMove(LPARAM lparam);
    void onMouseWheel(bool horizontal, WPARAM wparam, LPARAM lparam);
    void onKey(bool down, WPARAM wparam, LPARAM lparam);
    void onChar(WPARAM wparam, LPARAM lparam);

    void dispatchMouse(std::string_view type, std::string_view button, int click_count, int delta_x = 0, int delta_y = 0);

    // CDP 命令串行执行：页面里登记坐标的 Runtime.evaluate 必须先于 Input.dispatchMouseEvent 完成
    void enqueue(std::string method, std::string params);
    void evaluate(std::string_view expression);
    void pump();
    void onWatchdog();

private:
    HWND hwnd_ = nullptr;
    Microsoft::WRL::ComPtr<ICoreWebView2> webview_;

    std::deque<Command> queue_;
    bool busy_ = false;
    // 看门狗丢弃超时命令后，用它认出迟到的完成回调
    uint64_t generation_ = 0;

    // 控制器消息里的按键状态与坐标不可靠（按下前的 WM_MOUSEMOVE 已带 MK_LBUTTON），以这里记录的为准
    int buttons_ = 0;
    POINT pointer_ {};
    int modifiers_ = 0;
    wchar_t pending_high_surrogate_ = 0;
    bool foreground_ = true;
};

} // namespace cloudshell
