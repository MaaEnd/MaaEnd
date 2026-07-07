#include <algorithm>
#include <cmath>

#include <MaaUtils/Logger.h>

#include "steering_controller.h"

namespace mapnavigator
{

namespace
{

constexpr double kHeadingDeadband = 2.6;
constexpr double kMovingMaxCmd = 90.0;
constexpr double kTurningMaxCmd = 70.0;
constexpr double kKp = 0.3;
constexpr double kHeadingRateDampingGain = 0.5;
constexpr double kHeadingRateDeadbandDeg = 3.0;
constexpr double kHeadingRatePlausibleMaxDeg = 60.0;
constexpr double kHeadingRateDampingCapDeg = 14.0;

} // namespace

SteeringCommand SteeringController::Update(double heading_error, double heading_rate_deg, bool moving_forward)
{
    SteeringCommand command;
    const double rate_magnitude = std::abs(heading_rate_deg);

    if (rate_magnitude > kHeadingRatePlausibleMaxDeg) {
        LogDebug << "SteeringController glitch-suppressed." << VAR(heading_error) << VAR(heading_rate_deg);
        return command;
    }

    // Deadband gates the P term only: it suppresses chasing sub-noise heading error on a straight. A real corner
    // still drives a large error, so large turns are never blocked here.
    const double p_term = std::abs(heading_error) < kHeadingDeadband ? 0.0 : heading_error * kKp;
    // Damping is applied regardless of the P deadband — arresting an overshoot near alignment is the whole point.
    // The implausible-rate ceiling is already handled by the early return above, so only the low deadband remains.
    double damping = 0.0;
    if (rate_magnitude >= kHeadingRateDeadbandDeg) {
        damping =
            std::clamp(-kHeadingRateDampingGain * heading_rate_deg, -kHeadingRateDampingCapDeg, kHeadingRateDampingCapDeg);
    }
    const double max_cmd = moving_forward ? kMovingMaxCmd : kTurningMaxCmd;
    const double cmd = std::clamp(p_term + damping, -max_cmd, max_cmd);
    command.yaw_delta_deg = cmd;
    command.issued = std::abs(cmd) >= 2.0;
    LogDebug << "SteeringController update." << VAR(heading_error) << VAR(heading_rate_deg) << VAR(damping)
             << VAR(moving_forward) << VAR(cmd);
    return command;
}

} // namespace mapnavigator
