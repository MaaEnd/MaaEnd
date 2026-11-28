#include "heading_turn.h"

#include <algorithm>
#include <cmath>
#include <optional>

#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
#include "../control/motion_controller.h"
#include "../control/view_turn.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../process_state.h"
#include "../sensing/position_provider.h"
#include "../state_machine/navigation_state_machine.h"

namespace mapnavigator
{

namespace semantic_nodes
{

std::optional<HeadingTurnSteps> PlanHeadingTurn(const InputBackend& input_backend, double heading_delta)
{
    if (std::abs(heading_delta) <= 1.0) {
        return std::nullopt;
    }

    const SteeringTransportProfile profile = SteeringProfileOf(input_backend);
    const int step_count = static_cast<int>(std::ceil(std::abs(heading_delta) / profile.max_batch_delta_deg));
    const double step_deg = heading_delta / step_count;
    const int step_interval_ms = std::max<int>(kHeadingTurnStepIntervalMs, profile.min_send_interval_ms);
    LogInfo << "Heading-only node turn." << VAR(heading_delta) << VAR(step_count) << VAR(step_deg);
    return HeadingTurnSteps { .step_count = step_count, .step_deg = step_deg, .step_interval_ms = step_interval_ms };
}

bool SendHeadingTurnStep(InputBackend* input_backend, sensitivity::SensitivityRun* sensitivity, const HeadingTurnSteps& turn, int step)
{
    const int units = YawUnitsAtLeastOne(*input_backend, *sensitivity, turn.step_deg);
    if (!SendYawDeltaAndNoteTurn(input_backend, sensitivity, units, 0)) {
        return false;
    }
    if (step + 1 < turn.step_count) {
        SleepFor(turn.step_interval_ms);
    }
    return true;
}

bool TurnToHeadingOnce(Navigator* nav, double heading_delta)
{
    const std::optional<HeadingTurnSteps> turn = PlanHeadingTurn(*nav->input_backend, heading_delta);
    if (!turn) {
        return true;
    }
    for (int step = 0; step < turn->step_count; ++step) {
        if (!SendHeadingTurnStep(nav->input_backend, &nav->process->sensitivity, *turn, step)) {
            return false;
        }
    }
    return true;
}

// 调用方须保证 nav->position 是刚取的一帧且人已站定
void AlignCameraAndRecapture(Navigator* nav)
{
    if (!nav->position->camera_angle.has_value()) {
        LogInfo << "Camera align skipped: no camera orientation.";
        return;
    }

    const double heading = nav->position->angle;
    const double camera_before = *nav->position->camera_angle;
    const double delta = CalcDeltaRotation(camera_before, heading);
    if (std::abs(delta) < kCameraAlignMinDegrees) {
        LogInfo << "Camera already aligned." << VAR(heading) << VAR(camera_before) << VAR(delta);
        return;
    }
    if (!TurnToHeadingOnce(nav, delta)) {
        LogWarn << "Camera align turn not sent." << VAR(heading) << VAR(camera_before) << VAR(delta);
        return;
    }
    SleepFor(kWaitAfterFirstTurnMs);

    const bool captured = CaptureTracked(CapturePartsOf(*nav), nav->position, nav->session->current_zone_id);
    LogInfo << "Camera aligned to navigation heading." << VAR(heading) << VAR(camera_before) << VAR(delta) << VAR(captured);
}

bool CaptureStableHeading(Navigator* nav, double* out_heading)
{
    std::optional<double> previous;
    for (int frame = 0; frame < kHeadingStableReadMaxFrames; ++frame) {
        if (frame > 0) {
            SleepFor(kHeadingStableReadIntervalMs);
        }
        if (!CaptureTracked(CapturePartsOf(*nav), nav->position, nav->session->current_zone_id)) {
            previous.reset();
            continue;
        }
        const double current = NormalizeAngle(nav->position->angle);
        if (previous && std::abs(NormalizeAngle(current - *previous)) <= kHeadingStableReadToleranceDeg) {
            *out_heading = current;
            return true;
        }
        previous = current;
    }
    return false;
}

bool TurnAndPulseForward(Navigator* nav, double heading_delta)
{
    if (!TurnToHeadingOnce(nav, heading_delta)) {
        return false;
    }
    SleepFor(kWaitAfterFirstTurnMs);
    PulseForwardSync(nav->input_backend, kPostHeadingForwardPulseMs);
    HoldPosition(nav->motion_controller);
    SleepFor(kWaitAfterFirstTurnMs);
    return true;
}

double VerifyAndCorrectHeading(Navigator* nav, double target_heading, double fallback_heading)
{
    double achieved = fallback_heading;
    for (int correction = 0; correction <= kHeadingVerifyMaxRetries; ++correction) {
        double stable_heading = 0.0;
        if (!CaptureStableHeading(nav, &stable_heading)) {
            LogWarn << "Heading verify skipped: no stable locator fix." << VAR(target_heading) << VAR(achieved);
            return achieved;
        }
        achieved = stable_heading;
        const double residual = NormalizeAngle(target_heading - achieved);
        if (std::abs(residual) <= kHeadingAcceptToleranceDeg) {
            return achieved;
        }
        if (correction == kHeadingVerifyMaxRetries) {
            LogWarn << "Heading retries exhausted, accepting." << VAR(target_heading) << VAR(achieved) << VAR(residual);
            return achieved;
        }
        LogInfo << "Heading off after turn, re-issuing." << VAR(target_heading) << VAR(achieved) << VAR(residual) << VAR(correction);
        if (!TurnAndPulseForward(nav, residual)) {
            return achieved;
        }
    }
    return achieved;
}

Result ConsumeHeadingNodes(Navigator* nav)
{
    bool consumed = false;
    while (HasCurrentWaypoint(*nav->session) && IsHeadingOnly(CurrentWaypoint(*nav->session))) {
        const Waypoint heading_node = CurrentWaypoint(*nav->session);
        double target_heading = 0.0;
        if (heading_node.heading_uses_target) {
            target_heading = CalcTargetRotation(nav->position->x, nav->position->y, heading_node.x, heading_node.y);
        }
        else {
            target_heading = std::fmod(heading_node.heading_angle, 360.0);
            if (target_heading < 0.0) {
                target_heading += 360.0;
            }
        }

        const double start_heading = NormalizeAngle(nav->position->angle);
        const double heading_delta = NormalizeAngle(target_heading - start_heading);

        HoldPosition(nav->motion_controller);
        SleepFor(kStopWaitMs);

        if (std::abs(heading_delta) <= 1.0) {
            LogInfo << "Heading-only node already aligned." << VAR(target_heading) << VAR(start_heading);
            PulseForwardSync(nav->input_backend, kPostHeadingForwardPulseMs);
            HoldPosition(nav->motion_controller);
        }
        else if (!TurnAndPulseForward(nav, heading_delta)) {
            // 不推进, 下一拍重试
            return {};
        }

        const double achieved_heading = VerifyAndCorrectHeading(nav, target_heading, start_heading);
        LogInfo << "Heading-only node completed." << VAR(target_heading) << VAR(start_heading) << VAR(heading_delta)
                << VAR(achieved_heading);
        LeaveCurrentWaypoint(nav->session, ActionType::HEADING, "heading_consumed");
        ResetProgress(nav->session);
        ResetOnWaypointAdvance(&nav->runtime_state);
        consumed = true;

        if (!HasCurrentWaypoint(*nav->session)) {
            ConcludeRouteTail(nav->session, *nav->position, "heading_route_consumed");
        }
        // 后续节点等下一拍重新取位, 免得用到缺失的朝向
        if (!nav->position->valid) {
            break;
        }
    }
    return consumed ? Handled() : Result {};
}

} // namespace semantic_nodes

} // namespace mapnavigator
