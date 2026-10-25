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
// 接近掉头时朝向抖动会让指令左右翻转, 进入时锁定方向, 转到退出角以内才解锁
constexpr double kTurnLatchEnterDeg = 150.0;
constexpr double kTurnLatchExitDeg = 120.0;

} // namespace

SteeringCommand SteeringController::update(
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

    // 扣掉已发但朝向还没体现的转向, 夹在零与误差之间, 只会减弱指令
    const double pending = std::clamp(pending_turn_deg, std::min(0.0, heading_error), std::max(0.0, heading_error));
    const double effective_error = heading_error - pending;

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
