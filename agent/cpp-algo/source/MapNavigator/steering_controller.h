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
    static SteeringCommand Update(double heading_error, double heading_rate_deg, bool moving_forward);
};

} // namespace mapnavigator
