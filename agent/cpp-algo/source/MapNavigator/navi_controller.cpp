#include <mutex>
#include <set>
#include <string>
#include <utility>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../MapLocator/MapLocateAction.h"
#include "action_executor.h"
#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_controller.h"
#include "navigation_session.h"
#include "navigation_state_machine.h"
#include "navmesh_path_expander.h"
#include "position_provider.h"

namespace mapnavigator
{

namespace
{

// 导航期间调起的交互子任务是 pipeline, 它的 next 能被接回导航动作本身。子任务同步调起, 那会在同一条 nav
// 线程上套第二层导航: 两层抢同一个控制器与定位器, 而且能无限套下去。这道闸把栈深封死在 1, 按 tasker 分,
// 一个进程里跑多台设备时互不误伤。
std::mutex g_navigating_taskers_mutex;
std::set<MaaTasker*> g_navigating_taskers;

class NavigationEntryGuard
{
public:
    explicit NavigationEntryGuard(MaaTasker* tasker)
        : tasker_(tasker)
    {
        const std::lock_guard<std::mutex> lock(g_navigating_taskers_mutex);
        taken_ = g_navigating_taskers.insert(tasker).second;
    }

    ~NavigationEntryGuard()
    {
        if (!taken_) {
            return;
        }
        const std::lock_guard<std::mutex> lock(g_navigating_taskers_mutex);
        g_navigating_taskers.erase(tasker_);
    }

    NavigationEntryGuard(const NavigationEntryGuard&) = delete;
    NavigationEntryGuard& operator=(const NavigationEntryGuard&) = delete;

    bool taken() const { return taken_; }

private:
    MaaTasker* tasker_;
    bool taken_ = false;
};

} // namespace

NaviController::NaviController(MaaContext* ctx)
    : ctx_(ctx)
{
}

bool NaviController::Navigate(const NaviParam& requested_param)
{
    // 先取闸再做任何事: 作者的图若无限重试这个被拒的节点, 每次都必须是零成本的。
    const NavigationEntryGuard entry_guard(MaaContextGetTasker(ctx_));
    if (!entry_guard.taken()) {
        LogError << "Refusing to nest navigation: this tasker is already navigating.";
        return false;
    }

    NaviParam param = requested_param;

    ActionWrapper action_wrapper(ctx_);
    PositionProvider position_provider(action_wrapper.GetCtrl(), maplocator::getOrInitLocator());
    position_provider.ResetTracking();
    if (param.normalize_position_via_navmesh) {
        position_provider.SetPositionNormalizer([&param](NaviPosition& pos) { NormalizeLivePositionToBase(param, pos); });
    }
    const char* controller_type = action_wrapper.controller_type();
    const bool uses_touch_backend = action_wrapper.uses_touch_backend();
    LogInfo << "MapNavigator controller initialized." << VAR(controller_type) << VAR(uses_touch_backend);
    if (!action_wrapper.is_supported()) {
        const char* unsupported_reason = action_wrapper.unsupported_reason();
        LogError << "MapNavigator controller backend is unsupported." << VAR(controller_type) << VAR(unsupported_reason);
        return false;
    }

    // 触屏后端没有独立的鼠标左右键: 起滑那一下会打出攻击, 下索那一下会变成冲刺。
    // 站在架子上做不成这两件事, 所以这类后端一律纯走路。
    if (uses_touch_backend && param.zipline_enabled) {
        LogWarn << "Zipline disabled: this backend has no mouse buttons to aim and launch with." << VAR(controller_type);
        param.zipline_enabled = false;
    }

    const auto is_stopping = [&]() {
        return MaaTaskerStopping(MaaContextGetTasker(ctx_));
    };

    if (param.path.empty()) {
        return true;
    }

    const size_t target_count = param.path.size();
    LogInfo << "Starting navigation to targets." << VAR(target_count);
    PreloadNavmeshWaypoints(param);
    LogInfo << "Waiting for first valid GPS signal...";

    NaviPosition pos;
    const std::string initial_expected_zone = InitialExpectedZone(param);
    if (!position_provider.WaitForFix(&pos, initial_expected_zone, kLocatorWaitMaxRetries, kLocatorWaitIntervalMs, is_stopping)
        || is_stopping()) {
        return false;
    }

    const double position_x = pos.x;
    const double position_y = pos.y;
    LogInfo << "Initial Pos fixed:" << VAR(position_x) << VAR(position_y);

    std::vector<Waypoint> expanded_path;
    if (!ExpandNavmeshWaypoints(param, pos, is_stopping, expanded_path)) {
        return false;
    }
    if (expanded_path.empty()) {
        return true;
    }

    NaviParam expanded_param = param;
    expanded_param.path = std::move(expanded_path);
    expanded_param.authored_path = std::move(param.path);

    NavigationSession session(expanded_param.path, pos);
    session.UpdatePhase(NaviPhase::Bootstrap, "initial_fix");

    MotionController motion_controller(&action_wrapper, param.enable_local_driver);
    ActionExecutor action_executor(&action_wrapper, &motion_controller, param.enable_local_driver);
    NavigationStateMachine state_machine(
        expanded_param,
        &action_wrapper,
        &position_provider,
        &session,
        &motion_controller,
        &action_executor,
        &pos,
        is_stopping,
        ctx_);

    return state_machine.Run();
}

} // namespace mapnavigator
