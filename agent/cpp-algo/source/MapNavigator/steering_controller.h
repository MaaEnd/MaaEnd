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
    static SteeringCommand Update(double heading_error, bool moving_forward);
};

} // namespace mapnavigator
