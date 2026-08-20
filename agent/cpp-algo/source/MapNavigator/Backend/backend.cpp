#include <utility>

#include <MaaUtils/Logger.h>

#include "../../MapLocator/MapLocateAction.h"
#include "../controller_type_utils.h"
#include "Adb/adb_input_backend.h"
#include "Desktop/desktop_input_backend.h"
#include "MapNavigator/controller_info_utils.h"
#include "Linux/linux_input_backend.h"
#include "backend.h"

namespace mapnavigator
{

std::unique_ptr<IInputBackend> CreateInputBackend(MaaController* ctrl)
{
    std::string controller_type = DetectControllerType(ctrl);
    if (controller_type.empty()) {
        controller_type = "unknown";
    }

    if (IsAdbLikeControllerType(controller_type)) {
        return backend::adb::CreateAdbInputBackend(ctrl, std::move(controller_type), maplocator::getOrInitLocator());
    }

    if (IsLinuxControllerType(controller_type)) {
        return backend::Linux::CreateLinuxInputBackend(ctrl, std::move(controller_type));
    }

    return backend::desktop::CreateDesktopInputBackend(ctrl, std::move(controller_type), "win32");
}

} // namespace mapnavigator
