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
    ctx.motion_controller->SetForwardState(false);
    // 走 ActionExecutor 自行停车的 JUMP/FIGHT/普通 INTERACT 绕过这里, 须在调用处各自置位
    ctx.runtime_state->ArmCameraAlign();
}

void SelectPhaseForCurrentWaypoint(const Context& ctx, const char* reason)
{
    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        return;
    }
    ctx.session->UpdatePhase(NaviPhase::Navigate, reason);
}

Result CompleteArrival(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason)
{
    ctx.session->NoteCanonicalFinalGoalConsumed(node_idx, *ctx.position, reason);
    ctx.session->AdvanceToNextWaypoint(waypoint.action, reason);
    ctx.runtime_state->OnWaypointAdvance();
    SelectPhaseForCurrentWaypoint(ctx, reason);
    return Handled();
}

void ConsumeMatchedZoneNodes(const Context& ctx)
{
    while (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().IsZoneDeclaration()) {
        const std::string& zone_id = ctx.session->CurrentWaypoint().zone_id;
        if (!zone_id.empty() && zone_id != ctx.session->current_zone_id()) {
            break;
        }
        ctx.session->AdvanceToNextWaypoint(ActionType::ZONE, "zone_declaration_consumed");
        ctx.runtime_state->OnWaypointAdvance();
    }
}

Result WaitNextFix(int interval_ms)
{
    SleepFor(interval_ms);
    return Pending();
}

Result ResumeAfterRelocation(const Context& ctx, const char* reason)
{
    ctx.session->UpdateCurrentZone(ctx.position->zone_id);
    ctx.session->ResetProgress();
    ctx.runtime_state->ResetNavigationAssistState();
    SelectPhaseForCurrentWaypoint(ctx, reason);
    return Handled();
}

} // namespace semantic_nodes

} // namespace mapnavigator
