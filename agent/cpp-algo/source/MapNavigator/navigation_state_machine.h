#pragma once

#include <functional>

#include "navigation_runtime_state.h"
#include "Runners/advance_route_runner.h"
#include "Runners/heading_align_runner.h"
#include "Runners/serial_route_runner.h"
#include "Runners/transfer_wait_runner.h"
#include "Runners/zone_transition_runner.h"
#include "navi_controller.h"
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
    bool TickPhase(NaviPhase phase);

    const NaviParam& param_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    NaviPosition* position_;
    std::function<bool()> should_stop_;

    NavigationRuntimeState runtime_state_ {};
    ZoneTransitionRunner zone_transition_runner_;
    TransferWaitRunner transfer_wait_runner_;
    HeadingAlignRunner heading_align_runner_;
    AdvanceRouteRunner advance_route_runner_;
    SerialRouteRunner serial_route_runner_;
    bool use_serial_route_runner_ = false;
};

} // namespace mapnavigator
