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
    HoldPosition(ctx.motion_controller);
    // 走 Execute* 自行停车的 JUMP/FIGHT/普通 INTERACT 绕过这里, 须在调用处各自置位
    ArmCameraAlign(ctx.runtime_state);
}

void SelectPhaseForCurrentWaypoint(const Context& ctx, const char* reason)
{
    if (!HasCurrentWaypoint(*ctx.session)) {
        NoteRouteTailConsumed(ctx.session, *ctx.position, "route_tail_consumed");
        return;
    }
    UpdatePhase(ctx.session, NaviPhase::Navigate, reason);
}

Result CompleteArrival(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason)
{
    NoteCanonicalFinalGoalConsumed(ctx.session, node_idx, *ctx.position, reason);
    AdvanceToNextWaypoint(ctx.session, waypoint.action, reason);
    ResetOnWaypointAdvance(ctx.runtime_state);
    SelectPhaseForCurrentWaypoint(ctx, reason);
    return Handled();
}

void ConsumeMatchedZoneNodes(const Context& ctx)
{
    while (HasCurrentWaypoint(*ctx.session) && IsZoneDeclaration(CurrentWaypoint(*ctx.session))) {
        const std::string& zone_id = CurrentWaypoint(*ctx.session).zone_id;
        if (!zone_id.empty() && zone_id != ctx.session->current_zone_id) {
            break;
        }
        AdvanceToNextWaypoint(ctx.session, ActionType::ZONE, "zone_declaration_consumed");
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
    UpdateCurrentZone(ctx.session, ctx.position->zone_id);
    ResetProgress(ctx.session);
    ResetNavigationAssistState(ctx.runtime_state);
    SelectPhaseForCurrentWaypoint(ctx, reason);
    return Handled();
}

} // namespace semantic_nodes

} // namespace mapnavigator
