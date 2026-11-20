#include <cstdint>
#include <optional>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../../utils.h"
#include "controller_info_utils.h"

namespace mapnavigator
{

namespace
{

struct ControllerInfo
{
    std::string type;
    std::optional<int> hwnd;
    std::optional<int> mouse_method;

    MEO_JSONIZATION(MEO_OPT type, MEO_OPT hwnd, MEO_OPT mouse_method)
};

std::optional<json::value> parse_controller_json(MaaController* controller)
{
    if (controller == nullptr) {
        return std::nullopt;
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaControllerGetInfo(controller, buffer.Get())) {
        return std::nullopt;
    }

    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr) {
        return std::nullopt;
    }

    const auto parsed = json::parse(raw);
    if (!parsed) {
        LogWarn << "Failed to parse controller info" << VAR(raw);
        return std::nullopt;
    }

    return *parsed;
}

ControllerInfo parse_controller_info(MaaController* controller)
{
    const std::optional<json::value> parsed = parse_controller_json(controller);
    if (!parsed.has_value()) {
        return {};
    }

    ControllerInfo info;
    if (!info.from_json(*parsed)) {
        LogWarn << "Controller info lacks expected fields" << VAR(parsed->to_string());
        return {};
    }

    return info;
}

} // namespace

std::optional<uintptr_t> TryGetWin32WindowHandle(MaaController* controller)
{
    const std::optional<json::value> parsed = parse_controller_json(controller);
    if (!parsed.has_value()) {
        return std::nullopt;
    }

    const json::value* handle = parsed->find_value("hwnd");
    if (handle == nullptr || !handle->is_number()) {
        return std::nullopt;
    }

    return static_cast<uintptr_t>(handle->as_unsigned_long_long());
}

std::string DetectControllerType(MaaController* controller)
{
    const auto info = parse_controller_info(controller);
    return info.type;
}

bool TryGetWin32Hwnd(MaaController* controller, uint64_t* out_hwnd)
{
    if (out_hwnd == nullptr) {
        return false;
    }

    const auto info = parse_controller_info(controller);
    if (!info.hwnd.has_value() || info.hwnd.value() <= 0) {
        return false;
    }

    *out_hwnd = static_cast<uint64_t>(info.hwnd.value());
    return true;
}

bool TryGetWin32MouseInputMethod(MaaController* controller, MaaWin32InputMethod* out_method)
{
    if (out_method == nullptr) {
        return false;
    }

    const auto info = parse_controller_info(controller);
    if (!info.mouse_method.has_value()) {
        return false;
    }

    *out_method = static_cast<MaaWin32InputMethod>(info.mouse_method.value());
    return true;
}

bool IsMessageInputMethod(MaaWin32InputMethod method)
{
    switch (method) {
    case MaaWin32InputMethod_SendMessage:
    case MaaWin32InputMethod_PostMessage:
    case MaaWin32InputMethod_SendMessageWithCursorPos:
    case MaaWin32InputMethod_PostMessageWithCursorPos:
    case MaaWin32InputMethod_SendMessageWithWindowPos:
    case MaaWin32InputMethod_PostMessageWithWindowPos:
        return true;
    default:
        return false;
    }
}

} // namespace mapnavigator
