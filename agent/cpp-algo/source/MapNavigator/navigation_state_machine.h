#pragma once

#include <functional>

#include "navi_controller.h"
#include "navigation_runtime_state.h"
#include "navigation_session.h"

namespace mapnavigator
{

class IActionExecutor;
class ActionWrapper;
class MotionController;
class PositionProvider;

class NavigationStateMachine
{
public:
    NavigationStateMachine(
        const NaviParam& param,
        ActionWrapper* action_wrapper,
        PositionProvider* position_provider,
        NavigationSession* session,
        MotionController* motion_controller,
        IActionExecutor* action_executor,
        NaviPosition* position,
        std::function<bool()> should_stop);

    bool Run();

private:
    bool Bootstrap();
    bool TickNavigate();
    bool TickPhase(NaviPhase phase);
    bool CaptureCurrentPosition(bool force_global_search = false);
    void SelectPhaseForCurrentWaypoint(const char* reason);
    void StopMotion();
    bool FailNavigation(const char* reason, const char* log_message, double current_distance, double yaw_error, int64_t stalled_ms);

    const NaviParam& param_;
    ActionWrapper* action_wrapper_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    IActionExecutor* action_executor_;
    NaviPosition* position_;
    std::function<bool()> should_stop_;
    NavigationRuntimeState runtime_state_ {};
};

} // namespace mapnavigator
