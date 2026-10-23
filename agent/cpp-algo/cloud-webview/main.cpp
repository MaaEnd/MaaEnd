#include <chrono>
#include <exception>
#include <iostream>
#include <memory>
#include <optional>
#include <string>
#include <string_view>
#include <thread>
#include <vector>

#include <MaaUtils/SafeWindows.hpp>

#include "CloudBridge.h"
#include "CloudWindow.h"
#include "Common/output_paths.h"
#include "cloud_protocol.h"

namespace
{

constexpr wchar_t kWindowTitle[] = L"MaaEnd - Cloud Endfield";
constexpr wchar_t kWindowClass[] = L"MaaEnd.Common.FramelessWindow";
constexpr wchar_t kInstanceMutex[] = L"Local\\MaaEndCloudWebView";
constexpr wchar_t kReadyEventPrefix[] = L"Local\\MaaEndCloudWebViewReady_";
// 仅等待进程初始化，不参与游戏流程或节点重试。
constexpr DWORD kStartupTimeoutMs = 60000;

using Handle = std::unique_ptr<void, decltype(&CloseHandle)>;

int run_host(const wchar_t* ready_event)
{
    const Handle instance(CreateMutexW(nullptr, FALSE, kInstanceMutex), &CloseHandle);
    if (!instance || GetLastError() == ERROR_ALREADY_EXISTS) {
        std::cerr << "The cloud window is already running or could not be created.\n";
        return 1;
    }
    // Only allow a fixed software-rendering opt-in, never inherited arbitrary
    // Chromium arguments. The default continues to use WebView2 auto detection.
    wchar_t software_mode[2] {};
    const bool software = GetEnvironmentVariableW(L"MAAEND_CLOUD_SOFTWARE_RENDERING", software_mode, 2) == 1 && software_mode[0] == L'1';
    const wchar_t* arguments =
        software
            ? L"--no-proxy-server --disable-gpu --disable-gpu-compositing --disable-accelerated-video-decode --disable-webrtc-hw-decoding"
            : L"--no-proxy-server";
    if (!SetEnvironmentVariableW(L"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", arguments)) {
        return 1;
    }
    if (software) {
        std::cout << "Cloud WebView: software rendering and video decoding enabled.\n";
    }
    CloudWindow window;
    window.SetAdditionalBrowserArguments(arguments);
    if (!window.Open()) {
        std::cerr << "Could not initialize WebView2. Check the installed WebView2 Runtime.\n";
        return 1;
    }
    if (!window.publish()) {
        return 1;
    }
    CloudBridge bridge(window);
    if (ready_event) {
        const Handle event(OpenEventW(EVENT_MODIFY_STATE, FALSE, ready_event), &CloseHandle);
        if (!event || !SetEvent(event.get())) {
            return 1;
        }
    }
    window.waitForClose();
    return 0;
}

void report_stalled_startup(
    DWORD process_id,
    const json::value& state,
    std::chrono::steady_clock::time_point& loading_since,
    bool& reported)
{
    if (state.get("page", std::string {}) != "loading") {
        loading_since = std::chrono::steady_clock::now();
        reported = false;
        return;
    }
    if (!reported && std::chrono::steady_clock::now() - loading_since >= std::chrono::seconds(15)) {
        reported = true;
        const auto diagnostics = cloud::Request(process_id, json::object { { "op", "diagnostics" } });
        std::cerr << "Cloud website is still loading; startup diagnostics: " << (diagnostics ? diagnostics->to_string() : "unavailable")
                  << std::endl;
    }
}

// Failed website bootstrap scripts cannot initialize without a fresh navigation.
// Recover this explicit resource failure once, never a login form or live game.
bool recover_startup_resource(DWORD process_id, bool& reloaded, double document_epoch)
{
    if (reloaded) {
        std::cerr << "The official website startup script failed to load after recovery. No daily tasks were started.\n";
        return false;
    }
    reloaded = true;
    std::cerr << "An official website startup script failed to load. Reloading the startup page once.\n";
    const auto response = cloud::Request(process_id, json::object { { "op", "reload" } });
    if (!response || !response->get("ok", false)) {
        return false;
    }
    // Page.reload acknowledges dispatch before the old document is replaced.
    // Do not mistake that document's old error for a second failed load.
    const auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(15);
    while (std::chrono::steady_clock::now() < deadline) {
        const auto state = cloud::Request(process_id, json::object { { "op", "status" } });
        if (!state || !state->get("ok", false)) {
            return false;
        }
        if (state->at("value").get("document_epoch", document_epoch) != document_epoch) {
            return true;
        }
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    std::cerr << "The cloud startup page did not navigate after its startup script failed.\n";
    return false;
}

int inspect_host(bool wait_for_login, bool diagnostics = false)
{
    const HWND hwnd = FindWindowW(kWindowClass, kWindowTitle);
    if (!hwnd) {
        std::cerr << "Cloud window is not running.\n";
        return 1;
    }
    DWORD process_id = 0;
    GetWindowThreadProcessId(hwnd, &process_id);
    std::string previous_state;
    auto loading_since = std::chrono::steady_clock::now();
    bool reported = false;
    bool resource_reloaded = false;
    while (IsWindow(hwnd)) {
        const auto response = cloud::Request(process_id, json::object { { "op", diagnostics ? "diagnostics" : "status" } });
        if (!response || !response->get("ok", false)) {
            std::cerr << "Could not inspect the cloud page.\n";
            return 1;
        }
        if (!wait_for_login) {
            std::cout << response->to_string() << std::endl;
            return 0;
        }
        const auto& state = response->at("value");
        if (state.get("startup_resource_failed", false)) {
            if (!recover_startup_resource(process_id, resource_reloaded, state.get("document_epoch", 0.0))) {
                return 3;
            }
            continue;
        }
        report_stalled_startup(process_id, state, loading_since, reported);
        const auto account = response->at("value").get("account", std::string { "unknown" });
        if (account == "signed_in") {
            std::cout << "Cloud account is signed in." << std::endl;
            if (wait_for_login) {
                std::cout << "You may remove the login pre-task; startup will still check the account.\n";
            }
            return 0;
        }
        if (previous_state != account) {
            std::cout << "Complete login in the cloud window using any method offered by the official website.\n" << std::flush;
            previous_state = account;
        }
        // Poll user-controlled authentication state, not task Pipeline progress.
        std::this_thread::sleep_for(std::chrono::milliseconds(500));
    }
    std::cerr << "Cloud window closed before login was confirmed.\n";
    return 1;
}

int prepare_host()
{
    const HWND hwnd = FindWindowW(kWindowClass, kWindowTitle);
    if (!hwnd) {
        return 1;
    }
    DWORD process_id = 0;
    GetWindowThreadProcessId(hwnd, &process_id);
    bool start_sent = false;
    bool busy_confirm_sent = false;
    bool session_retry_pending = false;
    std::optional<std::chrono::steady_clock::time_point> session_recovery_deadline;
    auto retry_not_before = std::chrono::steady_clock::now();
    std::string previous_page;
    auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(60);
    auto loading_since = std::chrono::steady_clock::now();
    bool reported = false;
    bool resource_reloaded = false;
    bool awaiting_login = false;
    while (IsWindow(hwnd)) {
        const auto response = cloud::Request(process_id, json::object { { "op", "status" } });
        if (!response || !response->get("ok", false)) {
            std::cerr << "Could not inspect the cloud page. No daily tasks were started.\n";
            return 1;
        }
        const auto& state = response->at("value");
        if (state.get("startup_resource_failed", false)) {
            if (!recover_startup_resource(process_id, resource_reloaded, state.get("document_epoch", 0.0))) {
                return 3;
            }
            continue;
        }
        report_stalled_startup(process_id, state, loading_since, reported);
        const auto page = state.get("page", std::string { "unknown" });
        const auto now = std::chrono::steady_clock::now();
        if (page != previous_page) {
            std::cout << "Cloud startup: " << page << ". Waiting for a decoded game frame before starting daily tasks." << std::endl;
            previous_page = page;
        }
        if (page == "ended") {
            std::cerr << "The cloud session ended. Restart the game in the cloud window, then retry.\n";
            return 2;
        }
        if (page == "login" || state.get("account", std::string {}) == "signed_out") {
            if (!awaiting_login) {
                std::cout << "Complete login in the cloud window using any method offered by the official website. "
                             "Startup will continue automatically after login.\n"
                          << std::flush;
                awaiting_login = true;
                start_sent = false;
                busy_confirm_sent = false;
                session_retry_pending = false;
                session_recovery_deadline.reset();
            }
            // Authentication is user-controlled; its duration must not consume
            // the website startup deadline or cause game input to be submitted.
            deadline = now + std::chrono::seconds(60);
            std::this_thread::sleep_for(std::chrono::milliseconds(500));
            continue;
        }
        if (awaiting_login && state.get("account", std::string {}) == "signed_in") {
            awaiting_login = false;
            deadline = now + std::chrono::seconds(60);
            std::cout << "Cloud account is signed in. Continuing startup.\n" << std::flush;
        }
        if (page == "stream" && state.get("video_ready", false)) {
            // An authenticated page or a video tag alone does not prove there is
            // a decoded game image. Verify the same frame source used by the GUI.
            const auto frame = cloud::Request(process_id, json::object { { "op", "frame" } });
            if (frame && frame->get("ok", false) && !frame->get("value", json::value {}).is_null()) {
                std::cout << "Cloud game frame is ready. Starting daily tasks." << std::endl;
                return 0;
            }
        }
        const auto click_target = [&](const json::value& target) {
            // Use the visible, hit-tested DOM target in the fixed CSS viewport.
            // This is browser startup only; all in-game flow stays in Pipeline.
            const auto send_mouse = [&](const char* type, int buttons) {
                return cloud::Request(
                    process_id,
                    json::object { { "op", "mouse" },
                                   { "params",
                                     json::object { { "type", type },
                                                    { "x", target.at("x") },
                                                    { "y", target.at("y") },
                                                    { "button", "left" },
                                                    { "buttons", buttons },
                                                    { "clickCount", 1 } } } });
            };
            const auto down = send_mouse("mousePressed", 1);
            const auto up = send_mouse("mouseReleased", 0);
            return down && up && down->get("ok", false) && up->get("ok", false);
        };
        if (page == "session_busy") {
            if (!session_recovery_deadline) {
                session_recovery_deadline = now + std::chrono::seconds(60);
            }
            const auto target = state.get("session_busy_confirm_target", json::value {});
            if (!busy_confirm_sent && !target.is_null()) {
                if (!click_target(target)) {
                    std::cerr << "Could not confirm the previous-session notice. No input will be replayed.\n";
                    return 1;
                }
                busy_confirm_sent = true;
                session_retry_pending = true;
                // Official error 21002 has no readiness event. After its
                // acknowledgement, pace only this service-lock recovery;
                // normal startup and game inputs incur no additional delay.
                retry_not_before = now + std::chrono::seconds(5);
                std::cout << "Previous cloud session is still saving. Notice confirmed; waiting for the landing page before restarting."
                          << std::endl;
            }
        }
        else {
            busy_confirm_sent = false;
        }
        if (page == "queue" || page == "starting") {
            // These official states mean dispatch has progressed beyond the
            // previous-session lock; keep their existing waiting semantics.
            session_recovery_deadline.reset();
        }
        if (session_recovery_deadline && now >= *session_recovery_deadline) {
            std::cerr << "The previous cloud session is still unavailable. Retry later; no daily tasks were started.\n";
            return 3;
        }
        if (page == "landing" && state.get("account", std::string {}) == "signed_in" && state.get("start_available", false)
            && ((!start_sent && !session_retry_pending) || (session_retry_pending && now >= retry_not_before))) {
            if (!click_target(state.at("start_target"))) {
                std::cerr << "Cloud start click failed. No input will be replayed and no daily tasks were started.\n";
                return 1;
            }
            start_sent = true;
            session_retry_pending = false;
            deadline = std::chrono::steady_clock::now() + std::chrono::seconds(60);
            std::cout << "Cloud start clicked. Waiting for the official page to accept startup." << std::endl;
        }
        if (page == "queue" || page == "starting") {
            // Explicit service waiting states are not game-node failures. Queue
            // duration is controlled by the website; the GUI can cancel this process.
            deadline = std::chrono::steady_clock::now() + std::chrono::seconds(60);
        }
        else if (!session_recovery_deadline && std::chrono::steady_clock::now() >= deadline) {
            std::cerr << "Cloud startup did not become ready. Check the official page; no daily tasks were started.\n";
            return 3;
        }
        std::this_thread::sleep_for(std::chrono::milliseconds(500));
    }
    std::cerr << "Cloud window closed before the game frame was ready.\n";
    return 1;
}

int close_host()
{
    const HWND hwnd = FindWindowW(kWindowClass, kWindowTitle);
    if (!hwnd) {
        return 0;
    }
    DWORD process_id = 0;
    GetWindowThreadProcessId(hwnd, &process_id);
    const Handle process(OpenProcess(SYNCHRONIZE | PROCESS_QUERY_LIMITED_INFORMATION, FALSE, process_id), &CloseHandle);
    const auto response = cloud::Request(process_id, json::object { { "op", "close" } });
    if (!response || !response->get("ok", false) || !process
        || WaitForSingleObject(process.get(), cloud::kRequestTimeoutMs) != WAIT_OBJECT_0) {
        return 1;
    }
    DWORD exit_code = 0;
    if (!GetExitCodeProcess(process.get(), &exit_code) || exit_code != 0) {
        std::cerr << "The cloud window exited abnormally: " << exit_code << '\n';
        return 1;
    }
    return 0;
}

int launch_host()
{
    if (FindWindowW(kWindowClass, kWindowTitle)) {
        // 重复执行启动预任务只确认宿主存在，保留用户选择的窗口状态与焦点。
        return 0;
    }
    const auto event_name = std::wstring(kReadyEventPrefix) + std::to_wstring(GetCurrentProcessId());
    const Handle event(CreateEventW(nullptr, TRUE, FALSE, event_name.c_str()), &CloseHandle);
    if (!event) {
        return 1;
    }
    std::vector<wchar_t> executable(32768);
    const auto size = GetModuleFileNameW(nullptr, executable.data(), static_cast<DWORD>(executable.size()));
    if (!size || size >= executable.size()) {
        return 1;
    }
    std::wstring command = L"\"" + std::wstring(executable.data(), size) + L"\" --host " + event_name;
    STARTUPINFOW startup { sizeof(startup) };
    PROCESS_INFORMATION process {};
    if (!CreateProcessW(
            executable.data(),
            command.data(),
            nullptr,
            nullptr,
            FALSE,
            CREATE_NO_WINDOW,
            nullptr,
            nullptr,
            &startup,
            &process)) {
        std::cerr << "Could not launch the cloud window: " << GetLastError() << '\n';
        return 1;
    }
    const Handle process_handle(process.hProcess, &CloseHandle);
    const Handle thread_handle(process.hThread, &CloseHandle);
    const HANDLE handles[] = { event.get(), process_handle.get() };
    if (WaitForMultipleObjects(2, handles, FALSE, kStartupTimeoutMs) != WAIT_OBJECT_0) {
        std::cerr << "The cloud window did not become ready.\n";
        return 1;
    }
    return 0;
}

} // namespace

int wmain(int argc, wchar_t** argv)
{
    try {
        common::StartupDir();
        if (argc == 1) {
            return run_host(nullptr);
        }
        if ((argc == 2 || (argc == 3 && std::wstring_view(argv[2]) == L"{}")) && std::wstring_view(argv[1]) == L"--launch") {
            return launch_host();
        }
        if (argc == 3 && std::wstring_view(argv[1]) == L"--host" && std::wstring_view(argv[2]).starts_with(kReadyEventPrefix)) {
            return run_host(argv[2]);
        }
        if ((argc == 2 || (argc == 3 && std::wstring_view(argv[2]) == L"{}"))) {
            const std::wstring_view operation(argv[1]);
            if (operation == L"--login") {
                return launch_host() == 0 ? inspect_host(true) : 1;
            }
            if (operation == L"--prepare") {
                return launch_host() == 0 ? prepare_host() : 1;
            }
            if (operation == L"--status") {
                return inspect_host(false);
            }
            if (operation == L"--diagnostics") {
                return inspect_host(false, true);
            }
            if (operation == L"--close") {
                return close_host();
            }
        }
        std::cerr << "Usage: cloud-webview [--launch|--prepare|--login|--status|--diagnostics|--close]\n";
    }
    catch (const std::exception& error) {
        std::cerr << "Cloud window failed: " << error.what() << '\n';
    }
    return 1;
}
