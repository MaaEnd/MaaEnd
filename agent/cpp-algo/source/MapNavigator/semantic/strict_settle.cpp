#include "strict_settle.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <deque>
#include <limits>
#include <mutex>
#include <optional>
#include <thread>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../sensing/position_provider.h"
#include "heading_turn.h"
#include "semantic_helpers.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

struct StrictSettleBudget
{
    std::chrono::steady_clock::time_point deadline;
    const std::function<bool()>& should_stop;

    bool exhausted(std::chrono::steady_clock::duration reserve = {}) const
    {
        return should_stop() || std::chrono::steady_clock::now() + reserve >= deadline;
    }

    bool sleep(std::chrono::steady_clock::duration duration) const
    {
        const auto until = std::chrono::steady_clock::now() + duration;
        if (until > deadline) {
            return false;
        }
        while (!should_stop()) {
            const auto now = std::chrono::steady_clock::now();
            if (now >= until) {
                return true;
            }
            std::this_thread::sleep_for(
                std::min(until - now, std::chrono::steady_clock::duration(std::chrono::milliseconds(kTargetTickMs))));
        }
        return false;
    }

    const char* cancelledOr(const char* reason) const { return should_stop() ? "cancelled" : reason; }
};

bool capture_strict_goal_fix(const Context& ctx, const StrictSettleBudget& budget, NaviPosition* out_pos)
{
    for (int frame = 0; frame < kStrictSettleFixMaxFrames; ++frame) {
        if (budget.exhausted()) {
            break;
        }
        if (frame > 0 && !budget.sleep(std::chrono::milliseconds(kStrictSettleFixIntervalMs))) {
            break;
        }
        if (!ctx.position_provider->captureForStrictGoal(out_pos, ctx.session->currentZoneId())) {
            continue;
        }
        if (budget.exhausted()) {
            break;
        }
        *ctx.position = *out_pos;
        return true;
    }
    ctx.position->valid = false;
    return false;
}

bool capture_still_fix(const Context& ctx, const StrictSettleBudget& budget, NaviPosition* out_pos)
{
    std::optional<NaviPosition> first;
    int still_reads = 0;
    for (;;) {
        NaviPosition fix {};
        if (!capture_strict_goal_fix(ctx, budget, &fix)) {
            return false;
        }
        if (first && std::hypot(fix.x - first->x, fix.y - first->y) <= kStrictSettleStillWu) {
            ++still_reads;
        }
        else {
            first = fix;
            still_reads = 1;
        }
        if (still_reads >= kStrictSettleStillReads) {
            *out_pos = fix;
            return true;
        }
        if (!budget.sleep(std::chrono::milliseconds(kStrictSettleFixIntervalMs))) {
            return false;
        }
    }
}

double median_of(std::vector<double> values)
{
    const auto mid = values.begin() + static_cast<std::ptrdiff_t>(values.size() / 2);
    std::nth_element(values.begin(), mid, values.end());
    return *mid;
}

// 同一进程即同一台机器与按键链路, 量到的关系跨点跨任务都成立
class PulseResponseModel
{
public:
    void record(double hold_ms, double travel)
    {
        const std::lock_guard<std::mutex> guard(mutex_);
        responses_.push_back({ .hold_ms = hold_ms, .travel = travel });
        if (responses_.size() > kStrictSettlePulseHistory) {
            responses_.pop_front();
        }
    }

    // 按位移 = 斜率·时长 + 截距拟合, 斜率与截距都取中位数以抗离群样本
    int planHoldMs(double residual, int max_hold_ms) const
    {
        std::vector<PulseResponse> samples;
        {
            const std::lock_guard<std::mutex> guard(mutex_);
            samples.assign(responses_.begin(), responses_.end());
        }
        if (samples.empty()) {
            return kStrictSettleProbeHoldMs;
        }

        std::vector<double> slopes;
        for (size_t i = 0; i < samples.size(); ++i) {
            for (size_t j = i + 1; j < samples.size(); ++j) {
                const double spread_ms = samples[j].hold_ms - samples[i].hold_ms;
                if (std::abs(spread_ms) >= kStrictSettlePulseSpreadMs) {
                    slopes.push_back((samples[j].travel - samples[i].travel) / spread_ms);
                }
            }
        }

        double hold_ms = 0.0;
        const double slope = slopes.empty() ? 0.0 : median_of(slopes);
        if (slope > 0.0) {
            std::vector<double> intercepts;
            for (const PulseResponse& sample : samples) {
                intercepts.push_back(sample.travel - slope * sample.hold_ms);
            }
            hold_ms = (residual - median_of(intercepts)) / slope;
        }
        else {
            std::vector<double> rates;
            for (const PulseResponse& sample : samples) {
                rates.push_back(sample.travel / sample.hold_ms);
            }
            hold_ms = residual / median_of(rates);
        }
        return std::clamp(static_cast<int>(std::lround(hold_ms)), kStrictSettleMinHoldMs, max_hold_ms);
    }

private:
    struct PulseResponse
    {
        double hold_ms = 0.0;
        double travel = 0.0;
    };

    mutable std::mutex mutex_;
    std::deque<PulseResponse> responses_;
};

PulseResponseModel g_pulse_model;

struct StrictSettle
{
    const Context& ctx;
    const Waypoint& waypoint;
    StrictSettleBudget budget;
    const StrictSettleInputProfile& input;
    NaviPosition fix {};
    double residual = std::numeric_limits<double>::infinity();
    double bearing = 0.0;
    int pulses = 0;

    bool run()
    {
        if (!capture_still_fix(ctx, budget, &fix)) {
            return incomplete(budget.cancelledOr("no_settled_fix"));
        }

        std::optional<double> previous_residual;
        for (;;) {
            residual = std::hypot(waypoint.x - fix.x, waypoint.y - fix.y);
            if (residual <= kStrictSettleAcceptBandWu) {
                return verified();
            }
            if (pulses == kStrictSettleMaxPulses) {
                return incomplete("pulse_budget_exhausted");
            }
            // 测量容差内的过冲允许掉头, 超出即判发散
            if (previous_residual && residual > *previous_residual + kStrictSettleAcceptBandWu) {
                return incomplete("diverging");
            }
            if (!ctx.action_wrapper->supportsWalkToggle()) {
                return incomplete("walking_unavailable");
            }
            if (const std::optional<bool> verdict = alignTowardGoal()) {
                return *verdict;
            }
            previous_residual = residual;
            if (const std::optional<bool> verdict = pulseAndMeasure()) {
                return *verdict;
            }
        }
    }

    std::optional<bool> alignTowardGoal()
    {
        for (int turns = 0;; ++turns) {
            bearing = NaviMath::calcTargetRotation(fix.x, fix.y, waypoint.x, waypoint.y);
            const double heading_delta = NaviMath::calcDeltaRotation(*fix.camera_angle, bearing);
            if (std::abs(heading_delta) <= kStrictSettleHeadingToleranceDeg) {
                return std::nullopt;
            }
            if (turns == kStrictSettleMaxHeadingTurns) {
                return incomplete("camera_not_aligned");
            }
            LogInfo << "Strict arrival aligning while stopped." << VAR(bearing) << VAR(heading_delta) << VAR(turns);
            if (budget.exhausted() || !TurnToHeadingOnce(ctx, heading_delta, [this]() { return budget.exhausted(); })) {
                return incomplete(budget.cancelledOr("view_turn_failed_or_budget_exhausted"));
            }
            if (!budget.sleep(std::chrono::milliseconds(kWaitAfterFirstTurnMs)) || !capture_strict_goal_fix(ctx, budget, &fix)) {
                return incomplete(budget.cancelledOr("camera_feedback_unavailable"));
            }
            // 以实际读数为准, 不用指令预期的朝向
            residual = std::hypot(waypoint.x - fix.x, waypoint.y - fix.y);
            if (residual <= kStrictSettleAcceptBandWu) {
                return verified();
            }
        }
    }

    std::optional<bool> pulseAndMeasure()
    {
        const int hold_ms = g_pulse_model.planHoldMs(residual, input.max_hold_ms);
        const auto settle_reserve =
            std::chrono::milliseconds(hold_ms + input.slide_ms + kStrictSettleStillReads * kStrictSettleFixIntervalMs);
        if (budget.exhausted(settle_reserve)) {
            return incomplete(budget.cancelledOr("budget_exhausted"));
        }
        LogInfo << "Strict arrival forward pulse." << VAR(residual) << VAR(hold_ms) << VAR(pulses);
        ++pulses;
        const NaviPosition from = fix;
        ctx.motion_controller->setForwardState(true);
        const auto pressed_at = std::chrono::steady_clock::now();
        ctx.position->valid = false; // 一开始移动停稳读数就作废
        const bool held = budget.sleep(std::chrono::milliseconds(hold_ms));
        ctx.motion_controller->setForwardState(false);
        const double held_ms = std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - pressed_at).count();
        if (!held || !capture_still_fix(ctx, budget, &fix)) {
            return incomplete(budget.cancelledOr("budget_exhausted"));
        }

        // 挪得太少或偏得太多的这一下是被地形挡了, 不代表按键时长和位移的关系
        const double travel = std::hypot(fix.x - from.x, fix.y - from.y);
        const double deflection =
            std::abs(NaviMath::calcDeltaRotation(bearing, NaviMath::calcTargetRotation(from.x, from.y, fix.x, fix.y)));
        const bool usable = travel >= kStrictSettleStillWu && deflection <= kStrictSettlePulseDirectionToleranceDeg;
        if (usable) {
            g_pulse_model.record(held_ms, travel);
        }
        LogInfo << "Strict arrival pulse landed." << VAR(held_ms) << VAR(travel) << VAR(deflection) << VAR(usable);
        return std::nullopt;
    }

    bool incomplete(const char* reason) const
    {
        LogWarn << "Strict arrival not verified." << VAR(reason) << VAR(residual) << VAR(pulses);
        return false;
    }

    bool verified() const
    {
        LogInfo << "Strict arrival verified after settling." << VAR(residual) << VAR(pulses) << VAR(ctx.position->x)
                << VAR(ctx.position->y);
        return true;
    }
};

} // namespace

int PlanPulseHoldMs(double residual, int max_hold_ms)
{
    return g_pulse_model.planHoldMs(residual, max_hold_ms);
}

bool SettleAtStrictGoal(const Context& ctx, const Waypoint& waypoint, const std::function<bool()>& should_stop)
{
    // 取消、指令被拒与异常退出都须松开移动
    struct StopOnExit
    {
        const Context& ctx;

        ~StopOnExit() { StopMotionAndCommitment(ctx); }
    } stop_on_exit { ctx };

    StopMotionAndCommitment(ctx);
    StrictSettle settle {
        .ctx = ctx,
        .waypoint = waypoint,
        .budget = { .deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(kStrictSettleBudgetMs),
                    .should_stop = should_stop },
        .input = ctx.action_wrapper->usesTouchBackend() ? kStrictSettleTouchProfile : kStrictSettleKeyboardProfile,
    };
    return settle.run();
}

} // namespace semantic_nodes

} // namespace mapnavigator
