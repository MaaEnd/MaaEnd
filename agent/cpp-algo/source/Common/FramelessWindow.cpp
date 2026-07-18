#include "FramelessWindow.h"

#ifdef _WIN32

#include <algorithm>
#include <cmath>

#include <windowsx.h>

#include <MaaUtils/Logger.h>

namespace
{

constexpr const wchar_t* kWindowClassName = L"MaaEnd.Common.FramelessWindow";
constexpr const wchar_t* kWindowTitle = L"";

// chrome 颜色：现代深灰，类似 Windows 11 dark mode caption。
// 由 onPaint 涂在 chrome 边距上；WS_CLIPCHILDREN 保证子控件区域不会被覆盖。
constexpr COLORREF kChromeColor = RGB(30, 30, 30);

// 多次创建窗口时只注册一次窗口类。线程安全由 std::call_once 保证。
ATOM ensureWindowClass(WNDPROC proc)
{
    static ATOM s_atom = 0;
    static std::once_flag s_flag;
    std::call_once(s_flag, [&] {
        WNDCLASSEXW wc { sizeof(WNDCLASSEXW) };
        wc.style = CS_HREDRAW | CS_VREDRAW;
        wc.lpfnWndProc = proc;
        wc.hInstance = GetModuleHandleW(nullptr);
        wc.hCursor = LoadCursor(nullptr, IDC_ARROW);
        // 不让默认背景刷介入；onPaint 自行用深色刷涂 chrome 区。
        wc.hbrBackground = nullptr;
        wc.lpszClassName = kWindowClassName;
        s_atom = RegisterClassExW(&wc);
        if (!s_atom) {
            LogError << "FramelessWindow: RegisterClassExW failed" << VAR(GetLastError());
        }
    });
    return s_atom;
}

} // namespace

FramelessWindow::FramelessWindow() = default;

FramelessWindow::~FramelessWindow()
{
    // 兜底关闭。派生类应当在自己的析构里更早调用 Close()，
    // 否则到这里时 onUiThreadShutdown 已经派发到基类，派生资源不会被正确释放。
    Close();
}

bool FramelessWindow::Open()
{
    // 一旦置为 true 就不会回退；后续的 SetXxx 与重复的 Open 都会被短路。
    // 重复调用时直接复用首次 Open 的结果。
    if (opened_.exchange(true, std::memory_order_acq_rel)) {
        return create_ok_;
    }

    {
        std::lock_guard<std::mutex> lock(create_mutex_);
        create_done_ = false;
        create_ok_ = false;
    }

    ui_thread_ = std::thread([this] { uiThreadMain(); });

    std::unique_lock<std::mutex> lock(create_mutex_);
    create_cv_.wait(lock, [this] { return create_done_; });
    bool ok = create_ok_;
    lock.unlock();

    if (!ok) {
        // 窗口创建失败，UI 线程会自然退出。这里 join 一下，确保 hwnd_ 一定为空。
        if (ui_thread_.joinable()) {
            ui_thread_.join();
        }
        LogError << "FramelessWindow::Open: window creation failed";
    }

    return ok;
}

void FramelessWindow::Close()
{
    if (HWND hwnd = hwnd_) {
        // PostMessage 不会阻塞调用方线程，UI 线程在处理 WM_CLOSE 时会触发 DestroyWindow。
        PostMessageW(hwnd, WM_CLOSE, 0, 0);
    }
    if (ui_thread_.joinable()) {
        ui_thread_.join();
    }
}

void FramelessWindow::SetTopMost(bool top_most)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetTopMost: ignored, must be called before Open()" << VAR(top_most);
        return;
    }
    top_most_ = top_most;
}

void FramelessWindow::SetSize(int width, int height)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetSize: ignored, must be called before Open()" << VAR(width) << VAR(height);
        return;
    }
    if (width <= 0 || height <= 0) {
        LogWarn << "FramelessWindow::SetSize: ignored, non-positive size" << VAR(width) << VAR(height);
        return;
    }
    initial_width_ = width;
    initial_height_ = height;
}

void FramelessWindow::SetShowInTaskbar(bool show)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetShowInTaskbar: ignored, must be called before Open()" << VAR(show);
        return;
    }
    show_in_taskbar_ = show;
}

void FramelessWindow::SetOpacity(double opacity)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetOpacity: ignored, must be called before Open()" << VAR(opacity);
        return;
    }
    opacity_ = std::clamp(opacity, 0.0, 1.0);
}

void FramelessWindow::SetExcludeFromCapture(bool exclude)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetExcludeFromCapture: ignored, must be called before Open()" << VAR(exclude);
        return;
    }
    exclude_from_capture_ = exclude;
}

RECT FramelessWindow::getContentRect() const
{
    RECT rc {};
    if (hwnd_) {
        GetClientRect(hwnd_, &rc);
    }
    rc.left += kResizeBorderPx;
    rc.right -= kResizeBorderPx;
    rc.top += kCaptionHeightPx; // 顶部 caption 区域同时覆盖 kResizeBorderPx 的 HTTOP 区
    rc.bottom -= kResizeBorderPx;
    if (rc.right < rc.left) {
        rc.right = rc.left;
    }
    if (rc.bottom < rc.top) {
        rc.bottom = rc.top;
    }
    return rc;
}

std::optional<LRESULT> FramelessWindow::onMessage(UINT, WPARAM, LPARAM)
{
    return std::nullopt;
}

LRESULT CALLBACK FramelessWindow::wndProcStatic(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam)
{
    FramelessWindow* self = nullptr;
    if (msg == WM_NCCREATE) {
        auto* cs = reinterpret_cast<CREATESTRUCTW*>(lparam);
        self = static_cast<FramelessWindow*>(cs->lpCreateParams);
        SetWindowLongPtrW(hwnd, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(self));
        self->hwnd_ = hwnd;
    }
    else {
        self = reinterpret_cast<FramelessWindow*>(GetWindowLongPtrW(hwnd, GWLP_USERDATA));
    }

    if (self) {
        return self->wndProc(msg, wparam, lparam);
    }
    return DefWindowProcW(hwnd, msg, wparam, lparam);
}

LRESULT FramelessWindow::wndProc(UINT msg, WPARAM wparam, LPARAM lparam)
{
    // 派生类优先；返回 nullopt 才走基类默认处理。
    if (auto handled = onMessage(msg, wparam, lparam); handled) {
        return *handled;
    }

    switch (msg) {
    case WM_NCCALCSIZE:
        return onNcCalcSize(wparam, lparam);
    case WM_NCHITTEST:
        return onNcHitTest(lparam);
    case WM_GETMINMAXINFO: {
        auto* mmi = reinterpret_cast<MINMAXINFO*>(lparam);
        mmi->ptMinTrackSize.x = kMinWidthPx;
        mmi->ptMinTrackSize.y = kMinHeightPx;
        return 0;
    }
    case WM_PAINT:
        return onPaint();
    case WM_ERASEBKGND:
        // 由 WM_PAINT 完成绘制，避免闪烁。
        return 1;
    case WM_CLOSE:
        DestroyWindow(hwnd_);
        return 0;
    case WM_DESTROY:
        PostQuitMessage(0);
        return 0;
    default:
        return DefWindowProcW(hwnd_, msg, wparam, lparam);
    }
}

void FramelessWindow::uiThreadMain()
{
    // FramelessWindow 本身只依赖 GDI/USER32，不做 CoInitializeEx；
    // 需要 COM/STA 的派生类（如 WebView2）应在 onUiThreadInit / onUiThreadShutdown 中
    // 自行 CoInitializeEx / CoUninitialize。

    // 让本 UI 线程独立声明 Per-Monitor v2 DPI 感知。
    // 不用 SetProcessDpiAwarenessContext 是为了避免影响 cpp-algo 主线程及 MaaFW 其他模块。
    // 影响：
    //   * SetSize 给的尺寸被解释为物理像素（不再被 Windows 按 96 DPI 虚拟放大）
    //   * 子 WebView2 / Chromium 会以正确的 devicePixelRatio 看待窗口，
    //     于是 window.innerWidth = 物理宽度 / 系统缩放，能命中页面真实的响应式断点。
    // 该 API 自 Windows 10 1607 起可用，cpp-algo 的目标平台覆盖之。
    SetThreadDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);

    bool ok = createNativeWindow();
    {
        std::lock_guard<std::mutex> lock(create_mutex_);
        create_done_ = true;
        create_ok_ = ok;
    }
    create_cv_.notify_all();

    if (!ok) {
        return;
    }

    // 应用 Open() 之前由 SetTopMost 设置的状态。SetWindowPos 在 UI 线程上调用最稳妥。
    if (top_most_) {
        SetWindowPos(hwnd_, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE);
    }

    // 应用 SetExcludeFromCapture 配置：先试 Win10 2004+ 的 EXCLUDEFROMCAPTURE（录屏完全看不到本窗口），
    // 失败再回退到 Win7+ 的 MONITOR（录屏里画成黑块），都失败则记日志。
    // WDA_EXCLUDEFROMCAPTURE 在较老的 Windows SDK 头文件里可能没有，本地补一个常量。
    if (exclude_from_capture_) {
#ifndef WDA_EXCLUDEFROMCAPTURE
        constexpr DWORD WDA_EXCLUDEFROMCAPTURE = 0x00000011;
#endif
        if (!SetWindowDisplayAffinity(hwnd_, WDA_EXCLUDEFROMCAPTURE)) {
            const DWORD ec1 = GetLastError();
            if (!SetWindowDisplayAffinity(hwnd_, WDA_MONITOR)) {
                LogWarn << "FramelessWindow: SetWindowDisplayAffinity failed" << VAR(ec1) << VAR(GetLastError());
            }
        }
    }

    ShowWindow(hwnd_, SW_SHOW);
    UpdateWindow(hwnd_);

    onUiThreadInit();

    MSG msg {};
    while (GetMessageW(&msg, nullptr, 0, 0) > 0) {
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }

    onUiThreadShutdown();

    // 走到这里时 hwnd_ 已经被 DestroyWindow 释放，但 GWLP_USERDATA 不再可用，
    // 显式置空让外部线程的访问能立即跳过。
    hwnd_ = nullptr;
}

bool FramelessWindow::createNativeWindow()
{
    ATOM atom = ensureWindowClass(&FramelessWindow::wndProcStatic);
    if (!atom) {
        return false;
    }

    // WS_THICKFRAME 是无边框窗口启用系统级边框拉伸（DWM 自带光标变化）的关键。
    // WS_CAPTION 让 Windows 的窗口动画/最大化/Aero Snap 行为保持正常，
    // 实际的标题栏会被 WM_NCCALCSIZE 抹掉。
    constexpr DWORD kStyle = WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_MINIMIZEBOX | WS_MAXIMIZEBOX | WS_CLIPCHILDREN;

    // WS_EX_APPWINDOW 强制窗口出现在任务栏；WS_EX_TOOLWINDOW 把窗口标记为工具窗，
    // 不进任务栏也不进 Alt+Tab 列表。两者互斥，由 SetShowInTaskbar 控制。
    DWORD ex_style = show_in_taskbar_ ? WS_EX_APPWINDOW : WS_EX_TOOLWINDOW;

    // 透明度 < 1 时再叠加 WS_EX_LAYERED；= 1 时不开 layered，避免无谓的 DWM 合成开销。
    const bool need_layered = opacity_ < 1.0;
    if (need_layered) {
        ex_style |= WS_EX_LAYERED;
    }

    HWND hwnd = CreateWindowExW(
        ex_style,
        kWindowClassName,
        kWindowTitle,
        kStyle,
        CW_USEDEFAULT,
        CW_USEDEFAULT,
        initial_width_,
        initial_height_,
        nullptr,
        nullptr,
        GetModuleHandleW(nullptr),
        this);

    if (!hwnd) {
        LogError << "FramelessWindow: CreateWindowExW failed" << VAR(GetLastError());
        return false;
    }

    // 用 LWA_ALPHA 模式：整个窗口（含 WebView2 等子控件）按统一 alpha 合成。
    // 区别于 UpdateLayeredWindow（per-pixel alpha），那种模式不能正确显示子窗口内容。
    if (need_layered) {
        const BYTE alpha = static_cast<BYTE>(std::lround(opacity_ * 255.0));
        if (!SetLayeredWindowAttributes(hwnd, 0, alpha, LWA_ALPHA)) {
            LogWarn << "FramelessWindow: SetLayeredWindowAttributes failed" << VAR(GetLastError());
        }
    }

    // 强制重新计算 NC 区域，让 WM_NCCALCSIZE 立刻生效（移除标题栏视觉残留）。
    SetWindowPos(hwnd, nullptr, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_FRAMECHANGED);

    return true;
}

LRESULT FramelessWindow::onNcCalcSize(WPARAM wparam, LPARAM lparam)
{
    // 当 wparam == TRUE 时返回 0 把整个窗口当作客户区使用，从而抹掉系统标题栏与边框；
    // 仍然保留 WS_THICKFRAME 让命中测试能继续触发原生 Resize。
    if (wparam == TRUE) {
        return 0;
    }
    return DefWindowProcW(hwnd_, WM_NCCALCSIZE, wparam, lparam);
}

LRESULT FramelessWindow::onNcHitTest(LPARAM lparam)
{
    // 因为 WM_NCCALCSIZE 返回 0 把全窗当客户区使，窗口本地坐标和客户区坐标一致，
    // 这里直接转成客户区坐标后复用 hitTestClientPoint。
    POINT cursor { GET_X_LPARAM(lparam), GET_Y_LPARAM(lparam) };
    ScreenToClient(hwnd_, &cursor);
    return hitTestClientPoint(cursor.x, cursor.y);
}

LRESULT FramelessWindow::hitTestClientPoint(int client_x, int client_y) const noexcept
{
    if (!hwnd_) {
        return HTCLIENT;
    }

    RECT rc {};
    GetClientRect(hwnd_, &rc);
    const int width = rc.right - rc.left;
    const int height = rc.bottom - rc.top;

    const bool on_left = client_x < kResizeBorderPx;
    const bool on_right = client_x >= width - kResizeBorderPx;
    const bool on_top = client_y < kResizeBorderPx;
    const bool on_bottom = client_y >= height - kResizeBorderPx;

    if (on_top && on_left) {
        return HTTOPLEFT;
    }
    if (on_top && on_right) {
        return HTTOPRIGHT;
    }
    if (on_bottom && on_left) {
        return HTBOTTOMLEFT;
    }
    if (on_bottom && on_right) {
        return HTBOTTOMRIGHT;
    }
    if (on_left) {
        return HTLEFT;
    }
    if (on_right) {
        return HTRIGHT;
    }
    if (on_top) {
        return HTTOP;
    }
    if (on_bottom) {
        return HTBOTTOM;
    }

    // 顶部区域作为可拖动的伪 caption；其它区域交给客户端（派生类的 child window 等）。
    if (client_y < kCaptionHeightPx) {
        return HTCAPTION;
    }
    return HTCLIENT;
}

LRESULT FramelessWindow::onPaint()
{
    PAINTSTRUCT ps {};
    HDC hdc = BeginPaint(hwnd_, &ps);
    if (hdc) {
        // 用深色填充整个客户区。WS_CLIPCHILDREN 会把派生类子控件（如 WebView2）的区域裁掉，
        // 因此实际着色的只有 chrome 边距：顶部 32px caption + 四周 6px resize 边框。
        // 派生类只要把子控件放在 getContentRect() 里，chrome 颜色就自然出现在边上。
        HBRUSH brush = CreateSolidBrush(kChromeColor);
        if (brush) {
            FillRect(hdc, &ps.rcPaint, brush);
            DeleteObject(brush);
        }
        EndPaint(hwnd_, &ps);
    }
    return 0;
}

#else // !_WIN32

#include <algorithm>

#include <MaaUtils/Logger.h>

FramelessWindow::FramelessWindow() = default;

FramelessWindow::~FramelessWindow()
{
    Close();
}

bool FramelessWindow::Open()
{
    if (opened_.exchange(true, std::memory_order_acq_rel)) {
        return create_ok_;
    }
    create_ok_ = true;
    return true;
}

void FramelessWindow::Close()
{
}

void FramelessWindow::SetTopMost(bool top_most)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetTopMost: ignored, must be called before Open()" << VAR(top_most);
        return;
    }
    top_most_ = top_most;
}

void FramelessWindow::SetSize(int width, int height)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetSize: ignored, must be called before Open()" << VAR(width) << VAR(height);
        return;
    }
    if (width <= 0 || height <= 0) {
        LogWarn << "FramelessWindow::SetSize: ignored, non-positive size" << VAR(width) << VAR(height);
        return;
    }
    initial_width_ = width;
    initial_height_ = height;
}

void FramelessWindow::SetShowInTaskbar(bool show)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetShowInTaskbar: ignored, must be called before Open()" << VAR(show);
        return;
    }
    show_in_taskbar_ = show;
}

void FramelessWindow::SetOpacity(double opacity)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetOpacity: ignored, must be called before Open()" << VAR(opacity);
        return;
    }
    opacity_ = std::clamp(opacity, 0.0, 1.0);
}

void FramelessWindow::SetExcludeFromCapture(bool exclude)
{
    if (isOpened()) {
        LogWarn << "FramelessWindow::SetExcludeFromCapture: ignored, must be called before Open()" << VAR(exclude);
        return;
    }
    exclude_from_capture_ = exclude;
}

#endif // _WIN32
