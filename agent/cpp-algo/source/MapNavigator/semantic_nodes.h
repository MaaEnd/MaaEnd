#pragma once

#include "action_executor.h"
#include "navigation_runtime_state.h"
#include "navigation_session.h"

namespace mapnavigator
{

class ActionWrapper;
class MotionController;
class PositionProvider;

namespace semantic_nodes
{

struct Context
{
    ActionWrapper* action_wrapper = nullptr;
    PositionProvider* position_provider = nullptr;
    NavigationSession* session = nullptr;
    MotionController* motion_controller = nullptr;
    IActionExecutor* action_executor = nullptr;
    NaviPosition* position = nullptr;
    NavigationRuntimeState* runtime_state = nullptr;
};

struct Result
{
    bool consumed = false;
    bool stay_in_current_tick = false;
    bool request_failure = false;
    bool changed_zone = false;
    const char* failure_reason = "";
    const char* failure_log_message = "";
};

Result TickSemanticFlow(const Context& ctx, NaviPhase phase);
Result ConsumeInlineSemantics(const Context& ctx);
Result HandleArrivalSemantic(const Context& ctx, const Waypoint& waypoint, double actual_distance);

} // namespace semantic_nodes

} // namespace mapnavigator
