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

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../sensing/position_provider.h"
#include "heading_turn.h"
#include "semantic_helpers.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

struct SettleBudget
{
    TimePoint deadline {};
    MaaContext* maa_context = nullptr;
};

struct PulseSample
{
    double hold_ms = 0.0;
    double travel = 0.0;
};

// 同一进程即同一台机器与按键链路, 量到的关系跨点跨任务都成立
struct PulseHistory
{
    std::mutex mutex;
    std::deque<PulseSample> samples;
};

PulseHistory g_pulse_history;

struct SettleRun
{
    NaviPosition fix {};
    double residual = std::numeric_limits<double>::infinity();
    double bearing = 0.0;
    int pulses = 0;
};

enum class SettleStep
{
    Continue,
    Verified,
    Incomplete,
};

bool task_stopping(MaaContext* maa_context)
{
    return MaaTaskerStopping(MaaContextGetTasker(maa_context));
}

bool budget_exhausted(const SettleBudget& budget, Duration reserve = {})
{
    return task_stopping(budget.maa_context) || std::chrono::steady_clock::now() + reserve >= budget.deadline;
}

bool sleep_within_budget(const SettleBudget& budget, Duration duration)
{
    const auto until = std::chrono::steady_clock::now() + duration;
    if (until > budget.deadline) {
        return false;
    }
    while (!task_stopping(budget.maa_context)) {
        const auto now = std::chrono::steady_clock::now();
        if (now >= until) {
            return true;
        }
        std::this_thread::sleep_for(std::min(until - now, Duration(std::chrono::milliseconds(kTargetTickMs))));
    }
    return false;
}

const char* cancelled_or(const SettleBudget& budget, const char* reason)
{
    return task_stopping(budget.maa_context) ? "cancelled" : reason;
}

bool turn_within_budget(const Context& ctx, const SettleBudget& budget, double heading_delta)
{
    const std::optional<HeadingTurnSteps> turn = PlanHeadingTurn(*ctx.input_backend, heading_delta);
    if (!turn) {
        return true;
    }
    for (int step = 0; step < turn->step_count; ++step) {
        if (budget_exhausted(budget)) {
            return false;
        }
        if (!SendHeadingTurnStep(ctx.input_backend, *turn, step)) {
            return false;
        }
    }
    return true;
}

bool capture_strict_goal_fix(const Context& ctx, const SettleBudget& budget, NaviPosition* out_pos)
{
    for (int frame = 0; frame < kStrictSettleFixMaxFrames; ++frame) {
        if (budget_exhausted(budget)) {
            break;
        }
        bool waited = true;
        if (frame > 0) {
            waited = sleep_within_budget(budget, std::chrono::milliseconds(kStrictSettleFixIntervalMs));
        }
        if (!waited) {
            break;
        }
        if (!CaptureForStrictGoal(ctx.position_provider, out_pos, ctx.session->current_zone_id)) {
            continue;
        }
        if (budget_exhausted(budget)) {
            break;
        }
        *ctx.position = *out_pos;
        return true;
    }
    ctx.position->valid = false;
    return false;
}

bool capture_still_fix(const Context& ctx, const SettleBudget& budget, NaviPosition* out_pos)
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
        if (!sleep_within_budget(budget, std::chrono::milliseconds(kStrictSettleFixIntervalMs))) {
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

void record_pulse(PulseHistory* history, double hold_ms, double travel)
{
    const std::lock_guard<std::mutex> guard(history->mutex);
    history->samples.push_back({ .hold_ms = hold_ms, .travel = travel });
    if (history->samples.size() > kStrictSettlePulseHistory) {
        history->samples.pop_front();
    }
}

std::vector<PulseSample> snapshot_pulses(PulseHistory* history)
{
    const std::lock_guard<std::mutex> guard(history->mutex);
    return std::vector<PulseSample>(history->samples.begin(), history->samples.end());
}

// 按位移 = 斜率·时长 + 截距拟合, 斜率与截距都取中位数以抗离群样本
int plan_hold_ms(const std::vector<PulseSample>& samples, double residual, int max_hold_ms)
{
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
        for (const PulseSample& sample : samples) {
            intercepts.push_back(sample.travel - slope * sample.hold_ms);
        }
        hold_ms = (residual - median_of(intercepts)) / slope;
    }
    else {
        std::vector<double> rates;
        for (const PulseSample& sample : samples) {
            rates.push_back(sample.travel / sample.hold_ms);
        }
        hold_ms = residual / median_of(rates);
    }
    return std::clamp(static_cast<int>(std::lround(hold_ms)), kStrictSettleMinHoldMs, max_hold_ms);
}

SettleStep report_incomplete(const SettleRun& run, const char* reason)
{
    LogWarn << "Strict arrival not verified." << VAR(reason) << VAR_AS(residual, run.residual) << VAR_AS(pulses, run.pulses);
    return SettleStep::Incomplete;
}

SettleStep report_verified(const Context& ctx, const SettleRun& run)
{
    LogInfo << "Strict arrival verified after settling." << VAR_AS(residual, run.residual) << VAR_AS(pulses, run.pulses)
            << VAR(ctx.position->x) << VAR(ctx.position->y);
    return SettleStep::Verified;
}

SettleStep align_toward_goal(const Context& ctx, SettleRun* run, const Waypoint& waypoint, const SettleBudget& budget)
{
    for (int turns = 0;; ++turns) {
        run->bearing = CalcTargetRotation(run->fix.x, run->fix.y, waypoint.x, waypoint.y);
        const double heading_delta = CalcDeltaRotation(*run->fix.camera_angle, run->bearing);
        if (std::abs(heading_delta) <= kStrictSettleHeadingToleranceDeg) {
            return SettleStep::Continue;
        }
        if (turns == kStrictSettleMaxHeadingTurns) {
            return report_incomplete(*run, "camera_not_aligned");
        }
        LogInfo << "Strict arrival aligning while stopped." << VAR_AS(bearing, run->bearing) << VAR(heading_delta) << VAR(turns);
        if (budget_exhausted(budget)) {
            return report_incomplete(*run, cancelled_or(budget, "view_turn_failed_or_budget_exhausted"));
        }
        if (!turn_within_budget(ctx, budget, heading_delta)) {
            return report_incomplete(*run, cancelled_or(budget, "view_turn_failed_or_budget_exhausted"));
        }
        if (!sleep_within_budget(budget, std::chrono::milliseconds(kWaitAfterFirstTurnMs))) {
            return report_incomplete(*run, cancelled_or(budget, "camera_feedback_unavailable"));
        }
        if (!capture_strict_goal_fix(ctx, budget, &run->fix)) {
            return report_incomplete(*run, cancelled_or(budget, "camera_feedback_unavailable"));
        }
        // 以实际读数为准, 不用指令预期的朝向
        run->residual = std::hypot(waypoint.x - run->fix.x, waypoint.y - run->fix.y);
        if (run->residual <= kStrictSettleAcceptBandWu) {
            return report_verified(ctx, *run);
        }
    }
}

SettleStep pulse_and_measure(const Context& ctx, SettleRun* run, const SettleBudget& budget, const StrictSettleInputProfile& input)
{
    const int hold_ms = plan_hold_ms(snapshot_pulses(&g_pulse_history), run->residual, input.max_hold_ms);
    const auto settle_reserve = std::chrono::milliseconds(hold_ms + input.slide_ms + kStrictSettleStillReads * kStrictSettleFixIntervalMs);
    if (budget_exhausted(budget, settle_reserve)) {
        return report_incomplete(*run, cancelled_or(budget, "budget_exhausted"));
    }
    LogInfo << "Strict arrival forward pulse." << VAR_AS(residual, run->residual) << VAR(hold_ms) << VAR_AS(pulses, run->pulses);
    ++run->pulses;
    const NaviPosition from = run->fix;
    MoveForward(ctx.motion_controller);
    const auto pressed_at = std::chrono::steady_clock::now();
    ctx.position->valid = false; // 一开始移动停稳读数就作废
    const bool held = sleep_within_budget(budget, std::chrono::milliseconds(hold_ms));
    HoldPosition(ctx.motion_controller);
    const double held_ms = std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - pressed_at).count();
    if (!held) {
        return report_incomplete(*run, cancelled_or(budget, "budget_exhausted"));
    }
    if (!capture_still_fix(ctx, budget, &run->fix)) {
        return report_incomplete(*run, cancelled_or(budget, "budget_exhausted"));
    }

    // 挪得太少或偏得太多的这一下是被地形挡了, 不代表按键时长和位移的关系
    const double travel = std::hypot(run->fix.x - from.x, run->fix.y - from.y);
    const double deflection = std::abs(CalcDeltaRotation(run->bearing, CalcTargetRotation(from.x, from.y, run->fix.x, run->fix.y)));
    const bool usable = travel >= kStrictSettleStillWu && deflection <= kStrictSettlePulseDirectionToleranceDeg;
    if (usable) {
        record_pulse(&g_pulse_history, held_ms, travel);
    }
    LogInfo << "Strict arrival pulse landed." << VAR(held_ms) << VAR(travel) << VAR(deflection) << VAR(usable);
    return SettleStep::Continue;
}

SettleStep settle_toward_goal(
    const Context& ctx,
    SettleRun* run,
    const Waypoint& waypoint,
    const SettleBudget& budget,
    const StrictSettleInputProfile& input)
{
    if (!capture_still_fix(ctx, budget, &run->fix)) {
        return report_incomplete(*run, cancelled_or(budget, "no_settled_fix"));
    }

    std::optional<double> previous_residual;
    for (;;) {
        run->residual = std::hypot(waypoint.x - run->fix.x, waypoint.y - run->fix.y);
        if (run->residual <= kStrictSettleAcceptBandWu) {
            return report_verified(ctx, *run);
        }
        if (run->pulses == kStrictSettleMaxPulses) {
            return report_incomplete(*run, "pulse_budget_exhausted");
        }
        // 测量容差内的过冲允许掉头, 超出即判发散
        if (previous_residual && run->residual > *previous_residual + kStrictSettleAcceptBandWu) {
            return report_incomplete(*run, "diverging");
        }
        if (!SupportsWalkToggle(*ctx.input_backend)) {
            return report_incomplete(*run, "walking_unavailable");
        }
        const SettleStep aligned = align_toward_goal(ctx, run, waypoint, budget);
        if (aligned != SettleStep::Continue) {
            return aligned;
        }
        previous_residual = run->residual;
        const SettleStep pulsed = pulse_and_measure(ctx, run, budget, input);
        if (pulsed != SettleStep::Continue) {
            return pulsed;
        }
    }
}

} // namespace

int PlanPulseHoldMs(double residual, int max_hold_ms)
{
    return plan_hold_ms(snapshot_pulses(&g_pulse_history), residual, max_hold_ms);
}

bool SettleAtStrictGoal(const Context& ctx, const Waypoint& waypoint)
{
    // 取消、指令被拒与异常退出都须松开移动
    struct StopOnExit
    {
        const Context& ctx;

        ~StopOnExit() { HoldAndArmCameraAlign(ctx); }
    } stop_on_exit { ctx };

    HoldAndArmCameraAlign(ctx);
    const SettleBudget budget {
        .deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(kStrictSettleBudgetMs),
        .maa_context = ctx.maa_context,
    };
    const StrictSettleInputProfile& input = UsesTouchBackend(*ctx.input_backend) ? kStrictSettleTouchProfile : kStrictSettleKeyboardProfile;
    SettleRun run;
    return settle_toward_goal(ctx, &run, waypoint, budget, input) == SettleStep::Verified;
}

} // namespace semantic_nodes

} // namespace mapnavigator
