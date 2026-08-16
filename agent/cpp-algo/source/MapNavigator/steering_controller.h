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
    // pending_turn_deg is the turn already sent that the heading has not shown yet; the caller keeps the tally.
    static SteeringCommand
        Update(double heading_error, double heading_rate_deg, bool moving_forward, int& turn_latch_sign, double pending_turn_deg);
};

} // namespace mapnavigator
