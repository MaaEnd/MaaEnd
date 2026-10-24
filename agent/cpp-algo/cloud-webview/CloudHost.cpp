#include <chrono>
#include <exception>
#include <memory>
#include <string>
#include <string_view>
#include <vector>

#include <MaaUtils/Logger.h>
#include <MaaUtils/SafeWindows.hpp>
#include <tlhelp32.h>

#include "CloudHost.h"

#include "CloudBridge.h"
#include "CloudWindow.h"
#include "Win32ControllerBridge.h"
#include "cloud_protocol.h"
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
        LogError << "The cloud window is already running or could not be created.";
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
        LogInfo << L"Cloud WebView: proxy enabled at " << proxy_value << L".";
    }
    else if (proxy_length) {
        LogError << "Ignoring invalid MAAEND_CLOUD_PROXY; expected a host:port value.";
    }
    if (software) {
        LogInfo << "Cloud WebView: software rendering and video decoding enabled.";
    }
    CloudWindow window;
    window.SetAdditionalBrowserArguments(arguments);
    if (!window.Open()) {
        LogError << "Could not initialize WebView2. Check the installed WebView2 Runtime.";
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
        LogError << "The cloud window exited abnormally: " << exit_code;
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
    std::wstring command = L"\"" + std::wstring(executable.data(), size) + L"\" --cloud-host " + event_name;
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
        LogError << "Could not launch the cloud window: " << GetLastError();
        return 1;
    }
    const Handle process_handle(process.hProcess, &CloseHandle);
    const Handle thread_handle(process.hThread, &CloseHandle);
    const HANDLE handles[] = { event.get(), process_handle.get() };
    if (WaitForMultipleObjects(2, handles, FALSE, kStartupTimeoutMs) != WAIT_OBJECT_0) {
        // The new host may not have published an HWND yet. Reclaim only the
        // process created above, using its handle instead of window discovery.
        if (WaitForSingleObject(process_handle.get(), 0) == WAIT_TIMEOUT) {
            if (!TerminateProcess(process_handle.get(), 1)
                || WaitForSingleObject(process_handle.get(), cloud::kRequestTimeoutMs) != WAIT_OBJECT_0) {
                LogError << "Could not reclaim the failed cloud startup process: " << GetLastError();
            }
        }
        LogError << "The cloud window did not become ready.";
        return 1;
    }
    return 0;
}

} // namespace

namespace cloud
{
int HostCommand(int argc, char** argv)
{
    try {
        const std::string_view operation = argc >= 2 ? argv[1] : "";
        if (argc == 3 && operation == "--cloud-host") {
            const std::string event = argv[2];
            const std::wstring event_name(event.begin(), event.end());
            if (event_name.starts_with(kReadyEventPrefix)) {
                return run_host(event_name.c_str());
            }
        }
        if ((argc == 2 || (argc == 3 && std::string_view(argv[2]) == "{}")) && operation == "--cloud-launch") {
            if (launch_host() == 0 && bind_gui_owner()) {
                const auto bridge = FindWindowW(win32bridge::kClass, win32bridge::kTitle);
                DWORD_PTR result = 0;
                if (bridge
                    && SendMessageTimeoutW(
                        bridge,
                        win32bridge::kPrepareFrame,
                        0,
                        0,
                        SMTO_ABORTIFHUNG | SMTO_BLOCK,
                        kRequestTimeoutMs,
                        &result)
                    && result == 1) {
                    return 0;
                }
            }
            close_host();
            return 1;
        }
        if (argc == 2 && operation == "--cloud-close") {
            return close_host();
        }
        LogError << "Invalid cloud host command";
    }
    catch (const std::exception& error) {
        LogError << "Cloud window failed" << error.what();
    }
    return 1;
}
} // namespace cloud
