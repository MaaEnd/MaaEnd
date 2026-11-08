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
#include "../state_machine/navigation_state_machine.h"
#include "semantic_helpers.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

constexpr const char* kTriggerPipelineOverride = "{}";

Result hold_and_fail_trigger(Navigator* nav, const char* reason, const char* log_message)
{
    HoldPosition(nav->motion_controller);
    return ConsumedFailure(reason, log_message);
}

} // namespace

bool IsTriggerPending(const Session& session)
{
    return HasCurrentWaypoint(session) && IsTriggerPoint(session.current_path.back());
}

Result ProbeTriggerWhileNavigating(Navigator* nav)
{
    if (nav->maa_context == nullptr || !IsTriggerPending(*nav->session)) {
        return {};
    }

    TriggerState& trigger = nav->runtime_state.trigger;
    const auto now = std::chrono::steady_clock::now();
    if (!IsUnset(trigger.last_probe_at) && ElapsedMs(trigger.last_probe_at, now) < kTriggerProbeIntervalMs) {
        return {};
    }
    trigger.last_probe_at = now;

    // 定位这一拍刚截过图, 缓存即当前画面
    MaaController* controller = nav->input_backend->ctrl;
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

    const size_t trigger_idx = nav->session->current_path.size() - 1;
    const Waypoint& waypoint = CurrentPathAt(*nav->session, trigger_idx);
    NodeSighting sighting {};
    if (!RunRecognitionNode(nav->maa_context, waypoint.trigger_node, kTriggerPipelineOverride, image.Get(), &sighting)) {
        return hold_and_fail_trigger(nav, "trigger_recognition_failed", "TRIGGER node failed to recognize.");
    }
    if (!sighting.hit) {
        return {};
    }

    const double remaining = std::hypot(waypoint.x - nav->position->x, waypoint.y - nav->position->y);
    LogInfo << "TRIGGER: node hit on the way, navigation complete." << VAR(waypoint.trigger_node)
            << VAR_AS(ctx.session->current_node_idx(), nav->session->current_node_idx) << VAR(trigger_idx) << VAR(nav->position->valid)
            << VAR(remaining);
    HoldPosition(nav->motion_controller);
    NoteCanonicalFinalGoalConsumed(nav->session, CanonicalIndexAtCurrentPath(*nav->session, trigger_idx), *nav->position, "trigger_hit");
    NoteInteraction(nav->session);
    LeaveWaypointsThrough(nav->session, trigger_idx, "trigger_hit");
    ResetOnWaypointAdvance(&nav->runtime_state);
    ConcludeRouteTail(nav->session, *nav->position, "route_tail_consumed");
    return Handled();
}

Result ArriveTrigger(Navigator* nav, const Waypoint& waypoint, double actual_distance)
{
    HoldAndArmCameraAlign(nav);
    LogInfo << "Action: TRIGGER reached the point, navigation complete." << VAR(actual_distance) << VAR(waypoint.trigger_node);
    CompleteArrival(nav, waypoint, CurrentAbsoluteNodeIndex(*nav->session), "trigger_arrived");
    return Handled();
}

} // namespace semantic_nodes

} // namespace mapnavigator
