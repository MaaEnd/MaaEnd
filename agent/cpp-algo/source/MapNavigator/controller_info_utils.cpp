#include "controller_info_utils.h"

#include <optional>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <meojson/json.hpp>

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

    MaaStringBuffer* buffer = MaaStringBufferCreate();
    if (buffer == nullptr) {
        return {};
    }

    if (!MaaControllerGetInfo(controller, buffer)) {
        MaaStringBufferDestroy(buffer);
        return {};
    }

    const char* raw = MaaStringBufferGet(buffer);
    ControllerInfo info = json::parse(raw).value().as<ControllerInfo>();
    MaaStringBufferDestroy(buffer);
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
