#include "Win32ControllerBridge.h"

#include <future>
#include <stdexcept>

#include <MaaUtils/Logger.h>
#include <windowsx.h>

#include "transport/win_key_identity.h"
#include "win32_bridge_protocol.h"

using namespace cloud::win32bridge;

namespace
{
constexpr UINT kFrameAvailable = kCloseHost + 1;
constexpr UINT kFrameFailed = kCloseHost + 2;
constexpr UINT_PTR kOwnerTimer = 1;
// Match a 30 fps stream without adding encoding or desktop capture. PrintWindow
// reads a DWM surface on modern Windows, so WM_PRINT alone is insufficient.
constexpr auto kFrameInterval = std::chrono::milliseconds(33);
}

Win32ControllerBridge::Win32ControllerBridge(CloudTransport& transport, std::function<void()> close_host)
    : transport_(transport)
    , input_(transport_, [](const char* message) { LogError << message; })
    , close_host_(std::move(close_host))
{
    std::promise<bool> ready;
    auto initialized = ready.get_future();
    thread_ = std::jthread([this, &ready] {
        SetThreadDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
        WNDCLASSW wc {};
        wc.lpfnWndProc = WindowProc;
        wc.hInstance = GetModuleHandleW(nullptr);
        wc.lpszClassName = kClass;
        if (!RegisterClassW(&wc) && GetLastError() != ERROR_CLASS_ALREADY_EXISTS) {
            ready.set_value(false);
            return;
        }
        // Toolkit needs WS_VISIBLE, and DWM needs an on-screen surface. Alpha
        // zero and WS_EX_TRANSPARENT make this tool window invisible to users
        // and physical input. It never owns or activates the actual WebView.
        const int x = GetSystemMetrics(SM_XVIRTUALSCREEN);
        const int y = GetSystemMetrics(SM_YVIRTUALSCREEN);
        const auto hwnd = CreateWindowExW(
            WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_LAYERED | WS_EX_TRANSPARENT,
            kClass,
            kTitle,
            WS_POPUP,
            x,
            y,
            kWidth,
            kHeight,
            nullptr,
            nullptr,
            wc.hInstance,
            this);
        if (hwnd) {
            SetLayeredWindowAttributes(hwnd, 0, 0, LWA_ALPHA);
            SetWindowPos(hwnd, HWND_BOTTOM, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE | SWP_SHOWWINDOW);
            SetTimer(hwnd, kOwnerTimer, 1000, nullptr);
        }
        hwnd_.store(hwnd);
        ready.set_value(hwnd != nullptr);
        if (!hwnd) {
            return;
        }
        MSG message {};
        while (GetMessageW(&message, nullptr, 0, 0) > 0) {
            // Do not TranslateMessage: injected VK events already carry their
            // identity and must not also insert a generated WM_CHAR.
            DispatchMessageW(&message);
        }
        hwnd_.store(nullptr);
    });
    if (!initialized.get()) {
        thread_.join();
        throw std::runtime_error("Could not initialize cloud Win32 controller bridge");
    }
    capture_thread_ = std::jthread([this](std::stop_token stop) { CaptureFrames(stop); });
}

Win32ControllerBridge::~Win32ControllerBridge()
{
    capture_thread_.request_stop();
    capture_cv_.notify_all();
    capture_thread_.join();
    if (const auto hwnd = hwnd_.load()) {
        PostMessageW(hwnd, WM_CLOSE, 0, 0);
    }
    thread_.join();
}

LRESULT CALLBACK Win32ControllerBridge::WindowProc(HWND hwnd, UINT message, WPARAM wparam, LPARAM lparam)
{
    if (message == WM_NCCREATE) {
        const auto create = reinterpret_cast<const CREATESTRUCTW*>(lparam);
        SetWindowLongPtrW(hwnd, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(create->lpCreateParams));
    }
    const auto self = reinterpret_cast<Win32ControllerBridge*>(GetWindowLongPtrW(hwnd, GWLP_USERDATA));
    if (!self) {
        return DefWindowProcW(hwnd, message, wparam, lparam);
    }
    // Exceptions must never unwind through user32 or an external SendMessage.
    try {
        return self->HandleMessage(hwnd, message, wparam, lparam);
    }
    catch (const std::exception& error) {
        LogError << "Cloud Win32 bridge failed" << error.what();
        PostMessageW(hwnd, WM_CLOSE, 0, 0);
        return 0;
    }
}

bool Win32ControllerBridge::PrintFrame(HDC dc)
{
    std::lock_guard lock(frame_mutex_);
    if (!dc) {
        return false;
    }
    if (frame_.pixels.empty()) {
        RECT rect { 0, 0, kWidth, kHeight };
        return FillRect(dc, &rect, static_cast<HBRUSH>(GetStockObject(BLACK_BRUSH))) != 0;
    }
    BITMAPINFO bitmap {};
    bitmap.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    bitmap.bmiHeader.biWidth = kWidth;
    bitmap.bmiHeader.biHeight = -kHeight;
    bitmap.bmiHeader.biPlanes = 1;
    bitmap.bmiHeader.biBitCount = 24;
    bitmap.bmiHeader.biCompression = BI_RGB;
    return StretchDIBits(dc, 0, 0, kWidth, kHeight, 0, 0, kWidth, kHeight, frame_.pixels.data(), &bitmap, DIB_RGB_COLORS, SRCCOPY)
           == kHeight;
}

bool Win32ControllerBridge::RefreshFrame()
{
    // Preparation can overlap the capture worker. Keep its writable buffer
    // separate from the published frame and reuse both allocations.
    std::lock_guard refresh_lock(refresh_mutex_);
    if (!transport_.capture(next_frame_) || next_frame_.width != kWidth || next_frame_.height != kHeight) {
        return false;
    }
    if (next_frame_.pixels.size() != static_cast<size_t>(kWidth) * kHeight * 3) {
        return false;
    }
    {
        std::lock_guard lock(frame_mutex_);
        game_frame_.store(next_frame_.game_frame);
        std::swap(frame_, next_frame_);
    }
    return true;
}

void Win32ControllerBridge::SetCapturing(bool enabled)
{
    {
        std::lock_guard lock(capture_mutex_);
        capturing_ = enabled;
        capture_available_ = capture_available_ || enabled;
    }
    capture_cv_.notify_all();
}

void Win32ControllerBridge::CaptureFrames(std::stop_token stop)
{
    try {
        std::unique_lock lock(capture_mutex_);
        while (!stop.stop_requested() && capture_cv_.wait(lock, stop, [this] { return capture_available_; })) {
            const bool active = capturing_;
            const auto next =
                std::chrono::steady_clock::now() + (active && game_frame_.load() ? kFrameInterval : std::chrono::milliseconds(500));
            lock.unlock();
            const bool ok = RefreshFrame();
            if (const auto hwnd = hwnd_.load()) {
                // A busy input handler needs only the newest frame, not a
                // backlog of paints of that same frame when it returns.
                if (!ok) {
                    PostMessageW(hwnd, kFrameFailed, 0, 0);
                }
                else if (!frame_message_pending_.exchange(true) && !PostMessageW(hwnd, kFrameAvailable, 0, 0)) {
                    frame_message_pending_.store(false);
                }
            }
            lock.lock();
            if (!ok) {
                capturing_ = false;
                capture_available_ = false;
            }
            capture_cv_.wait_until(lock, stop, next, [this, active] { return capturing_ != active; });
        }
    }
    catch (const std::exception& error) {
        LogError << "Cloud frame bridge failed" << error.what();
        if (const auto hwnd = hwnd_.load()) {
            PostMessageW(hwnd, kFrameFailed, 0, 0);
        }
    }
}

LRESULT Win32ControllerBridge::HandleMessage(HWND hwnd, UINT message, WPARAM wparam, LPARAM lparam)
{
    bool ok = true;
    switch (message) {
    case WM_TIMER:
        // A GUI may disconnect its agents before the asynchronous task-end
        // event reaches their sinks. Process lifetime also covers cancellation
        // and agent crashes without requiring any change to the GUI.
        if (task_process_ && WaitForSingleObject(task_process_, 0) == WAIT_OBJECT_0) {
            LogInfo << "Cloud task owner exited; reducing frame refresh";
            CloseHandle(task_process_);
            task_process_ = nullptr;
            SetCapturing(false);
            input_.releaseAll();
        }
        if (owner_ && WaitForSingleObject(owner_, 0) == WAIT_OBJECT_0) {
            SetCapturing(false);
            PostMessageW(hwnd, WM_CLOSE, 0, 0);
        }
        if (preparation_deadline_ != std::chrono::steady_clock::time_point {}
            && std::chrono::steady_clock::now() >= preparation_deadline_) {
            preparation_deadline_ = {};
            SetCapturing(false);
        }
        return 0;
    case kFrameAvailable:
        frame_message_pending_.store(false);
        RedrawWindow(hwnd, nullptr, nullptr, RDW_INVALIDATE | RDW_UPDATENOW);
        return 0;
    case kFrameFailed:
        ok = false;
        break;
    case kPrepareFrame:
        ok = RefreshFrame();
        if (ok) {
            RedrawWindow(hwnd, nullptr, nullptr, RDW_INVALIDATE | RDW_UPDATENOW);
            preparation_deadline_ = std::chrono::steady_clock::now() + std::chrono::seconds(5);
            SetCapturing(true);
        }
        break;
    case kBeginTask: {
        // Remote sink callbacks can reenter while resolving their controller.
        // Framework task IDs increase within the owning GUI process. Never let
        // a delayed start/end overwrite a newer task's capture state.
        if (lparam <= 0) {
            return 0;
        }
        if (lparam <= latest_task_id_) {
            return 1;
        }
        LogInfo << "Cloud frame bridge task starting" << VAR(wparam) << VAR(lparam);
        const auto task_process = OpenProcess(SYNCHRONIZE, FALSE, static_cast<DWORD>(wparam));
        if (!task_process) {
            return 0;
        }
        if (task_process_) {
            CloseHandle(task_process_);
        }
        task_process_ = task_process;
        latest_task_id_ = lparam;
        preparation_deadline_ = {};
        SetCapturing(true);
        return 1;
    }
    case kEndTask:
        if (lparam <= 0) {
            return 0;
        }
        if (lparam < latest_task_id_) {
            return 1;
        }
        latest_task_id_ = lparam;
        LogInfo << "Cloud frame bridge task ended" << VAR(lparam);
        if (task_process_) {
            CloseHandle(task_process_);
            task_process_ = nullptr;
        }
        SetCapturing(false);
        preparation_deadline_ = {};
        ok = input_.releaseAll();
        break;
    case kWatchOwner: {
        const auto owner = OpenProcess(SYNCHRONIZE, FALSE, static_cast<DWORD>(wparam));
        if (!owner) {
            return 0;
        }
        if (owner_) {
            CloseHandle(owner_);
        }
        owner_ = owner;
        return 1;
    }
    case kCloseHost:
        SetCapturing(false);
        ok = input_.releaseAll();
        if (close_host_) {
            close_host_();
        }
        break;
    case WM_PRINT:
    case WM_PRINTCLIENT:
        ok = PrintFrame(reinterpret_cast<HDC>(wparam));
        break;
    case WM_MOUSEACTIVATE:
        return MA_NOACTIVATE;
    case WM_NCHITTEST:
        return HTTRANSPARENT;
    case WM_ACTIVATE:
    case WM_SETFOCUS:
    case WM_KILLFOCUS:
    case WM_ACTIVATEAPP:
        // Framework's synthetic activation must never activate the real WebView.
        return 0;
    case WM_ERASEBKGND:
        return 1;
    case WM_PAINT: {
        if (wparam) {
            return PrintFrame(reinterpret_cast<HDC>(wparam)) ? 1 : 0;
        }
        PAINTSTRUCT paint {};
        BeginPaint(hwnd, &paint);
        PrintFrame(paint.hdc);
        EndPaint(hwnd, &paint);
        return 0;
    }
    case WM_MOUSEMOVE:
        ok = input_.touchMove(-1, GET_X_LPARAM(lparam), GET_Y_LPARAM(lparam));
        break;
    case WM_LBUTTONDOWN:
    case WM_RBUTTONDOWN:
    case WM_MBUTTONDOWN:
        ok = input_.touchDown(
            message == WM_LBUTTONDOWN   ? 0
            : message == WM_RBUTTONDOWN ? 1
                                        : 2,
            GET_X_LPARAM(lparam),
            GET_Y_LPARAM(lparam));
        break;
    case WM_LBUTTONUP:
    case WM_RBUTTONUP:
    case WM_MBUTTONUP:
        ok = input_.touchUp(message == WM_LBUTTONUP ? 0 : message == WM_RBUTTONUP ? 1 : 2);
        break;
    case WM_MOUSEWHEEL:
    case WM_MOUSEHWHEEL:
        ok = input_.scroll(
            message == WM_MOUSEHWHEEL ? GET_WHEEL_DELTA_WPARAM(wparam) : 0,
            message == WM_MOUSEWHEEL ? -GET_WHEEL_DELTA_WPARAM(wparam) : 0);
        break;
    case WM_KEYDOWN:
    case WM_SYSKEYDOWN:
    case WM_KEYUP:
    case WM_SYSKEYUP:
        if (cloud_key_identity(static_cast<int32_t>(wparam)).first.empty()) {
            LogWarn << "Unsupported cloud key" << VAR(wparam);
            return 0;
        }
        ok = input_.key(static_cast<int32_t>(wparam), message == WM_KEYDOWN || message == WM_SYSKEYDOWN);
        break;
    case WM_CHAR: {
        const wchar_t ch = static_cast<wchar_t>(wparam);
        if (ch >= 0xd800 && ch <= 0xdbff) {
            high_surrogate_ = ch;
            return 1;
        }
        wchar_t utf16[] = { ch, 0 };
        int length = 1;
        if (high_surrogate_ && ch >= 0xdc00 && ch <= 0xdfff) {
            utf16[0] = high_surrogate_;
            utf16[1] = ch;
            length = 2;
        }
        high_surrogate_ = 0;
        char utf8[8] {};
        const int bytes = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, utf16, length, utf8, sizeof(utf8), nullptr, nullptr);
        ok = bytes > 0 && transport_.insert_text(std::string(utf8, bytes));
        break;
    }
    case kRelativeMove:
        ok = input_.relativeMove(static_cast<int32_t>(wparam), static_cast<int32_t>(lparam));
        break;
    case kReleaseInput:
        ok = input_.releaseAll();
        break;
    case WM_CLOSE:
        SetCapturing(false);
        KillTimer(hwnd, kOwnerTimer);
        if (owner_) {
            CloseHandle(owner_);
            owner_ = nullptr;
        }
        if (task_process_) {
            CloseHandle(task_process_);
            task_process_ = nullptr;
        }
        input_.releaseAll();
        hwnd_.store(nullptr);
        if (close_host_) {
            close_host_();
        }
        DestroyWindow(hwnd);
        return 0;
    case WM_DESTROY:
        PostQuitMessage(0);
        return 0;
    default:
        return DefWindowProcW(hwnd, message, wparam, lparam);
    }
    if (!ok) {
        // Plain SendMessage in Framework does not inspect LRESULT. Invalidate
        // this controller on failure so the next capture cannot appear healthy.
        LogError << "Cloud Win32 controller transport failed" << VAR(message);
        PostMessageW(hwnd, WM_CLOSE, 0, 0);
    }
    return ok ? 1 : 0;
}
