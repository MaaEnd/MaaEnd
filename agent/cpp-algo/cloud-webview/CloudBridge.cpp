#include "CloudBridge.h"

#include <algorithm>
#include <chrono>
#include <future>
#include <memory>
#include <stdexcept>

#include <MaaUtils/Logger.h>

#include "video_frame_policy.h"

namespace
{

using Handle = std::unique_ptr<void, decltype(&CloseHandle)>;

// Only public DOM state is inspected. No cookies, tokens, account identifiers or
// login form values leave the browser. Unknown is deliberately not logged-in.
constexpr char kPageState[] = R"JS(
(() => {
    const visible = e => !!(e && e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden');
    const video = [...document.querySelectorAll('video')].find(visible);
    const gameSurface = document.getElementById('WelinkGameVideo');
    const text = document.body?.innerText || '';
    const normalizedText = text.replace(/\s+/g, '');
    const ended = /本次游戏已自动结束|游戏已自动结束/.test(normalizedText);
    const starting = /游戏启动中[，,]?请耐心等待/.test(normalizedText);
    const loginForm = [...document.querySelectorAll('input')].some(e => visible(e) &&
        (e.type === 'password' || /手机号|验证码|手机号码/.test(e.placeholder)));
    const loginPrompt = /扫码登录|扫描二维码|短信登录|密码登录|账号登录|手机号登录/.test(text) || loginForm;
    const userCenterLabel = [...document.querySelectorAll('a,button,span,div')].find(e =>
        visible(e) && e.children.length === 0 && e.textContent.trim() === '用户中心');
    // The official desktop header renders a direct dropdown arrow only when
    // its authenticated personalItems menu exists. The label alone is public.
    const userCenter = userCenterLabel?.closest('li');
    const hasAccountMenu = !!userCenter?.querySelector(':scope > svg');
    const hasLogout = [...document.querySelectorAll('a,button,span,div')].some(e =>
        visible(e) && e.children.length === 0 && e.textContent.trim() === '退出登录');
    const hasLogin = [...document.querySelectorAll('a,button,span,div')].some(e =>
        visible(e) && e.children.length === 0 && /^(登录|立即登录|登 录)$/.test(e.textContent.trim()));
    const startButton = [...document.querySelectorAll('button,a,div,span')].find(e =>
        visible(e) && e.textContent.trim() === '开始游戏' &&
        ![...e.children].some(child => child.textContent.trim() === '开始游戏'));
    const clickTarget = element => {
        if (!element || !visible(element) || element.disabled || element.closest('[aria-disabled="true"]')) return null;
        const rect = element.getBoundingClientRect();
        const x = rect.x + rect.width / 2, y = rect.y + rect.height / 2;
        const hit = document.elementFromPoint(x, y);
        return hit && (element === hit || element.contains(hit) ||
            (hit.contains(element) && hit.textContent.trim() === element.textContent.trim())) ? {x, y} : null;
    };
    const startTarget = clickTarget(startButton);
    const ready = document.readyState === 'complete';
    const videoReady = !ended && !starting && !!video && video.readyState >= 2 && !video.paused &&
        !video.ended && video.videoWidth > 0 && video.videoHeight > 0;
    const authenticated = ready && !loginPrompt && !hasLogin &&
        (hasAccountMenu || hasLogout || videoReady);
    const queue = /正在排队|排队中|前方还有|预计等待/.test(text);
    const startupResourceFailed = !authenticated && !loginPrompt && !video &&
        (window.__maaCloudStartupErrors || []).some(error => error.type === 'resource' && (
            error.source === 'https://web.hycdn.cn/hg_web_sdk/lib/sdk.entry.js' ||
            error.source === 'https://web.hycdn.cn/static/js/umd/react/react@18.3.1.js' ||
            error.source === 'https://web.hycdn.cn/static/js/umd/react-dom/react-dom@18.3.1.js' ||
            /^https:\/\/cloud-game-web-static\.hycdn\.cn\/cloud-game\/prod\/[\w.-]+\.js$/.test(error.source)));
    const busyMessage = '您上次游戏的数据正在火速处理中，请稍后再试';
    const message = [...document.querySelectorAll('div,span,p')].find(e => visible(e) &&
        e.textContent.replace(/\s+/g, '') === busyMessage &&
        ![...e.children].some(child => child.textContent.replace(/\s+/g, '') === busyMessage));
    const sessionBusy = !!message;
    const busyConfirmTarget = (() => {
        // Scope confirmation to this exact official error, never a generic
        // "Got it" elsewhere on the page or an invisible/stale dialog.
        for (let parent = message?.parentElement; parent && parent !== document.body; parent = parent.parentElement) {
            const confirm = [...parent.querySelectorAll('button,a,div,span')].find(e =>
                visible(e) && e.textContent.trim() === '知道了' &&
                ![...e.children].some(child => child.textContent.trim() === '知道了'));
            if (confirm) return clickTarget(confirm);
        }
        return null;
    })();
    return {account: authenticated ? 'signed_in' : (ready && (loginPrompt || hasLogin) ? 'signed_out' : 'unknown'),
        page: ended ? 'ended' : startupResourceFailed ? 'startup_error' : loginPrompt ? 'login' : sessionBusy ? 'session_busy' : queue ? 'queue' : starting ? 'starting' : video ? 'stream' : startButton ? 'landing' : 'loading',
        video_ready: !queue && !sessionBusy && videoReady,
        ended, startup_resource_failed: startupResourceFailed,
        video_width: video?.videoWidth || 0, video_height: video?.videoHeight || 0,
        game_surface: gameSurface?.tagName.toLowerCase() || 'none',
        screen_width: Math.round(screen.width * devicePixelRatio),
        screen_height: Math.round(screen.height * devicePixelRatio),
        viewport_width: innerWidth, viewport_height: innerHeight,
        device_pixel_ratio: devicePixelRatio,
        start_available: !!startTarget, start_target: startTarget,
        session_busy_confirm_target: busyConfirmTarget, document_ready: ready,
        document_epoch: performance.timeOrigin};
})()
)JS";

json::value evaluate(CloudWindow& window, const std::string& script)
{
    auto promise = std::make_shared<std::promise<std::optional<json::value>>>();
    auto future = promise->get_future();
    window.CallDevToolsMethod(
        "Runtime.evaluate",
        json::value(json::object { { "expression", script }, { "returnByValue", true }, { "awaitPromise", true } }).to_string(),
        [promise](bool ok, std::string result) { promise->set_value(ok ? json::parse(result) : std::nullopt); });
    // This is a browser RPC deadline, not a game-flow delay or retry.
    if (future.wait_for(std::chrono::seconds(5)) != std::future_status::ready) {
        return json::object { { "ok", false }, { "error", "browser_timeout" } };
    }
    const auto response = future.get();
    if (!response || response->contains("exceptionDetails")) {
        return json::object { { "ok", false }, { "error", "page_unavailable" } };
    }
    return json::object { { "ok", true }, { "value", response->at("result").get("value", json::value {}) } };
}

json::value call(CloudWindow& window, const std::string& method, const json::value& params)
{
    auto promise = std::make_shared<std::promise<std::optional<json::value>>>();
    auto future = promise->get_future();
    window.CallDevToolsMethod(method, params.to_string(), [promise](bool ok, std::string result) {
        promise->set_value(ok ? json::parse(result) : std::nullopt);
    });
    if (future.wait_for(std::chrono::seconds(5)) != std::future_status::ready) {
        return json::object { { "ok", false }, { "error", "browser_timeout" } };
    }
    const auto result = future.get();
    return json::object { { "ok", result.has_value() }, { "value", result.value_or(json::value {}) } };
}

} // namespace

CloudBridge::CloudBridge(CloudWindow& window)
    : window_(window)
{
    const auto name = cloud::PipeName(GetCurrentProcessId());
    pipe_ = CreateNamedPipeW(
        name.c_str(),
        PIPE_ACCESS_DUPLEX | FILE_FLAG_OVERLAPPED | FILE_FLAG_FIRST_PIPE_INSTANCE,
        PIPE_TYPE_BYTE | PIPE_READMODE_BYTE | PIPE_WAIT | PIPE_REJECT_REMOTE_CLIENTS,
        1,
        65536,
        65536,
        0,
        nullptr);
    if (pipe_ == INVALID_HANDLE_VALUE) {
        throw std::runtime_error("Could not create the cloud controller pipe");
    }
    worker_ = std::jthread([this](std::stop_token stop) { serve(stop); });
}

CloudBridge::~CloudBridge()
{
    worker_.request_stop();
    worker_.join();
    CloseHandle(pipe_);
    if (owner_process_) {
        CloseHandle(owner_process_);
    }
}

void CloudBridge::serve(std::stop_token stop)
{
    while (!stop.stop_requested()) {
        const Handle event(CreateEventW(nullptr, TRUE, FALSE, nullptr), &CloseHandle);
        if (!event) {
            LogError << "Cloud: could not create local controller pipe" << VAR(GetLastError());
            return;
        }
        OVERLAPPED connection {};
        connection.hEvent = event.get();
        const BOOL connected = ConnectNamedPipe(pipe_, &connection);
        const DWORD error = connected ? ERROR_SUCCESS : GetLastError();
        if (!connected && error == ERROR_IO_PENDING) {
            while (!stop.stop_requested() && WaitForSingleObject(event.get(), 100) == WAIT_TIMEOUT) {
            }
            if (stop.stop_requested()) {
                CancelIoEx(pipe_, &connection);
                DWORD transferred = 0;
                GetOverlappedResult(pipe_, &connection, &transferred, TRUE);
                return;
            }
        }
        else if (!connected && error != ERROR_PIPE_CONNECTED) {
            LogError << "Cloud: local controller connection failed" << VAR(error);
            return;
        }
        const auto request = cloud::ReadMessage(pipe_, stop);
        if (request) {
            try {
                const auto response = dispatch(*request);
                if (!cloud::WriteMessage(pipe_, response, stop)) {
                    LogWarn << "Cloud: controller response delivery failed";
                }
            }
            catch (const std::exception&) {
                cloud::WriteMessage(pipe_, json::object { { "ok", false }, { "error", "invalid_request" } }, stop);
            }
        }
        // Do not discard a buffered reply before the client has read it.
        char acknowledged = 0;
        cloud::Transfer(pipe_, &acknowledged, 1, false, stop);
        // Each request is independent, so cancelling a login task cannot retain
        // a pipe connection or prevent another controller from inspecting state.
        DisconnectNamedPipe(pipe_);
        if (close_requested_.load(std::memory_order_acquire)) {
            if (const auto hwnd = window_.nativeHandle()) {
                PostMessageW(hwnd, WM_CLOSE, 0, 0);
            }
            return;
        }
    }
}

json::value CloudBridge::dispatch(const json::value& request)
{
    const auto operation = request.get("op", std::string {});
    DWORD client_pid = 0;
    if (!GetNamedPipeClientProcessId(pipe_, &client_pid) || !client_pid) {
        return json::object { { "ok", false }, { "error", "client_process_unavailable" } };
    }
    // A crashed GUI must not permanently reserve this host. The process handle
    // also prevents a recycled PID from inheriting an earlier run's ownership.
    if (owner_process_ && WaitForSingleObject(owner_process_, 0) == WAIT_OBJECT_0) {
        CloseHandle(owner_process_);
        owner_process_ = nullptr;
        owner_pid_ = 0;
        owner_token_.clear();
    }
    if (operation == "run_acquire") {
        const auto token = request.get("token", std::string {});
        if (token.empty() || token.size() > 256 || close_requested_.load(std::memory_order_acquire)) {
            return json::object { { "ok", false }, { "error", "invalid_run" } };
        }
        if (owner_process_) {
            return json::object { { "ok", false }, { "error", "host_busy" } };
        }
        owner_process_ = OpenProcess(SYNCHRONIZE, FALSE, client_pid);
        if (!owner_process_) {
            return json::object { { "ok", false }, { "error", "client_process_unavailable" } };
        }
        owner_pid_ = client_pid;
        owner_token_ = token;
        return json::object { { "ok", true } };
    }
    if (operation == "run_release" || operation == "run_close") {
        if (!owner_process_ || client_pid != owner_pid_ || request.get("token", std::string {}) != owner_token_) {
            return json::object { { "ok", false }, { "error", "run_not_owned" } };
        }
        if (operation == "run_close") {
            close_requested_.store(true, std::memory_order_release);
        }
        else {
            CloseHandle(owner_process_);
            owner_process_ = nullptr;
            owner_pid_ = 0;
            owner_token_.clear();
        }
        return json::object { { "ok", true } };
    }
    if (owner_process_ && client_pid != owner_pid_ && operation != "status" && operation != "diagnostics" && operation != "close") {
        return json::object { { "ok", false }, { "error", "host_busy" } };
    }
    if (operation == "status") {
        return evaluate(window_, kPageState);
    }
    if (operation == "diagnostics") {
        // Public startup diagnostics only: never expose cookies, form values,
        // request bodies, query strings or account/session identifiers.
        auto response = evaluate(window_, R"JS((() => {
            const resourceName = value => {
                try { const u = new URL(value, location.href); return u.origin + u.pathname; }
                catch { return ''; }
            };
            return {ready: document.readyState, online: navigator.onLine,
                react: typeof window.React, react_dom: typeof window.ReactDOM,
                webtransport: typeof window.WebTransport, video_decoder: typeof window.VideoDecoder,
                webgl2: !!document.createElement('canvas').getContext('webgl2'),
                media: [...document.querySelectorAll('video,canvas')].map(e => ({
                    tag: e.tagName.toLowerCase(), game: e.id === 'WelinkGameVideo',
                    connected: e.isConnected, width: e.videoWidth ?? e.width,
                    height: e.videoHeight ?? e.height, ready: e.readyState,
                    paused: e.paused, ended: e.ended,
                    decoded_frames: e.getVideoPlaybackQuality?.().totalVideoFrames})),
                scripts: [...document.scripts].filter(s => s.src).map(s => resourceName(s.src)),
                resources: performance.getEntriesByType('resource').map(r => ({
                    name: resourceName(r.name), type: r.initiatorType,
                    duration: Math.round(r.duration), transfer: r.transferSize,
                    status: r.responseStatus})),
                errors: window.__maaCloudStartupErrors || []};
        })())JS");
        if (response.get("ok", false)) {
            const auto state = evaluate(window_, kPageState);
            if (state.get("ok", false)) {
                response["value"]["state"] = state.at("value");
            }
        }
        return response;
    }
    if (operation == "reload") {
        return call(window_, "Page.reload", json::object {});
    }
    if (operation == "close") {
        // Acknowledge before the main thread joins and destroys the bridge.
        close_requested_.store(true, std::memory_order_release);
        return json::object { { "ok", true } };
    }
    if (operation == "mouse") {
        const auto& params = request.at("params");
        return call(window_, "Input.dispatchMouseEvent", params);
    }
    if (operation == "key") {
        const auto params = request.at("params");
        return call(window_, "Input.dispatchKeyEvent", params);
    }
    if (operation == "text") {
        return call(window_, "Input.insertText", json::object { { "text", request.at("text") } });
    }
    if (operation == "frame") {
        // Keep the baseline protocol available for existing clients and direct
        // comparison. Production controllers use raw pixels via frame_shared.
        const auto response = evaluate(window_, cloud::kCaptureVideoPng);
        if (response.get("ok", false) && response.at("value").is_null()) {
            return json::object { { "ok", false }, { "error", "video_unavailable" } };
        }
        return response;
    }
    if (operation == "frame_shared") {
        // A timed-out browser callback can still arrive later. Do not let a
        // second capture overlap it and overwrite an acknowledged frame.
        if (frame_pending_->exchange(true, std::memory_order_acq_rel)) {
            return json::object { { "ok", false }, { "error", "frame_capture_pending" } };
        }
        auto promise = std::make_shared<std::promise<json::value>>();
        auto future = promise->get_future();
        window_.captureVideoFrame([promise, pending = frame_pending_](json::value response) {
            promise->set_value(std::move(response));
            pending->store(false, std::memory_order_release);
        });
        if (future.wait_for(std::chrono::seconds(5)) != std::future_status::ready) {
            return json::object { { "ok", false }, { "error", "browser_timeout" } };
        }
        auto response = future.get();
        if (!response.get("ok", false)) {
            return response;
        }
        DWORD client_process_id = 0;
        if (!GetNamedPipeClientProcessId(pipe_, &client_process_id) || !client_process_id) {
            return json::object { { "ok", false }, { "error", "client_process_unavailable" } };
        }
        const Handle client_process(OpenProcess(PROCESS_DUP_HANDLE, FALSE, client_process_id), &CloseHandle);
        if (!client_process) {
            return json::object { { "ok", false }, { "error", "client_process_unavailable" } };
        }
        const auto source_handle = reinterpret_cast<HANDLE>(static_cast<uintptr_t>(response.at("value").at("mapping_handle").as_integer()));
        HANDLE client_mapping = nullptr;
        if (!DuplicateHandle(GetCurrentProcess(), source_handle, client_process.get(), &client_mapping, FILE_MAP_READ, FALSE, 0)) {
            return json::object { { "ok", false }, { "error", "frame_buffer_duplication_failed" } };
        }
        response["value"]["mapping_handle"] = static_cast<int64_t>(reinterpret_cast<uintptr_t>(client_mapping));
        return response;
    }
    if (operation == "relative") {
        const auto dx = request.at("dx").as_integer();
        const auto dy = request.at("dy").as_integer();
        const auto buttons = request.get("buttons", 0);
        const auto modifiers = request.get("modifiers", 0);
        // The streaming SDK clamps movementX/Y to signed 8-bit magnitude
        // before encoding them. Partition the requested motion instead of
        // silently losing the part beyond 127. This is one input transaction;
        // failed dispatches are never replayed.
        const auto steps = std::max<int64_t>(1, (std::max(std::abs(dx), std::abs(dy)) + 126) / 127);
        json::value response;
        int64_t previous_x = 0;
        int64_t previous_y = 0;
        for (int64_t step = 1; step <= steps; ++step) {
            const auto x = dx * step / steps;
            const auto y = dy * step / steps;
            const auto prepared = evaluate(
                window_,
                "window.__maaCloudRelativeMove(" + std::to_string(x - previous_x) + "," + std::to_string(y - previous_y) + ")");
            if (!prepared.get("ok", false) || !prepared.get("value", false)) {
                return json::object { { "ok", false }, { "error", "relative_input_unavailable" } };
            }
            // The document policy supplies movementX/Y on this trusted event.
            // Accumulating/clamping absolute positions loses deltas at the edge.
            response = call(
                window_,
                "Input.dispatchMouseEvent",
                json::object { { "type", "mouseMoved" },
                               { "x", 640 },
                               { "y", 360 },
                               { "button", "none" },
                               { "buttons", buttons },
                               { "modifiers", modifiers } });
            if (!response.get("ok", false)) {
                evaluate(window_, "window.__maaCloudRelativeMove(null,null)");
                return response;
            }
            previous_x = x;
            previous_y = y;
        }
        return response;
    }
    if (operation == "screencap") {
        // An explicit CSS-pixel clip captures the whole logical page despite
        // the native display scale. The surface remains available minimized;
        // capturing the native view instead can return a black title bar.
        return call(
            window_,
            "Page.captureScreenshot",
            json::object { { "format", "png" },
                           { "fromSurface", true },
                           { "captureBeyondViewport", true },
                           { "clip", json::object { { "x", 0 }, { "y", 0 }, { "width", 1280 }, { "height", 720 }, { "scale", 1 } } } });
    }
    return json::object { { "ok", false }, { "error", "unsupported_operation" } };
}
