#include <algorithm>
#include <cmath>
#include <limits>

#include <MaaUtils/Logger.h>

#include "steering_controller.h"

namespace mapnavigator
{

namespace
{

constexpr double kHeadingDeadband = 2.6;
constexpr double kCrossTrackGain = 3.2;
constexpr double kCrossTrackMaxBias = 28.0;
constexpr double kMovingMaxCmd = 28.0;
constexpr double kTurningMaxCmd = 70.0;
constexpr double kKp = 0.3;

} // namespace

SteeringCommand SteeringController::Update(
    double heading_error,
    double signed_cross_track,
    double projection_anchor,
    bool moving_forward,
    ControllerState* state)
{
    SteeringCommand command;
    if (state == nullptr) {
        return command;
    }

    double lateral_bias = std::clamp(signed_cross_track * kCrossTrackGain, -kCrossTrackMaxBias, kCrossTrackMaxBias);
    if (projection_anchor > 0.35) {
        lateral_bias *= 0.45;
    }

    const double desired_error = heading_error + lateral_bias;

    state->filtered_heading_error = desired_error;

    if (std::abs(desired_error) < kHeadingDeadband) {
        return command;
    }

    const double max_cmd = moving_forward ? kMovingMaxCmd : kTurningMaxCmd;
    const double cmd = std::clamp(desired_error * kKp, -max_cmd, max_cmd);
    command.yaw_delta_deg = cmd;
    command.issued = std::abs(cmd) >= 2.0;
    LogDebug << "SteeringController update." << VAR(heading_error) << VAR(signed_cross_track)
             << VAR(projection_anchor) << VAR(moving_forward) << VAR(lateral_bias) << VAR(desired_error) << VAR(cmd);
    return command;
}

} // namespace mapnavigator
