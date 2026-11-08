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
#include "Backend/backend.h"
#include "control/motion_controller.h"
#include "navi_config.h"
#include "navi_controller.h"
#include "planning/navigation_session.h"
#include "planning/navmesh_path_expander.h"
#include "planning/navmesh_queries.h"
#include "sensing/position_provider.h"
#include "sensing/task_stop.h"
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

// 返回跳过的作者点数
std::optional<size_t>
    expand_route(std::vector<Waypoint>* out_path, const NaviParam& param, const NaviPosition& pos, MaaContext* maa_context)
{
    if (ExpandNavmeshWaypoints(out_path, nullptr, param, pos, maa_context)) {
        return 0;
    }
    const std::optional<size_t> resume = ResolveRouteResumeIndex(param.path, pos);
    if (!resume) {
        return std::nullopt;
    }
    NaviParam resumed_param = param;
    resumed_param.path.assign(param.path.begin() + static_cast<std::ptrdiff_t>(*resume), param.path.end());
    if (!ExpandNavmeshWaypoints(out_path, nullptr, resumed_param, pos, maa_context)) {
        return std::nullopt;
    }
    const size_t resume_index = *resume;
    LogInfo << "Expanded the route again after skipping the traversed prefix." << VAR(resume_index);
    return resume_index;
}

// 展开时记的组首是相对跳过前缀后的路线, 补回跳过的点数
void offset_authored_groups(std::vector<Waypoint>* path, size_t resume_index)
{
    for (Waypoint& waypoint : *path) {
        if (waypoint.authored_group_begin != std::numeric_limits<size_t>::max()) {
            waypoint.authored_group_begin += resume_index;
        }
    }
}

bool navigate_with_backend(NaviParam* param, InputBackend* input_backend, MaaContext* ctx)
{
    PositionSensor position_provider = MakePositionSensor(input_backend->ctrl, maplocator::getOrInitLocator(), param->heading_source);
    ResetSensorTracking(&position_provider);
    if (param->normalize_position_via_navmesh) {
        position_provider.normalize_param = param;
    }
    const char* controller_type = input_backend->controller_type.c_str();
    const bool uses_touch_backend = UsesTouchBackend(*input_backend);
    LogInfo << "MapNavigator controller initialized." << VAR(controller_type) << VAR(uses_touch_backend);
    if (!IsInputBackendSupported(*input_backend)) {
        const char* unsupported_reason = input_backend->unsupported_reason.c_str();
        LogError << "MapNavigator controller backend is unsupported." << VAR(controller_type) << VAR(unsupported_reason);
        return false;
    }

    if (param->path.empty()) {
        return true;
    }

    const size_t target_count = param->path.size();
    LogInfo << "Starting navigation to targets." << VAR(target_count);
    PreloadNavmeshWaypoints(*param);
    LogInfo << "Waiting for first valid GPS signal...";

    NaviPosition pos;
    const std::string initial_expected_zone = InitialExpectedZone(*param);
    if (!WaitForFix(&position_provider, &pos, initial_expected_zone, kLocatorWaitMaxRetries, kLocatorWaitIntervalMs, ctx)) {
        return false;
    }
    if (TaskStopping(ctx)) {
        return false;
    }

    const double position_x = pos.x;
    const double position_y = pos.y;
    LogInfo << "Initial Pos fixed:" << VAR(position_x) << VAR(position_y);

    std::vector<Waypoint> expanded_path;
    const std::optional<size_t> resume_index = expand_route(&expanded_path, *param, pos, ctx);
    if (!resume_index) {
        return false;
    }
    if (expanded_path.empty()) {
        return true;
    }

    NaviParam expanded_param = *param;
    expanded_param.path = std::move(expanded_path);
    expanded_param.authored_path = std::move(param->path);
    offset_authored_groups(&expanded_param.path, *resume_index);

    Session session = MakeSession(expanded_param.path, pos);
    UpdatePhase(&session, NaviPhase::Bootstrap, "initial_fix");

    Motion motion_controller = MakeMotion(input_backend);
    motion_controller.enable_local_driver = param->enable_local_driver;
    motion_controller.enable_sprint = param->enable_sprint;
    Navigator navigator = MakeNavigator(expanded_param, input_backend, &position_provider, &session, &motion_controller, &pos, ctx);
    const walkmode::RestoreJogOnExit restore_jog_on_exit { navigator.walk_mode };
    LogInfo << "Navigation route runner selected. backend=orchestrated";

    const bool succeeded = RunNavigator(&navigator);
    // 帧扫描器指针指向 navigator 的成员, 析构前必须先拆掉
    StopScanners(&navigator);
    return succeeded;
}

} // namespace

bool RunNavigation(MaaContext* context, const NaviParam& requested_param)
{
    // 须最先取闸, 被拒的节点可能被无限重试
    const NavigationEntryGuard entry_guard(MaaContextGetTasker(context));
    if (!entry_guard.taken()) {
        LogError << "Refusing to nest navigation: this tasker is already navigating.";
        return false;
    }

    NaviParam param = requested_param;

    InputBackend input_backend = OpenInputBackend(MaaTaskerGetController(MaaContextGetTasker(context)));

    // 异常退出也须恢复按键和鼠标锁定、松开摇杆
    struct CloseInputBackendOnExit
    {
        InputBackend* input_backend;

        ~CloseInputBackendOnExit() { CloseInputBackend(input_backend); }
    } close_on_exit { &input_backend };

    return navigate_with_backend(&param, &input_backend, context);
}

} // namespace mapnavigator
