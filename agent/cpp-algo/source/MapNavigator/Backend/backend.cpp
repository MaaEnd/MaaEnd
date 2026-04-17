#include <utility>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../../MapLocator/MapLocateAction.h"
#include "../controller_type_utils.h"
#include "Adb/adb_input_backend.h"
#include "Desktop/desktop_input_backend.h"
#include "WlRoots/wlroots_input_backend.h"
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

    // WlRoots 与 Win32 共享同一套 Win32 VK 键码语义（前者在 interface.json 中开启
    // use_win32_vk_code，由 MaaFramework 翻译为 Linux evdev 码），但 WlRoots 仍需
    // 自己的 SendViewDeltaSync 实现来处理 Xwayland 鼠标捕获下的视角旋转。
    if (IsWlrootsControllerType(controller_type)) {
        return backend::wlroots::CreateWlrootsInputBackend(ctrl, std::move(controller_type));
    }

    return backend::desktop::CreateDesktopInputBackend(ctrl, std::move(controller_type), "win32");
}

} // namespace mapnavigator
