#include <utility>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <meojson/json.hpp>

#include "../../MapLocator/MapLocateAction.h"
#include "../controller_type_utils.h"
#include "Adb/adb_input_backend.h"
#include "WlRoots/wlroots_input_backend.h"
#include "Win32/win32_input_backend.h"
#include "backend.h"

namespace mapnavigator
{

namespace
{

std::string DetectControllerType(MaaController* ctrl)
{
    if (ctrl == nullptr) {
        return {};
    }

    MaaStringBuffer* buffer = MaaStringBufferCreate();
    if (buffer == nullptr) {
        return {};
    }

    std::string controller_type;
    if (MaaControllerGetInfo(ctrl, buffer) && !MaaStringBufferIsEmpty(buffer)) {
        const char* raw = MaaStringBufferGet(buffer);
        if (raw != nullptr && raw[0] != '\0') {
            const auto info = json::parse(raw).value_or(json::object {});
            if (info.contains("type") && info.at("type").is_string()) {
                controller_type = info.at("type").as_string();
            }
        }
    }

    MaaStringBufferDestroy(buffer);
    return controller_type;
}

} // namespace

std::unique_ptr<IInputBackend> CreateInputBackend(MaaController* ctrl)
{
    std::string controller_type = DetectControllerType(ctrl);
    if (controller_type.empty()) {
        controller_type = "unknown";
    }

    if (IsAdbLikeControllerType(controller_type)) {
        return backend::adb::CreateAdbInputBackend(ctrl, std::move(controller_type), maplocator::getOrInitLocator());
    }

    if (IsWlrootsControllerType(controller_type)) {
        return backend::wlroots::CreateWlrootsInputBackend(ctrl, std::move(controller_type));
    }

    return backend::win32::CreateWin32InputBackend(ctrl, std::move(controller_type));
}

} // namespace mapnavigator
