#include "ShellWindow.h"

#include <format>

#include <wrl.h>

#include <WebView2EnvironmentOptions.h>

#include "Log.h"
#include "ResourceIds.h"
#include "Resources.h"

namespace cloudshell
{

using Microsoft::WRL::Callback;
using Microsoft::WRL::ComPtr;

namespace
{

// 不可拉伸、不可最大化：客户区尺寸固定，截图尺寸才固定
constexpr DWORD kWindowStyle = WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX | WS_CLIPCHILDREN;

// 后台、被遮挡时页面不能被节流；视频允许无手势自动播放
constexpr const wchar_t* kBrowserArguments =
    L"--disable-features=CalculateNativeWinOcclusion --disable-backgrounding-occluded-windows "
    L"--disable-renderer-backgrounding --disable-background-timer-throttling --autoplay-policy=no-user-gesture-required";

// 前台事件回调拿不到 this，每个进程只有一个外壳窗口
ShellWindow* g_foreground_owner = nullptr;

bool isOfficialSite(std::string_view uri)
{
    constexpr std::string_view kScheme = "https://";
    if (!uri.starts_with(kScheme)) {
        return false;
    }
    std::string_view host = uri.substr(kScheme.size());
    host = host.substr(0, host.find_first_of("/:?#"));
    return host == "hypergryph.com" || host.ends_with(".hypergryph.com");
}

} // namespace

ShellWindow::ShellWindow(ShellOptions options)
    : options_(std::move(options))
{
}

ShellWindow::~ShellWindow()
{
    if (foreground_hook_) {
        UnhookWinEvent(foreground_hook_);
    }
    g_foreground_owner = nullptr;
}

bool ShellWindow::Create()
{
    WNDCLASSEXW wc { sizeof(WNDCLASSEXW) };
    wc.lpfnWndProc = &ShellWindow::windowProc;
    wc.hInstance = GetModuleHandleW(nullptr);
    wc.hCursor = LoadCursorW(nullptr, IDC_ARROW);
    wc.hbrBackground = static_cast<HBRUSH>(GetStockObject(BLACK_BRUSH));
    wc.lpszClassName = kWindowClassName;
    if (!RegisterClassExW(&wc)) {
        log::Error("RegisterClassExW failed: {}", GetLastError());
        return false;
    }

    hwnd_ = CreateWindowExW(
        WS_EX_APPWINDOW,
        kWindowClassName,
        options_.title.c_str(),
        kWindowStyle,
        CW_USEDEFAULT,
        CW_USEDEFAULT,
        options_.width,
        options_.height,
        nullptr,
        nullptr,
        wc.hInstance,
        this);
    if (!hwnd_) {
        log::Error("CreateWindowExW failed: {}", GetLastError());
        return false;
    }

    // 创建前不知道窗口落在哪块显示器上，拿到 hwnd 后再按实际 DPI 算整窗尺寸
    applyFixedClientSize(GetDpiForWindow(hwnd_), nullptr);
    input_ = std::make_unique<InputBridge>(hwnd_);

    g_foreground_owner = this;
    foreground_hook_ = SetWinEventHook(
        EVENT_SYSTEM_FOREGROUND,
        EVENT_SYSTEM_FOREGROUND,
        nullptr,
        &ShellWindow::onForegroundChanged,
        0,
        0,
        WINEVENT_OUTOFCONTEXT);
    if (!foreground_hook_) {
        log::Error("SetWinEventHook failed: {}", GetLastError());
    }

    ShowWindow(hwnd_, SW_SHOW);
    UpdateWindow(hwnd_);

    initWebView();
    return true;
}

int ShellWindow::Run()
{
    MSG msg {};
    while (GetMessageW(&msg, nullptr, 0, 0) > 0) {
        // PostMessage 输入每次操作前都会投递 WM_ACTIVATE，真实的激活是同步发送的、不经过这里。
        // 交给 DefWindowProc 会把焦点从网页抢到顶层窗口，网页失焦时 SDK 会释放所有按住的键
        if (msg.message == WM_ACTIVATE) {
            continue;
        }
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }
    return static_cast<int>(msg.wParam);
}

LRESULT CALLBACK ShellWindow::windowProc(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam)
{
    if (msg == WM_NCCREATE) {
        auto* self = static_cast<ShellWindow*>(reinterpret_cast<CREATESTRUCTW*>(lparam)->lpCreateParams);
        SetWindowLongPtrW(hwnd, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(self));
        self->hwnd_ = hwnd;
    }

    auto* self = reinterpret_cast<ShellWindow*>(GetWindowLongPtrW(hwnd, GWLP_USERDATA));
    if (!self) {
        return DefWindowProcW(hwnd, msg, wparam, lparam);
    }
    return self->handleMessage(msg, wparam, lparam);
}

LRESULT ShellWindow::handleMessage(UINT msg, WPARAM wparam, LPARAM lparam)
{
    if (input_) {
        if (auto handled = input_->HandleMessage(msg, wparam, lparam)) {
            return *handled;
        }
    }

    switch (msg) {
    case WM_SETFOCUS:
        // 真实激活时焦点落在顶层窗口上，键盘与输入法会绕开网页
        if (controller_) {
            controller_->MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC);
            return 0;
        }
        break;

    case WM_DPICHANGED: {
        // 建议矩形会按新 DPI 缩放客户区，只取它的左上角
        const auto* suggested = reinterpret_cast<const RECT*>(lparam);
        const POINT origin { suggested->left, suggested->top };
        applyFixedClientSize(HIWORD(wparam), &origin);
        return 0;
    }

    case WM_SIZE:
        // 最小化时客户区为空，跟着改会让页面视口变成 0×0
        if (controller_ && wparam != SIZE_MINIMIZED) {
            RECT bounds {};
            GetClientRect(hwnd_, &bounds);
            controller_->put_Bounds(bounds);
        }
        break;

    case WM_DESTROY:
        if (input_) {
            input_->Detach();
        }
        if (controller_) {
            controller_->Close();
        }
        webview_.Reset();
        controller_.Reset();
        environment_.Reset();
        PostQuitMessage(exit_code_);
        return 0;

    default:
        break;
    }

    return DefWindowProcW(hwnd_, msg, wparam, lparam);
}

void ShellWindow::applyFixedClientSize(UINT dpi, const POINT* origin)
{
    const auto ex_style = static_cast<DWORD>(GetWindowLongPtrW(hwnd_, GWL_EXSTYLE));

    // 进程是 Per-Monitor v2 感知，客户区就是物理像素，只按 DPI 补上非客户区
    RECT rect { 0, 0, options_.width, options_.height };
    if (!AdjustWindowRectExForDpi(&rect, kWindowStyle, FALSE, ex_style, dpi)) {
        log::Error("AdjustWindowRectExForDpi failed: {}", GetLastError());
        return;
    }

    const POINT position = origin ? *origin : POINT {};
    const UINT flags = SWP_NOZORDER | SWP_NOACTIVATE | (origin ? 0 : SWP_NOMOVE);
    SetWindowPos(hwnd_, nullptr, position.x, position.y, rect.right - rect.left, rect.bottom - rect.top, flags);
}

void ShellWindow::initWebView()
{
    // WEBVIEW2_* 环境变量优先于传入的参数，宿主进程可能设置过，统一改成本程序的值
    std::wstring arguments = kBrowserArguments;
    if (options_.debug_port > 0) {
        arguments += std::format(L" --remote-debugging-port={}", options_.debug_port);
    }
    SetEnvironmentVariableW(L"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", nullptr);
    SetEnvironmentVariableW(L"WEBVIEW2_USER_DATA_FOLDER", options_.user_data_dir.c_str());

    auto environment_options = Microsoft::WRL::Make<CoreWebView2EnvironmentOptions>();
    environment_options->put_AdditionalBrowserArguments(arguments.c_str());

    std::error_code ec;
    std::filesystem::create_directories(options_.user_data_dir, ec);
    log::Info("user data: {}", WideToUtf8(options_.user_data_dir.wstring()));

    const HRESULT hr = CreateCoreWebView2EnvironmentWithOptions(
        nullptr,
        options_.user_data_dir.c_str(),
        environment_options.Get(),
        Callback<ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler>(
            [this](HRESULT result, ICoreWebView2Environment* environment) -> HRESULT {
                onEnvironmentCreated(result, environment);
                return S_OK;
            })
            .Get());
    if (FAILED(hr)) {
        MessageBoxW(hwnd_, L"WebView2 初始化失败，请确认已安装 Microsoft Edge WebView2 Runtime。", options_.title.c_str(), MB_ICONERROR);
        fail("CreateCoreWebView2EnvironmentWithOptions", hr);
    }
}

void ShellWindow::fail(std::string_view what, HRESULT hr)
{
    log::Error("{} failed: 0x{:08x}", what, static_cast<unsigned>(hr));
    exit_code_ = 1;
    DestroyWindow(hwnd_);
}

void ShellWindow::onEnvironmentCreated(HRESULT result, ICoreWebView2Environment* environment)
{
    if (FAILED(result) || !environment) {
        fail("WebView2 environment creation", result);
        return;
    }
    environment_ = environment;

    const HRESULT hr = environment_->CreateCoreWebView2Controller(
        hwnd_,
        Callback<ICoreWebView2CreateCoreWebView2ControllerCompletedHandler>(
            [this](HRESULT controller_result, ICoreWebView2Controller* controller) -> HRESULT {
                onControllerCreated(controller_result, controller);
                return S_OK;
            })
            .Get());
    if (FAILED(hr)) {
        fail("CreateCoreWebView2Controller", hr);
    }
}

void ShellWindow::onControllerCreated(HRESULT result, ICoreWebView2Controller* controller)
{
    if (FAILED(result) || !controller) {
        fail("WebView2 controller creation", result);
        return;
    }

    controller_ = controller;
    if (const HRESULT hr = controller_->get_CoreWebView2(&webview_); FAILED(hr) || !webview_) {
        fail("get_CoreWebView2", hr);
        return;
    }
    webview_->get_BrowserProcessId(&browser_pid_);

    RECT bounds {};
    GetClientRect(hwnd_, &bounds);
    controller_->put_Bounds(bounds);

    applySettings();
    injectScripts();
    subscribeEvents();
    input_->Attach(webview_.Get());
    updateForeground(GetForegroundWindow());

    // 窗口显示时控件还没创建，焦点留在了顶层窗口上，用户的按键会被当成控制器输入
    if (GetFocus() == hwnd_) {
        controller_->MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC);
    }

    if (options_.devtools) {
        webview_->OpenDevToolsWindow();
    }

    if (const HRESULT hr = webview_->Navigate(options_.url.c_str()); FAILED(hr)) {
        fail("Navigate", hr);
        return;
    }
    log::Info("navigate {}", WideToUtf8(options_.url));
}

void ShellWindow::applySettings()
{
    ComPtr<ICoreWebView2Controller3> controller3;
    if (SUCCEEDED(controller_.As(&controller3))) {
        controller3->put_ShouldDetectMonitorScaleChanges(FALSE);
        controller3->put_RasterizationScale(1.0);
    }
    else {
        log::Error("ICoreWebView2Controller3 unavailable, viewport follows system DPI");
    }

    // 关掉会改变视口或页面状态的浏览器交互
    ComPtr<ICoreWebView2Settings> settings;
    if (FAILED(webview_->get_Settings(&settings)) || !settings) {
        log::Error("get_Settings failed");
        return;
    }
    settings->put_AreDefaultContextMenusEnabled(FALSE);
    settings->put_AreDefaultScriptDialogsEnabled(FALSE);
    settings->put_IsZoomControlEnabled(FALSE);
    settings->put_IsStatusBarEnabled(FALSE);

    ComPtr<ICoreWebView2Settings3> settings3;
    if (SUCCEEDED(settings.As(&settings3))) {
        settings3->put_AreBrowserAcceleratorKeysEnabled(FALSE);
    }
    ComPtr<ICoreWebView2Settings5> settings5;
    if (SUCCEEDED(settings.As(&settings5))) {
        settings5->put_IsPinchZoomEnabled(FALSE);
    }
    ComPtr<ICoreWebView2Settings6> settings6;
    if (SUCCEEDED(settings.As(&settings6))) {
        settings6->put_IsSwipeNavigationEnabled(FALSE);
    }
}

void ShellWindow::injectScripts()
{
    std::vector<std::wstring> scripts { LoadTextResource(IDR_SCRIPT_CLOUD_POLICY) };
    scripts.insert(scripts.end(), options_.extra_scripts.begin(), options_.extra_scripts.end());

    for (const auto& script : scripts) {
        if (script.empty()) {
            log::Error("empty page script, embedded resource missing?");
            continue;
        }
        const HRESULT hr = webview_->AddScriptToExecuteOnDocumentCreated(script.c_str(), nullptr);
        if (FAILED(hr)) {
            log::Error("AddScriptToExecuteOnDocumentCreated failed: 0x{:08x}", static_cast<unsigned>(hr));
        }
    }
}

void ShellWindow::subscribeEvents()
{
    EventRegistrationToken token {};

    // 窗口标题跟随页面标题；控制器按类名找窗口，不受影响
    webview_->add_DocumentTitleChanged(
        Callback<ICoreWebView2DocumentTitleChangedEventHandler>([this](ICoreWebView2* sender, IUnknown*) -> HRESULT {
            LPWSTR title = nullptr;
            if (SUCCEEDED(sender->get_DocumentTitle(&title)) && title) {
                std::wstring text = options_.title;
                if (*title) {
                    text += L" - ";
                    text += title;
                }
                CoTaskMemFree(title);
                SetWindowTextW(hwnd_, text.c_str());
            }
            return S_OK;
        }).Get(),
        &token);

    // 新文档里的页面策略默认窗口在前台，导航完成后补发实际状态
    webview_->add_NavigationCompleted(
        Callback<ICoreWebView2NavigationCompletedEventHandler>([this](ICoreWebView2*, ICoreWebView2NavigationCompletedEventArgs*) -> HRESULT {
            input_->SyncPageState();
            return S_OK;
        }).Get(),
        &token);

    // 官网会申请读取剪贴板，权限气泡会挡住截图与点击；只对官网放行剪贴板读取
    webview_->add_PermissionRequested(
        Callback<ICoreWebView2PermissionRequestedEventHandler>(
            [](ICoreWebView2*, ICoreWebView2PermissionRequestedEventArgs* args) -> HRESULT {
                COREWEBVIEW2_PERMISSION_KIND kind = COREWEBVIEW2_PERMISSION_KIND_UNKNOWN_PERMISSION;
                LPWSTR uri = nullptr;
                if (FAILED(args->get_PermissionKind(&kind)) || FAILED(args->get_Uri(&uri)) || !uri) {
                    return S_OK;
                }
                const std::string origin = WideToUtf8(uri);
                CoTaskMemFree(uri);

                if (kind == COREWEBVIEW2_PERMISSION_KIND_CLIPBOARD_READ && isOfficialSite(origin)) {
                    args->put_State(COREWEBVIEW2_PERMISSION_STATE_ALLOW);
                    log::Info("clipboard read allowed for {}", origin);
                }
                return S_OK;
            })
            .Get(),
        &token);

    // 云端会让页面打开外部链接；只有一个视图，在当前视图打开会断开游戏会话，一律不开
    webview_->add_NewWindowRequested(
        Callback<ICoreWebView2NewWindowRequestedEventHandler>(
            [](ICoreWebView2*, ICoreWebView2NewWindowRequestedEventArgs* args) -> HRESULT {
                args->put_Handled(TRUE);
                LPWSTR uri = nullptr;
                if (SUCCEEDED(args->get_Uri(&uri)) && uri) {
                    log::Warn("new window blocked: {}", WideToUtf8(uri));
                    CoTaskMemFree(uri);
                }
                return S_OK;
            })
            .Get(),
        &token);

    // 页面对话框会阻塞渲染主线程，输入随之卡住。提示框和离开页面的确认直接接受，其余按取消处理
    webview_->add_ScriptDialogOpening(
        Callback<ICoreWebView2ScriptDialogOpeningEventHandler>(
            [](ICoreWebView2*, ICoreWebView2ScriptDialogOpeningEventArgs* args) -> HRESULT {
                COREWEBVIEW2_SCRIPT_DIALOG_KIND kind = COREWEBVIEW2_SCRIPT_DIALOG_KIND_ALERT;
                args->get_Kind(&kind);
                LPWSTR message = nullptr;
                args->get_Message(&message);
                log::Warn("script dialog kind={}: {}", static_cast<int>(kind), message ? WideToUtf8(message) : "");
                CoTaskMemFree(message);
                if (kind == COREWEBVIEW2_SCRIPT_DIALOG_KIND_ALERT || kind == COREWEBVIEW2_SCRIPT_DIALOG_KIND_BEFOREUNLOAD) {
                    args->Accept();
                }
                return S_OK;
            })
            .Get(),
        &token);

    // 长时间挂机时渲染进程可能崩溃或卡死，重新加载页面；浏览器进程退出就无法恢复，退出程序
    webview_->add_ProcessFailed(
        Callback<ICoreWebView2ProcessFailedEventHandler>(
            [this](ICoreWebView2* sender, ICoreWebView2ProcessFailedEventArgs* args) -> HRESULT {
                COREWEBVIEW2_PROCESS_FAILED_KIND kind = COREWEBVIEW2_PROCESS_FAILED_KIND_BROWSER_PROCESS_EXITED;
                args->get_ProcessFailedKind(&kind);
                switch (kind) {
                case COREWEBVIEW2_PROCESS_FAILED_KIND_BROWSER_PROCESS_EXITED:
                    fail("WebView2 browser process", E_FAIL);
                    break;
                case COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED:
                case COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_UNRESPONSIVE:
                    log::Error("render process failed (kind={}), reloading", static_cast<int>(kind));
                    sender->Reload();
                    break;
                default:
                    log::Warn("WebView2 process failed (kind={})", static_cast<int>(kind));
                    break;
                }
                return S_OK;
            })
            .Get(),
        &token);
}

void CALLBACK ShellWindow::onForegroundChanged(HWINEVENTHOOK, DWORD, HWND hwnd, LONG, LONG, DWORD, DWORD)
{
    if (g_foreground_owner) {
        g_foreground_owner->updateForeground(hwnd);
    }
}

void ShellWindow::updateForeground(HWND foreground)
{
    DWORD pid = 0;
    if (foreground) {
        GetWindowThreadProcessId(foreground, &pid);
    }
    // 指针锁定提示气泡等窗口属于浏览器进程，它们在前台时用户仍在操作本窗口
    input_->SetForeground(pid != 0 && (pid == GetCurrentProcessId() || pid == browser_pid_));
}

} // namespace cloudshell
