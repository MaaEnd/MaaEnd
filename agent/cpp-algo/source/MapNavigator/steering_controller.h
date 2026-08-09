#pragma once

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
    // turn_latch_sign carries the committed direction of a near-about-face turn across ticks; see the .cpp.
    static SteeringCommand Update(double heading_error, double heading_rate_deg, bool moving_forward, int& turn_latch_sign);
};

} // namespace mapnavigator
