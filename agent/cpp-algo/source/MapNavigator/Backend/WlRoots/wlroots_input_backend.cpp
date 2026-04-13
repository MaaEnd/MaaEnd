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

    SteeringTransportProfile steering_transport_profile() const override
    {
        return SteeringTransportProfile {
            .supports_concurrent_move_and_look = false,
            .min_send_interval_ms = 100,
            .min_emit_delta_deg = 2.0,
            .max_batch_delta_deg = 18.0,
            .action_quiet_period_ms = 50,
        };
    }

    // linux 桌面端运行终末地的架构如下所示：
    // [niri(main compositor)] -> [sway(wlroots-based sub-compositor)] -> [Xwayland] -> [Endfield]
    // 当游戏进入鼠标捕获状态时，Xwayland 不再关心鼠标绝对位置，而是根据鼠标相对移动来驱动游戏视角旋转
    // 相对移动的功能其实写起来容易，但会遇到移动至屏幕边缘时鼠标位置被限制在边缘导致无法继续移动的问题
    // 暂时只能通过按住 Alt 键进行一次绝对位置重置来处理，这会带来一些额外的输入延迟
    //
    // 另一个思路是，不通过 Xwayland 运行，直接让游戏以 wayland 客户端的身份运行
    // 但目前我用 wayland 方案虚拟鼠标会失效，不清楚是哪里的问题，先放在后面再看吧
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

        LogInfo << "SendViewDeltaByAltRecenterThenOffset" << VAR(dx) << VAR(dy) << VAR(end_x) << VAR(end_y);

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
