#include <limits>
#include <mutex>
#include <optional>
#include <set>
#include <string>
#include <utility>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../MapLocator/MapLocateAction.h"
#include "control/action_wrapper.h"
#include "control/motion_controller.h"
#include "navi_config.h"
#include "navi_controller.h"
#include "planning/navigation_session.h"
#include "planning/navmesh_path_expander.h"
#include "semantic/action_executor.h"
#include "sensing/position_provider.h"
#include "state_machine/navigation_state_machine.h"

namespace mapnavigator
{

namespace
{

// 子任务 pipeline 可能重入导航动作, 按 tasker 把导航嵌套深度限制为 1
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
    // 须最先取闸, 被拒的节点可能被无限重试
    const NavigationEntryGuard entry_guard(MaaContextGetTasker(ctx_));
    if (!entry_guard.taken()) {
        LogError << "Refusing to nest navigation: this tasker is already navigating.";
        return false;
    }

    NaviParam param = requested_param;

    ActionWrapper action_wrapper(ctx_);
    PositionProvider position_provider(action_wrapper.GetCtrl(), maplocator::getOrInitLocator(), param.heading_source);
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
    size_t resume_index = 0;
    if (!ExpandNavmeshWaypoints(param, pos, is_stopping, expanded_path)) {
        const std::optional<size_t> resume = ResolveRouteResumeIndex(param.path, pos);
        if (!resume) {
            return false;
        }
        NaviParam resumed_param = param;
        resumed_param.path.assign(param.path.begin() + static_cast<std::ptrdiff_t>(*resume), param.path.end());
        if (!ExpandNavmeshWaypoints(resumed_param, pos, is_stopping, expanded_path)) {
            return false;
        }
        resume_index = *resume;
        LogInfo << "Expanded the route again after skipping the traversed prefix." << VAR(resume_index);
    }
    if (expanded_path.empty()) {
        return true;
    }

    NaviParam expanded_param = param;
    expanded_param.path = std::move(expanded_path);
    expanded_param.authored_path = std::move(param.path);
    if (resume_index > 0) {
        for (Waypoint& waypoint : expanded_param.path) {
            if (waypoint.authored_group_begin != std::numeric_limits<size_t>::max()) {
                waypoint.authored_group_begin += resume_index;
            }
        }
    }

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
