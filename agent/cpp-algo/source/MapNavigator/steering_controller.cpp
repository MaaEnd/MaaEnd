#include <algorithm>
#include <cmath>

#include <MaaUtils/Logger.h>

#include "steering_controller.h"

namespace mapnavigator
{

namespace
{

constexpr double kHeadingDeadband = 2.6;
constexpr double kMovingMaxCmd = 28.0;
constexpr double kTurningMaxCmd = 70.0;
constexpr double kKp = 0.3;

} // namespace

SteeringCommand SteeringController::Update(double heading_error, bool moving_forward)
{
    SteeringCommand command;
    if (std::abs(heading_error) < kHeadingDeadband) {
        return command;
    }
    const double max_cmd = moving_forward ? kMovingMaxCmd : kTurningMaxCmd;
    const double cmd = std::clamp(heading_error * kKp, -max_cmd, max_cmd);
    command.yaw_delta_deg = cmd;
    command.issued = std::abs(cmd) >= 2.0;
    LogDebug << "SteeringController update." << VAR(heading_error) << VAR(moving_forward) << VAR(cmd);
    return command;
}

} // namespace mapnavigator
