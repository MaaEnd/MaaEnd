#pragma once

#include "../../navigation_runtime_state.h"

namespace mapnavigator
{

class IActionExecutor;
class MotionController;
class PositionProvider;
struct NavigationSession;
struct NaviPosition;

class WaypointArrivalHandler
{
public:
    WaypointArrivalHandler(
        PositionProvider* position_provider,
        NavigationSession* session,
        MotionController* motion_controller,
        IActionExecutor* action_executor,
        NaviPosition* position,
        NavigationRuntimeState* runtime_state);

    bool HandleArrival(
        double guidance_pos_x,
        double guidance_pos_y,
        double actual_distance,
        double portal_distance,
        bool advanced_by_pass_through,
        bool portal_commit_ready);

private:
    PositionProvider* position_provider_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    IActionExecutor* action_executor_;
    NaviPosition* position_;
    NavigationRuntimeState* runtime_state_;
};

} // namespace mapnavigator
