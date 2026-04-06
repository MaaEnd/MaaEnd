#include <cmath>

#include "../../motion_controller.h"
#include "../../navi_config.h"
#include "../../navi_math.h"
#include "../../navigation_session.h"
#include "runner_phase_utils.h"

namespace mapnavigator
{

void SelectPhaseForCurrentWaypoint(NavigationSession* session, const NaviPosition* position, const char* reason)
{
    if (!session->HasCurrentWaypoint()) {
        if (position != nullptr) {
            session->NoteRouteTailConsumed(*position, "route_tail_consumed");
        }
        else {
            session->UpdatePhase(NaviPhase::Finished, "route_tail_consumed");
        }
        return;
    }

    if (session->is_waiting_for_zone_switch() || session->CurrentWaypoint().IsZoneDeclaration()) {
        session->UpdatePhase(NaviPhase::WaitZoneTransition, reason);
        return;
    }

    if (session->CurrentWaypoint().IsHeadingOnly()) {
        session->UpdatePhase(NaviPhase::AlignHeading, reason);
        return;
    }

    session->UpdatePhase(NaviPhase::AdvanceOnRoute, reason);
}

void StopMotionAndCommitment(MotionController* motion_controller)
{
    motion_controller->Stop();
    motion_controller->ClearForwardCommit();
}

void ResumeMotionTowardsCurrentWaypoint(
    NavigationSession* session,
    MotionController* motion_controller,
    double origin_x,
    double origin_y,
    const char* reason,
    int settle_wait_ms)
{
    if (!session->HasCurrentWaypoint() || session->CurrentWaypoint().IsControlNode()) {
        return;
    }

    const Waypoint& waypoint = session->CurrentWaypoint();
    const double target_yaw = NaviMath::CalcTargetRotation(origin_x, origin_y, waypoint.x, waypoint.y);
    const double required_turn = NaviMath::NormalizeAngle(target_yaw - session->virtual_yaw());

    const TurnCommandResult turn_result = motion_controller->InjectMouseAndTrack(
        required_turn,
        false,
        session->CurrentExpectedZone(),
        settle_wait_ms,
        TurnActionKind::RouteResume);
    if (!turn_result.issued) {
        return;
    }
    motion_controller->ArmForwardCommit(required_turn, reason);
    motion_controller->EnsureForwardMotion(true);
}

bool CanArriveSamePointAction(const Waypoint& waypoint, const NaviPosition& position)
{
    if (!waypoint.HasPosition()) {
        return false;
    }

    return (waypoint.zone_id.empty() || waypoint.zone_id == position.zone_id)
           && std::hypot(waypoint.x - position.x, waypoint.y - position.y) <= kSamePointActionChainDistance;
}

bool CanChainSamePointAction(const Waypoint& from_waypoint, const Waypoint& to_waypoint, const NaviPosition& position)
{
    if (!from_waypoint.HasPosition() || !to_waypoint.HasPosition()) {
        return false;
    }

    return (to_waypoint.zone_id.empty() || to_waypoint.zone_id == position.zone_id)
           && std::hypot(to_waypoint.x - from_waypoint.x, to_waypoint.y - from_waypoint.y) <= kSamePointActionChainDistance;
}

} // namespace mapnavigator
