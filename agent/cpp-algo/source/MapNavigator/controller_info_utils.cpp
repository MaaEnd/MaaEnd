#include <optional>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../utils.h"
#include "controller_info_utils.h"

namespace mapnavigator
{

namespace
{

struct ControllerInfo
{
    std::string type;
    std::optional<int> mouse_method;

    MEO_JSONIZATION(MEO_OPT type, MEO_OPT mouse_method)
};

ControllerInfo ParseControllerInfo(MaaController* controller)
{
    if (controller == nullptr) {
        return {};
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaControllerGetInfo(controller, buffer.Get())) {
        return {};
    }

    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr) {
        return {};
    }

    // 格式不由本模块保证。解析失败按「拿不到信息」处理，与 controller 为空同路。
    const auto parsed = json::parse(raw);
    ControllerInfo info;
    if (!parsed || !info.from_json(*parsed)) {
        LogWarn << "Failed to parse controller info" << VAR(raw);
        return {};
    }

    return info;
}

} // namespace

std::string DetectControllerType(MaaController* controller)
{
    const auto info = ParseControllerInfo(controller);
    return info.type;
}

bool TryGetWin32MouseInputMethod(MaaController* controller, MaaWin32InputMethod* out_method)
{
    if (out_method == nullptr) {
        return false;
    }

    const auto info = ParseControllerInfo(controller);
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
