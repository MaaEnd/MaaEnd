#include <optional>
#include <string>
#include <utility>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../MapLocator/MapLocateAction.h"
#include "Backend/backend.h"
#include "control/motion_controller.h"
#include "control/walk_mode.h"
#include "navi_config.h"
#include "navi_controller.h"
#include "navigation_entry.h"
#include "planning/navigation_session.h"
#include "planning/navmesh_path_expander.h"
#include "planning/navmesh_queries.h"
#include "process_state.h"
#include "sensing/position_provider.h"
#include "sensing/task_stop.h"
#include "state_machine/navigation_state_machine.h"

namespace mapnavigator
{

namespace
{

// 返回跳过的作者点数
std::optional<size_t> expand_route(
    std::vector<Waypoint>* out_path,
    ZiplineOutcome* zipline_outcome,
    const NaviParam& param,
    const NaviPosition& pos,
    MaaContext* maa_context)
{
    if (ExpandNavmeshWaypoints(out_path, nullptr, zipline_outcome, param, pos, maa_context)) {
        return 0;
    }
    const std::optional<size_t> resume = ResolveRouteResumeIndex(param.path, pos);
    if (!resume) {
        return std::nullopt;
    }
    NaviParam resumed_param = param;
    resumed_param.path.assign(param.path.begin() + static_cast<std::ptrdiff_t>(*resume), param.path.end());
    if (!ExpandNavmeshWaypoints(out_path, nullptr, zipline_outcome, resumed_param, pos, maa_context)) {
        return std::nullopt;
    }
    const size_t resume_index = *resume;
    LogInfo << "Expanded the route again after skipping the traversed prefix." << VAR(resume_index);
    return resume_index;
}

bool navigate_with_backend(
    NavProcessState* process,
    ZiplineOutcome* zipline_outcome,
    NaviParam* param,
    InputBackend* input_backend,
    MaaContext* ctx)
{
    PositionSensor position_provider = MakePositionSensor(input_backend->ctrl, maplocator::getOrInitLocator(), param->heading_source);
    ResetSensorTracking(&position_provider);
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
    if (param->normalize_position_via_navmesh) {
        FlattenWaypointZones(&param->path, *param);
    }
    LogInfo << "Waiting for first valid GPS signal...";

    NaviPosition pos;
    const std::string initial_expected_zone = InitialExpectedZone(*param);
    const CaptureParts initial_parts {
        .sensor = &position_provider,
        .frame_sinks = nullptr,
        .latency = &process->latency,
        .normalize_param = param->normalize_position_via_navmesh ? param : nullptr,
    };
    if (!WaitForFix(initial_parts, &pos, initial_expected_zone, kLocatorWaitMaxRetries, kLocatorWaitIntervalMs, ctx)) {
        return false;
    }
    if (TaskStopping(ctx)) {
        return false;
    }

    const double position_x = pos.x;
    const double position_y = pos.y;
    LogInfo << "Initial Pos fixed:" << VAR(position_x) << VAR(position_y);

    std::vector<Waypoint> expanded_path;
    const std::optional<size_t> resume_index = expand_route(&expanded_path, zipline_outcome, *param, pos, ctx);
    if (!resume_index) {
        return false;
    }
    if (expanded_path.empty()) {
        return true;
    }

    NaviParam expanded_param = *param;
    expanded_param.path = std::move(expanded_path);
    expanded_param.authored_path = std::move(param->path);
    // 展开时记的组首是相对跳过前缀后的路线, 补回跳过的点数
    OffsetAuthoredGroups(&expanded_param.path, *resume_index);

    Session session = MakeSession(expanded_param.path, pos);
    UpdatePhase(&session, NaviPhase::Bootstrap, "initial_fix");

    Motion motion_controller = MakeMotion(input_backend);
    motion_controller.enable_local_driver = param->enable_local_driver;
    motion_controller.enable_sprint = param->enable_sprint;
    Navigator navigator =
        MakeNavigator(expanded_param, input_backend, &position_provider, &session, &motion_controller, &pos, process, zipline_outcome, ctx);
    walkmode::RestoreJogIfBeliefSurvived(&process->walk, input_backend);
    const walkmode::RestoreJogOnExit restore_jog_on_exit { &process->walk, input_backend };
    LogInfo << "Navigation route runner selected. backend=orchestrated";

    const bool succeeded = RunNavigator(&navigator);
    StopScanners(&navigator);
    return succeeded;
}

} // namespace

bool RunNavigation(NavProcessState* process, ZiplineOutcome* zipline_outcome, MaaContext* context, const NaviParam& requested_param)
{
    MaaTasker* tasker = MaaContextGetTasker(context);
    // 须最先取闸, 被拒的节点可能被无限重试
    if (!TryEnterNavigation(&process->navigating, tasker)) {
        LogError << "Refusing to nest navigation: this tasker is already navigating.";
        return false;
    }
    const LeaveNavigationOnExit leave_on_exit { &process->navigating, tasker };

    NaviParam param = requested_param;

    InputBackend input_backend = OpenInputBackend(MaaTaskerGetController(MaaContextGetTasker(context)));

    // 异常退出也须恢复按键和鼠标锁定、松开摇杆
    struct CloseInputBackendOnExit
    {
        InputBackend* input_backend;

        ~CloseInputBackendOnExit() { CloseInputBackend(input_backend); }
    } close_on_exit { &input_backend };

    return navigate_with_backend(process, zipline_outcome, &param, &input_backend, context);
}

} // namespace mapnavigator
