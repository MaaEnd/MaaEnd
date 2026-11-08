#include "trigger_action.h"

#include <chrono>
#include <cmath>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
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

Result hold_and_fail_trigger(const Context& ctx, const char* reason, const char* log_message)
{
    HoldPosition(ctx.motion_controller);
    return ConsumedFailure(reason, log_message);
}

} // namespace

bool IsTriggerPending(const Session& session)
{
    return HasCurrentWaypoint(session) && IsTriggerPoint(session.current_path.back());
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
    MaaController* controller = ctx.input_backend->ctrl;
    ScopedImageBuffer image;
    if (controller == nullptr) {
        return {};
    }
    if (!MaaControllerCachedImage(controller, image.Get())) {
        return {};
    }
    if (MaaImageBufferIsEmpty(image.Get())) {
        return {};
    }

    const size_t trigger_idx = ctx.session->current_path.size() - 1;
    const Waypoint& waypoint = CurrentPathAt(*ctx.session, trigger_idx);
    NodeSighting sighting {};
    if (!RunRecognitionNode(ctx.maa_context, waypoint.trigger_node, kTriggerPipelineOverride, image.Get(), &sighting)) {
        return hold_and_fail_trigger(ctx, "trigger_recognition_failed", "TRIGGER node failed to recognize.");
    }
    if (!sighting.hit) {
        return {};
    }

    const double remaining = std::hypot(waypoint.x - ctx.position->x, waypoint.y - ctx.position->y);
    LogInfo << "TRIGGER: node hit on the way, navigation complete." << VAR(waypoint.trigger_node)
            << VAR_AS(ctx.session->current_node_idx(), ctx.session->current_node_idx) << VAR(trigger_idx) << VAR(ctx.position->valid)
            << VAR(remaining);
    HoldPosition(ctx.motion_controller);
    NoteCanonicalFinalGoalConsumed(ctx.session, CanonicalIndexAtCurrentPath(*ctx.session, trigger_idx), *ctx.position, "trigger_hit");
    SkipPastWaypoint(ctx.session, trigger_idx, "trigger_hit");
    ResetOnWaypointAdvance(ctx.runtime_state);
    NoteRouteTailConsumed(ctx.session, *ctx.position, "route_tail_consumed");
    return Handled();
}

Result ArriveTrigger(const Context& ctx, const Waypoint& waypoint, double actual_distance)
{
    HoldAndArmCameraAlign(ctx);
    LogInfo << "Action: TRIGGER reached the point, navigation complete." << VAR(actual_distance) << VAR(waypoint.trigger_node);
    CompleteArrival(ctx, waypoint, CurrentAbsoluteNodeIndex(*ctx.session), "trigger_arrived");
    return Handled();
}

} // namespace semantic_nodes

} // namespace mapnavigator
