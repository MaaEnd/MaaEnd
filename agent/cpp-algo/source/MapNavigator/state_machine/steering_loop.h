#pragma once

#include <chrono>
#include <cstdint>
#include <optional>
#include <utility>

#include "../control/steering_controller.h"
#include "../control/walk_mode.h"
#include "../planning/nav_run_controller.h"
#include "navigation_runtime_state.h"

namespace mapnavigator
{

class MotionController;

class SteeringLoop
{
public:
    struct TurnObservation
    {
        double achieved_deg = 0.0;
        double residual_deg = 0.0;
        int64_t elapsed_ms = 0;
    };

    struct HeadingRate
    {
        double rate_deg = 0.0;
        double raw_delta_deg = 0.0;
        int64_t gap_ms = 0;
        uint64_t gap_ticks = 0;
    };

    struct Reading
    {
        TurnObservation last_turn;
        HeadingRate heading_rate;
        std::optional<int64_t> steer_period_ms;
        bool slow_loop = false;
    };

    SteeringLoop(
        SteeringRateState& steering_rate,
        FlowState& flow,
        MotionController* motion_controller,
        const walkmode::Toggle& walk_mode,
        const NaviPosition* position);

    Reading observe(double steer_heading, std::chrono::steady_clock::time_point now, bool drops_turn_sends);
    SteeringCommand decide(double heading_error, double heading_rate_deg);
    bool brakeForSharpCorner(const NavRunTickResult& nav_run_result, bool slow_loop);
    std::pair<double, int64_t> send(
        const SteeringCommand& steering,
        int64_t tick_gap_ms,
        double steer_heading,
        std::chrono::steady_clock::time_point now,
        bool drops_turn_sends);
    // 前进键被吞时补按; recovering 为真时按键归恢复流程管
    void updateForwardHold(double issued_delta_deg, double heading_error, bool recovering);

private:
    TurnObservation observeLastTurn(double steer_heading, std::chrono::steady_clock::time_point now) const;
    HeadingRate trackHeadingRate(double steer_heading, std::chrono::steady_clock::time_point now);
    std::optional<int64_t> sampleSteerPeriod(std::chrono::steady_clock::time_point now);
    void settlePendingTurn(
        double steer_heading,
        std::chrono::steady_clock::time_point now,
        bool drops_turn_sends,
        std::optional<int64_t> steer_period_ms);

    SteeringRateState& steering_rate_;
    FlowState& flow_;
    MotionController* motion_controller_;
    const walkmode::Toggle& walk_mode_;
    const NaviPosition* position_;
};

} // namespace mapnavigator
