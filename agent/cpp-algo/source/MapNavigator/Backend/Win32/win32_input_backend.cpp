#include <utility>

#include <MaaUtils/Logger.h>

#include "../Desktop/desktop_input_backend.h"
#include "win32_input_backend.h"
#include "win32_key_codes.h"

namespace mapnavigator::backend::win32
{

namespace
{

desktop::DesktopKeyCodes MakeWin32KeyCodes()
{
    return desktop::DesktopKeyCodes {
        .move_forward = kMoveForwardKey,
        .move_left = kMoveLeftKey,
        .move_backward = kMoveBackwardKey,
        .move_right = kMoveRightKey,
        .interact = kInteractKey,
        .jump = kJumpKey,
    };
}

} // namespace

std::unique_ptr<IInputBackend> CreateWin32InputBackend(MaaController* ctrl, std::string controller_type)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=win32";
    return std::make_unique<desktop::DesktopInputBackend>(
        ctrl,
        std::move(controller_type),
        "win32",
        MakeWin32KeyCodes());
}

} // namespace mapnavigator::backend::win32
