#include "heading_turn.h"

#include <algorithm>
#include <cmath>
#include <optional>

#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../sensing/position_provider.h"

namespace mapnavigator
{

namespace semantic_nodes
{

bool TurnToHeadingOnce(const Context& ctx, double heading_delta, const std::function<bool()>& should_stop)
{
    if (std::abs(heading_delta) <= 1.0) {
        return true;
    }

    const SteeringTransportProfile profile = ctx.action_wrapper->steeringProfile();
    const int step_count = static_cast<int>(std::ceil(std::abs(heading_delta) / profile.max_batch_delta_deg));
    const double step_deg = heading_delta / step_count;
    const int step_interval_ms = std::max<int>(kHeadingTurnStepIntervalMs, profile.min_send_interval_ms);
    LogInfo << "Heading-only node turn." << VAR(heading_delta) << VAR(step_count) << VAR(step_deg);
    for (int step = 0; step < step_count; ++step) {
        if (should_stop && should_stop()) {
            return false;
        }
        int units = static_cast<int>(std::lround(step_deg * ctx.action_wrapper->defaultTurnUnitsPerDegree()));
        if (units == 0) {
            units = step_deg > 0.0 ? 1 : -1;
        }
        if (!ctx.action_wrapper->sendViewDeltaSync(units, 0)) {
            return false;
        }
        if (step + 1 < step_count) {
            SleepFor(step_interval_ms);
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
    const double delta = NaviMath::calcDeltaRotation(camera_before, heading);
    if (std::abs(delta) < kCameraAlignMinDegrees) {
        LogInfo << "Camera already aligned." << VAR(heading) << VAR(camera_before) << VAR(delta);
        return;
    }
    if (!TurnToHeadingOnce(ctx, delta)) {
        LogWarn << "Camera align turn not sent." << VAR(heading) << VAR(camera_before) << VAR(delta);
        return;
    }
    SleepFor(kWaitAfterFirstTurnMs);

    const bool captured = ctx.position_provider->capture(ctx.position, false, ctx.session->currentZoneId());
    LogInfo << "Camera aligned to navigation heading." << VAR(heading) << VAR(camera_before) << VAR(delta) << VAR(captured);
}

bool CaptureStableHeading(const Context& ctx, double* out_heading)
{
    std::optional<double> previous;
    for (int frame = 0; frame < kHeadingStableReadMaxFrames; ++frame) {
        if (frame > 0) {
            SleepFor(kHeadingStableReadIntervalMs);
        }
        if (!ctx.position_provider->capture(ctx.position, false, ctx.session->currentZoneId())) {
            previous.reset();
            continue;
        }
        const double current = NaviMath::normalizeAngle(ctx.position->angle);
        if (previous && std::abs(NaviMath::normalizeAngle(current - *previous)) <= kHeadingStableReadToleranceDeg) {
            *out_heading = current;
            return true;
        }
        previous = current;
    }
    return false;
}

bool CommitHeadingTurn(const Context& ctx, double heading_delta)
{
    if (!TurnToHeadingOnce(ctx, heading_delta)) {
        return false;
    }
    SleepFor(kWaitAfterFirstTurnMs);
    ctx.action_wrapper->pulseForwardSync(kPostHeadingForwardPulseMs);
    ctx.motion_controller->setForwardState(false);
    SleepFor(kWaitAfterFirstTurnMs);
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
        const double residual = NaviMath::normalizeAngle(target_heading - achieved);
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

Result ConsumeHeadingNodes(const Context& ctx)
{
    bool consumed = false;
    while (ctx.session->hasCurrentWaypoint() && ctx.session->currentWaypoint().isHeadingOnly()) {
        const Waypoint heading_node = ctx.session->currentWaypoint();
        double target_heading = 0.0;
        if (heading_node.heading_uses_target) {
            target_heading = NaviMath::calcTargetRotation(ctx.position->x, ctx.position->y, heading_node.x, heading_node.y);
        }
        else {
            target_heading = std::fmod(heading_node.heading_angle, 360.0);
            if (target_heading < 0.0) {
                target_heading += 360.0;
            }
        }

        const double start_heading = NaviMath::normalizeAngle(ctx.position->angle);
        const double heading_delta = NaviMath::normalizeAngle(target_heading - start_heading);

        ctx.motion_controller->setForwardState(false);
        SleepFor(kStopWaitMs);

        if (std::abs(heading_delta) <= 1.0) {
            LogInfo << "Heading-only node already aligned." << VAR(target_heading) << VAR(start_heading);
            ctx.action_wrapper->pulseForwardSync(kPostHeadingForwardPulseMs);
            ctx.motion_controller->setForwardState(false);
        }
        else if (!CommitHeadingTurn(ctx, heading_delta)) {
            // 不推进, 下一拍重试
            return {};
        }

        const double achieved_heading = VerifyAndCorrectHeading(ctx, target_heading, start_heading);
        LogInfo << "Heading-only node completed." << VAR(target_heading) << VAR(start_heading) << VAR(heading_delta)
                << VAR(achieved_heading);
        ctx.session->advanceToNextWaypoint(ActionType::HEADING, "heading_consumed");
        ctx.session->resetProgress();
        ctx.runtime_state->onWaypointAdvance();
        consumed = true;

        if (!ctx.session->hasCurrentWaypoint()) {
            ctx.session->noteRouteTailConsumed(*ctx.position, "heading_route_consumed");
        }
        // 后续节点等下一拍重新取位, 免得用到缺失的朝向
        if (!ctx.position->valid) {
            break;
        }
    }
    return consumed ? Handled() : Result {};
}

} // namespace semantic_nodes

} // namespace mapnavigator
