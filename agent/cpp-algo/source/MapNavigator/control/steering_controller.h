#pragma once

namespace mapnavigator
{

struct SteeringCommand
{
    double yaw_delta_deg = 0.0;
    bool issued = false;
};

// turn_latch_sign 由调用方跨拍保存, pending_turn_deg 是已发但朝向还没体现的转向
SteeringCommand
    UpdateSteering(int* turn_latch_sign, double heading_error, double heading_rate_deg, bool moving_forward, double pending_turn_deg);

} // namespace mapnavigator
