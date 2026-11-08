#include "semantic_helpers.h"

#include <string>

#include "../control/motion_controller.h"
#include "../navi_time.h"
#include "../state_machine/navigation_state_machine.h"

namespace mapnavigator
{

namespace semantic_nodes
{

void HoldAndArmCameraAlign(Navigator* nav)
{
    HoldPosition(nav->motion_controller);
    // JUMP/FIGHT/普通 INTERACT 不走这里, 须在调用处各自置位
    ArmCameraAlign(&nav->runtime_state);
}

void SelectPhaseOrFinishRoute(Navigator* nav, const char* reason)
{
    if (!HasCurrentWaypoint(*nav->session)) {
        NoteRouteTailConsumed(nav->session, *nav->position, "route_tail_consumed");
        return;
    }
    UpdatePhase(nav->session, NaviPhase::Navigate, reason);
}

void CompleteArrival(Navigator* nav, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason)
{
    NoteCanonicalFinalGoalConsumed(nav->session, node_idx, *nav->position, reason);
    AdvanceToNextWaypoint(nav->session, waypoint.action, reason);
    ResetOnWaypointAdvance(&nav->runtime_state);
    SelectPhaseOrFinishRoute(nav, reason);
}

void ConsumeMatchedZoneNodes(Navigator* nav)
{
    while (HasCurrentWaypoint(*nav->session) && IsZoneDeclaration(CurrentWaypoint(*nav->session))) {
        const std::string& zone_id = CurrentWaypoint(*nav->session).zone_id;
        if (!zone_id.empty() && zone_id != nav->session->current_zone_id) {
            break;
        }
        AdvanceToNextWaypoint(nav->session, ActionType::ZONE, "zone_declaration_consumed");
        ResetOnWaypointAdvance(&nav->runtime_state);
    }
}

Result WaitNextFix(int interval_ms)
{
    SleepFor(interval_ms);
    return Pending();
}

void ResumeAfterRelocation(Navigator* nav, const char* reason)
{
    UpdateCurrentZone(nav->session, nav->position->zone_id);
    ResetProgress(nav->session);
    ResetNavigationAssistState(&nav->runtime_state);
    SelectPhaseOrFinishRoute(nav, reason);
}

} // namespace semantic_nodes

} // namespace mapnavigator
