#include <algorithm>
#include <chrono>
#include <thread>
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

constexpr int32_t kWlrootsReferenceFrameHeight = 720;
constexpr int32_t kWlrootsCenterX = kWorkWidth / 2;
constexpr int32_t kWlrootsCenterY = kWlrootsReferenceFrameHeight / 2;
constexpr int32_t kWlrootsAltSettleDelayMs = 33;

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

        const int end_x = std::clamp(kWlrootsCenterX + dx, 0, kWorkWidth - 1);
        const int end_y = std::clamp(kWlrootsCenterY + dy, 0, kWlrootsReferenceFrameHeight - 1);

        LogInfo << "SendViewDeltaByAltRecenterThenOffset"
                << VAR(dx) << VAR(dy) << VAR(end_x) << VAR(end_y);

        PostKeyDownSync(kLeftAltKey, kWlrootsAltSettleDelayMs);

        const MaaCtrlId recenter_id = MaaControllerPostTouchMove(controller, 0, kWlrootsCenterX, kWlrootsCenterY, 0);
        const bool recentered = recenter_id != MaaInvalidId && MaaControllerWait(controller, recenter_id) == MaaStatus_Succeeded;
        PostKeyUpSync(kLeftAltKey, kWlrootsAltSettleDelayMs);
        if (!recentered) {
            return false;
        }

        const MaaCtrlId move_end_id = MaaControllerPostTouchMove(controller, 0, end_x, end_y, 0);
        return move_end_id != MaaInvalidId && MaaControllerWait(controller, move_end_id) == MaaStatus_Succeeded;
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
