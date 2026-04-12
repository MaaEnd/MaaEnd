#include <algorithm>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../../navi_config.h"
#include "../Desktop/desktop_input_backend.h"
#include "wlroots_input_backend.h"
#include "wlroots_key_codes.h"

namespace mapnavigator::backend::wlroots
{

namespace
{

desktop::DesktopKeyCodes MakeWlrootsKeyCodes();

class WlrootsInputBackend final : public desktop::DesktopInputBackend
{
public:
    WlrootsInputBackend(MaaController* ctrl, std::string controller_type)
        : desktop::DesktopInputBackend(ctrl, std::move(controller_type), "wlroots", MakeWlrootsKeyCodes())
    {
    }

    bool SendViewDeltaSync(int dx, int dy) override
    {
        if (dx == 0 && dy == 0) {
            return true;
        }

        MaaController* controller = GetCtrl();
        if (controller == nullptr) {
            return false;
        }

        EnsureHoverAnchorSync();

        const int start_x = hover_x();
        const int start_y = hover_y();
        const int end_x = start_x + dx;
        const int end_y = start_y + dy;

        LogInfo << "SendViewDeltaFallbackByTouchMoveHoverAnchored"
            << VAR(dx) << VAR(dy) << VAR(start_x) << VAR(start_y) << VAR(end_x) << VAR(end_y);

        const MaaCtrlId move_start_id = MaaControllerPostTouchMove(controller, 0, start_x, start_y, 0);
        if (move_start_id == MaaInvalidId || MaaControllerWait(controller, move_start_id) != MaaStatus_Succeeded) {
            return false;
        }

        const MaaCtrlId move_end_id = MaaControllerPostTouchMove(controller, 0, end_x, end_y, 0);
        if (move_end_id == MaaInvalidId || MaaControllerWait(controller, move_end_id) != MaaStatus_Succeeded) {
            return false;
        }

        return true;
    }
};

desktop::DesktopKeyCodes MakeWlrootsKeyCodes()
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

std::unique_ptr<IInputBackend> CreateWlrootsInputBackend(MaaController* ctrl, std::string controller_type)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=wlroots";
    return std::make_unique<WlrootsInputBackend>(ctrl, std::move(controller_type));
}

} // namespace mapnavigator::backend::wlroots
