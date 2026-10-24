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
    // turn_latch_sign 由调用方跨拍保存, pending_turn_deg 是已发但朝向还没体现的转向
    static SteeringCommand
        Update(double heading_error, double heading_rate_deg, bool moving_forward, int& turn_latch_sign, double pending_turn_deg);
};

} // namespace mapnavigator
