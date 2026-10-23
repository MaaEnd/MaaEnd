#pragma once

#include "action_executor.h"
#include "navigation_runtime_state.h"
#include "navigation_session.h"
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
    bool changed_zone = false;
    const char* failure_reason = "";
    const char* failure_log_message = "";
    // 到了绳底认不出交互提示: 这根升降索的底端 uid, 由导航换掉它重新展开
    uint32_t rope_unavailable = 0;
};

// Aligns the camera to the character's facing before a start-off. No-op when the camera orientation is unavailable.
void AlignCameraToHeadingOnce(const Context& ctx);

Result TickSemanticFlow(const Context& ctx, NaviPhase phase);
Result ConsumeInlineSemantics(const Context& ctx);
Result HandleArrival(const Context& ctx, const Waypoint& waypoint, double actual_distance);
// 绳底交互已按下、当前点已推进到绳顶落脚点: 停住等这段升降索坐完
Result StartRopeRide(const Context& ctx, const RopeRideInfo& ride);

} // namespace semantic_nodes

} // namespace mapnavigator
