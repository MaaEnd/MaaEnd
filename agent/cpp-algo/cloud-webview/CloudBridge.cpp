#include "CloudBridge.h"

#include <algorithm>
#include <chrono>
#include <future>
#include <memory>
#include <stdexcept>

#include <MaaUtils/Logger.h>

#include "cloud_scripts.h"

namespace
{

using Handle = std::unique_ptr<void, decltype(&CloseHandle)>;

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

json::value CloudBridge::input(const json::value& request)
{
    const auto operation = request.get("op", std::string {});
    if (operation != "mouse" && operation != "key" && operation != "text" && operation != "relative") {
        return json::object { { "ok", false }, { "error", "unsupported_operation" } };
    }
    return dispatch(request);
}

json::value CloudBridge::dispatch(const json::value& request)
{
    const auto operation = request.get("op", std::string {});
    // Preserve input transaction order for both local and pipe callers,
    // including the prepare/dispatch pairs in a relative movement.
    std::unique_lock input_lock(input_mutex_, std::defer_lock);
    if (operation == "mouse" || operation == "key" || operation == "text" || operation == "relative") {
        input_lock.lock();
    }
    if (operation == "status") {
        return evaluate(window_, cloud::kPageState);
    }
    if (operation == "diagnostics") {
        // Public startup diagnostics only: never expose cookies, form values,
        // request bodies, query strings or account/session identifiers.
        auto response = evaluate(window_, cloud::kDiagnostics);
        if (response.get("ok", false)) {
            const auto state = evaluate(window_, cloud::kPageState);
            if (state.get("ok", false)) {
                response["value"]["state"] = state.at("value");
            }
        }
        return response;
    }
    if (operation == "reload") {
        return call(window_, "Page.reload", json::object { { "ignoreCache", request.get("ignore_cache", false) } });
    }
    if (operation == "close") {
        // Acknowledge before the main thread joins and destroys the bridge.
        close_requested_.store(true, std::memory_order_release);
        return json::object { { "ok", true } };
    }
    if (operation == "mouse") {
        const auto& params = request.at("params");
        const auto move = params.get("type", std::string {}) == "mouseMoved";
        const auto prepare =
            move ? "window.__maaCloudMouseMove(" + params.at("x").to_string() + "," + params.at("y").to_string() + ",0,0) && "
                 : std::string {};
        const auto prepared = evaluate(
            window_,
            prepare + "window.__maaCloudMouseInput(true," + params.at("x").to_string() + "," + params.at("y").to_string() + ")");
        if (!prepared.get("ok", false) || !prepared.at("value").is_boolean()) {
            evaluate(window_, "window.__maaCloudMouseMove(null,null,null,null); window.__maaCloudMouseInput(false)");
            return json::object { { "ok", false }, { "error", "mouse_input_unavailable" } };
        }
        const auto response = call(window_, "Input.dispatchMouseEvent", params);
        if (prepared.get("value", false)) {
            const auto restored = evaluate(window_, "window.__maaCloudMouseInput(false)");
            if (!restored.get("ok", false) || !restored.get("value", false)) {
                return json::object { { "ok", false }, { "error", "mouse_input_restore_failed" } };
            }
        }
        if (move && !response.get("ok", false)) {
            evaluate(window_, "window.__maaCloudMouseMove(null,null,null,null)");
        }
        return response;
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
        bool menu_shielded = false;
        for (int64_t step = 1; step <= steps; ++step) {
            const auto x = dx * step / steps;
            const auto y = dy * step / steps;
            const auto prepared = evaluate(
                window_,
                "window.__maaCloudMouseMove(640,360," + std::to_string(x - previous_x) + "," + std::to_string(y - previous_y) + ")"
                    + (step == 1 ? " && window.__maaCloudMouseInput(true,640,360)" : ""));
            if (!prepared.get("ok", false) || !prepared.at("value").is_boolean() || (step != 1 && !prepared.get("value", false))) {
                evaluate(window_, "window.__maaCloudMouseMove(null,null,null,null); window.__maaCloudMouseInput(false)");
                return json::object { { "ok", false }, { "error", "relative_input_unavailable" } };
            }
            if (step == 1) {
                menu_shielded = prepared.get("value", false);
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
                evaluate(window_, "window.__maaCloudMouseMove(null,null,null,null); window.__maaCloudMouseInput(false)");
                return response;
            }
            previous_x = x;
            previous_y = y;
        }
        if (menu_shielded) {
            const auto restored = evaluate(window_, "window.__maaCloudMouseInput(false)");
            if (!restored.get("ok", false) || !restored.get("value", false)) {
                return json::object { { "ok", false }, { "error", "mouse_input_restore_failed" } };
            }
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
