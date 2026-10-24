#include <atomic>
#include <chrono>
#include <iostream>
#include <stdexcept>
#include <string_view>
#include <tuple>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaToolkit/MaaToolkitAPI.h>

#include "Common/CloudViewInput.h"
#include "Win32ControllerBridge.h"
#include "transport/win_cloud_transport.h"
#include "win32_bridge_protocol.h"

namespace
{
void Check(bool condition, const char* message)
{
    if (!condition) {
        throw std::runtime_error(message);
    }
}

class RecordingTransport final : public CloudTransport
{
public:
    bool connect() override { return true; }

    bool capture(CloudFrame& output) override
    {
        const auto sequence = ++captures;
        output.width = 1280;
        output.height = 720;
        if (output.pixels.capacity() < 1280 * 720 * 3) {
            ++frame_allocations;
        }
        output.pixels.resize(1280 * 720 * 3);
        for (size_t i = 0; i < output.pixels.size(); i += 3) {
            output.pixels[i] = static_cast<uint8_t>((sequence % 254) + 1);
            output.pixels[i + 1] = 83;
            output.pixels[i + 2] = 231;
        }
        return true;
    }

    bool mouse(const CloudMouseEvent& event) override
    {
        mouse_events.push_back(event);
        return true;
    }

    bool key(int32_t key, bool down, int32_t, bool) override
    {
        keys.emplace_back(key, down);
        return true;
    }

    bool insert_text(const std::string& text) override
    {
        inserted += text;
        return true;
    }

    bool relative_move(int32_t dx, int32_t dy, int32_t, int32_t) override
    {
        moves.emplace_back(dx, dy);
        return true;
    }

    std::vector<CloudMouseEvent> mouse_events;
    std::vector<std::pair<int32_t, bool>> keys;
    std::vector<std::pair<int32_t, int32_t>> moves;
    std::string inserted;
    std::atomic<int> captures = 0;
    std::atomic<int> frame_allocations = 0;
};
} // namespace

int main(int argc, char** argv)
{
    if (argc == 2 && std::string_view(argv[1]) == "--bridge-owner") {
        Sleep(500);
        return 0;
    }
    try {
        SetThreadDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
        std::vector<std::string> local_operations;
        bool accepted = true;
        // PID zero makes an accidental pipe fallback fail. In-process input
        // must reach the callback once, including failed dispatches.
        WinCloudTransport local(0, [&](const json::value& request) {
            local_operations.push_back(request.at("op").as_string());
            return json::value(json::object { { "ok", accepted } });
        });
        Check(local.mouse({ CloudMouseAction::Move, 640, 360 }), "local mouse dispatch failed");
        Check(local.key(27, false, 0, false), "local key dispatch failed");
        Check(local.insert_text("A测试"), "local text dispatch failed");
        Check(local.relative_move(-70000, 80000, 0, 0), "local relative dispatch failed");
        accepted = false;
        Check(!local.key(27, false, 0, false), "failed local input was accepted");
        Check(
            local_operations == std::vector<std::string> { "mouse", "key", "text", "relative", "key" },
            "local input was lost or replayed");
        RecordingTransport transport;
        Win32ControllerBridge bridge(transport);
        HWND hwnd = nullptr;
        EnumWindows(
            [](HWND candidate, LPARAM result) -> BOOL {
                DWORD pid = 0;
                GetWindowThreadProcessId(candidate, &pid);
                wchar_t name[128] {};
                if (pid == GetCurrentProcessId() && GetClassNameW(candidate, name, static_cast<int>(std::size(name)))
                    && std::wstring_view(name) == cloud::win32bridge::kClass) {
                    *reinterpret_cast<HWND*>(result) = candidate;
                    return FALSE;
                }
                return TRUE;
            },
            reinterpret_cast<LPARAM>(&hwnd));
        Check(hwnd != nullptr, "bridge window absent");
        auto windows = MaaToolkitDesktopWindowListCreate();
        Check(MaaToolkitDesktopWindowFindAll(windows), "Toolkit enumeration failed");
        bool found = false;
        for (MaaSize i = 0; i < MaaToolkitDesktopWindowListSize(windows); ++i) {
            found |= MaaToolkitDesktopWindowGetHandle(MaaToolkitDesktopWindowListAt(windows, i)) == hwnd;
        }
        MaaToolkitDesktopWindowListDestroy(windows);
        Check(found, "unmodified Toolkit cannot enumerate bridge");

        const auto foreground = GetForegroundWindow();
        POINT before {};
        GetCursorPos(&before);
        auto ctrl = MaaWin32ControllerCreate(
            hwnd,
            MaaWin32ScreencapMethod_PrintWindow,
            MaaWin32InputMethod_SendMessage,
            MaaWin32InputMethod_SendMessage);
        Check(ctrl != nullptr, "Win32 controller creation failed");
        const auto wait = [&](MaaCtrlId id) {
            Check(MaaControllerWait(ctrl, id) == MaaStatus_Succeeded, "controller action failed");
        };
        wait(MaaControllerPostConnection(ctrl));
        Check(SendMessageW(hwnd, cloud::win32bridge::kPrepareFrame, 0, 0) == 1, "frame preparation failed");
        auto tasker = MaaTaskerCreate();
        Check(MaaTaskerBindController(tasker, ctrl), "bind tasker controller failed");
        cloud::TaskerEvent(tasker, "Tasker.Task.Starting", R"({"task_id":1})", nullptr);
        auto image = MaaImageBufferCreate();
        const auto started = std::chrono::steady_clock::now();
        uint8_t previous = 0;
        int changes = 0;
        for (int i = 0; i < 50; ++i) {
            wait(MaaControllerPostScreencap(ctrl));
            Check(MaaControllerCachedImage(ctrl, image), "cached image unavailable");
            Check(MaaImageBufferWidth(image) == 1280 && MaaImageBufferHeight(image) == 720, "frame size changed");
            int32_t controller_width = 0;
            int32_t controller_height = 0;
            Check(
                MaaControllerGetResolution(ctrl, &controller_width, &controller_height) && controller_width == 1280
                    && controller_height == 720,
                "controller resolution left the fixed cloud input space");
            const auto pixels = static_cast<const uint8_t*>(MaaImageBufferGetRawData(image));
            if (pixels && (pixels[0] == 0 || pixels[1] != 83 || pixels[2] != 231)) {
                std::cerr << "Got BGR " << int(pixels[0]) << ',' << int(pixels[1]) << ',' << int(pixels[2]) << "; capture callbacks "
                          << transport.captures.load() << '\n';
            }
            Check(pixels && pixels[0] != 0 && pixels[1] == 83 && pixels[2] == 231, "BGR frame mismatch");
            changes += pixels[0] != previous;
            previous = pixels[0];
        }
        Check(changes >= 30, "PrintWindow surface stopped updating");
        Check(transport.frame_allocations.load() == 2, "frame buffers were not reused");
        std::cout << "50 PrintWindow captures: "
                  << std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - started).count() << " ms\n";
        std::cout << "Total frame callbacks: " << transport.captures.load() << '\n';
        std::cout << "Frame allocations: " << transport.frame_allocations.load() << '\n';
        wait(MaaControllerPostClick(ctrl, 1001, 613));
        Check(transport.mouse_events.size() >= 3, "mouse input missing");
        const auto& press = transport.mouse_events[transport.mouse_events.size() - 2];
        Check(
            press.action == CloudMouseAction::Press && press.x == 1001 && press.y == 613 && press.buttons == 1,
            "click coordinate or button mismatch");
        wait(MaaControllerPostSwipe(ctrl, 1025, 575, 1180, 575, 200));
        const auto& release = transport.mouse_events.back();
        Check(
            release.action == CloudMouseAction::Release && release.x == 1180 && release.y == 575 && release.buttons == 0,
            "Win32 swipe released at its starting position");
        wait(MaaControllerPostKeyDown(ctrl, 27));
        wait(MaaControllerPostKeyUp(ctrl, 27));
        Check(transport.keys == std::vector<std::pair<int32_t, bool>> { { 27, true }, { 27, false } }, "Esc mismatch");
        wait(MaaControllerPostInputText(ctrl, "A测试"));
        Check(transport.inserted == "A测试", "text mismatch");
        const auto relative = cloud::CreateViewInput(ctrl);
        Check(static_cast<bool>(relative), "cloud relative extension not detected");
        Check(relative(-70000, 80000), "relative input failed");
        Check(transport.moves == std::vector<std::pair<int32_t, int32_t>> { { -70000, 80000 } }, "relative delta truncated");
        wait(MaaControllerPostKeyDown(ctrl, 87));
        cloud::TaskerEvent(tasker, "Tasker.Task.Failed", R"({"task_id":1})", nullptr);
        Check(transport.keys.back() == std::pair<int32_t, bool> { 87, false }, "cancel left key held");
        // Allow the single in-flight capture to settle, then idle must do no work.
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
        const auto idle_captures = transport.captures.load();
        std::this_thread::sleep_for(std::chrono::milliseconds(150));
        Check(transport.captures.load() == idle_captures, "idle bridge still captures");
        cloud::TaskerEvent(tasker, "Tasker.Task.Starting", R"({"task_id":2})", nullptr);
        std::this_thread::sleep_for(std::chrono::milliseconds(120));
        Check(transport.captures.load() > idle_captures, "restart did not resume captures");
        // Reentrant AgentServer RPC can deliver an older end after the next
        // start. It must neither stop captures nor release the new task's hold.
        wait(MaaControllerPostKeyDown(ctrl, 87));
        const auto held_keys = transport.keys.size();
        cloud::TaskerEvent(tasker, "Tasker.Task.Succeeded", R"({"task_id":1})", nullptr);
        const auto before_delayed_end = transport.captures.load();
        std::this_thread::sleep_for(std::chrono::milliseconds(150));
        Check(transport.captures.load() > before_delayed_end, "older task end paused a newer task");
        Check(transport.keys.size() == held_keys, "older task end released a newer task's key");
        cloud::TaskerEvent(tasker, "Tasker.Task.Succeeded", R"({"task_id":2})", nullptr);
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
        const auto completed_captures = transport.captures.load();
        cloud::TaskerEvent(tasker, "Tasker.Task.Starting", R"({"task_id":2})", nullptr);
        cloud::TaskerEvent(tasker, "Tasker.Task.Starting", R"({"task_id":1})", nullptr);
        std::this_thread::sleep_for(std::chrono::milliseconds(150));
        Check(transport.captures.load() == completed_captures, "delayed start resumed a completed task");
        wchar_t executable[32768] {};
        Check(GetModuleFileNameW(nullptr, executable, static_cast<DWORD>(std::size(executable))), "test executable unavailable");
        std::wstring command = L"\"" + std::wstring(executable) + L"\" --bridge-owner";
        STARTUPINFOW startup { sizeof(startup) };
        PROCESS_INFORMATION process {};
        Check(
            CreateProcessW(executable, command.data(), nullptr, nullptr, FALSE, CREATE_NO_WINDOW, nullptr, nullptr, &startup, &process),
            "could not create task owner");
        const auto watched = SendMessageW(hwnd, cloud::win32bridge::kBeginTask, process.dwProcessId, 3);
        const auto exited = WaitForSingleObject(process.hProcess, 5000);
        CloseHandle(process.hThread);
        CloseHandle(process.hProcess);
        Check(watched == 1 && exited == WAIT_OBJECT_0, "could not watch task owner");
        wait(MaaControllerPostKeyDown(ctrl, 87));
        std::this_thread::sleep_for(std::chrono::milliseconds(1200));
        // A synchronous message establishes a barrier after the timer callback.
        Check(SendMessageW(hwnd, WM_NULL, 0, 0) == 0, "bridge stopped responding");
        Check(transport.keys.back() == std::pair<int32_t, bool> { 87, false }, "agent exit left key held");
        const auto exited_captures = transport.captures.load();
        std::this_thread::sleep_for(std::chrono::milliseconds(150));
        Check(transport.captures.load() == exited_captures, "agent exit left frame capture running");
        POINT after {};
        GetCursorPos(&after);
        Check(before.x == after.x && before.y == after.y, "bridge moved system cursor");
        Check(GetForegroundWindow() == foreground, "bridge stole foreground");
        MaaImageBufferDestroy(image);
        MaaTaskerDestroy(tasker);
        MaaControllerDestroy(ctrl);
        std::cout << "Unmodified Toolkit + Framework bridge integration passed.\n";
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
    return 0;
}
