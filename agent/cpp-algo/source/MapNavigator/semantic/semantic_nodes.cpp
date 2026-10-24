#include <algorithm>
#include <chrono>
#include <cmath>
#include <deque>
#include <limits>
#include <mutex>
#include <optional>
#include <string>
#include <thread>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../sensing/position_provider.h"
#include "../zipline/zipline_action.h"
#include "async_prompt_action.h"
#include "find_action.h"
#include "jump_pad_action.h"
#include "semantic_helpers.h"
#include "semantic_nodes.h"
#include "trigger_action.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

void ConsumeMatchedZoneNodes(const Context& ctx)
{
    while (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().IsZoneDeclaration()) {
        const std::string& zone_id = ctx.session->CurrentWaypoint().zone_id;
        if (!zone_id.empty() && zone_id != ctx.session->current_zone_id()) {
            break;
        }
        ctx.session->AdvanceToNextWaypoint(ActionType::ZONE, "zone_declaration_consumed");
        ctx.runtime_state->OnWaypointAdvance();
    }
}

size_t FindFutureZoneDeclaration(const Context& ctx, const std::string& zone_id)
{
    const std::vector<Waypoint>& path = ctx.session->current_path();
    for (size_t index = ctx.session->current_node_idx(); index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        if (waypoint.IsZoneDeclaration() && !waypoint.zone_id.empty() && waypoint.zone_id == zone_id) {
            return index;
        }
    }
    return std::numeric_limits<size_t>::max();
}

Result FinalizePortalTransitZone(const Context& ctx, const std::string& zone_id, size_t matched_zone_index)
{
    if (matched_zone_index > ctx.session->current_node_idx()) {
        ctx.session->SkipPastWaypoint(matched_zone_index - 1, "portal_zone_fast_forward");
    }
    ctx.session->UpdateCurrentZone(zone_id);
    ctx.session->ResetProgress();
    ctx.runtime_state->OnWaypointAdvance();
    ConsumeMatchedZoneNodes(ctx);
    StopMotionAndCommitment(ctx);
    ctx.position_provider->ResetTracking();
    ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix = false;
    ctx.runtime_state->semantic.portal_transit_needs_reacquire = true;
    LogInfo << "Portal transit accepted zone transition." << VAR(zone_id) << VAR(matched_zone_index);

    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;
    result.changed_zone = true;
    return result;
}

Result TickPortalTransit(const Context& ctx)
{
    Result result;
    if (!ctx.runtime_state->semantic.portal_transit_active) {
        return result;
    }

    const auto now = std::chrono::steady_clock::now();
    const auto waited_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(now - ctx.runtime_state->semantic.portal_transit_started).count();
    if (waited_ms > kZoneConfirmTimeoutMs) {
        result.request_failure = true;
        result.failure_reason = "portal_transit_timeout";
        result.failure_log_message = "PORTAL transit timed out before a valid zone landing was confirmed.";
        return result;
    }

    if (ctx.runtime_state->semantic.portal_transit_needs_reacquire) {
        if (!ctx.position_provider->Capture(ctx.position, false, ctx.session->current_zone_id())) {
            result.stay_in_current_tick = true;
            utils::SleepFor(kZoneConfirmRetryIntervalMs);
            return result;
        }
        if (ctx.position->zone_id != ctx.session->current_zone_id()) {
            result.stay_in_current_tick = true;
            utils::SleepFor(kZoneConfirmRetryIntervalMs);
            return result;
        }

        ctx.runtime_state->semantic.portal_transit_active = false;
        ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix = false;
        ctx.runtime_state->semantic.portal_transit_needs_reacquire = false;
        ctx.runtime_state->semantic.portal_transit_started = {};
        ctx.runtime_state->dynamic_replan_requested = true;
        LogInfo << "Portal transit landing confirmed." << VAR(ctx.position->zone_id);
        result.consumed = true;
        result.stay_in_current_tick = true;
        result.changed_zone = true;
        return result;
    }

    if (ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix) {
        ctx.motion_controller->SetForwardState(true);
    }

    NaviPosition candidate;
    if (!ctx.position_provider->Capture(&candidate, true, {})) {
        result.stay_in_current_tick = true;
        utils::SleepFor(kZoneConfirmRetryIntervalMs);
        return result;
    }

    *ctx.position = candidate;
    if (candidate.zone_id.empty() || candidate.zone_id == ctx.session->current_zone_id()) {
        result.stay_in_current_tick = true;
        utils::SleepFor(kZoneConfirmRetryIntervalMs);
        return result;
    }

    const size_t matched_zone_index = FindFutureZoneDeclaration(ctx, candidate.zone_id);
    if (matched_zone_index == std::numeric_limits<size_t>::max()) {
        StopMotionAndCommitment(ctx);
        result.stay_in_current_tick = true;
        utils::SleepFor(kZoneConfirmRetryIntervalMs);
        return result;
    }

    *ctx.position = candidate;
    return FinalizePortalTransitZone(ctx, candidate.zone_id, matched_zone_index);
}

Result TickTransferWaitImpl(const Context& ctx)
{
    Result result;
    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.runtime_state->semantic.transfer_wait_started = {};
        ctx.runtime_state->semantic.transfer_anchor_pos = {};
        ctx.runtime_state->semantic.transfer_stable_hits = 0;
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    ctx.motion_controller->SetForwardState(false);

    const auto now = std::chrono::steady_clock::now();
    if (ctx.runtime_state->semantic.transfer_wait_started.time_since_epoch().count() == 0) {
        ctx.runtime_state->semantic.transfer_wait_started = now;
    }

    if (!ctx.position_provider->Capture(ctx.position, false, {})) {
        if (std::chrono::duration_cast<std::chrono::milliseconds>(now - ctx.runtime_state->semantic.transfer_wait_started).count()
            > kRelocationWaitTimeoutMs) {
            result.request_failure = true;
            result.failure_reason = "transfer_wait_timeout";
            result.failure_log_message = "TRANSFER wait timed out before capture stabilized.";
            return result;
        }
        result.stay_in_current_tick = true;
        utils::SleepFor(kRelocationRetryIntervalMs);
        return result;
    }

    const int64_t waited_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(now - ctx.runtime_state->semantic.transfer_wait_started).count();
    const double moved_from_anchor = std::hypot(
        ctx.position->x - ctx.runtime_state->semantic.transfer_anchor_pos.x,
        ctx.position->y - ctx.runtime_state->semantic.transfer_anchor_pos.y);
    const bool movement_observed = ctx.position->zone_id != ctx.runtime_state->semantic.transfer_anchor_pos.zone_id
                                   || moved_from_anchor >= kRelocationResumeMinDistance;
    if (!movement_observed) {
        ctx.runtime_state->semantic.transfer_stable_hits = 0;
        if (waited_ms > kRelocationWaitTimeoutMs) {
            result.request_failure = true;
            result.failure_reason = "transfer_wait_timeout";
            result.failure_log_message = "TRANSFER wait timed out without external movement.";
            return result;
        }
        result.stay_in_current_tick = true;
        utils::SleepFor(kRelocationRetryIntervalMs);
        return result;
    }

    ++ctx.runtime_state->semantic.transfer_stable_hits;
    if (ctx.runtime_state->semantic.transfer_stable_hits < kRelocationStableFixes) {
        result.stay_in_current_tick = true;
        utils::SleepFor(kRelocationRetryIntervalMs);
        return result;
    }

    ctx.session->UpdateCurrentZone(ctx.position->zone_id);
    ctx.session->ResetProgress();
    ctx.runtime_state->ResetNavigationAssistState();
    ctx.runtime_state->semantic.transfer_wait_started = {};
    ctx.runtime_state->semantic.transfer_anchor_pos = {};
    ctx.runtime_state->semantic.transfer_stable_hits = 0;

    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    SelectPhaseForCurrentWaypoint(ctx, "transfer_wait_complete");
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

// 坐升降索时小地图坐标几乎不动, 所以按乘坐时长等满再要求连续取位成功
Result TickRopeRideImpl(const Context& ctx)
{
    SemanticState& s = ctx.runtime_state->semantic;
    ctx.motion_controller->SetForwardState(false);
    const int64_t waited_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - s.rope_ride_started).count();
    if (waited_ms < s.rope_ride_ms) {
        utils::SleepFor(kRelocationRetryIntervalMs);
        return { .stay_in_current_tick = true };
    }
    if (waited_ms > s.rope_ride_ms + kRelocationWaitTimeoutMs) {
        return { .request_failure = true,
                 .failure_reason = "rope_ride_timeout",
                 .failure_log_message = "Rope ride finished but the position never stabilized." };
    }
    if (!ctx.position_provider->Capture(ctx.position, false, {})) {
        s.rope_stable_hits = 0;
        utils::SleepFor(kRelocationRetryIntervalMs);
        return { .stay_in_current_tick = true };
    }
    if (++s.rope_stable_hits < kRelocationStableFixes) {
        utils::SleepFor(kRelocationRetryIntervalMs);
        return { .stay_in_current_tick = true };
    }

    LogInfo << "Rope ride finished." << VAR(waited_ms) << VAR(ctx.position->x) << VAR(ctx.position->y) << VAR(ctx.position->zone_id);
    s.rope_ride_started = {};
    s.rope_ride_ms = 0;
    s.rope_stable_hits = 0;
    ctx.session->UpdateCurrentZone(ctx.position->zone_id);
    if (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().rope_top) {
        const Waypoint& top = ctx.session->CurrentWaypoint();
        return CompleteArrival(ctx, top, ctx.session->CurrentAbsoluteNodeIndex(), "rope_ride_completed");
    }
    ctx.session->ResetProgress();
    ctx.runtime_state->ResetNavigationAssistState();
    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        return { .consumed = true, .stay_in_current_tick = true };
    }
    SelectPhaseForCurrentWaypoint(ctx, "rope_ride_completed");
    return { .consumed = true, .stay_in_current_tick = true };
}

Result ConsumeHeadingNodesImpl(const Context& ctx)
{
    Result result;
    bool consumed = false;
    while (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().IsHeadingOnly()) {
        const Waypoint heading_node = ctx.session->CurrentWaypoint();
        double target_heading = 0.0;
        if (heading_node.heading_uses_target) {
            target_heading = NaviMath::CalcTargetRotation(ctx.position->x, ctx.position->y, heading_node.x, heading_node.y);
        }
        else {
            target_heading = std::fmod(heading_node.heading_angle, 360.0);
            if (target_heading < 0.0) {
                target_heading += 360.0;
            }
        }

        const double start_heading = NaviMath::NormalizeAngle(ctx.position->angle);
        const double heading_delta = NaviMath::NormalizeAngle(target_heading - start_heading);

        ctx.motion_controller->SetForwardState(false);
        utils::SleepFor(kStopWaitMs);

        double achieved_heading = start_heading;
        if (std::abs(heading_delta) <= 1.0) {
            LogInfo << "Heading-only node already aligned." << VAR(target_heading) << VAR(start_heading);
            ctx.action_wrapper->PulseForwardSync(kPostHeadingForwardPulseMs);
            ctx.motion_controller->SetForwardState(false);
        }
        else if (!CommitHeadingTurn(ctx, heading_delta)) {
            result.request_failure = true;
            result.failure_reason = "heading_turn_failed";
            result.failure_log_message = "HEADING node failed to issue view turn.";
            return result;
        }

        achieved_heading = VerifyAndCorrectHeading(ctx, target_heading, start_heading);

        LogInfo << "Heading-only node completed." << VAR(target_heading) << VAR(start_heading) << VAR(heading_delta)
                << VAR(achieved_heading);
        ctx.session->AdvanceToNextWaypoint(ActionType::HEADING, "heading_consumed");
        ctx.session->ResetProgress();
        ctx.runtime_state->OnWaypointAdvance();
        consumed = true;

        if (!ctx.session->HasCurrentWaypoint()) {
            ctx.session->NoteRouteTailConsumed(*ctx.position, "heading_route_consumed");
        }
        // 后续节点等下一拍重新取位, 免得用到缺失的朝向
        if (!ctx.position->valid) {
            break;
        }
    }

    result.consumed = consumed;
    result.stay_in_current_tick = consumed;
    return result;
}

bool wait_for_strict_settle(
    std::chrono::steady_clock::duration duration,
    std::chrono::steady_clock::time_point deadline,
    const std::function<bool()>& should_stop)
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
        std::this_thread::sleep_for(std::min(until - now, std::chrono::steady_clock::duration(std::chrono::milliseconds(kTargetTickMs))));
    }
    return false;
}

bool capture_strict_goal_fix(
    const Context& ctx,
    NaviPosition* out_pos,
    std::chrono::steady_clock::time_point deadline,
    const std::function<bool()>& should_stop)
{
    for (int frame = 0; frame < kStrictSettleFixMaxFrames; ++frame) {
        if (should_stop() || std::chrono::steady_clock::now() >= deadline) {
            break;
        }
        if (frame > 0 && !wait_for_strict_settle(std::chrono::milliseconds(kStrictSettleFixIntervalMs), deadline, should_stop)) {
            break;
        }
        if (!ctx.position_provider->captureForStrictGoal(out_pos, ctx.session->current_zone_id())) {
            continue;
        }
        if (should_stop() || std::chrono::steady_clock::now() >= deadline) {
            break;
        }
        *ctx.position = *out_pos;
        return true;
    }
    ctx.position->valid = false;
    return false;
}

bool CaptureStillFix(
    const Context& ctx,
    NaviPosition* out_pos,
    std::chrono::steady_clock::time_point deadline,
    const std::function<bool()>& should_stop)
{
    std::optional<NaviPosition> first;
    int still_reads = 0;
    for (;;) {
        NaviPosition fix {};
        if (!capture_strict_goal_fix(ctx, &fix, deadline, should_stop)) {
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
        if (!wait_for_strict_settle(std::chrono::milliseconds(kStrictSettleFixIntervalMs), deadline, should_stop)) {
            return false;
        }
    }
}

struct PulseResponse
{
    double hold_ms = 0.0;
    double travel = 0.0;
};

// 同一进程即同一台机器与按键链路, 量到的关系跨点跨任务都成立
std::mutex g_pulse_mutex;
std::deque<PulseResponse> g_pulse_responses;

double MedianOf(std::vector<double> values)
{
    const auto mid = values.begin() + static_cast<std::ptrdiff_t>(values.size() / 2);
    std::nth_element(values.begin(), mid, values.end());
    return *mid;
}

void RecordPulseResponse(double hold_ms, double travel)
{
    const std::lock_guard<std::mutex> guard(g_pulse_mutex);
    g_pulse_responses.push_back({ .hold_ms = hold_ms, .travel = travel });
    if (g_pulse_responses.size() > kStrictSettlePulseHistory) {
        g_pulse_responses.pop_front();
    }
}

} // namespace

// 按位移 = 斜率·时长 + 截距拟合, 斜率与截距都取中位数以抗离群样本
int PlanPulseHoldMs(double residual, int max_hold_ms)
{
    std::vector<PulseResponse> samples;
    {
        const std::lock_guard<std::mutex> guard(g_pulse_mutex);
        samples.assign(g_pulse_responses.begin(), g_pulse_responses.end());
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
    const double slope = slopes.empty() ? 0.0 : MedianOf(slopes);
    if (slope > 0.0) {
        std::vector<double> intercepts;
        for (const PulseResponse& sample : samples) {
            intercepts.push_back(sample.travel - slope * sample.hold_ms);
        }
        hold_ms = (residual - MedianOf(intercepts)) / slope;
    }
    else {
        std::vector<double> rates;
        for (const PulseResponse& sample : samples) {
            rates.push_back(sample.travel / sample.hold_ms);
        }
        hold_ms = residual / MedianOf(rates);
    }
    return std::clamp(static_cast<int>(std::lround(hold_ms)), kStrictSettleMinHoldMs, max_hold_ms);
}

namespace
{

template <typename CanCaptureFrame>
bool CaptureStableHeadingImpl(const Context& ctx, double* out_heading, const CanCaptureFrame& can_capture_frame)
{
    std::optional<double> previous;
    for (int frame = 0; can_capture_frame(frame); ++frame) {
        if (frame > 0) {
            utils::SleepFor(kHeadingStableReadIntervalMs);
        }
        if (!ctx.position_provider->Capture(ctx.position, false, ctx.session->current_zone_id())) {
            previous.reset();
            continue;
        }
        const double current = NaviMath::NormalizeAngle(ctx.position->angle);
        if (previous && std::abs(NaviMath::NormalizeAngle(current - *previous)) <= kHeadingStableReadToleranceDeg) {
            *out_heading = current;
            return true;
        }
        previous = current;
    }
    return false;
}

} // namespace

bool TurnToHeadingOnce(const Context& ctx, double heading_delta, const std::function<bool()>& should_stop)
{
    if (std::abs(heading_delta) <= 1.0) {
        return true;
    }

    const SteeringTransportProfile profile = ctx.action_wrapper->SteeringProfile();
    const int step_count = static_cast<int>(std::ceil(std::abs(heading_delta) / profile.max_batch_delta_deg));
    const double step_deg = heading_delta / step_count;
    const int step_interval_ms = std::max<int>(kHeadingTurnStepIntervalMs, profile.min_send_interval_ms);
    LogInfo << "Heading-only node turn." << VAR(heading_delta) << VAR(step_count) << VAR(step_deg);
    for (int step = 0; step < step_count; ++step) {
        if (should_stop && should_stop()) {
            return false;
        }
        int units = static_cast<int>(std::lround(step_deg * ctx.action_wrapper->DefaultTurnUnitsPerDegree()));
        if (units == 0) {
            units = step_deg > 0.0 ? 1 : -1;
        }
        if (!ctx.action_wrapper->SendViewDeltaSync(units, 0)) {
            return false;
        }
        if (step + 1 < step_count) {
            utils::SleepFor(step_interval_ms);
        }
    }
    return true;
}

// 调用方须保证 ctx.position 是刚取的一帧且人已站定
void AlignCameraToHeadingOnce(const Context& ctx)
{
    if (!ctx.position->camera_angle.has_value()) {
        LogInfo << "Camera align skipped: no camera orientation.";
        return;
    }

    const double heading = ctx.position->angle;
    const double camera_before = *ctx.position->camera_angle;
    const double delta = NaviMath::CalcDeltaRotation(camera_before, heading);
    if (std::abs(delta) < kCameraAlignMinDegrees) {
        LogInfo << "Camera already aligned." << VAR(heading) << VAR(camera_before) << VAR(delta);
        return;
    }
    if (!TurnToHeadingOnce(ctx, delta)) {
        LogWarn << "Camera align turn not sent." << VAR(heading) << VAR(camera_before) << VAR(delta);
        return;
    }
    utils::SleepFor(kWaitAfterFirstTurnMs);

    const bool captured = ctx.position_provider->Capture(ctx.position, false, ctx.session->current_zone_id());
    LogInfo << "Camera aligned to navigation heading." << VAR(heading) << VAR(camera_before) << VAR(delta) << VAR(captured);
}

bool CaptureStableHeading(const Context& ctx, double* out_heading)
{
    return CaptureStableHeadingImpl(ctx, out_heading, [](int frame) { return frame < kHeadingStableReadMaxFrames; });
}

bool CaptureStableHeadingUntil(const Context& ctx, double* out_heading, std::chrono::steady_clock::time_point deadline)
{
    const auto interval = std::chrono::milliseconds(kHeadingStableReadIntervalMs);
    return CaptureStableHeadingImpl(ctx, out_heading, [&](int frame) {
        const auto now = std::chrono::steady_clock::now();
        return frame == 0 ? now < deadline : now + interval <= deadline;
    });
}

void StopMotionAndCommitment(const Context& ctx)
{
    ctx.motion_controller->SetForwardState(false);
    // 走 ActionExecutor 自行停车的 JUMP/FIGHT/普通 INTERACT 绕过这里, 须在调用处各自置位
    ctx.runtime_state->ArmCameraAlign();
}

void SelectPhaseForCurrentWaypoint(const Context& ctx, const char* reason)
{
    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        return;
    }
    ctx.session->UpdatePhase(NaviPhase::Navigate, reason);
}

bool CommitHeadingTurn(const Context& ctx, double heading_delta)
{
    if (!TurnToHeadingOnce(ctx, heading_delta)) {
        return false;
    }
    utils::SleepFor(kWaitAfterFirstTurnMs);
    ctx.action_wrapper->PulseForwardSync(kPostHeadingForwardPulseMs);
    ctx.motion_controller->SetForwardState(false);
    utils::SleepFor(kWaitAfterFirstTurnMs);
    return true;
}

double VerifyAndCorrectHeading(const Context& ctx, double target_heading, double fallback_heading)
{
    double achieved = fallback_heading;
    for (int correction = 0; correction <= kHeadingVerifyMaxRetries; ++correction) {
        double stable_heading = 0.0;
        if (!CaptureStableHeading(ctx, &stable_heading)) {
            LogWarn << "Heading verify skipped: no stable locator fix." << VAR(target_heading) << VAR(achieved);
            return achieved;
        }
        achieved = stable_heading;
        const double residual = NaviMath::NormalizeAngle(target_heading - achieved);
        if (std::abs(residual) <= kHeadingAcceptToleranceDeg) {
            return achieved;
        }
        if (correction == kHeadingVerifyMaxRetries) {
            LogWarn << "Heading retries exhausted, accepting." << VAR(target_heading) << VAR(achieved) << VAR(residual);
            return achieved;
        }
        LogInfo << "Heading off after turn, re-issuing." << VAR(target_heading) << VAR(achieved) << VAR(residual) << VAR(correction);
        if (!CommitHeadingTurn(ctx, residual)) {
            return achieved;
        }
    }
    return achieved;
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
    const auto deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(kStrictSettleBudgetMs);
    const StrictSettleInputProfile& input =
        ctx.action_wrapper->uses_touch_backend() ? kStrictSettleTouchProfile : kStrictSettleKeyboardProfile;
    int pulses = 0;
    double residual = std::numeric_limits<double>::infinity();
    const auto incomplete = [&](const char* reason) {
        LogWarn << "Strict arrival not verified." << VAR(reason) << VAR(residual) << VAR(pulses);
        return false;
    };
    const auto verified = [&]() {
        LogInfo << "Strict arrival verified after settling." << VAR(residual) << VAR(pulses) << VAR(ctx.position->x)
                << VAR(ctx.position->y);
        return true;
    };

    NaviPosition fix {};
    if (!CaptureStillFix(ctx, &fix, deadline, should_stop)) {
        return incomplete(should_stop() ? "cancelled" : "no_settled_fix");
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
        if (!ctx.action_wrapper->SupportsWalkToggle()) {
            return incomplete("walking_unavailable");
        }

        int turns = 0;
        double bearing = 0.0;
        for (;;) {
            bearing = NaviMath::CalcTargetRotation(fix.x, fix.y, waypoint.x, waypoint.y);
            const double heading_delta = NaviMath::CalcDeltaRotation(*fix.camera_angle, bearing);
            if (std::abs(heading_delta) <= kStrictSettleHeadingToleranceDeg) {
                break;
            }
            if (turns == kStrictSettleMaxHeadingTurns) {
                return incomplete("camera_not_aligned");
            }
            LogInfo << "Strict arrival aligning while stopped." << VAR(bearing) << VAR(heading_delta) << VAR(turns);
            if (should_stop() || std::chrono::steady_clock::now() >= deadline || !TurnToHeadingOnce(ctx, heading_delta, [&]() {
                    return should_stop() || std::chrono::steady_clock::now() >= deadline;
                })) {
                return incomplete(should_stop() ? "cancelled" : "view_turn_failed_or_budget_exhausted");
            }
            ++turns;
            if (!wait_for_strict_settle(std::chrono::milliseconds(kWaitAfterFirstTurnMs), deadline, should_stop)
                || !capture_strict_goal_fix(ctx, &fix, deadline, should_stop)) {
                return incomplete(should_stop() ? "cancelled" : "camera_feedback_unavailable");
            }
            // 以实际读数为准, 不用指令预期的朝向
            residual = std::hypot(waypoint.x - fix.x, waypoint.y - fix.y);
            if (residual <= kStrictSettleAcceptBandWu) {
                return verified();
            }
        }

        const int hold_ms = PlanPulseHoldMs(residual, input.max_hold_ms);
        const auto settle_reserve =
            std::chrono::milliseconds(hold_ms + input.slide_ms + kStrictSettleStillReads * kStrictSettleFixIntervalMs);
        if (should_stop() || std::chrono::steady_clock::now() + settle_reserve >= deadline) {
            return incomplete(should_stop() ? "cancelled" : "budget_exhausted");
        }
        LogInfo << "Strict arrival forward pulse." << VAR(residual) << VAR(hold_ms) << VAR(pulses);
        previous_residual = residual;
        ++pulses;
        const NaviPosition from = fix;
        ctx.motion_controller->SetForwardState(true);
        const auto pressed_at = std::chrono::steady_clock::now();
        ctx.position->valid = false; // 一开始移动停稳读数就作废
        const bool held = wait_for_strict_settle(std::chrono::milliseconds(hold_ms), deadline, should_stop);
        ctx.motion_controller->SetForwardState(false);
        const double held_ms = std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - pressed_at).count();
        if (!held || !CaptureStillFix(ctx, &fix, deadline, should_stop)) {
            return incomplete(should_stop() ? "cancelled" : "budget_exhausted");
        }

        // 挪得太少或偏得太多的这一下是被地形挡了, 不代表按键时长和位移的关系
        const double travel = std::hypot(fix.x - from.x, fix.y - from.y);
        const double deflection =
            std::abs(NaviMath::CalcDeltaRotation(bearing, NaviMath::CalcTargetRotation(from.x, from.y, fix.x, fix.y)));
        const bool usable = travel >= kStrictSettleStillWu && deflection <= kStrictSettlePulseDirectionToleranceDeg;
        if (usable) {
            RecordPulseResponse(held_ms, travel);
        }
        LogInfo << "Strict arrival pulse landed." << VAR(held_ms) << VAR(travel) << VAR(deflection) << VAR(usable);
    }
}

bool RunRecognitionNode(
    MaaContext* context,
    const std::string& node,
    const std::string& pipeline_override,
    const MaaImageBuffer* image,
    NodeSighting* out_sighting)
{
    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        LogError << "Tasker is unavailable for recognition." << VAR(node);
        return false;
    }

    const MaaRecoId reco_id = MaaContextRunRecognition(context, node.c_str(), pipeline_override.c_str(), image);
    if (reco_id == MaaInvalidId) {
        LogError << "Recognition failed to dispatch; check the node name and its params." << VAR(node);
        return false;
    }

    MaaBool hit = 0;
    MaaRect box {};
    if (!MaaTaskerGetRecognitionDetail(tasker, reco_id, nullptr, nullptr, &hit, &box, nullptr, nullptr, nullptr)) {
        LogError << "Recognition detail is unavailable." << VAR(node) << VAR(reco_id);
        return false;
    }

    out_sighting->hit = hit != 0;
    out_sighting->box = box;
    return true;
}

std::optional<std::vector<std::string>>
    RunTaskForCompletedNodes(MaaContext* context, const char* entry, const std::string& pipeline_override)
{
    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        return std::nullopt;
    }
    const MaaTaskId task_id = MaaContextRunTask(context, entry, pipeline_override.c_str());
    if (task_id == MaaInvalidId) {
        LogWarn << "Subtask failed to dispatch." << VAR(entry);
        return std::nullopt;
    }

    ScopedStringBuffer entry_name;
    MaaSize node_count = 0;
    MaaStatus status = MaaStatus_Invalid;
    if (entry_name.Get() == nullptr || !MaaTaskerGetTaskDetail(tasker, task_id, entry_name.Get(), nullptr, &node_count, &status)) {
        return std::nullopt;
    }
    std::vector<std::string> completed_nodes;
    if (node_count == 0) {
        return completed_nodes;
    }
    std::vector<MaaNodeId> node_ids(node_count);
    if (!MaaTaskerGetTaskDetail(tasker, task_id, entry_name.Get(), node_ids.data(), &node_count, &status)) {
        return std::nullopt;
    }

    for (const MaaNodeId node_id : node_ids) {
        ScopedStringBuffer node_name;
        MaaRecoId reco_id = 0;
        MaaActId action_id = 0;
        MaaBool completed = 0;
        if (node_name.Get() == nullptr || !MaaTaskerGetNodeDetail(tasker, node_id, node_name.Get(), &reco_id, &action_id, &completed)) {
            return std::nullopt;
        }
        const char* raw = MaaStringBufferGet(node_name.Get());
        if (raw != nullptr && completed != 0) {
            completed_nodes.emplace_back(raw);
        }
    }
    return completed_nodes;
}

bool CaptureFreshFrame(MaaController* controller, MaaImageBuffer* buffer)
{
    const MaaCtrlId screencap_id = MaaControllerPostScreencap(controller);
    if (screencap_id == MaaInvalidId) {
        LogWarn << "Screencap request was not posted.";
        return false;
    }
    if (MaaControllerWait(controller, screencap_id) != MaaStatus_Succeeded) {
        LogWarn << "Screencap did not succeed." << VAR(screencap_id);
        return false;
    }
    return MaaControllerCachedImage(controller, buffer) && !MaaImageBufferIsEmpty(buffer);
}

Result TickSemanticFlow(const Context& ctx, NaviPhase phase)
{
    if (phase == NaviPhase::WaitTransfer) {
        return TickTransferWaitImpl(ctx);
    }
    if (phase == NaviPhase::WaitZipline) {
        return TickZiplineRide(ctx);
    }
    if (phase == NaviPhase::WaitFind) {
        return TickFindTarget(ctx);
    }
    if (phase == NaviPhase::WaitJumpPad) {
        return TickJumpPad(ctx);
    }
    if (phase == NaviPhase::WaitRope) {
        return TickRopeRideImpl(ctx);
    }
    if (ctx.runtime_state->semantic.portal_transit_active) {
        return TickPortalTransit(ctx);
    }
    return {};
}

Result ConsumeInlineSemantics(const Context& ctx)
{
    Result result;

    ConsumeMatchedZoneNodes(ctx);
    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    Result heading_result = ConsumeHeadingNodesImpl(ctx);
    if (heading_result.consumed) {
        ConsumeMatchedZoneNodes(ctx);
        return heading_result;
    }

    Result find_result = ConsumeFindNodes(ctx);
    if (find_result.consumed) {
        return find_result;
    }

    if (ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().IsZoneDeclaration()) {
        ctx.motion_controller->SetForwardState(true);
        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    return result;
}

Result CompleteArrival(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason)
{
    ctx.session->NoteCanonicalFinalGoalConsumed(node_idx, *ctx.position, reason);
    ctx.session->AdvanceToNextWaypoint(waypoint.action, reason);
    ctx.runtime_state->OnWaypointAdvance();
    SelectPhaseForCurrentWaypoint(ctx, reason);
    return { .consumed = true, .stay_in_current_tick = true };
}

namespace
{

Result ArriveTransfer(const Context& ctx, const std::optional<size_t>& node_idx, double actual_distance)
{
    StopMotionAndCommitment(ctx);
    ctx.session->NoteCanonicalFinalGoalConsumed(node_idx, *ctx.position, "transfer_wait_started");
    ctx.session->AdvanceToNextWaypoint(ActionType::TRANSFER, "transfer_wait_started");
    ctx.runtime_state->OnWaypointAdvance();
    ctx.runtime_state->semantic.transfer_anchor_pos = *ctx.position;
    ctx.runtime_state->semantic.transfer_wait_started = std::chrono::steady_clock::now();
    ctx.runtime_state->semantic.transfer_stable_hits = 0;
    ctx.position_provider->ResetTracking();
    LogInfo << "Action: TRANSFER reached." << VAR(actual_distance);

    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.runtime_state->semantic.transfer_wait_started = {};
        ctx.runtime_state->semantic.transfer_anchor_pos = {};
        ctx.runtime_state->semantic.transfer_stable_hits = 0;
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
    }
    else {
        ctx.session->UpdatePhase(NaviPhase::WaitTransfer, "transfer_wait_started");
    }
    return { .consumed = true, .stay_in_current_tick = true };
}

Result ArrivePortal(const Context& ctx, const std::optional<size_t>& node_idx, double actual_distance)
{
    ctx.session->NoteCanonicalFinalGoalConsumed(node_idx, *ctx.position, "portal_entered");
    ctx.session->AdvanceToNextWaypoint(ActionType::PORTAL, "portal_entered");
    ctx.runtime_state->OnWaypointAdvance();
    ctx.runtime_state->semantic.portal_transit_active = true;
    ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix = true;
    ctx.runtime_state->semantic.portal_transit_needs_reacquire = false;
    ctx.runtime_state->semantic.portal_transit_started = std::chrono::steady_clock::now();
    ctx.position_provider->ResetTracking();
    ctx.motion_controller->SetForwardState(true);
    LogInfo << "Action: PORTAL entered transit flow." << VAR(actual_distance);

    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.runtime_state->semantic.portal_transit_active = false;
        ctx.runtime_state->semantic.portal_transit_keep_moving_until_fix = false;
        ctx.runtime_state->semantic.portal_transit_needs_reacquire = false;
        ctx.runtime_state->semantic.portal_transit_started = {};
    }
    SelectPhaseForCurrentWaypoint(ctx, "portal_entered");
    return { .consumed = true, .stay_in_current_tick = true };
}

Result ArriveDig(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance)
{
    StopMotionAndCommitment(ctx);
    if (ctx.maa_context == nullptr) {
        LogError << "Action: DIG triggered but maa_context is null." << VAR(actual_distance);
        return { .request_failure = true,
                 .failure_reason = "dig_context_missing",
                 .failure_log_message = "MaaContext is null when dispatching dig subtask." };
    }

    LogInfo << "Action: DIG triggered, dispatching subtask." << VAR(kDefaultDigEntry) << VAR(actual_distance);
    const MaaTaskId sub_id = MaaContextRunTask(ctx.maa_context, kDefaultDigEntry, kDigPipelineOverride);
    if (sub_id == MaaInvalidId) {
        LogError << "Action: DIG subtask failed to dispatch." << VAR(kDefaultDigEntry) << VAR(actual_distance);
        return { .request_failure = true,
                 .failure_reason = "dig_dispatch_failed",
                 .failure_log_message = "MaaContextRunTask returned MaaInvalidId for dig subtask." };
    }

    LogInfo << "Action: DIG subtask returned." << VAR(sub_id);
    utils::SleepFor(kDigPostSleepMs);
    ctx.runtime_state->route.Reset();
    return CompleteArrival(ctx, waypoint, node_idx, "dig_completed");
}

Result ArriveInteract(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance)
{
    if (waypoint.IsAsyncInteract() && ctx.maa_context != nullptr) {
        StopMotionAndCommitment(ctx);
        LogInfo << "Action: INTERACT reached, running the authoritative recognition." << VAR(actual_distance)
                << VAR(waypoint.interact_text.size()) << VAR(waypoint.interact_scan) << VAR(waypoint.interact_rec);
        const bool recognized = RunPromptSubtask(ctx.maa_context, kInteractPromptSpec, &waypoint.interact_text, waypoint.interact_rec);
        InteractApproachState& approach = ctx.runtime_state->interact_approach;
        if (!recognized && waypoint.HasPosition() && node_idx && !approach.PromptMissedAt(node_idx)
            && actual_distance > kStrictSettleAcceptBandWu) {
            approach.prompt_missed_node = node_idx;
            approach.missed_at = std::chrono::steady_clock::now();
            // 起步按已确认算, 否则要先挪够起步距离才放行到点判定
            ctx.runtime_state->route.Reset();
            ctx.runtime_state->route.startup_anchor_pos = *ctx.position;
            ctx.runtime_state->route.startup_anchor_initialized = true;
            ctx.runtime_state->route.startup_motion_confirmed = true;
            ctx.session->ResetHardProgress();
            LogInfo << "Action: INTERACT prompt missed, walking on onto the point." << VAR(actual_distance) << VAR(*node_idx);
            return { .consumed = true };
        }
        ctx.runtime_state->route.Reset();
        if (!recognized && waypoint.interact_rec) {
            LogWarn << "Action: INTERACT prompt still missing in rec mode." << VAR(actual_distance) << VAR(waypoint.interact_text_node);
            return { .request_failure = true,
                     .failure_reason = "interact_prompt_missing",
                     .failure_log_message = "Interact prompt not recognized on the point." };
        }
        if (waypoint.rope_ride) {
            const RopeRideInfo ride = *waypoint.rope_ride;
            if (!recognized) {
                LogWarn << "Action: rope prompt missing at the bottom." << VAR(actual_distance) << VAR(ride.uid);
                return { .consumed = true, .rope_unavailable = ride.uid };
            }
            CompleteArrival(ctx, waypoint, node_idx, "rope_interact_pressed");
            return StartRopeRide(ctx, ride);
        }
        return CompleteArrival(ctx, waypoint, node_idx, "async_interact_completed");
    }

    // 绳底没有提示文字就确认不了坐没坐上, 当这根用不了
    if (waypoint.rope_ride) {
        return { .consumed = true, .rope_unavailable = waypoint.rope_ride->uid };
    }
    // rec 模式到这里说明文本没解析出来, 不能按交互键
    if (waypoint.IsRecInteract()) {
        LogInfo << "Action: INTERACT in rec mode, skipping the key press." << VAR(waypoint.interact_text_node);
    }
    else {
        ctx.runtime_state->ArmCameraAlign();
        ctx.action_executor->Interact();
    }
    return CompleteArrival(ctx, waypoint, node_idx, "waypoint_action_completed");
}

} // namespace

Result StartRopeRide(const Context& ctx, const RopeRideInfo& ride)
{
    StopMotionAndCommitment(ctx);
    SemanticState& s = ctx.runtime_state->semantic;
    s.rope_ride_started = std::chrono::steady_clock::now();
    s.rope_ride_ms = static_cast<int64_t>(std::ceil(ride.ride_s * 1000.0));
    s.rope_stable_hits = 0;
    ctx.position_provider->ResetTracking();
    LogInfo << "Rope ride started." << VAR(ride.uid) << VAR(ride.ride_s) << VAR(ride.top_height);
    if (!ctx.session->HasCurrentWaypoint() || !ctx.session->CurrentWaypoint().rope_top) {
        LogWarn << "Rope ride started without a top landing as the next point." << VAR(ride.uid);
    }
    ctx.session->UpdatePhase(NaviPhase::WaitRope, "rope_ride_started");
    return { .consumed = true, .stay_in_current_tick = true };
}

Result HandleArrival(const Context& ctx, const Waypoint& waypoint, double actual_distance)
{
    const std::optional<size_t> node_idx = ctx.session->CurrentAbsoluteNodeIndex();
    if (waypoint.RequiresStrictArrival() && ctx.motion_controller->IsMoving()) {
        StopMotionAndCommitment(ctx);
        utils::SleepFor(kStopWaitMs);
    }

    switch (waypoint.action) {
    case ActionType::TRANSFER:
        return ArriveTransfer(ctx, node_idx, actual_distance);
    case ActionType::JUMP_PAD:
        return ArriveJumpPad(ctx, waypoint, node_idx, actual_distance);
    case ActionType::PORTAL:
        return ArrivePortal(ctx, node_idx, actual_distance);
    case ActionType::ZIPLINE:
        return StartZiplineHop(ctx, waypoint, actual_distance);
    case ActionType::DIG:
        return ArriveDig(ctx, waypoint, node_idx, actual_distance);
    case ActionType::FIND:
        return ArriveFind(ctx, waypoint, actual_distance);
    case ActionType::TRIGGER:
        return ArriveTrigger(ctx, waypoint, actual_distance);
    case ActionType::INTERACT:
        return ArriveInteract(ctx, waypoint, node_idx, actual_distance);
    case ActionType::SPRINT:
        ctx.action_executor->Sprint();
        break;
    case ActionType::JUMP:
        ctx.runtime_state->ArmCameraAlign();
        ctx.action_executor->Jump();
        break;
    case ActionType::FIGHT:
        ctx.runtime_state->ArmCameraAlign();
        ctx.action_executor->Fight();
        break;
    // HEADING/ZONE 没坐标, NAVMESH 展开后换成规划点, 实际到不了这里
    case ActionType::RUN:
    case ActionType::COLLECT:
    case ActionType::HEADING:
    case ActionType::NAVMESH:
    case ActionType::ZONE:
        break;
    }
    return CompleteArrival(ctx, waypoint, node_idx, "waypoint_action_completed");
}

} // namespace semantic_nodes

} // namespace mapnavigator
