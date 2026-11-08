#include "semantic_helpers.h"

#include <string>

#include "../control/motion_controller.h"
#include "../navi_time.h"

namespace mapnavigator
{

namespace semantic_nodes
{

void StopMotionAndCommitment(const Context& ctx)
{
    ctx.motion_controller->setForwardState(false);
    // 走 ActionExecutor 自行停车的 JUMP/FIGHT/普通 INTERACT 绕过这里, 须在调用处各自置位
    ArmCameraAlign(ctx.runtime_state);
}

void SelectPhaseForCurrentWaypoint(const Context& ctx, const char* reason)
{
    if (!ctx.session->hasCurrentWaypoint()) {
        ctx.session->noteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        return;
    }
    ctx.session->updatePhase(NaviPhase::Navigate, reason);
}

Result CompleteArrival(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason)
{
    ctx.session->noteCanonicalFinalGoalConsumed(node_idx, *ctx.position, reason);
    ctx.session->advanceToNextWaypoint(waypoint.action, reason);
    ResetOnWaypointAdvance(ctx.runtime_state);
    SelectPhaseForCurrentWaypoint(ctx, reason);
    return Handled();
}

void ConsumeMatchedZoneNodes(const Context& ctx)
{
    while (ctx.session->hasCurrentWaypoint() && IsZoneDeclaration(ctx.session->currentWaypoint())) {
        const std::string& zone_id = ctx.session->currentWaypoint().zone_id;
        if (!zone_id.empty() && zone_id != ctx.session->currentZoneId()) {
            break;
        }
        ctx.session->advanceToNextWaypoint(ActionType::ZONE, "zone_declaration_consumed");
        ResetOnWaypointAdvance(ctx.runtime_state);
    }
}

Result WaitNextFix(int interval_ms)
{
    SleepFor(interval_ms);
    return Pending();
}

Result ResumeAfterRelocation(const Context& ctx, const char* reason)
{
    ctx.session->updateCurrentZone(ctx.position->zone_id);
    ctx.session->resetProgress();
    ResetNavigationAssistState(ctx.runtime_state);
    SelectPhaseForCurrentWaypoint(ctx, reason);
    return Handled();
}

} // namespace semantic_nodes

} // namespace mapnavigator
