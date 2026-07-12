#pragma once

#ifdef _WIN32

#include <MaaUtils/SafeWindows.hpp>

#include <atomic>
#include <condition_variable>
#include <mutex>
#include <optional>
#include <thread>


// 无标题栏的可拖拽/可缩放原生窗口。
//
// 设计要点：
//   * 窗口在自有的 STA 消息循环线程中创建并运行，调用方可以在任意线程触发 Open/Close。
//   * Open() 同步等待窗口创建完成（成功或失败）后才返回，方便 GetHwnd() 在调用方线程使用。
//   * 派生类通过 onUiThreadInit / onUiThreadShutdown / onMessage 在 UI 线程上扩展窗口行为。
//
// Set 接口语义：
//   所有 SetXxx 接口（如 SetTopMost）都只能在 Open() 之前调用；Open() 之后调用是合法的、
//   不会崩溃，但配置不会生效，并且会输出警告日志，方便定位调用顺序问题。
//
// 派生类生命周期约束：
//   派生类必须在自己的析构函数最开始调用 Close()，否则 onUiThreadShutdown 会派发到基类，
//   派生类持有的资源不会被正确释放。
class FramelessWindow
{
public:
    FramelessWindow();
    virtual ~FramelessWindow();

    FramelessWindow(const FramelessWindow&) = delete;
    FramelessWindow& operator=(const FramelessWindow&) = delete;
    FramelessWindow(FramelessWindow&&) = delete;
    FramelessWindow& operator=(FramelessWindow&&) = delete;

    // 启动 UI 线程并显示窗口。返回原生窗口是否创建成功。
    //
    // 一旦被调用过（无论成功失败），再次调用直接返回缓存的结果；同时所有 SetXxx 进入「忽略」状态。
    // 不支持多线程并发调用（应由单一线程发起）。
    virtual bool Open();

    // 投递 WM_CLOSE 并等待 UI 线程退出。可重复调用。
    void Close();

    // 设置/取消置顶。仅在 Open() 之前调用有效；之后调用会被忽略。
    void SetTopMost(bool top_most);

    // 设置初始窗口尺寸（客户区+边框的整体尺寸）。仅在 Open() 之前调用有效；之后调用会被忽略。
    // 非正值会被忽略；过小的尺寸会在窗口创建时由 WM_GETMINMAXINFO 钳到最小限制。
    void SetSize(int width, int height);

    // 设置窗口是否出现在任务栏中。仅在 Open() 之前调用有效；之后调用会被忽略。
    // 实现方式：true 时使用 WS_EX_APPWINDOW，false 时使用 WS_EX_TOOLWINDOW。
    // 注意 WS_EX_TOOLWINDOW 同时会让窗口从 Alt+Tab 列表里消失（这是 Win32 工具窗的标准语义）。
    void SetShowInTaskbar(bool show);

    // 设置整窗的不透明度。范围 [0.0, 1.0]：0 = 完全透明，1 = 完全不透明；超出范围会被钳到边界。
    // 实现方式：< 1.0 时叠加 WS_EX_LAYERED 并调 SetLayeredWindowAttributes(..., LWA_ALPHA)，
    //         = 1.0 时不开 layered（省一份合成开销）。整个窗口（含 WebView2 等子控件）一同透明。
    // 仅在 Open() 之前调用有效；之后调用会被忽略。
    void SetOpacity(double opacity);

    // 设置窗口是否对系统录屏 / 截图（含 PrintScreen、第三方录屏、远程桌面等）不可见。
    // true 时通过 SetWindowDisplayAffinity 申请 WDA_EXCLUDEFROMCAPTURE：录屏里完全看不到本窗口，
    //   背后的内容会正常透出。需要 Windows 10 2004+；不支持时自动回退 WDA_MONITOR
    //   （老语义：录屏里把本窗口位置画成黑块）。
    // 主要用途：避免账号、密码、OTP 等敏感界面被无意中录进对外分享的视频。
    // 注意 GPU 截屏（NVIDIA Share / Xbox Game Bar 早期版本等）不一定遵守该标志，安全模型仅尽力而为。
    // 仅在 Open() 之前调用有效；之后调用会被忽略。
    void SetExcludeFromCapture(bool exclude);

protected:
    HWND GetHwnd() const noexcept { return hwnd_; }

    // 派生类的 SetXxx 接口可以用这个守卫做同样的「Open 前才生效」检查。
    bool isOpened() const noexcept { return opened_.load(std::memory_order_acquire); }

    // 返回去掉 chrome 边距（顶部 caption + 四周 resize 边框）之后留给派生类内容的客户区矩形。
    // 派生类必须把自己的子控件放在这个矩形内：留出 chrome 让基类的 WM_NCHITTEST 接收边/顶部
    // 鼠标事件并触发系统级拖拽 / 缩放；同时基类会在 onPaint 里把这块 chrome 涂成深色。
    RECT getContentRect() const;

    // 派生类钩子，均在 UI 线程上调用。
    virtual void onUiThreadInit() {}
    virtual void onUiThreadShutdown() {}

    // 派生类可重写以拦截窗口消息。返回 std::nullopt 表示走基类默认处理。
    virtual std::optional<LRESULT> onMessage(UINT msg, WPARAM wparam, LPARAM lparam);

private:
    // 命中测试：四角/四边用于缩放，顶部 caption 区用于拖动。
    static constexpr int kResizeBorderPx = 6;
    static constexpr int kCaptionHeightPx = 32;

    static constexpr int kMinWidthPx = 200;
    static constexpr int kMinHeightPx = 150;
    static constexpr int kDefaultWidthPx = 960;
    static constexpr int kDefaultHeightPx = 640;

    static LRESULT CALLBACK wndProcStatic(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam);
    LRESULT wndProc(UINT msg, WPARAM wparam, LPARAM lparam);

    void uiThreadMain();
    bool createNativeWindow();

    LRESULT onNcCalcSize(WPARAM wparam, LPARAM lparam);
    LRESULT onNcHitTest(LPARAM lparam);
    LRESULT onPaint();

    // 在父窗口客户区坐标系下判断点位归属，返回 HT* 代码。供 onNcHitTest 复用。
    LRESULT hitTestClientPoint(int client_x, int client_y) const noexcept;

private:
    HWND hwnd_ = nullptr;

    std::thread ui_thread_;

    std::mutex create_mutex_;
    std::condition_variable create_cv_;
    bool create_done_ = false;
    bool create_ok_ = false;

    // 一旦 Open() 被调用就置为 true 且永不回退；之后所有 SetXxx 都会被忽略。
    // 用 atomic 是为了允许 SetXxx 与 Open() 来自不同线程时仍能安全读取。
    std::atomic<bool> opened_ { false };

    // 配置字段：仅在 Open() 之前由业务线程写入；UI 线程在窗口创建后读取一次。
    // 因为 std::thread 的启动会同步业务线程到新线程的初始可见性，所以无需额外同步。
    bool top_most_ = false;
    bool show_in_taskbar_ = true;
    bool exclude_from_capture_ = false;
    double opacity_ = 1.0;
    int initial_width_ = kDefaultWidthPx;
    int initial_height_ = kDefaultHeightPx;
};

#else // !_WIN32

#include <atomic>

// 非 Windows 空实现：不提供原生无边框窗口，仅保证编译与调用方生命周期安全。
// Open() 恒为「成功」以便上层业务流程（如实时代办轮询）在非 Win 平台仍可继续；
// 不会出现真实窗口或 WebView。
class FramelessWindow
{
public:
    FramelessWindow();
    virtual ~FramelessWindow();

    FramelessWindow(const FramelessWindow&) = delete;
    FramelessWindow& operator=(const FramelessWindow&) = delete;
    FramelessWindow(FramelessWindow&&) = delete;
    FramelessWindow& operator=(FramelessWindow&&) = delete;

    virtual bool Open();
    void Close();

    void SetTopMost(bool top_most);
    void SetSize(int width, int height);
    void SetShowInTaskbar(bool show);
    void SetOpacity(double opacity);
    void SetExcludeFromCapture(bool exclude);

protected:
    bool isOpened() const noexcept { return opened_.load(std::memory_order_acquire); }

    virtual void onUiThreadInit() {}
    virtual void onUiThreadShutdown() {}

private:
    std::atomic<bool> opened_ { false };
    bool create_ok_ = false;

    bool top_most_ = false;
    bool show_in_taskbar_ = true;
    bool exclude_from_capture_ = false;
    double opacity_ = 1.0;
    int initial_width_ = 960;
    int initial_height_ = 640;
};

#endif // _WIN32
