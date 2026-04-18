#pragma once

#include <cstddef>

#include "navigation_runtime_state.h"
#include "navigation_session.h"

namespace mapnavigator
{

struct RouteTrackingState
{
    bool valid = false;
    bool on_route = false;
    bool startup_motion_confirmed = false;
    double projection_anchor = 0.0;
    double cross_track = std::numeric_limits<double>::infinity();
    double along_track_remaining = std::numeric_limits<double>::infinity();
    double route_heading = 0.0;
    double waypoint_heading = 0.0;
    double waypoint_distance = std::numeric_limits<double>::infinity();
    double progress_distance = std::numeric_limits<double>::infinity();
    double arrival_band = 0.0;
};

class RouteTracker
{
public:
    static RouteTrackingState Update(NavigationSession* session, RouteTrackerState* state, const NaviPosition& position);
};

} // namespace mapnavigator
