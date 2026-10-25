#include "trigger_action.h"

#include <chrono>
#include <cmath>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../sensing/maa_task.h"
#include "semantic_helpers.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

constexpr const char* kTriggerPipelineOverride = "{}";

Result fail_trigger(const Context& ctx, const char* reason, const char* log_message)
{
    ctx.motion_controller->setForwardState(false);
    return {
        .consumed = true,
        .stay_in_current_tick = true,
        .request_failure = true,
        .failure_reason = reason,
        .failure_log_message = log_message,
    };
}

} // namespace

bool IsTriggerPending(const NavigationSession& session)
{
    return session.hasCurrentWaypoint() && session.currentPath().back().isTriggerPoint();
}

Result ProbeTriggerWhileNavigating(const Context& ctx)
{
    if (ctx.maa_context == nullptr || !IsTriggerPending(*ctx.session)) {
        return {};
    }

    TriggerState& trigger = ctx.runtime_state->trigger;
    const auto now = std::chrono::steady_clock::now();
    if (!IsUnset(trigger.last_probe_at) && ElapsedMs(trigger.last_probe_at, now) < kTriggerProbeIntervalMs) {
        return {};
    }
    trigger.last_probe_at = now;

    // 定位这一拍刚截过图, 缓存即当前画面
    MaaController* controller = ctx.action_wrapper->getCtrl();
    ScopedImageBuffer image;
    if (controller == nullptr || !MaaControllerCachedImage(controller, image.Get()) || MaaImageBufferIsEmpty(image.Get())) {
        return {};
    }

    const size_t trigger_idx = ctx.session->currentPath().size() - 1;
    const Waypoint& waypoint = ctx.session->currentPathAt(trigger_idx);
    NodeSighting sighting {};
    if (!RunRecognitionNode(ctx.maa_context, waypoint.trigger_node, kTriggerPipelineOverride, image.Get(), &sighting)) {
        return fail_trigger(ctx, "trigger_recognition_failed", "TRIGGER node failed to recognize.");
    }
    if (!sighting.hit) {
        return {};
    }

    const double remaining = std::hypot(waypoint.x - ctx.position->x, waypoint.y - ctx.position->y);
    LogInfo << "TRIGGER: node hit on the way, navigation complete." << VAR(waypoint.trigger_node)
            << VAR_AS(ctx.session->current_node_idx(), ctx.session->currentNodeIdx()) << VAR(trigger_idx) << VAR(ctx.position->valid)
            << VAR(remaining);
    ctx.motion_controller->setForwardState(false);
    ctx.session->noteCanonicalFinalGoalConsumed(ctx.session->canonicalIndexAtCurrentPath(trigger_idx), *ctx.position, "trigger_hit");
    ctx.session->skipPastWaypoint(trigger_idx, "trigger_hit");
    ctx.runtime_state->onWaypointAdvance();
    ctx.session->noteRouteTailConsumed(*ctx.position, "route_tail_consumed");
    return Handled();
}

Result ArriveTrigger(const Context& ctx, const Waypoint& waypoint, double actual_distance)
{
    StopMotionAndCommitment(ctx);
    LogInfo << "Action: TRIGGER reached the point, navigation complete." << VAR(actual_distance) << VAR(waypoint.trigger_node);
    return CompleteArrival(ctx, waypoint, ctx.session->currentAbsoluteNodeIndex(), "trigger_arrived");
}

} // namespace semantic_nodes

} // namespace mapnavigator
