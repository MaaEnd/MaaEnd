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
#include <tlhelp32.h>

#include "CloudBridge.h"
#include "CloudWindow.h"
#include "Common/output_paths.h"
#include "Win32ControllerBridge.h"
#include "cloud_protocol.h"
#include "startup_notice_policy.h"
#include "transport/win_cloud_transport.h"
#include "win32_bridge_protocol.h"

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
    // Only allow fixed resolver, rendering and proxy options, never inherited arbitrary
    // Chromium arguments. The default continues to bypass system proxies.
    wchar_t software_mode[2] {};
    const bool software = GetEnvironmentVariableW(L"MAAEND_CLOUD_SOFTWARE_RENDERING", software_mode, 2) == 1 && software_mode[0] == L'1';
    wchar_t proxy_value[256] {};
    const DWORD proxy_length = GetEnvironmentVariableW(L"MAAEND_CLOUD_PROXY", proxy_value, static_cast<DWORD>(std::size(proxy_value)));
    bool valid_proxy = proxy_length > 0 && proxy_length < std::size(proxy_value) && proxy_value[0] != L'-';
    for (DWORD i = 0; valid_proxy && i < proxy_length; ++i) {
        const wchar_t ch = proxy_value[i];
        valid_proxy = (ch >= L'a' && ch <= L'z') || (ch >= L'A' && ch <= L'Z') || (ch >= L'0' && ch <= L'9') || ch == L'.' || ch == L':'
                      || ch == L'-' || ch == L'[' || ch == L']';
    }

    std::wstring arguments = valid_proxy ? L"--proxy-server=" + std::wstring(proxy_value) : L"--no-proxy-server";
    // Honor the desktop's DNS resolution path instead of Chromium's independent
    // resolver. This changes neither system DNS settings nor TLS validation.
    arguments += L" --disable-features=AsyncDns";
    if (software) {
        arguments += L" --disable-gpu --disable-gpu-compositing --disable-accelerated-video-decode --disable-webrtc-hw-decoding";
    }
    if (!SetEnvironmentVariableW(L"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", arguments.c_str())) {
        return 1;
    }
    if (valid_proxy) {
        std::wcout << L"Cloud WebView: proxy enabled at " << proxy_value << L".\n";
    }
    else if (proxy_length) {
        std::cerr << "Ignoring invalid MAAEND_CLOUD_PROXY; expected a host:port value.\n";
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
    WinCloudTransport transport(GetCurrentProcessId(), [&bridge](const json::value& request) { return bridge.input(request); });
    Win32ControllerBridge controller_bridge(transport, [&window] { PostMessageW(window.nativeHandle(), WM_CLOSE, 0, 0); });
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
// Recover explicit resource failures only, never a login form or live game.
bool recover_startup_resource(DWORD process_id, cloud::StartupResourcePolicy& policy, double document_epoch)
{
    const auto diagnostics = cloud::Request(process_id, json::object { { "op", "diagnostics" } });
    if (diagnostics && diagnostics->get("ok", false)) {
        const auto& value = diagnostics->at("value");
        json::array errors;
        for (const auto& error : value.get("errors", json::array {})) {
            const auto source = error.get("source", std::string {});
            if (source.starts_with("https://web.hycdn.cn/hg_web_sdk/") || source.starts_with("https://web.hycdn.cn/static/js/umd/")
                || source.starts_with("https://cloud-game-web-static.hycdn.cn/cloud-game/prod/")) {
                errors.emplace_back(error);
            }
        }
        std::cerr << "Cloud bootstrap failure diagnostics: "
                  << json::value(json::object { { "ready", value.get("ready", std::string {}) },
                                                { "online", value.get("online", false) },
                                                { "errors", errors } })
                         .to_string()
                  << std::endl;
    }
    const auto action = policy.recover();
    if (action == cloud::StartupResourcePolicy::Action::Fail) {
        std::cerr << "The official website startup script failed to load after recovery. No daily tasks were started.\n";
        return false;
    }
    const bool ignore_cache = action == cloud::StartupResourcePolicy::Action::ReloadIgnoringCache;
    // A rapid reload can hit the same TLS/CDN failure burst and exhaust the
    // budget immediately. Recheck state during bounded network backoff: the
    // website may recover itself, navigate, or open a login form meanwhile.
    const auto retry_at = std::chrono::steady_clock::now() + policy.retry_delay();
    while (std::chrono::steady_clock::now() < retry_at) {
        const auto response = cloud::Request(process_id, json::object { { "op", "status" } });
        if (!response || !response->get("ok", false)) {
            return false;
        }
        const auto& state = response->at("value");
        if (state.get("document_epoch", document_epoch) != document_epoch || !state.get("startup_resource_failed", false)) {
            return true;
        }
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    std::cerr
        << (ignore_cache ? "The bootstrap dependency failed again. Reloading once without the browser cache.\n"
                         : "An official website startup script failed to load. Reloading the startup page once.\n");
    const auto response = cloud::Request(process_id, json::object { { "op", "reload" }, { "ignore_cache", ignore_cache } });
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
    cloud::StartupResourcePolicy resource_recovery;
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
            if (!recover_startup_resource(process_id, resource_recovery, state.get("document_epoch", 0.0))) {
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

bool bind_gui_owner()
{
    const Handle snapshot(CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0), &CloseHandle);
    if (snapshot.get() == INVALID_HANDLE_VALUE) {
        return false;
    }
    PROCESSENTRY32W entry { sizeof(entry) };
    DWORD parent = 0;
    if (Process32FirstW(snapshot.get(), &entry)) {
        do {
            if (entry.th32ProcessID == GetCurrentProcessId()) {
                parent = entry.th32ParentProcessID;
                break;
            }
        } while (Process32NextW(snapshot.get(), &entry));
    }
    const auto bridge = FindWindowW(cloud::win32bridge::kClass, cloud::win32bridge::kTitle);
    DWORD_PTR result = 0;
    return parent && bridge
           && SendMessageTimeoutW(bridge, cloud::win32bridge::kWatchOwner, parent, 0, SMTO_ABORTIFHUNG | SMTO_BLOCK, 10000, &result)
           && result == 1;
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
    cloud::StartupNoticePolicy ended_notice;
    cloud::StartupNoticePolicy queue_timeout_notice;
    bool session_retry_pending = false;
    std::optional<std::chrono::steady_clock::time_point> session_recovery_deadline;
    auto retry_not_before = std::chrono::steady_clock::now();
    std::string previous_page;
    auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(60);
    auto loading_since = std::chrono::steady_clock::now();
    bool reported = false;
    cloud::StartupResourcePolicy resource_recovery;
    bool awaiting_login = false;
    while (IsWindow(hwnd)) {
        const auto response = cloud::Request(process_id, json::object { { "op", "status" } });
        if (!response || !response->get("ok", false)) {
            std::cerr << "Could not inspect the cloud page. No daily tasks were started.\n";
            return 1;
        }
        const auto& state = response->at("value");
        if (state.get("startup_resource_failed", false)) {
            if (!recover_startup_resource(process_id, resource_recovery, state.get("document_epoch", 0.0))) {
                return 3;
            }
            continue;
        }
        report_stalled_startup(process_id, state, loading_since, reported);
        const auto page = state.get("page", std::string { "unknown" });
        const auto now = std::chrono::steady_clock::now();
        const auto ended_action = ended_notice.observe(page == "ended", now, !state.get("ended_confirm_target", json::value {}).is_null());
        const auto queue_timeout_action = queue_timeout_notice.observe(
            page == "queue_timeout",
            now,
            !state.get("queue_timeout_confirm_target", json::value {}).is_null());
        if (page != previous_page) {
            std::cout << "Cloud startup: " << page << ". Waiting for a decoded game frame before starting daily tasks." << std::endl;
            previous_page = page;
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
                const auto bridge = FindWindowW(cloud::win32bridge::kClass, cloud::win32bridge::kTitle);
                DWORD_PTR result = 0;
                if (bridge
                    && SendMessageTimeoutW(bridge, cloud::win32bridge::kPrepareFrame, 0, 0, SMTO_ABORTIFHUNG | SMTO_BLOCK, 10000, &result)
                    && result == 1) {
                    std::cout << "Cloud game frame is ready. Starting daily tasks." << std::endl;
                    return 0;
                }
                std::cerr << "Could not prepare the GUI controller frame.\n";
                return 1;
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
        if (page == "ended") {
            const auto target = state.get("ended_confirm_target", json::value {});
            if (ended_action == cloud::StartupNoticePolicy::Action::Fail) {
                std::cerr << "The ended-session notice did not close or reappeared after recovery. No daily tasks were started.\n";
                return 2;
            }
            if (ended_action == cloud::StartupNoticePolicy::Action::Confirm) {
                if (target.is_null()) {
                    std::cerr << "The cloud session ended, but its confirmation button is unavailable. No daily tasks were started.\n";
                    return 2;
                }
                if (!click_target(target)) {
                    std::cerr << "Could not confirm the ended cloud session. No input will be replayed.\n";
                    return 1;
                }
                start_sent = false;
                busy_confirm_sent = false;
                session_retry_pending = false;
                session_recovery_deadline.reset();
                deadline = now + std::chrono::seconds(60);
                std::cout << "Cloud session ended after inactivity. Notice confirmed; waiting for the landing page before restarting."
                          << std::endl;
            }
            std::this_thread::sleep_for(std::chrono::milliseconds(500));
            continue;
        }
        if (page == "queue_timeout") {
            if (queue_timeout_action == cloud::StartupNoticePolicy::Action::Fail) {
                std::cerr << "The queue timeout notice did not close or the queue timed out again. No daily tasks were started.\n";
                return 3;
            }
            if (queue_timeout_action == cloud::StartupNoticePolicy::Action::Confirm) {
                const auto target = state.get("queue_timeout_confirm_target", json::value {});
                if (target.is_null() || !click_target(target)) {
                    std::cerr << "The cloud queue timed out, but its confirmation button is unavailable. No daily tasks were started.\n";
                    return 3;
                }
                start_sent = false;
                busy_confirm_sent = false;
                session_recovery_deadline = now + std::chrono::seconds(60);
                session_retry_pending = true;
                retry_not_before = now;
                std::cout << "Cloud queue timed out. Notice confirmed; waiting for the landing page before one restart attempt."
                          << std::endl;
            }
            std::this_thread::sleep_for(std::chrono::milliseconds(500));
            continue;
        }
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
                return launch_host() == 0 && bind_gui_owner() ? inspect_host(true) : 1;
            }
            if (operation == L"--prepare") {
                const int result = launch_host() == 0 && bind_gui_owner() ? prepare_host() : 1;
                if (result != 0) {
                    // Some GUIs continue their queue after a failed pre-task.
                    // Remove the controller before returning, so an empty or
                    // stale surface cannot be mistaken for a connected game.
                    if (const auto bridge = FindWindowW(cloud::win32bridge::kClass, cloud::win32bridge::kTitle)) {
                        DWORD_PTR ignored = 0;
                        SendMessageTimeoutW(bridge, WM_CLOSE, 0, 0, SMTO_ABORTIFHUNG | SMTO_BLOCK, 10000, &ignored);
                    }
                    close_host();
                }
                return result;
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
