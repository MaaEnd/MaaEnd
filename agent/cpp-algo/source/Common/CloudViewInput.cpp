#include "CloudViewInput.h"

#include <limits>
#include <string_view>

#include <MaaFramework/MaaMsg.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../utils.h"

#ifdef _WIN32
#include "../../cloud-webview/win32_bridge_protocol.h"
#include <MaaUtils/SafeWindows.hpp>
#endif

namespace cloud
{
namespace
{
using BridgeMessage = std::function<bool(uint32_t, intptr_t, intptr_t)>;

BridgeMessage GetBridge(MaaController* controller)
{
#ifdef _WIN32
    ScopedStringBuffer buffer;
    if (!controller || !buffer.Get() || !MaaControllerGetInfo(controller, buffer.Get())) {
        return {};
    }
    const auto info = json::parse(MaaStringBufferGet(buffer.Get()));
    if (!info || info->get("type", std::string {}) != "win32") {
        return {};
    }
    const auto hwnd = reinterpret_cast<HWND>(static_cast<uintptr_t>(info->get("hwnd", int64_t { 0 })));
    wchar_t class_name[128] {};
    if (!GetClassNameW(hwnd, class_name, static_cast<int>(std::size(class_name))) || std::wstring_view(class_name) != win32bridge::kClass) {
        return {};
    }
    DWORD pid = 0;
    GetWindowThreadProcessId(hwnd, &pid);
    return [hwnd, pid](uint32_t message, intptr_t first, intptr_t second) {
        DWORD current_pid = 0;
        wchar_t current_class[128] {};
        if (!IsWindow(hwnd) || !GetWindowThreadProcessId(hwnd, &current_pid) || current_pid != pid
            || !GetClassNameW(hwnd, current_class, static_cast<int>(std::size(current_class)))
            || std::wstring_view(current_class) != win32bridge::kClass) {
            return false;
        }
        DWORD_PTR result = 0;
        return SendMessageTimeoutW(
                   hwnd,
                   message,
                   static_cast<WPARAM>(first),
                   static_cast<LPARAM>(second),
                   SMTO_ABORTIFHUNG | SMTO_BLOCK,
                   10000,
                   &result)
               && result == 1;
    };
#else
    (void)controller;
    return {};
#endif
}

bool Int32(const json::value& value, int32_t& output)
{
    if (!value.is_number()) {
        return false;
    }
    const auto number = value.as_long_long();
    if (number < std::numeric_limits<int32_t>::min() || number > std::numeric_limits<int32_t>::max()) {
        return false;
    }
    output = static_cast<int32_t>(number);
    return true;
}
} // namespace

ViewInput CreateViewInput(MaaController* controller)
{
#ifdef _WIN32
    const auto bridge = GetBridge(controller);
    if (bridge) {
        return [bridge](int32_t dx, int32_t dy) {
            return bridge(win32bridge::kRelativeMove, dx, dy);
        };
    }
#else
    (void)controller;
#endif
    return {};
}

void MAA_CALL TaskerEvent(void* handle, const char* message, const char* details, void*)
{
#ifdef _WIN32
    try {
        const std::string_view event = message ? message : "";
        const bool starting = event == MaaMsg_Tasker_Task_Starting;
        if (!starting && event != MaaMsg_Tasker_Task_Succeeded && event != MaaMsg_Tasker_Task_Failed) {
            return;
        }
        const auto parsed = json::parse(details ? details : "{}");
        const auto task_id = parsed ? parsed->get("task_id", int64_t { 0 }) : 0;
        if (task_id <= 0 || task_id > std::numeric_limits<intptr_t>::max()) {
            return;
        }
        auto controller = MaaTaskerGetController(static_cast<MaaTasker*>(handle));
        const auto bridge = GetBridge(controller);
        if (bridge) {
            LogInfo << "Cloud task lifecycle" << event << VAR(task_id);
        }
        if (bridge && !bridge(starting ? win32bridge::kBeginTask : win32bridge::kEndTask, GetCurrentProcessId(), task_id)) {
            LogError << "Cloud task lifecycle bridge failed" << event;
        }
    }
    catch (const std::exception& error) {
        LogError << "Cloud task lifecycle bridge failed" << error.what();
    }
#else
    (void)handle;
    (void)message;
    (void)details;
#endif
}

MaaBool MAA_CALL CloseActionRun(MaaContext* context, MaaTaskId, const char*, const char*, const char*, MaaRecoId, const MaaRect*, void*)
{
#ifdef _WIN32
    try {
        auto tasker = MaaContextGetTasker(context);
        auto controller = tasker ? MaaTaskerGetController(tasker) : nullptr;
        const auto bridge = GetBridge(controller);
        return bridge && bridge(win32bridge::kCloseHost, 0, 0);
    }
    catch (const std::exception& error) {
        LogError << "Could not close cloud host" << error.what();
    }
#else
    (void)context;
#endif
    return false;
}

MaaBool MAA_CALL RelativeMoveActionRun(
    MaaContext* context,
    MaaTaskId,
    const char*,
    const char*,
    const char* params,
    MaaRecoId reco_id,
    const MaaRect* box,
    void*)
{
    try {
        const auto parsed = json::parse(params ? params : "{}");
        if (!parsed || !parsed->is_object()) {
            return false;
        }
        int32_t dx = 0, dy = 0;
        if (parsed->contains("begin")) {
            const auto& begin = parsed->at("begin");
            int32_t x = 0, y = 0;
            if (!begin.is_array() || begin.as_array().size() != 2 || !box || reco_id == MaaInvalidId || !Int32(begin[0], x)
                || !Int32(begin[1], y) || !Int32(json::value(int64_t(box->x) - x), dx) || !Int32(json::value(int64_t(box->y) - y), dy)) {
                return false;
            }
        }
        else if (!parsed->contains("dx") || !parsed->contains("dy") || !Int32(parsed->at("dx"), dx) || !Int32(parsed->at("dy"), dy)) {
            return false;
        }
        auto tasker = MaaContextGetTasker(context);
        auto controller = tasker ? MaaTaskerGetController(tasker) : nullptr;
        const auto send = CreateViewInput(controller);
        return send && send(dx, dy);
    }
    catch (const std::exception& error) {
        LogError << "Invalid cloud relative move" << error.what();
        return false;
    }
}
} // namespace cloud
