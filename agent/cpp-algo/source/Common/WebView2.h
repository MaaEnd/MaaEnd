#pragma once

#ifdef _WIN32

#include "FramelessWindow.h"

#include <condition_variable>
#include <mutex>
#include <string>

#include <wrl/client.h>

// 直接引入 WebView2 SDK 头文件。
// 注意：本文件位于 source/Common/，不在 cpp-algo 的 include 根目录下，
// 因此 <WebView2.h> 不会被本头文件遮蔽，仍会解析到 WebView2 SDK 的头文件。
#include <WebView2.h>


// 内嵌 Microsoft Edge WebView2 控件的无边框窗口。
//
// 用法：
//   WebView2 win;
//   win.SetTopMost(true);             // 可选，必须在 Open 之前
//   win.SetURL("https://example.com");// 可选，必须在 Open 之前
//   win.Open();
//   ...
//   win.Close();
//
// 设计要点：
//   * SetURL 与基类的 SetTopMost 同样遵循「Open 前生效，Open 后忽略」的规则。
//   * Open()/Close() 复用 FramelessWindow 的 UI 线程模型，
//     WebView2 的所有 COM 调用都被收敛到该 STA 线程。
//   * 析构函数最先调用 Close()，保证 onUiThreadShutdown 在派生类完整状态下执行，
//     从而在 UI 线程上释放 ComPtr，避免跨线程 Release。
class WebView2 : public FramelessWindow
{
public:
    WebView2();
    ~WebView2() override;

    WebView2(const WebView2&) = delete;
    WebView2& operator=(const WebView2&) = delete;

    // 在基类 Open 的基础上，同步等待 WebView2 控件初始化结束。
    // 返回 true 表示窗口与 WebView2 控件都已就绪（已经可以接收 Navigate 等调用），
    // 返回 false 表示窗口创建失败，或者 WebView2 Runtime 不可用 / 环境/控制器创建失败。
    //
    // 注意：会同步等待 WebView2 Runtime 的异步初始化（首次冷启动可能耗时若干百毫秒），
    // 不应在敏感的 UI/帧调度线程上调用。
    bool Open() override;

    // 设置初始要打开的 URL。仅在 Open() 之前调用有效；之后调用会被忽略。
    void SetURL(std::string url);

    // 启用 Chromium 触屏仿真（通过 CDP 的 Emulation.setTouchEmulationEnabled）。
    // 启用后 navigator/document 会上报具备触屏，document.createEvent("TouchEvent") 不再抛异常。
    // 主要用途：让那些通过 TouchEvent 探测来判定 isPC/isMobile 的网站把窗口认作移动端，
    // 从而走自带的 compact 布局，避免在窄窗口里 PC 布局被挤。
    // 仅在 Open() 之前调用有效；之后调用会被忽略。
    void SetTouchEmulation(bool enabled);

    // 设置是否启用 WebView2 默认右键菜单（即「检查」「重新加载」等弹出菜单）。
    // 默认 true。设置为 false 后右键不会再弹出菜单。
    // 仅在 Open() 之前调用有效；之后调用会被忽略。
    void SetContextMenuEnabled(bool enabled);

protected:
    void onUiThreadInit() override;
    void onUiThreadShutdown() override;
    std::optional<LRESULT> onMessage(UINT msg, WPARAM wparam, LPARAM lparam) override;

private:
    void initializeWebView();
    void onEnvironmentCreated(HRESULT result, ICoreWebView2Environment* env);
    void onControllerCreated(HRESULT result, ICoreWebView2Controller* controller);
    void resizeToClientRect();

    // 通知等在 Open() 上的业务线程：WebView2 初始化已完成（成功或失败）。幂等。
    void signalInitDone(bool ok);

private:
    Microsoft::WRL::ComPtr<ICoreWebView2Environment> environment_;
    Microsoft::WRL::ComPtr<ICoreWebView2Controller> controller_;
    Microsoft::WRL::ComPtr<ICoreWebView2> webview_;

    // 配置字段：仅在 Open() 之前由业务线程写入，UI 线程在 onControllerCreated 中读取一次。
    std::string initial_url_;
    bool touch_emulation_ = false;
    bool context_menu_enabled_ = true;

    // 标记 UI 线程上的 CoInitializeEx 是否成功，用于决定是否需要配对 CoUninitialize。
    bool com_initialized_ = false;

    // WebView2 异步初始化的同步原语：UI 线程在所有完成路径上调 signalInitDone，
    // 业务线程在 Open() 中等待。
    std::mutex webview_init_mutex_;
    std::condition_variable webview_init_cv_;
    bool webview_init_done_ = false;
    bool webview_init_ok_ = false;
};

#else // !_WIN32

#include "FramelessWindow.h"

#include <string>

// 非 Windows 空实现：无 WebView2 运行时与内嵌浏览器；Open() 与基类一致（恒真），
// 便于任务主流程在非 Win 平台继续执行。
class WebView2 : public FramelessWindow
{
public:
    WebView2();
    ~WebView2() override;

    WebView2(const WebView2&) = delete;
    WebView2& operator=(const WebView2&) = delete;

    bool Open() override;

    void SetURL(std::string url);
    void SetTouchEmulation(bool enabled);
    void SetContextMenuEnabled(bool enabled);
};

#endif // _WIN32
