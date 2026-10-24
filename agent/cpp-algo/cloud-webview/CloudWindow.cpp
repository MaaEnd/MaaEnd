#include "CloudWindow.h"

#include <algorithm>
#include <cctype>
#include <memory>
#include <unordered_map>
#include <utility>

#include <MaaUtils/Logger.h>
#include <wrl.h>

#include "input_policy.h"
#include "video_frame_policy.h"

namespace
{

constexpr wchar_t kWindowTitle[] = L"MaaEnd - Cloud Endfield";
constexpr int kViewportWidth = 1280;
constexpr int kViewportHeight = 720;

} // namespace

CloudWindow::CloudWindow()
{
    SetSystemTitleBar(true);
    SetActivateOnShow(false);
    SetURL("https://endfield.hypergryph.com/cloud");
    SetContextMenuEnabled(false);
}

CloudWindow::~CloudWindow()
{
    Close();
}

bool CloudWindow::publish()
{
    return SetWindowTextW(GetHwnd(), kWindowTitle);
}

void CloudWindow::waitForClose()
{
    std::unique_lock lock(mutex_);
    closed_cv_.wait(lock, [this] { return closed_; });
}

void CloudWindow::onUiThreadInit()
{
    const auto hwnd = GetHwnd();
    WNDCLASSW shield_class {};
    shield_class.lpfnWndProc = inputShieldProc;
    shield_class.hInstance = GetModuleHandleW(nullptr);
    shield_class.hCursor = LoadCursorW(nullptr, MAKEINTRESOURCEW(32512)); // IDC_ARROW
    shield_class.hbrBackground = static_cast<HBRUSH>(GetStockObject(BLACK_BRUSH));
    shield_class.lpszClassName = L"MaaEnd.CloudInputShield";
    RegisterClassW(&shield_class);
    input_shield_ = CreateWindowExW(
        WS_EX_LAYERED,
        shield_class.lpszClassName,
        L"",
        WS_CHILD | WS_VISIBLE,
        0,
        0,
        kViewportWidth,
        kViewportHeight,
        hwnd,
        nullptr,
        shield_class.hInstance,
        this);
    if (input_shield_) {
        // Alpha zero would make Windows pass mouse events through the shield.
        // This sibling intercepts desktop input only; CDP and video capture
        // still address the underlying WebView directly, including iframes.
        SetLayeredWindowAttributes(input_shield_, 0, 1, LWA_ALPHA);
    }
    else {
        LogError << "Cloud: input shield creation failed" << VAR(GetLastError());
    }
    RECT rect { 0, 0, kViewportWidth, kViewportHeight };
    if (AdjustWindowRectExForDpi(
            &rect,
            static_cast<DWORD>(GetWindowLongPtrW(hwnd, GWL_STYLE)),
            FALSE,
            static_cast<DWORD>(GetWindowLongPtrW(hwnd, GWL_EXSTYLE)),
            GetDpiForWindow(hwnd))) {
        SetWindowPos(hwnd, nullptr, 0, 0, rect.right - rect.left, rect.bottom - rect.top, SWP_NOMOVE | SWP_NOZORDER | SWP_NOACTIVATE);
    }
    WebView2::onUiThreadInit();
}

LRESULT CALLBACK CloudWindow::inputShieldProc(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam)
{
    if (msg == WM_NCCREATE) {
        const auto create = reinterpret_cast<CREATESTRUCTW*>(lparam);
        SetWindowLongPtrW(hwnd, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(create->lpCreateParams));
    }
    const auto window = reinterpret_cast<CloudWindow*>(GetWindowLongPtrW(hwnd, GWLP_USERDATA));
    if (msg == WM_MOUSEACTIVATE) {
        return MA_ACTIVATE;
    }
    if (window && msg == WM_CAPTURECHANGED) {
        window->shield_release_message_ = 0;
        return 0;
    }
    if (window && (msg == WM_LBUTTONDOWN || msg == WM_RBUTTONDOWN || msg == WM_MBUTTONDOWN || msg == WM_XBUTTONDOWN)) {
        // A deliberate click is the only way to enable physical page input.
        // Eat its full down/up pair so it cannot release an automated hold or
        // activate a game control.
        window->shield_release_message_ = msg == WM_LBUTTONDOWN   ? WM_LBUTTONUP
                                          : msg == WM_RBUTTONDOWN ? WM_RBUTTONUP
                                          : msg == WM_MBUTTONDOWN ? WM_MBUTTONUP
                                                                  : WM_XBUTTONUP;
        SetCapture(hwnd);
        return 0;
    }
    if (window && GetCapture() == hwnd && msg == window->shield_release_message_) {
        ReleaseCapture();
        window->shield_release_message_ = 0;
        if (GetForegroundWindow() != window->GetHwnd()) {
            window->shieldPhysicalInput();
            return 0;
        }
        window->manual_input_enabled_ = true;
        ShowWindow(hwnd, SW_HIDE);
        if (window->GetController()) {
            window->GetController()->MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC);
        }
        return 0;
    }
    if (window) {
        // Never let hover, wheel, extra buttons or an unmatched release leak
        // through while the shield is active.
        switch (msg) {
        case WM_MOUSEMOVE:
        case WM_MOUSEHOVER:
        case WM_MOUSELEAVE:
        case WM_MOUSEWHEEL:
        case WM_MOUSEHWHEEL:
        case WM_LBUTTONUP:
        case WM_RBUTTONUP:
        case WM_MBUTTONUP:
        case WM_XBUTTONUP:
        case WM_NCMOUSEMOVE:
            return 0;
        default:
            break;
        }
    }
    return DefWindowProcW(hwnd, msg, wparam, lparam);
}

void CloudWindow::shieldPhysicalInput()
{
    manual_input_enabled_ = false;
    shield_release_message_ = 0;
    if (input_shield_ && GetCapture() == input_shield_) {
        ReleaseCapture();
    }
    RECT client {};
    if (input_shield_ && GetClientRect(GetHwnd(), &client)) {
        SetWindowPos(input_shield_, HWND_TOP, 0, 0, client.right, client.bottom, SWP_NOACTIVATE | SWP_SHOWWINDOW);
    }
}

std::optional<LRESULT> CloudWindow::onMessage(UINT msg, WPARAM wparam, LPARAM lparam)
{
    if (msg == WM_DPICHANGED) {
        // 采用新显示器建议的外窗大小；网页和输入仍使用同一套 720p 坐标。
        const auto rect = reinterpret_cast<const RECT*>(lparam);
        if (rect
            && !SetWindowPos(
                GetHwnd(),
                nullptr,
                rect->left,
                rect->top,
                rect->right - rect->left,
                rect->bottom - rect->top,
                SWP_NOZORDER | SWP_NOACTIVATE)) {
            LogWarn << "Cloud: DPI window resize failed" << VAR(GetLastError());
        }
        return 0;
    }
    if ((msg == WM_ACTIVATE && LOWORD(wparam) == WA_INACTIVE) || (msg == WM_ACTIVATEAPP && !wparam)) {
        shieldPhysicalInput();
    }
    if (msg == WM_SETFOCUS && input_shield_ && IsWindowVisible(input_shield_)) {
        // SetFocus also activates the parent. A restore/resize notification
        // must not transfer desktop focus away from another application.
        if (GetForegroundWindow() == GetHwnd()) {
            SetFocus(input_shield_);
        }
        return 0;
    }
    if ((msg == WM_MOUSEMOVE || msg == WM_MOUSEWHEEL || msg == WM_MOUSEHWHEEL) && GetForegroundWindow() != GetHwnd() && input_shield_
        && !IsWindowVisible(input_shield_)) {
        // Defend against activation-message ordering races: an inactive host
        // must be shielded before a hover is delivered to its WebView child.
        shieldPhysicalInput();
        return 0;
    }
    return WebView2::onMessage(msg, wparam, lparam);
}

void CloudWindow::onUiThreadShutdown()
{
    if (input_shield_) {
        DestroyWindow(input_shield_);
        input_shield_ = nullptr;
    }
    if (video_buffer_) {
        video_buffer_->Close();
        video_buffer_.Reset();
    }
    WebView2::onUiThreadShutdown();
    {
        std::lock_guard lock(mutex_);
        closed_ = true;
    }
    closed_cv_.notify_one();
}

void CloudWindow::captureVideoFrame(std::function<void(json::value)> on_done)
{
    const auto fail = [on_done](const char* error) {
        on_done(json::object { { "ok", false }, { "error", error } });
    };
    const auto complete = [this, on_done, fail](bool ok, const std::string& response) {
        const auto parsed = ok ? json::parse(response) : std::nullopt;
        if (!parsed || parsed->contains("exceptionDetails")) {
            fail("frame_capture_failed");
            return;
        }
        auto value = parsed->at("result").get("value", json::value {});
        if (value.is_null()) {
            fail("video_unavailable");
            return;
        }
        HANDLE mapping = nullptr;
        if (!video_buffer_ || FAILED(video_buffer_->get_FileMappingHandle(&mapping)) || !mapping) {
            fail("frame_buffer_unavailable");
            return;
        }
        value["mapping_handle"] = static_cast<int64_t>(reinterpret_cast<uintptr_t>(mapping));
        value["byte_size"] = kViewportWidth * kViewportHeight * 4;
        value["width"] = kViewportWidth;
        value["height"] = kViewportHeight;
        value["stride"] = kViewportWidth * 4;
        value["format"] = "RGBA8";
        value["protocol"] = 1;
        on_done(json::object { { "ok", true }, { "value", std::move(value) } });
    };
    const auto params =
        json::value(json::object { { "expression", cloud::kCaptureVideoFrame }, { "returnByValue", true }, { "awaitPromise", true } })
            .to_string();
    // CallDevToolsMethod dispatches both this callback and all COM work to the
    // WebView STA. No shared-buffer COM pointer is accessed by the pipe worker.
    CallDevToolsMethod("Runtime.evaluate", params, [this, complete, fail](bool ok, const std::string& response) {
        const auto parsed = ok ? json::parse(response) : std::nullopt;
        if (!parsed || parsed->contains("exceptionDetails")) {
            fail("frame_capture_failed");
            return;
        }
        const auto value = parsed->at("result").get("value", json::value {});
        if (!value.is_object() || !value.get("needs_buffer", false)) {
            complete(ok, response);
            return;
        }
        Microsoft::WRL::ComPtr<ICoreWebView2_2> view2;
        Microsoft::WRL::ComPtr<ICoreWebView2_17> view17;
        Microsoft::WRL::ComPtr<ICoreWebView2Environment> environment;
        Microsoft::WRL::ComPtr<ICoreWebView2Environment12> environment12;
        if (FAILED(GetWebView()->QueryInterface(IID_PPV_ARGS(&view2))) || FAILED(GetWebView()->QueryInterface(IID_PPV_ARGS(&view17)))
            || FAILED(view2->get_Environment(&environment)) || FAILED(environment.As(&environment12))) {
            fail("shared_buffer_unsupported");
            return;
        }
        if (!video_buffer_ && FAILED(environment12->CreateSharedBuffer(kViewportWidth * kViewportHeight * 4, &video_buffer_))) {
            fail("frame_buffer_unavailable");
            return;
        }
        if (FAILED(view17->PostSharedBufferToScript(
                video_buffer_.Get(),
                COREWEBVIEW2_SHARED_BUFFER_ACCESS_READ_WRITE,
                LR"({"purpose":"maaend-video-frame"})"))) {
            fail("frame_buffer_delivery_failed");
            return;
        }
        const auto capture = json::value(json::object { { "expression", "window.__maaEndVideoFrame.capture()" },
                                                        { "returnByValue", true },
                                                        { "awaitPromise", true } })
                                 .to_string();
        CallDevToolsMethod("Runtime.evaluate", capture, complete);
    });
}

void CloudWindow::onBeforeNavigate(std::function<void(bool)> on_done)
{
    using Microsoft::WRL::Callback;
    using ScriptHandler = ICoreWebView2AddScriptToExecuteOnDocumentCreatedCompletedHandler;
    using DevToolsHandler = ICoreWebView2CallDevToolsProtocolMethodCompletedHandler;

    if (!input_shield_) {
        LogError << "Cloud: physical input shield creation failed";
        on_done(false);
        return;
    }
    shieldPhysicalInput();

    // Record only public bootstrap script failures. Never log request headers,
    // bodies, query strings or account API traffic. Keep monitoring across
    // reloads so a repeated failure retains its actual Chromium error.
    struct BootstrapRequest
    {
        std::string source;
        json::object response;
    };

    const auto scripts = std::make_shared<std::unordered_map<std::string, BootstrapRequest>>();
    SubscribeDevToolsEvent("Network.requestWillBeSent", [scripts](const std::string& params) {
        const auto event = json::parse(params);
        if (!event || event->get("type", std::string {}) != "Script") {
            return;
        }
        auto source = event->at("request").get("url", std::string {});
        source.erase(source.find_first_of("?#") == std::string::npos ? source.size() : source.find_first_of("?#"));
        if (source == "https://web.hycdn.cn/hg_web_sdk/lib/sdk.entry.js"
            || source == "https://web.hycdn.cn/static/js/umd/react/react@18.3.1.js"
            || source == "https://web.hycdn.cn/static/js/umd/react-dom/react-dom@18.3.1.js"
            || (source.starts_with("https://cloud-game-web-static.hycdn.cn/cloud-game/prod/") && source.ends_with(".js"))) {
            scripts->insert_or_assign(event->at("requestId").as_string(), BootstrapRequest { std::move(source), {} });
        }
    });
    SubscribeDevToolsEvent("Network.responseReceived", [scripts](const std::string& params) {
        const auto event = json::parse(params);
        if (!event) {
            return;
        }
        const auto it = scripts->find(event->get("requestId", std::string {}));
        if (it == scripts->end()) {
            return;
        }
        const auto& response = event->at("response");
        it->second.response["status"] = response.get("status", 0);
        it->second.response["mime"] = response.get("mimeType", std::string {});
        it->second.response["disk_cache"] = response.get("fromDiskCache", false);
        it->second.response["service_worker"] = response.get("fromServiceWorker", false);
    });
    SubscribeDevToolsEvent("Network.responseReceivedExtraInfo", [scripts](const std::string& params) {
        const auto event = json::parse(params);
        if (!event) {
            return;
        }
        const auto it = scripts->find(event->get("requestId", std::string {}));
        if (it == scripts->end()) {
            return;
        }
        // ORB can reject before responseReceived. Retain only public response
        // metadata, never the complete headers or account/session information.
        it->second.response["status"] = event->get("statusCode", 0);
        const auto& headers = event->at("headers");
        for (const auto name : { "content-type", "content-encoding", "content-length", "x-content-type-options", "server" }) {
            for (const auto& [key, value] : headers.as_object()) {
                auto lower = key;
                std::transform(lower.begin(), lower.end(), lower.begin(), [](unsigned char c) { return std::tolower(c); });
                if (lower == name) {
                    it->second.response[name] = value;
                }
            }
        }
    });
    SubscribeDevToolsEvent("Network.loadingFailed", [this, scripts](const std::string& params) {
        const auto event = json::parse(params);
        if (!event) {
            return;
        }
        const auto it = scripts->find(event->get("requestId", std::string {}));
        if (it == scripts->end()) {
            return;
        }
        const auto source = it->second.source;
        const auto error = event->get("errorText", std::string {});
        const auto blocked = event->get("blockedReason", std::string {});
        // Preserve the public Chromium reason in pre-task diagnostics as well:
        // the detached host's console is not the GUI pre-task's output stream.
        // Canceled requests from document replacement are not load failures.
        if (!event->get("canceled", false)) {
            LogWarn << "Cloud: website bootstrap script failed" << VAR(source) << VAR(error) << VAR(blocked) << VAR(it->second.response);
            const auto failure = json::value(json::object { { "type", "network" },
                                                            { "source", source },
                                                            { "name", error },
                                                            { "blocked", blocked },
                                                            { "response", it->second.response } });
            const auto expression = "(() => { const errors = window.__maaCloudStartupErrors; if (errors && errors.length < 20) errors.push("
                                    + failure.to_string() + "); })()";
            CallDevToolsMethod("Runtime.evaluate", json::value(json::object { { "expression", expression } }).to_string());
        }
        scripts->erase(it);
    });
    SubscribeDevToolsEvent("Network.loadingFinished", [scripts](const std::string& params) {
        if (const auto event = json::parse(params)) {
            scripts->erase(event->get("requestId", std::string {}));
        }
    });

    Microsoft::WRL::ComPtr<ICoreWebView2Settings> settings;
    if (FAILED(GetWebView()->get_Settings(&settings)) || FAILED(settings->put_IsZoomControlEnabled(FALSE))) {
        LogError << "Cloud: fixed viewport zoom configuration failed";
        on_done(false);
        return;
    }

    // The cloud game does not need desktop clipboard contents. Decide before
    // the default browser prompt can interrupt input or focus.
    EventRegistrationToken permission_token {};
    const HRESULT permission_hr = GetWebView()->add_PermissionRequested(
        Callback<ICoreWebView2PermissionRequestedEventHandler>(
            [](ICoreWebView2*, ICoreWebView2PermissionRequestedEventArgs* args) -> HRESULT {
                COREWEBVIEW2_PERMISSION_KIND kind {};
                const HRESULT hr = args->get_PermissionKind(&kind);
                if (FAILED(hr) || kind != COREWEBVIEW2_PERMISSION_KIND_CLIPBOARD_READ) {
                    return hr;
                }
                LogInfo << "Cloud: clipboard read permission denied";
                return args->put_State(COREWEBVIEW2_PERMISSION_STATE_DENY);
            })
            .Get(),
        &permission_token);
    if (FAILED(permission_hr)) {
        LogError << "Cloud: clipboard permission handler registration failed" << VAR(permission_hr);
        on_done(false);
        return;
    }

    // 必须在官网脚本运行前安装，刷新/导航/子框架也继承同一策略。
    const HRESULT hr = GetWebView()->AddScriptToExecuteOnDocumentCreated(
        cloud::kInputPolicy,
        Callback<ScriptHandler>([this, on_done](HRESULT error, LPCWSTR) -> HRESULT {
            if (FAILED(error)) {
                LogError << "Cloud: input policy registration failed" << VAR(error);
                on_done(false);
                return S_OK;
            }
            // The SDK uses screen size and devicePixelRatio for stream quality
            // and relative input. Keep those metrics in the same 720p space as
            // the controller instead of inheriting desktop/window zoom.
            configureViewport([this, on_done](bool ok) {
                if (!ok) {
                    on_done(false);
                    return;
                }
                // Page-local focus only; desktop focus and cursor stay user-controlled.
                const HRESULT focus_hr = GetWebView()->CallDevToolsProtocolMethod(
                    L"Emulation.setFocusEmulationEnabled",
                    L"{\"enabled\":true}",
                    Callback<DevToolsHandler>([this, on_done](HRESULT focus_result, LPCWSTR) -> HRESULT {
                        if (FAILED(focus_result)) {
                            on_done(false);
                            return S_OK;
                        }
                        CallDevToolsMethod("Network.enable", "{}", [on_done](bool ok, const std::string&) { on_done(ok); });
                        return S_OK;
                    }).Get());
                if (FAILED(focus_hr)) {
                    LogError << "Cloud: focus emulation dispatch failed" << VAR(focus_hr);
                    on_done(false);
                }
            });
            return S_OK;
        }).Get());
    if (FAILED(hr)) {
        LogError << "Cloud: input policy dispatch failed" << VAR(hr);
        on_done(false);
    }
}

void CloudWindow::configureViewport(std::function<void(bool)> on_done)
{
    // CDP 的显示缩放不参与 WebView2 的原生鼠标坐标换算。两者必须使用相同
    // 比例，否则 125% 系统缩放会把 (640,360) 的点击传成 (512,288)。
    // 仅配置云游戏控件，其他 WebView（例如导入滑索）仍跟随系统 DPI。
    Microsoft::WRL::ComPtr<ICoreWebView2Controller3> controller;
    HRESULT hr = GetController()->QueryInterface(IID_PPV_ARGS(&controller));
    if (SUCCEEDED(hr)) {
        hr = controller->put_ShouldDetectMonitorScaleChanges(FALSE);
    }
    if (SUCCEEDED(hr)) {
        hr = controller->put_BoundsMode(COREWEBVIEW2_BOUNDS_MODE_USE_RAW_PIXELS);
    }
    if (SUCCEEDED(hr)) {
        hr = controller->put_RasterizationScale(viewport_scale_);
    }
    if (FAILED(hr)) {
        LogError << "Cloud: viewport input scale configuration failed" << VAR(hr);
        on_done(false);
        return;
    }
    // Browser zoom changes devicePixelRatio and the SDK's input conversion.
    // CDP's display scale fits the fixed page into the native window without
    // changing the page's 720p coordinate system or device metrics.
    // The SDK divides relative motion by screen.width * devicePixelRatio.
    // Keep that denominator at 1280 regardless of desktop resolution/DPI;
    // viewport_scale_ belongs only to presentation, never to camera deltas.
    const auto params = json::value(json::object { { "width", kViewportWidth },
                                                   { "height", kViewportHeight },
                                                   { "deviceScaleFactor", 1 },
                                                   { "mobile", false },
                                                   { "screenWidth", kViewportWidth },
                                                   { "screenHeight", kViewportHeight },
                                                   { "viewport",
                                                     json::object { { "x", 0 },
                                                                    { "y", 0 },
                                                                    { "width", kViewportWidth },
                                                                    { "height", kViewportHeight },
                                                                    { "scale", viewport_scale_ } } } })
                            .to_string();
    CallDevToolsMethod("Emulation.setDeviceMetricsOverride", params, [on_done](bool ok, const std::string&) {
        if (!ok) {
            LogError << "Cloud: fixed device metrics configuration failed";
        }
        on_done(ok);
    });
}

void CloudWindow::resizeToClientRect()
{
    RECT client {};
    if (!GetClientRect(GetHwnd(), &client) || client.right <= 0 || client.bottom <= 0 || IsIconic(GetHwnd())) {
        // 最小化时保留最后一次有效的网页视口，避免视频布局被压成零尺寸。
        return;
    }
    // 以整数 16×9 块计算物理视口，避免分别取整导致逻辑高度成为 719/721。
    // 外窗仍可自由缩放，余下不足一块的像素留边。
    const LONG units = std::max(1L, std::min(client.right / 16, client.bottom / 9));
    const LONG width = units * 16;
    const LONG height = units * 9;
    viewport_scale_ = static_cast<double>(width) / kViewportWidth;
    const LONG left = (client.right - width) / 2;
    const LONG top = (client.bottom - height) / 2;
    const RECT bounds { left, top, left + width, top + height };
    const HRESULT hr = GetController()->SetBoundsAndZoomFactor(bounds, 1.0);
    if (SUCCEEDED(hr)) {
        configureViewport([](bool) {});
    }
    if (input_shield_) {
        SetWindowPos(
            input_shield_,
            HWND_TOP,
            0,
            0,
            client.right,
            client.bottom,
            SWP_NOACTIVATE | (manual_input_enabled_ ? SWP_HIDEWINDOW : SWP_SHOWWINDOW));
    }
    if (FAILED(hr)) {
        LogWarn << "Cloud: viewport resize failed" << VAR(hr);
    }
}
