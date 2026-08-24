#include "WebView2.h"

#ifdef _WIN32

#include <algorithm>
#include <filesystem>
#include <system_error>

#include <objbase.h>

#include <wrl.h>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

namespace
{

// UI 线程上取出待办并执行。COM 调用只允许发生在创建控件的 STA 线程上，
// 业务线程的 ExecuteScript / CDP 调用都要先绕这条消息回到 UI 线程。
constexpr UINT kMsgDrainPendingCalls = WM_APP + 1;

std::string wideToUtf8(const std::wstring& src)
{
    if (src.empty()) {
        return {};
    }
    int needed = WideCharToMultiByte(CP_UTF8, 0, src.data(), static_cast<int>(src.size()), nullptr, 0, nullptr, nullptr);
    if (needed <= 0) {
        return {};
    }
    std::string out(static_cast<size_t>(needed), '\0');
    WideCharToMultiByte(CP_UTF8, 0, src.data(), static_cast<int>(src.size()), out.data(), needed, nullptr, nullptr);
    return out;
}

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
        LogWarn << "WebView2: GetModuleFileNameW failed, fall back to inherited UDF env" << VAR(GetLastError());
        return {};
    }

    std::filesystem::path udf(exe_buf);
    udf += L".WebView2";

    std::error_code ec;
    std::filesystem::create_directories(udf, ec);
    if (ec) {
        // 即便目录创建失败，仍然继续推进；后续 CreateCoreWebView2EnvironmentWithOptions
        // 会给出更精确的错误（如 E_ACCESSDENIED），避免我们在此默默回退到宿主共享 UDF。
        LogWarn << "WebView2: create user data folder failed" << VAR(MAA_NS::path_to_utf8_string(udf)) << VAR(ec.message());
    }

    if (!SetEnvironmentVariableW(L"WEBVIEW2_USER_DATA_FOLDER", udf.c_str())) {
        LogWarn << "WebView2: SetEnvironmentVariableW(WEBVIEW2_USER_DATA_FOLDER) failed" << VAR(GetLastError());
        // 写环境变量失败，仍然把路径返回出去；调用方至少能通过显式参数尝试一次。
        return udf;
    }

    LogInfo << "WebView2: redirected user data folder" << VAR(MAA_NS::path_to_utf8_string(udf));
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

void WebView2::SetUserAgent(std::string user_agent)
{
    if (isOpened()) {
        LogWarn << "WebView2::SetUserAgent: ignored, must be called before Open()" << VAR(user_agent);
        return;
    }
    user_agent_ = std::move(user_agent);
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
    // 待办里捕获的回调可能持有业务侧对象，必须在 UI 线程上、控件还活着时丢弃，
    // 不能留到析构后再释放。事件接收器同理，它们的生命周期绑着 webview_。
    {
        std::lock_guard<std::mutex> lock(pending_mutex_);
        pending_calls_.clear();
    }
    devtools_receivers_.clear();

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
    if (msg == kMsgDrainPendingCalls) {
        drainPendingCalls();
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

    // 应用 settings 类配置。所有 settings 必须在 Navigate 之前生效，否则首屏的对应行为已经按默认值发生（比如右键菜单已弹、首次请求 UA
    // 已发出）。
    Microsoft::WRL::ComPtr<ICoreWebView2Settings> settings;
    if (SUCCEEDED(webview_->get_Settings(&settings)) && settings) {
        settings->put_AreDefaultContextMenusEnabled(context_menu_enabled_ ? TRUE : FALSE);

        if (!user_agent_.empty()) {
            Microsoft::WRL::ComPtr<ICoreWebView2Settings2> settings2;
            // put_UserAgent 是 Settings2 才有的扩展接口（WebView2 Runtime 86+），
            // QI 失败说明运行时太旧；此时只记 warn，不打断初始化。
            if (SUCCEEDED(settings.As(&settings2)) && settings2) {
                std::wstring wua = utf8ToWide(user_agent_);
                HRESULT ua_hr = settings2->put_UserAgent(wua.c_str());
                if (FAILED(ua_hr)) {
                    LogError << "WebView2: put_UserAgent failed" << VAR(ua_hr) << VAR(user_agent_);
                }
                else {
                    LogInfo << "WebView2: user agent overridden" << VAR(user_agent_);
                }
            }
            else {
                LogWarn << "WebView2: ICoreWebView2Settings2 unavailable, user agent override skipped" << VAR(user_agent_);
            }
        }
    }
    else {
        LogWarn << "WebView2: get_Settings failed, settings (context menu / user agent) not applied";
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

bool WebView2::postToUiThread(std::function<void()> fn)
{
    HWND hwnd = GetHwnd();
    if (!isOpened() || !hwnd) {
        return false;
    }

    uint64_t id = 0;
    {
        std::lock_guard<std::mutex> lock(pending_mutex_);
        id = ++next_pending_id_;
        pending_calls_.push_back(PendingCall { .id = id, .fn = std::move(fn) });
    }

    if (!PostMessageW(hwnd, kMsgDrainPendingCalls, 0, 0)) {
        // 消息没投出去（窗口正在销毁），把刚排进去的那条撤回来，避免它悬在队列里永不执行。
        // 必须按 id 撤：这期间别的线程可能也排了新的，按尾部撤会撤掉别人的待办。
        std::lock_guard<std::mutex> lock(pending_mutex_);
        auto it = std::find_if(pending_calls_.begin(), pending_calls_.end(), [id](const PendingCall& call) { return call.id == id; });
        if (it == pending_calls_.end()) {
            // 已经被别的消息整批换出去执行了（或关窗时清掉了），这条不归我们撤；
            // 此时再返回 false 会让调用方补一次失败回调，和已经执行的那次凑成两次。
            return true;
        }
        pending_calls_.erase(it);
        return false;
    }
    return true;
}

void WebView2::drainPendingCalls()
{
    // 先整体换出再执行：待办里可能继续调 postToUiThread，持锁执行会自死锁。
    std::vector<PendingCall> batch;
    {
        std::lock_guard<std::mutex> lock(pending_mutex_);
        batch.swap(pending_calls_);
    }

    for (auto& call : batch) {
        call.fn();
    }
}

void WebView2::ExecuteScript(std::string script, std::function<void(bool ok, std::string result_json)> on_done)
{
    auto task = [this, script = std::move(script), on_done]() {
        if (!webview_) {
            LogWarn << "WebView2: ExecuteScript before webview ready";
            if (on_done) {
                on_done(false, {});
            }
            return;
        }

        using Microsoft::WRL::Callback;
        using ScriptHandler = ICoreWebView2ExecuteScriptCompletedHandler;

        std::wstring wscript = utf8ToWide(script);
        HRESULT hr = webview_->ExecuteScript(wscript.c_str(), Callback<ScriptHandler>([on_done](HRESULT err, LPCWSTR result) -> HRESULT {
                                                                  if (FAILED(err)) {
                                                                      LogError << "WebView2: ExecuteScript failed" << VAR(err);
                                                                  }
                                                                  if (on_done) {
                                                                      on_done(SUCCEEDED(err), result ? wideToUtf8(result) : std::string {});
                                                                  }
                                                                  return S_OK;
                                                              }).Get());

        if (FAILED(hr)) {
            LogError << "WebView2: ExecuteScript dispatch failed" << VAR(hr);
            if (on_done) {
                on_done(false, {});
            }
        }
    };

    if (!postToUiThread(std::move(task)) && on_done) {
        on_done(false, {});
    }
}

void WebView2::CallDevToolsMethod(
    std::string method,
    std::string params_json,
    std::function<void(bool ok, std::string result_json)> on_done)
{
    auto task = [this, method = std::move(method), params_json = std::move(params_json), on_done]() {
        if (!webview_) {
            LogWarn << "WebView2: CallDevToolsMethod before webview ready" << VAR(method);
            if (on_done) {
                on_done(false, {});
            }
            return;
        }

        using Microsoft::WRL::Callback;
        using DevToolsHandler = ICoreWebView2CallDevToolsProtocolMethodCompletedHandler;

        std::wstring wmethod = utf8ToWide(method);
        std::wstring wparams = utf8ToWide(params_json.empty() ? std::string { "{}" } : params_json);
        HRESULT hr = webview_->CallDevToolsProtocolMethod(
            wmethod.c_str(),
            wparams.c_str(),
            Callback<DevToolsHandler>([on_done, method](HRESULT err, LPCWSTR result) -> HRESULT {
                if (FAILED(err)) {
                    // 方法级失败（如响应体已被回收）是可恢复错误，是否兜底由调用方决定。
                    const std::string result_str = result ? wideToUtf8(result) : std::string {};
                    LogWarn << "WebView2: CallDevToolsProtocolMethod failed" << VAR(err) << VAR(method) << VAR(result_str);
                }
                if (on_done) {
                    on_done(SUCCEEDED(err), result ? wideToUtf8(result) : std::string {});
                }
                return S_OK;
            }).Get());

        if (FAILED(hr)) {
            LogError << "WebView2: CallDevToolsProtocolMethod dispatch failed" << VAR(hr) << VAR(method);
            if (on_done) {
                on_done(false, {});
            }
        }
    };

    if (!postToUiThread(std::move(task)) && on_done) {
        on_done(false, {});
    }
}

void WebView2::SubscribeDevToolsEvent(std::string method, std::function<void(std::string params_json)> on_event)
{
    auto task = [this, method = std::move(method), on_event = std::move(on_event)]() {
        if (!webview_ || !on_event) {
            LogWarn << "WebView2: SubscribeDevToolsEvent before webview ready" << VAR(method);
            return;
        }

        using Microsoft::WRL::Callback;
        using EventHandler = ICoreWebView2DevToolsProtocolEventReceivedEventHandler;

        std::wstring wmethod = utf8ToWide(method);
        Microsoft::WRL::ComPtr<ICoreWebView2DevToolsProtocolEventReceiver> receiver;
        HRESULT hr = webview_->GetDevToolsProtocolEventReceiver(wmethod.c_str(), &receiver);
        if (FAILED(hr) || !receiver) {
            LogError << "WebView2: GetDevToolsProtocolEventReceiver failed" << VAR(hr) << VAR(method);
            return;
        }

        EventRegistrationToken token {};
        hr = receiver->add_DevToolsProtocolEventReceived(
            Callback<EventHandler>(
                [on_event, method](ICoreWebView2*, ICoreWebView2DevToolsProtocolEventReceivedEventArgs* args) -> HRESULT {
                    if (!args) {
                        return S_OK;
                    }
                    // 参数串由 WebView2 用 CoTaskMemAlloc 分配，所有权转交给我们。
                    LPWSTR params = nullptr;
                    if (FAILED(args->get_ParameterObjectAsJson(&params)) || !params) {
                        LogWarn << "WebView2: devtools event without parameters" << VAR(method);
                        return S_OK;
                    }
                    std::string params_json = wideToUtf8(params);
                    CoTaskMemFree(params);
                    on_event(std::move(params_json));
                    return S_OK;
                })
                .Get(),
            &token);

        if (FAILED(hr)) {
            LogError << "WebView2: add_DevToolsProtocolEventReceived failed" << VAR(hr) << VAR(method);
            return;
        }

        // 保活到 onUiThreadShutdown；接收器一旦释放，上面注册的 token 随之失效。
        devtools_receivers_.push_back(std::move(receiver));
        LogInfo << "WebView2: subscribed devtools event" << VAR(method);
    };

    postToUiThread(std::move(task));
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

void WebView2::SetUserAgent(std::string user_agent)
{
    if (isOpened()) {
        LogWarn << "WebView2::SetUserAgent: ignored, must be called before Open()" << VAR(user_agent);
        return;
    }
}

void WebView2::ExecuteScript(std::string script, std::function<void(bool ok, std::string result_json)> on_done)
{
    LogWarn << "WebView2::ExecuteScript: no webview on this platform" << VAR(script.size());
    if (on_done) {
        on_done(false, {});
    }
}

void WebView2::CallDevToolsMethod(
    std::string method,
    [[maybe_unused]] std::string params_json,
    std::function<void(bool ok, std::string result_json)> on_done)
{
    LogWarn << "WebView2::CallDevToolsMethod: no webview on this platform" << VAR(method);
    if (on_done) {
        on_done(false, {});
    }
}

void WebView2::SubscribeDevToolsEvent(std::string method, [[maybe_unused]] std::function<void(std::string params_json)> on_event)
{
    LogWarn << "WebView2::SubscribeDevToolsEvent: no webview on this platform" << VAR(method);
}

#endif // _WIN32
