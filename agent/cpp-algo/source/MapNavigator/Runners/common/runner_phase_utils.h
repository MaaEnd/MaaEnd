#pragma once

#include "../../navi_controller.h"

namespace mapnavigator
{

class MotionController;
struct NavigationSession;
struct NaviPosition;

void SelectPhaseForCurrentWaypoint(NavigationSession* session, const NaviPosition* position, const char* reason);
void StopMotionAndCommitment(MotionController* motion_controller);
void ResumeMotionTowardsCurrentWaypoint(
    NavigationSession* session,
    MotionController* motion_controller,
    double origin_x,
    double origin_y,
    const char* reason,
    int settle_wait_ms);
bool CanArriveSamePointAction(const Waypoint& waypoint, const NaviPosition& position);
bool CanChainSamePointAction(const Waypoint& from_waypoint, const Waypoint& to_waypoint, const NaviPosition& position);

} // namespace mapnavigator
