#pragma once

#include "../planning/navigation_session.h"
#include "../state_machine/navigation_runtime_state.h"
#include "action_executor.h"
struct MaaContext;

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
    ActionExecutor* action_executor = nullptr;
    NaviPosition* position = nullptr;
    NavigationRuntimeState* runtime_state = nullptr;
    MaaContext* maa_context = nullptr;
};

struct Result
{
    bool consumed = false;
    bool stay_in_current_tick = false;
    bool request_failure = false;
    const char* failure_reason = "";
    const char* failure_log_message = "";
    // 绳底认不出交互提示时填该升降索的底端 uid
    uint32_t rope_unavailable = 0;
};

inline Result Handled()
{
    return { .consumed = true, .stay_in_current_tick = true };
}

inline Result Pending()
{
    return { .stay_in_current_tick = true };
}

inline Result Failure(const char* reason, const char* log_message)
{
    return { .request_failure = true, .failure_reason = reason, .failure_log_message = log_message };
}

Result TickWaitPhase(const Context& ctx, NaviPhase phase);
Result ConsumeInlineSemantics(const Context& ctx);
Result HandleArrival(const Context& ctx, const Waypoint& waypoint, double actual_distance);
// 调用前绳底交互须已按下, 当前点须已推进到绳顶落脚点
Result StartRopeRide(const Context& ctx, const RopeRideInfo& ride);

} // namespace semantic_nodes

} // namespace mapnavigator
