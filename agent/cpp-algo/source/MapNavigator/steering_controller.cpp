#include <algorithm>
#include <cmath>
#include <limits>

#include <MaaUtils/Logger.h>

#include "steering_controller.h"

namespace mapnavigator
{

namespace
{

constexpr double kHeadingAlpha = 0.30;
constexpr double kHeadingDeadband = 2.6;
constexpr double kCrossTrackGain = 3.2;
constexpr double kCrossTrackMaxBias = 28.0;
constexpr double kMovingMaxCmd = 28.0;
constexpr double kTurningMaxCmd = 70.0;
constexpr double kKp = 0.68;

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
    if (std::abs(state->filtered_heading_error) <= std::numeric_limits<double>::epsilon()) {
        state->filtered_heading_error = desired_error;
    }
    else {
        state->filtered_heading_error = ((1.0 - kHeadingAlpha) * state->filtered_heading_error) + (kHeadingAlpha * desired_error);
    }

    const double filtered_error = state->filtered_heading_error;
    if (std::abs(filtered_error) < kHeadingDeadband) {
        return command;
    }

    const double max_cmd = moving_forward ? kMovingMaxCmd : kTurningMaxCmd;
    const double cmd = std::clamp(filtered_error * kKp, -max_cmd, max_cmd);
    command.yaw_delta_deg = cmd;
    command.issued = std::abs(cmd) >= 2.0;
    LogDebug << "SteeringController update." << VAR(heading_error) << VAR(signed_cross_track) << VAR(projection_anchor)
             << VAR(moving_forward) << VAR(lateral_bias) << VAR(desired_error) << VAR(filtered_error) << VAR(command.yaw_delta_deg);
    return command;
}

} // namespace mapnavigator
