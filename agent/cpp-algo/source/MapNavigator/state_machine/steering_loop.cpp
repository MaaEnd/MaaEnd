#include <algorithm>
#include <chrono>
#include <cmath>
#include <deque>
#include <optional>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../sensing/latency_observer.h"
#include "steering_loop.h"

namespace mapnavigator
{

SteeringLoop::SteeringLoop(
    SteeringRateState& steering_rate,
    FlowState& flow,
    MotionController* motion_controller,
    const walkmode::Toggle& walk_mode,
    const NaviPosition* position)
    : steering_rate_(steering_rate)
    , flow_(flow)
    , motion_controller_(motion_controller)
    , walk_mode_(walk_mode)
    , position_(position)
{
}

SteeringLoop::Reading SteeringLoop::Observe(double steer_heading, std::chrono::steady_clock::time_point now, bool drops_turn_sends)
{
    Reading reading;
    reading.last_turn = ObserveLastTurn(steer_heading, now);
    reading.heading_rate = TrackHeadingRate(steer_heading, now);
    reading.steer_period_ms = SampleSteerPeriod(now);
    reading.slow_loop = drops_turn_sends && reading.steer_period_ms.has_value() && *reading.steer_period_ms >= kSlowLoopPivotPeriodMs;
    SettlePendingTurn(steer_heading, now, drops_turn_sends, reading.steer_period_ms);
    return reading;
}

SteeringCommand SteeringLoop::Decide(double heading_error, double heading_rate_deg)
{
    return SteeringController::Update(
        heading_error,
        heading_rate_deg,
        motion_controller_->IsMovingForward(),
        steering_rate_.turn_latch_sign,
        steering_rate_.pending_turn_deg);
}

SteeringLoop::TurnObservation SteeringLoop::ObserveLastTurn(double steer_heading, std::chrono::steady_clock::time_point now) const
{
    if (!steering_rate_.has_cmd) {
        return {};
    }
    const double achieved_deg = NaviMath::NormalizeAngle(steer_heading - steering_rate_.cmd_heading_deg);
    return {
        .achieved_deg = achieved_deg,
        .residual_deg = NaviMath::NormalizeAngle(achieved_deg - steering_rate_.cmd_delta_deg),
        .elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - steering_rate_.cmd_at).count(),
    };
}

SteeringLoop::HeadingRate SteeringLoop::TrackHeadingRate(double steer_heading, std::chrono::steady_clock::time_point now)
{
    SteeringRateState& steering_rate = steering_rate_;
    const uint64_t tick_seq = flow_.tick_seq;
    if (!steering_rate.has_prev) {
        steering_rate.prev_heading_deg = steer_heading;
        steering_rate.at = now;
        steering_rate.at_tick = tick_seq;
        steering_rate.has_prev = true;
        return {};
    }
    HeadingRate rate;
    rate.raw_delta_deg = NaviMath::NormalizeAngle(steer_heading - steering_rate.prev_heading_deg);
    rate.gap_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - steering_rate.at).count();
    rate.gap_ticks = tick_seq - steering_rate.at_tick;
    const bool heading_changed = std::abs(rate.raw_delta_deg) > kSteeringHeadingChangeEpsilonDeg;
    // 按拍数判过期: 截图慢只会拉长间隔, 观察机会并没有变少
    if (heading_changed && rate.gap_ms > 0 && rate.gap_ticks <= kSteeringRateMaxGapTicks) {
        rate.rate_deg = rate.raw_delta_deg * static_cast<double>(kSteeringRateReferenceMs) / static_cast<double>(rate.gap_ms);
    }
    if (heading_changed) {
        steering_rate.prev_heading_deg = steer_heading;
        steering_rate.at = now;
        steering_rate.at_tick = tick_seq;
    }
    return rate;
}

std::optional<int64_t> SteeringLoop::SampleSteerPeriod(std::chrono::steady_clock::time_point now)
{
    FlowState& flow = flow_;
    if (flow.last_steer_tick_at.time_since_epoch().count() > 0) {
        const int64_t period_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - flow.last_steer_tick_at).count();
        flow.steer_periods_ms.push_back(std::min(period_ms, kSteerPeriodMaxSampleMs));
        if (flow.steer_periods_ms.size() > kSteerPeriodWindow) {
            flow.steer_periods_ms.erase(flow.steer_periods_ms.begin());
        }
    }
    flow.last_steer_tick_at = now;
    return flow.steer_periods_ms.empty() ? std::nullopt : std::optional<int64_t>(latency::Median(flow.steer_periods_ms));
}

void SteeringLoop::SettlePendingTurn(
    double steer_heading,
    std::chrono::steady_clock::time_point now,
    bool drops_turn_sends,
    std::optional<int64_t> steer_period_ms)
{
    SteeringRateState& steering_rate = steering_rate_;
    const double batch_cap_deg = motion_controller_->SteeringBatchCapDeg();
    const bool walk_engaged = walk_mode_.engaged();
    const auto pending_lifetime_ms = [&](double delta_deg) {
        const double extra_sweep_deg = std::max(0.0, std::abs(delta_deg) - batch_cap_deg);
        const int64_t floor_ms = drops_turn_sends
                                     ? std::max(kSteeringPendingLifetimeMs, kSteeringPendingLifetimeTicks * steer_period_ms.value_or(0))
                                     : kSteeringPendingLifetimeMs;
        const int64_t base_ms = floor_ms + static_cast<int64_t>(extra_sweep_deg / kYawRateDegPerSec * 1000.0);
        return walk_engaged ? base_ms * kWalkModeSlowFactor : base_ms;
    };
    if (drops_turn_sends) {
        std::erase_if(steering_rate.in_flight, [&](const SteeringRateState::InFlightTurn& turn) {
            return std::chrono::duration_cast<std::chrono::milliseconds>(now - turn.sent_at).count() >= pending_lifetime_ms(turn.delta_deg);
        });
        if (steering_rate.pending_turn_deg != 0.0) {
            if (steering_rate.in_flight.empty()) {
                steering_rate.pending_turn_deg = 0.0;
            }
            else {
                const double landed = NaviMath::NormalizeAngle(steer_heading - steering_rate.pending_ref_heading_deg);
                const double owed = std::abs(steering_rate.pending_turn_deg);
                double negative_sent_deg = 0.0;
                double positive_sent_deg = 0.0;
                for (const SteeringRateState::InFlightTurn& turn : steering_rate.in_flight) {
                    (turn.delta_deg < 0.0 ? negative_sent_deg : positive_sent_deg) += turn.delta_deg;
                }
                steering_rate.pending_turn_deg =
                    std::clamp(std::clamp(steering_rate.pending_turn_deg - landed, -owed, owed), negative_sent_deg, positive_sent_deg);
            }
        }
    }
    else if (steering_rate.pending_turn_deg != 0.0) {
        const int64_t pending_age_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - steering_rate.cmd_at).count();
        if (pending_age_ms >= pending_lifetime_ms(steering_rate.cmd_delta_deg)) {
            steering_rate.pending_turn_deg = 0.0;
        }
        else {
            const double landed = NaviMath::NormalizeAngle(steer_heading - steering_rate.pending_ref_heading_deg);
            const double owed = std::abs(steering_rate.pending_turn_deg);
            steering_rate.pending_turn_deg = std::clamp(steering_rate.pending_turn_deg - landed, -owed, owed);
        }
    }
    steering_rate.pending_ref_heading_deg = steer_heading;
}

bool SteeringLoop::BrakeForSharpCorner(const NavRunTickResult& nav_run_result, bool slow_loop)
{
    FlowState& flow = flow_;
    bool corner_brake = false;
    const std::optional<SharpCorner>& corner = nav_run_result.corner;
    const auto ahead_is_braked_corner = [&] {
        return corner && flow.braked_corner
               && std::hypot(corner->point.x - flow.braked_corner->x, corner->point.y - flow.braked_corner->y) <= kCornerBrakeLatchRadiusM;
    };
    if (slow_loop && corner && nav_run_result.step_per_tick) {
        const double stop_distance = *nav_run_result.step_per_tick * kCornerBrakeLeadTicks;
        corner_brake = !ahead_is_braked_corner() && corner->distance <= stop_distance && corner->lead_distance < stop_distance;
        if (corner_brake) {
            flow.braked_corner = corner->point;
            LogInfo << "Braking before a sharp corner." << VAR(corner->distance) << VAR(corner->turn_deg) << VAR(corner->lead_distance)
                    << VAR(stop_distance) << VAR(corner->point.x) << VAR(corner->point.y);
        }
    }
    if (const bool corner_walk = ahead_is_braked_corner(); corner_walk != flow.corner_walk) {
        flow.corner_walk = corner_walk;
        LogInfo << (corner_walk ? "Walking into a braked corner." : "Braked corner behind; walking ends.") << VAR(position_->x)
                << VAR(position_->y);
    }
    return corner_brake;
}

std::pair<double, int64_t> SteeringLoop::Send(
    const SteeringCommand& steering,
    int64_t tick_gap_ms,
    double steer_heading,
    std::chrono::steady_clock::time_point now,
    bool drops_turn_sends)
{
    SteeringRateState& steering_rate = steering_rate_;
    double issued_delta_deg = 0.0;
    int64_t steer_send_ms = 0;
    // 存活期从真正发出时算起, 这一拍前面若重规划过, 拍头时间已经过去很久
    const auto steer_sent_at = std::chrono::steady_clock::now();
    if (steering.issued) {
        const TurnCommandResult steering_result = motion_controller_->ApplySteering(steering.yaw_delta_deg, tick_gap_ms);
        steer_send_ms = steering_result.send_ms;
        if (steering_result.issued) {
            issued_delta_deg = steering_result.issued_delta_degrees;
        }
    }
    if (issued_delta_deg != 0.0) {
        steering_rate.cmd_heading_deg = steer_heading;
        steering_rate.cmd_delta_deg = issued_delta_deg;
        steering_rate.cmd_at = now;
        steering_rate.has_cmd = true;
        steering_rate.pending_turn_deg += issued_delta_deg;
        if (drops_turn_sends) {
            steering_rate.in_flight.push_back({ .delta_deg = issued_delta_deg, .sent_at = steer_sent_at });
        }
    }
    return { issued_delta_deg, steer_send_ms };
}

void SteeringLoop::UpdateForwardHold(double issued_delta_deg, double heading_error, bool recovering)
{
    FlowState& flow = flow_;
    const bool moved = flow.has_last_steer_position
                       && std::hypot(position_->x - flow.last_steer_position.x, position_->y - flow.last_steer_position.y)
                              > kMeasurementDefaultPositionQuantum;
    // 重发计数在恢复启动后也保留, 只有真的走动或转向才清零
    const bool hold_progressing = moved || issued_delta_deg != 0.0 || !motion_controller_->IsMovingForward();
    if (hold_progressing) {
        flow.futile_forward_reasserts = 0;
    }
    if (hold_progressing || recovering) {
        flow.motionless_hold_ticks = 0;
    }
    else if (flow.has_last_steer_position) {
        ++flow.motionless_hold_ticks;
    }
    flow.last_steer_position = *position_;
    flow.has_last_steer_position = true;
    const int32_t hold_reassert_ticks = walk_mode_.engaged() ? kForwardHoldReassertTicks * kWalkModeSlowFactor : kForwardHoldReassertTicks;
    if (flow.motionless_hold_ticks >= hold_reassert_ticks) {
        ++flow.futile_forward_reasserts;
        LogWarn << "Forward hold produced no motion; re-sending it." << VAR(flow.motionless_hold_ticks) << VAR(heading_error)
                << VAR(flow.futile_forward_reasserts) << VAR(position_->x) << VAR(position_->y);
        motion_controller_->ReassertForward();
        flow.motionless_hold_ticks = 0;
    }
}

} // namespace mapnavigator
