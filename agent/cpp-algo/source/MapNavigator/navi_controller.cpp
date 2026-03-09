#include <MaaUtils/Logger.h>

#include "../MapLocator/MapLocateAction.h"
#include "action_executor.h"
#include "action_wrapper.h"
#include "local_driver_lite.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_controller.h"
#include "navigation_session.h"
#include "navigation_state_machine.h"
#include "position_provider.h"
#include "route_rejoin.h"
#include "zone_transition_controller.h"

namespace mapnavigator
{

NaviController::NaviController(MaaContext* ctx)
    : ctx_(ctx)
{
}

bool NaviController::Navigate(const NaviParam& param)
{
    ActionWrapper action_wrapper(ctx_);
    PositionProvider position_provider(action_wrapper.GetCtrl(), maplocator::getOrInitLocator());
    position_provider.ResetTracking();
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
    LogInfo << "Waiting for first valid GPS signal...";

    NaviPosition pos;
    const std::string initial_expected_zone = param.path.front().zone_id.empty() ? param.map_name : param.path.front().zone_id;
    if (!position_provider.WaitForFix(&pos, initial_expected_zone, kLocatorWaitMaxRetries, kLocatorWaitIntervalMs, is_stopping)
        || is_stopping()) {
        return false;
    }

    const double position_x = pos.x;
    const double position_y = pos.y;
    LogInfo << "Initial Pos fixed:" << VAR(position_x) << VAR(position_y);

    NavigationSession session(param.path, pos);
    session.UpdatePhase(NaviPhase::Bootstrap, "initial_fix");

    RouteRejoinPlanner rejoin_planner(param.rejoin_abort_distance, param.rejoin_candidate_limit, param.rejoin_backtrack_window);
    LocalDriverLite local_driver;
    MotionController motion_controller(&action_wrapper, &position_provider, &session, &pos, param.enable_local_driver, is_stopping);
    ZoneTransitionController zone_transition_controller(&motion_controller, &position_provider, &session, &pos, &local_driver, is_stopping);
    ActionExecutor action_executor(&action_wrapper, &motion_controller, &local_driver, param.enable_local_driver);
    NavigationStateMachine state_machine(
        param,
        &action_wrapper,
        &position_provider,
        &session,
        &rejoin_planner,
        &motion_controller,
        &zone_transition_controller,
        &local_driver,
        &action_executor,
        &pos,
        is_stopping);

    return state_machine.Run();
}

} // namespace mapnavigator
