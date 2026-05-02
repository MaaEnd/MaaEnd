#include "WebView2.h"

#ifdef _WIN32

#include <filesystem>
#include <system_error>

#include <objbase.h>

#include <wrl.h>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>


namespace
{

std::wstring utf8ToWide(const std::string& src)
{
    if (src.empty()) {
        return {};
    }
    int needed = MultiByteToWideChar(CP_UTF8, 0, src.data(), static_cast<int>(src.size()), nullptr, 0);
    if (needed <= 0) {
        return {};
    }
    std::wstring out(static_cast<size_t>(needed), L'\0');
    MultiByteToWideChar(CP_UTF8, 0, src.data(), static_cast<int>(src.size()), out.data(), needed);
    return out;
}

// 计算 cpp-algo 专属的 WebView2 user data folder，并把它同步写到环境变量。
//
// 背景：MXU 等基于 Tauri 的宿主在启动时会设置进程级 WEBVIEW2_USER_DATA_FOLDER
// （为了规避中文用户名导致默认 UDF 创建失败），并会被它派生的子进程整体继承。
// 而 WebView2 SDK 在 CreateCoreWebView2EnvironmentWithOptions 里**会让该环境
// 变量无条件覆盖**我们传入的 userDataFolder 参数（参见微软 webview2-idl 文档：
// "If you find an override environment variable, use the ... userDataFolder
// values as replacements for the corresponding values in
// CreateCoreWebView2EnvironmentWithOptions parameters."），于是 cpp-algo 与
// 宿主共享同一份 UDF，实际进入同一个 shared browser process。
//
// 一旦宿主 WebView2 已经用某套配置（DPI 感知 / additionalBrowserArguments 等）
// 抢先把 shared browser process 拉起来，cpp-algo 再用不一致的配置去访问同一个
// UDF，CreateCoreWebView2Controller 会以 0x8007139F (ERROR_INVALID_STATE) 失败。
//
// 解决方式：调用方需要在调用 SDK 之前同时做两件事——
//   1. 改写当前进程的 WEBVIEW2_USER_DATA_FOLDER 环境变量到 cpp-algo 专属 UDF；
//      （由本函数完成）
//   2. 把同一个路径作为 userDataFolder 参数显式传给 SDK。
//      （由调用方完成）
// 双保险：当前 SDK 的优先级是 env var > 参数，第 1 步生效；
// 一旦未来 Microsoft 把优先级调成参数 > env var（issue #1338 里 Microsoft
// 也提过想这样调），第 2 步也能让我们继续命中专属 UDF，向前兼容。
//
// 返回的路径默认是 cpp-algo.exe 同目录下的 "<exe>.WebView2"（与 SDK 没有任何
// override 时的默认命名规则保持一致）。GetModuleFileNameW 失败时返回空路径，
// 表示此次无法接管，调用方应当回退到 nullptr 让 SDK 自己处理。
std::filesystem::path redirect_user_data_folder()
{
    wchar_t exe_buf[MAX_PATH] = {};
    DWORD len = GetModuleFileNameW(nullptr, exe_buf, MAX_PATH);
    if (len == 0 || len >= MAX_PATH) {
        LogWarn << "WebView2: GetModuleFileNameW failed, fall back to inherited UDF env"
                << VAR(GetLastError());
        return {};
    }

    std::filesystem::path udf(exe_buf);
    udf += L".WebView2";

    std::error_code ec;
    std::filesystem::create_directories(udf, ec);
    if (ec) {
        // 即便目录创建失败，仍然继续推进；后续 CreateCoreWebView2EnvironmentWithOptions
        // 会给出更精确的错误（如 E_ACCESSDENIED），避免我们在此默默回退到宿主共享 UDF。
        LogWarn << "WebView2: create user data folder failed"
                << VAR(MAA_NS::path_to_utf8_string(udf)) << VAR(ec.message());
    }

    if (!SetEnvironmentVariableW(L"WEBVIEW2_USER_DATA_FOLDER", udf.c_str())) {
        LogWarn << "WebView2: SetEnvironmentVariableW(WEBVIEW2_USER_DATA_FOLDER) failed"
                << VAR(GetLastError());
        // 写环境变量失败，仍然把路径返回出去；调用方至少能通过显式参数尝试一次。
        return udf;
    }

    LogInfo << "WebView2: redirected user data folder"
            << VAR(MAA_NS::path_to_utf8_string(udf));
    return udf;
}

} // namespace

WebView2::WebView2() = default;

WebView2::~WebView2()
{
    Close();
}

bool WebView2::Open()
{
    if (!FramelessWindow::Open()) {
        return false;
    }

    std::unique_lock<std::mutex> lock(webview_init_mutex_);
    webview_init_cv_.wait(lock, [this] { return webview_init_done_; });
    return webview_init_ok_;
}

void WebView2::SetURL(std::string url)
{
    if (isOpened()) {
        LogWarn << "WebView2::SetURL: ignored, must be called before Open()" << VAR(url);
        return;
    }
    initial_url_ = std::move(url);
}

void WebView2::SetTouchEmulation(bool enabled)
{
    if (isOpened()) {
        LogWarn << "WebView2::SetTouchEmulation: ignored, must be called before Open()" << VAR(enabled);
        return;
    }
    touch_emulation_ = enabled;
}

void WebView2::SetContextMenuEnabled(bool enabled)
{
    if (isOpened()) {
        LogWarn << "WebView2::SetContextMenuEnabled: ignored, must be called before Open()" << VAR(enabled);
        return;
    }
    context_menu_enabled_ = enabled;
}

void WebView2::onUiThreadInit()
{
    // WebView2 必须运行在 STA 线程上。CoInit 必须在任何 WebView2 调用之前完成。
    HRESULT com_hr = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE);
    com_initialized_ = SUCCEEDED(com_hr);
    if (!com_initialized_) {
        // RPC_E_CHANGED_MODE 表示线程之前已经被初始化为别的模式，不算严重错误，
        // 这里仍尝试继续初始化 WebView2，由 SDK 自己报错。
        LogWarn << "WebView2: CoInitializeEx failed" << VAR(com_hr);
    }

    initializeWebView();
}

void WebView2::onUiThreadShutdown()
{
    // 先 Close controller 让 WebView2 自身释放进程资源；再清空 ComPtr 触发 Release。
    if (controller_) {
        controller_->Close();
    }
    webview_.Reset();
    controller_.Reset();
    environment_.Reset();

    // 防御性兜底：如果窗口在 WebView2 还没初始化完成时就被关掉，
    // signalInitDone 还没被调用过，业务线程可能在 Open() 里永远等待。
    // 这里以失败状态唤醒它（signalInitDone 内部已经做了幂等判断）。
    signalInitDone(false);

    // 必须在所有 ComPtr 释放完成后再 CoUninitialize，避免悬挂的 COM 引用。
    if (com_initialized_) {
        CoUninitialize();
        com_initialized_ = false;
    }
}

std::optional<LRESULT> WebView2::onMessage(UINT msg, WPARAM, LPARAM)
{
    if (msg == WM_SIZE && controller_) {
        resizeToClientRect();
    }
    // 始终返回 nullopt 让基类继续处理。
    return std::nullopt;
}

void WebView2::initializeWebView()
{
    HWND hwnd = GetHwnd();
    if (!hwnd) {
        LogError << "WebView2::initializeWebView: hwnd is null";
        signalInitDone(false);
        return;
    }

    // 必须在 CreateCoreWebView2EnvironmentWithOptions 之前完成；已经创建过的
    // environment 不会回头读环境变量。详见 redirect_user_data_folder 注释。
    const std::filesystem::path udf = redirect_user_data_folder();
    const wchar_t* udf_param = udf.empty() ? nullptr : udf.c_str();

    using Microsoft::WRL::Callback;
    using EnvHandler = ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler;

    HRESULT hr = CreateCoreWebView2EnvironmentWithOptions(
        nullptr,   // browserExecutableFolder：使用系统已安装的 WebView2 Runtime
        udf_param, // userDataFolder：和环境变量保持一致，双保险
        nullptr,   // environmentOptions
        Callback<EnvHandler>([this](HRESULT result, ICoreWebView2Environment* env) -> HRESULT {
            // 该回调在 UI 线程上被触发（创建时所在的 STA 线程）。
            onEnvironmentCreated(result, env);
            return S_OK;
        }).Get());

    if (FAILED(hr)) {
        LogError << "WebView2: CreateCoreWebView2EnvironmentWithOptions failed" << VAR(hr);
        // 同步失败时回调不会触发，必须在这里直接通知等待方。
        signalInitDone(false);
    }
}

void WebView2::onEnvironmentCreated(HRESULT result, ICoreWebView2Environment* env)
{
    if (FAILED(result) || !env) {
        LogError << "WebView2: environment creation failed" << VAR(result);
        signalInitDone(false);
        return;
    }

    HWND hwnd = GetHwnd();
    if (!hwnd) {
        LogError << "WebView2: hwnd is gone before environment ready";
        signalInitDone(false);
        return;
    }

    environment_ = env;

    using Microsoft::WRL::Callback;
    using ControllerHandler = ICoreWebView2CreateCoreWebView2ControllerCompletedHandler;

    HRESULT hr = environment_->CreateCoreWebView2Controller(
        hwnd,
        Callback<ControllerHandler>([this](HRESULT r, ICoreWebView2Controller* controller) -> HRESULT {
            onControllerCreated(r, controller);
            return S_OK;
        }).Get());

    if (FAILED(hr)) {
        LogError << "WebView2: CreateCoreWebView2Controller failed" << VAR(hr);
        signalInitDone(false);
    }
}

void WebView2::onControllerCreated(HRESULT result, ICoreWebView2Controller* controller)
{
    if (FAILED(result) || !controller) {
        LogError << "WebView2: controller creation failed" << VAR(result);
        signalInitDone(false);
        return;
    }

    controller_ = controller;
    HRESULT hr = controller_->get_CoreWebView2(&webview_);
    if (FAILED(hr) || !webview_) {
        LogError << "WebView2: get_CoreWebView2 failed" << VAR(hr);
        signalInitDone(false);
        return;
    }

    // 应用 SetContextMenuEnabled 配置。设置接口必须在 Navigate 之前生效，否则首屏的右键菜单仍会出现。
    Microsoft::WRL::ComPtr<ICoreWebView2Settings> settings;
    if (SUCCEEDED(webview_->get_Settings(&settings)) && settings) {
        settings->put_AreDefaultContextMenusEnabled(context_menu_enabled_ ? TRUE : FALSE);
    }
    else {
        LogWarn << "WebView2: get_Settings failed, context menu config not applied";
    }

    resizeToClientRect();
    LogInfo << "WebView2: ready";

    // 触屏仿真必须在 Navigate 之前启用，否则页面初始化时读到的还是「无触屏」状态。
    // 用 CDP 的 Emulation.setTouchEmulationEnabled 让 Chromium 上报具备触屏，
    // 这会同时开启 TouchEvent 接口（document.createEvent("TouchEvent") 不再 throw）。
    if (touch_emulation_) {
        using Microsoft::WRL::Callback;
        using DevToolsHandler = ICoreWebView2CallDevToolsProtocolMethodCompletedHandler;

        HRESULT cdp_hr = webview_->CallDevToolsProtocolMethod(
            L"Emulation.setTouchEmulationEnabled",
            L"{\"enabled\":true,\"maxTouchPoints\":5}",
            Callback<DevToolsHandler>([](HRESULT err, LPCWSTR /*returnObjectAsJson*/) -> HRESULT {
                if (FAILED(err)) {
                    LogError << "WebView2: setTouchEmulationEnabled failed" << VAR(err);
                }
                return S_OK;
            }).Get());

        if (FAILED(cdp_hr)) {
            LogError << "WebView2: CallDevToolsProtocolMethod sync failed" << VAR(cdp_hr);
        }
    }

    // 应用 Open() 之前由 SetURL 配置的初始地址。空 URL 表示开发者不需要导航，留空白页即可。
    if (!initial_url_.empty()) {
        std::wstring wurl = utf8ToWide(initial_url_);
        HRESULT navigate_hr = webview_->Navigate(wurl.c_str());
        if (FAILED(navigate_hr)) {
            LogError << "WebView2: Navigate failed" << VAR(navigate_hr) << VAR(initial_url_);
        }
        else {
            LogInfo << "WebView2: navigated" << VAR(initial_url_);
        }
    }

    // 控件已就绪，业务线程可以继续。Navigate 失败不影响整体「窗口可用」的语义。
    signalInitDone(true);
}

void WebView2::signalInitDone(bool ok)
{
    std::lock_guard<std::mutex> lock(webview_init_mutex_);
    if (webview_init_done_) {
        return;
    }
    webview_init_done_ = true;
    webview_init_ok_ = ok;
    webview_init_cv_.notify_all();
}

void WebView2::resizeToClientRect()
{
    if (!controller_) {
        return;
    }

    // 留出基类的 chrome 边距：顶部 caption + 四周 resize 边框；这部分由基类 WM_NCHITTEST
    // 原生处理拖拽 / 缩放。基类在 onPaint 里把 chrome 涂成深色，配合 WS_CLIPCHILDREN 自然分割。
    RECT rc = getContentRect();
    controller_->put_Bounds(rc);
}

#else // !_WIN32

#include <MaaUtils/Logger.h>

WebView2::WebView2() = default;

WebView2::~WebView2()
{
    Close();
}

bool WebView2::Open()
{
    return FramelessWindow::Open();
}

void WebView2::SetURL(std::string url)
{
    if (isOpened()) {
        LogWarn << "WebView2::SetURL: ignored, must be called before Open()" << VAR(url);
        return;
    }
}

void WebView2::SetTouchEmulation(bool enabled)
{
    if (isOpened()) {
        LogWarn << "WebView2::SetTouchEmulation: ignored, must be called before Open()" << VAR(enabled);
        return;
    }
}

void WebView2::SetContextMenuEnabled(bool enabled)
{
    if (isOpened()) {
        LogWarn << "WebView2::SetContextMenuEnabled: ignored, must be called before Open()" << VAR(enabled);
        return;
    }
}

#endif // _WIN32
