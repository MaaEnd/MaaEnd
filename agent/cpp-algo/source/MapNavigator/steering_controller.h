#pragma once

#include "navigation_runtime_state.h"

namespace mapnavigator
{

struct SteeringCommand
{
    double yaw_delta_deg = 0.0;
    bool issued = false;
};

class SteeringController
{
public:
    static SteeringCommand
        Update(double heading_error, double signed_cross_track, double projection_anchor, bool moving_forward, ControllerState* state);
};

} // namespace mapnavigator
