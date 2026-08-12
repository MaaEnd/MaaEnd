#include <algorithm>
#include <cmath>

#include <MaaUtils/Logger.h>

#include "steering_controller.h"

namespace mapnavigator
{

namespace
{

constexpr double kHeadingDeadband = 6.6;
constexpr double kMovingMaxCmd = 90.0;
constexpr double kTurningMaxCmd = 70.0;
constexpr double kKp = 1.0;
constexpr double kHeadingRatePlausibleMaxDeg = 60.0;
// An about-face has no shorter side, so at the antipode a degree of heading jitter flips the command a full 360
// and the agent alternates left and right forever without crossing. Latch the direction on entry and hold it
// until the turn is well under way; either side reaches the target, so the latched one is never the wrong one.
constexpr double kTurnLatchEnterDeg = 150.0;
constexpr double kTurnLatchExitDeg = 120.0;

} // namespace

SteeringCommand SteeringController::Update(
    double heading_error,
    double heading_rate_deg,
    bool moving_forward,
    int& turn_latch_sign,
    double pending_turn_deg)
{
    SteeringCommand command;
    const double rate_magnitude = std::abs(heading_rate_deg);

    if (rate_magnitude > kHeadingRatePlausibleMaxDeg) {
        LogDebug << "SteeringController glitch-suppressed." << VAR(heading_error) << VAR(heading_rate_deg);
        return command;
    }

    if (std::abs(heading_error) < kTurnLatchExitDeg) {
        turn_latch_sign = 0;
    }
    else if (turn_latch_sign == 0 && std::abs(heading_error) >= kTurnLatchEnterDeg) {
        turn_latch_sign = heading_error >= 0.0 ? 1 : -1;
    }
    if (turn_latch_sign != 0) {
        heading_error = std::abs(heading_error) * turn_latch_sign;
    }

    // Discount the turn already sent that the heading has not shown yet. The game needs several ticks to yaw a
    // capped command while this runs every tick, so commanding the raw error each time sends it many times over,
    // overshoots, then hunts back — the left-right weave. Clamping the discount between zero and the error keeps
    // the remainder on the same side as the error, so this can only ever soften a command, never reverse one.
    const double pending = std::clamp(pending_turn_deg, std::min(0.0, heading_error), std::max(0.0, heading_error));
    const double effective_error = heading_error - pending;

    // Deadband gates the P term only: it suppresses chasing sub-noise heading error on a straight. A real corner
    // still drives a large error, so large turns are never blocked here.
    const double p_term = std::abs(effective_error) < kHeadingDeadband ? 0.0 : effective_error * kKp;
    const double max_cmd = moving_forward ? kMovingMaxCmd : kTurningMaxCmd;
    const double cmd = std::clamp(p_term, -max_cmd, max_cmd);
    command.yaw_delta_deg = cmd;
    command.issued = std::abs(cmd) >= 2.0;
    LogDebug << "SteeringController update." << VAR(heading_error) << VAR(pending) << VAR(heading_rate_deg) << VAR(moving_forward)
             << VAR(turn_latch_sign) << VAR(cmd);
    return command;
}

} // namespace mapnavigator
