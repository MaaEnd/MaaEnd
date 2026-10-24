#include "trigger_action.h"

#include <chrono>
#include <cmath>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "semantic_helpers.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

constexpr const char* kTriggerPipelineOverride = "{}";

int64_t ElapsedMs(std::chrono::steady_clock::time_point from, std::chrono::steady_clock::time_point now)
{
    return std::chrono::duration_cast<std::chrono::milliseconds>(now - from).count();
}

Result HoldTick()
{
    return { .consumed = true, .stay_in_current_tick = true };
}

Result FailTrigger(const Context& ctx, const char* reason, const char* log_message)
{
    ctx.motion_controller->SetForwardState(false);
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
    return session.HasCurrentWaypoint() && session.current_path().back().IsTriggerPoint();
}

Result ProbeTriggerWhileNavigating(const Context& ctx)
{
    if (ctx.maa_context == nullptr || !IsTriggerPending(*ctx.session)) {
        return {};
    }

    TriggerState& trigger = ctx.runtime_state->trigger;
    const auto now = std::chrono::steady_clock::now();
    if (trigger.last_probe_at.time_since_epoch().count() != 0 && ElapsedMs(trigger.last_probe_at, now) < kTriggerProbeIntervalMs) {
        return {};
    }
    trigger.last_probe_at = now;

    // 定位这一拍刚截过图, 缓存即当前画面
    MaaController* controller = ctx.action_wrapper->GetCtrl();
    ScopedImageBuffer image;
    if (controller == nullptr || !MaaControllerCachedImage(controller, image.Get()) || MaaImageBufferIsEmpty(image.Get())) {
        return {};
    }

    const size_t trigger_idx = ctx.session->current_path().size() - 1;
    const Waypoint& waypoint = ctx.session->CurrentPathAt(trigger_idx);
    NodeSighting sighting {};
    if (!RunRecognitionNode(ctx.maa_context, waypoint.trigger_node, kTriggerPipelineOverride, image.Get(), &sighting)) {
        return FailTrigger(ctx, "trigger_recognition_failed", "TRIGGER node failed to recognize.");
    }
    if (!sighting.hit) {
        return {};
    }

    const double remaining = std::hypot(waypoint.x - ctx.position->x, waypoint.y - ctx.position->y);
    LogInfo << "TRIGGER: node hit on the way, navigation complete." << VAR(waypoint.trigger_node) << VAR(ctx.session->current_node_idx())
            << VAR(trigger_idx) << VAR(ctx.position->valid) << VAR(remaining);
    ctx.motion_controller->SetForwardState(false);
    ctx.session->NoteCanonicalFinalGoalConsumed(ctx.session->CanonicalIndexAtCurrentPath(trigger_idx), *ctx.position, "trigger_hit");
    ctx.session->SkipPastWaypoint(trigger_idx, "trigger_hit");
    ctx.runtime_state->OnWaypointAdvance();
    ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
    return HoldTick();
}

Result ArriveTrigger(const Context& ctx, const Waypoint& waypoint, double actual_distance)
{
    StopMotionAndCommitment(ctx);
    LogInfo << "Action: TRIGGER reached the point, navigation complete." << VAR(actual_distance) << VAR(waypoint.trigger_node);
    return CompleteArrival(ctx, waypoint, ctx.session->CurrentAbsoluteNodeIndex(), "trigger_arrived");
}

} // namespace semantic_nodes

} // namespace mapnavigator
