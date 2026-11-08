#pragma once

#include <chrono>
#include <cstdint>
#include <optional>
#include <utility>

#include "../control/steering_controller.h"
#include "../planning/nav_run_controller.h"
#include "navigation_runtime_state.h"

namespace mapnavigator
{

struct Motion;

struct SteeringTurnObservation
{
    double achieved_deg = 0.0;
    double residual_deg = 0.0;
    int64_t elapsed_ms = 0;
};

struct SteeringHeadingRate
{
    double rate_deg = 0.0;
    double raw_delta_deg = 0.0;
    int64_t gap_ms = 0;
    uint64_t gap_ticks = 0;
};

struct SteeringReading
{
    SteeringTurnObservation last_turn;
    SteeringHeadingRate heading_rate;
    std::optional<int64_t> steer_period_ms;
    bool slow_loop = false;
};

SteeringReading ObserveSteering(
    SteeringRateState* steering_rate,
    FlowState* flow,
    const Motion& motion,
    double steer_heading,
    std::chrono::steady_clock::time_point now,
    bool drops_turn_sends);
SteeringCommand DecideSteering(SteeringRateState* steering_rate, const Motion& motion, double heading_error, double heading_rate_deg);
bool BrakeForSharpCorner(FlowState* flow, const NaviPosition& position, const NavRunTickResult& nav_run_result, bool slow_loop);
std::pair<double, int64_t> SendSteering(
    SteeringRateState* steering_rate,
    Motion* motion,
    const SteeringCommand& steering,
    int64_t tick_gap_ms,
    double steer_heading,
    std::chrono::steady_clock::time_point now,
    bool drops_turn_sends);
// 前进键被吞时补按; recovering 为真时按键归恢复流程管
void UpdateForwardHold(
    FlowState* flow,
    Motion* motion,
    const NaviPosition& position,
    double issued_delta_deg,
    double heading_error,
    bool recovering);

} // namespace mapnavigator
