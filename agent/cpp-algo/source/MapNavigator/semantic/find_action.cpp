#include "find_action.h"

#include <chrono>
#include <cmath>
#include <cstdlib>
#include <optional>
#include <string>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../Backend/backend.h"
#include "../control/controller_type_utils.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_time.h"
#include "../sensing/maa_task.h"
#include "../sensing/pipeline_node_reader.h"
#include "../sensing/position_provider.h"
#include "../sensing/prompt_scan_profile.h"
#include "../sensing/roi_template_scanner.h"
#include "../state_machine/navigation_state_machine.h"
#include "heading_turn.h"
#include "semantic_helpers.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

std::string build_inline_text_override(const std::vector<std::string>& texts)
{
    json::object root;
    root[kFindInlineOcrNode] = ExpectedTextOverride(texts);
    return json::value(std::move(root)).dumps();
}

// 不拷贝像素, 只在 buffer 下次截图前有效
cv::Mat frame_as_mat(const MaaImageBuffer* buffer)
{
    return { MaaImageBufferHeight(buffer), MaaImageBufferWidth(buffer), MaaImageBufferType(buffer), MaaImageBufferGetRawData(buffer) };
}

bool capture_and_check_arrive_point(Navigator* nav, const Waypoint& waypoint)
{
    if (!waypoint.find_arrive.has_value()) {
        return false;
    }
    const bool captured = CaptureTracked(CapturePartsOf(*nav), nav->position, nav->session->current_zone_id);
    if (!captured || nav->position_provider->last_capture_was_black_screen) {
        return false;
    }

    const double dx = nav->position->x - waypoint.find_arrive->at(0);
    const double dy = nav->position->y - waypoint.find_arrive->at(1);
    const double distance = std::hypot(dx, dy);
    LogDebug << "FIND: arrive point distance." << VAR(distance) << VAR(kStrictArrivalLookaheadRadius);
    return distance <= kStrictArrivalLookaheadRadius;
}

// 这一段转视角是开环的, 不清掉操舵状态下一段会拿旧朝向操舵
void stop_and_reset_find(Navigator* nav)
{
    HoldPosition(nav->motion_controller);
    ResetNavigationAssistState(&nav->runtime_state);
    ResetStallCounters(&nav->runtime_state.flow);
    ResetSensorTracking(nav->position_provider);
    ResetProgress(nav->session);
    nav->runtime_state.find = {};
}

// consumed 也要置位, 走路探测靠它把失败转给 TickFindTarget
Result fail_find(Navigator* nav, const char* reason, const char* message)
{
    LogError << message << VAR(reason) << VAR(nav->runtime_state.find.steps);
    stop_and_reset_find(nav);
    return ConsumedFailure(reason, message);
}

Result finish_find(Navigator* nav, const Waypoint& waypoint, const char* reason)
{
    stop_and_reset_find(nav);
    CompleteArrival(nav, waypoint, CurrentAbsoluteNodeIndex(*nav->session), reason);
    return Handled();
}

Result hold_find_step()
{
    SleepFor(kFindStepSleepMs);
    return Handled();
}

void arm_stop_probe(Navigator* nav, const Waypoint& waypoint, FindState* find)
{
    find->stop_probe.reset();
    if (waypoint.find_stop.empty() || nav->maa_context == nullptr) {
        return;
    }
    const std::string stop_type = ReadNodeRecognitionType(nav->maa_context, waypoint.find_stop);
    if (!EqualsIgnoreCase(stop_type, "TemplateMatch")) {
        LogInfo << "FIND stop node has no template pre-filter; probes run it directly." << VAR(waypoint.find_stop) << VAR(stop_type);
        return;
    }
    PromptScanProfile profile;
    if (!TryLoadPromptScanProfile(nav->maa_context, waypoint.find_stop, nav->input_backend->controller_type.c_str(), &profile)) {
        return;
    }
    find->stop_probe = std::move(profile);
}

Result begin_find(Navigator* nav, const char* reason)
{
    HoldAndArmCameraAlign(nav);
    SleepFor(kStopWaitMs);

    FindState& find = nav->runtime_state.find;
    find = {};
    find.started_at = std::chrono::steady_clock::now();
    UpdatePhase(nav->session, NaviPhase::WaitFind, reason);

    const Waypoint& waypoint = CurrentWaypoint(*nav->session);
    arm_stop_probe(nav, waypoint, &find);
    LogInfo << "Action: FIND started." << VAR(reason) << VAR(waypoint.find_target) << VAR(waypoint.find_text.size())
            << VAR(waypoint.find_stop);
    return Handled();
}

// 保持同向转, 否则正后方的目标会让镜头来回摆
bool search_one_step(Navigator* nav, const FindState& find)
{
    const int32_t sign = find.last_seen_side != 0 ? find.last_seen_side : 1;
    const double step_deg = kFindSearchStepDeg * static_cast<double>(sign);
    LogDebug << "FIND: target is not on screen, searching." << VAR(find.steps) << VAR(step_deg) << VAR(find.miss_streak);
    return TurnToHeadingOnce(nav, step_deg);
}

bool approach_sighting(Navigator* nav, FindState* find, const MaaRect& box, int32_t frame_width, int32_t frame_height)
{
    const int32_t center_x = box.x + box.width / 2;
    const int32_t center_y = box.y + box.height / 2;
    find->last_seen_side = center_x >= frame_width / 2 ? 1 : -1;
    const int32_t offset_px = center_x - frame_width / 2;
    // 判定用 1280 基准帧像素, 角度换算用真实帧宽
    const int32_t offset_base =
        static_cast<int32_t>(std::lround(static_cast<double>(offset_px) * kPipelineRoiBaseWidth / static_cast<double>(frame_width)));

    if (std::abs(offset_base) > kFindAlignTolerancePx) {
        const bool walk_through = std::abs(offset_base) <= kFindWalkWhileTurningPx;
        if (!walk_through) {
            HoldPosition(nav->motion_controller);
            // 停下也算移动指令, 紧接着的转向会被后端静默期吞掉
            SleepFor(SteeringProfileOf(*nav->input_backend).action_quiet_period_ms);
        }
        const double degrees_per_px = kTurnDegreesPerCircle / static_cast<double>(ComputeTurn360Units(frame_width));
        const double yaw_deg = static_cast<double>(offset_px) * degrees_per_px * kFindSteerGain;
        LogDebug << "FIND: turning toward the target." << VAR_AS(find.steps, find->steps) << VAR(offset_px) << VAR(yaw_deg)
                 << VAR(walk_through);
        const bool turned = TurnToHeadingOnce(nav, yaw_deg);
        if (walk_through) {
            MoveForward(nav->motion_controller);
        }
        return turned;
    }

    if (static_cast<double>(center_y) > static_cast<double>(frame_height) * kFindPassedCenterYRatio) {
        LogDebug << "FIND: target is behind, stepping back." << VAR_AS(find.steps, find->steps) << VAR(center_y);
        HoldPosition(nav->motion_controller);
        SetMovementStateSync(nav->input_backend, { .backward = true }, kFindBackwardPulseMs);
        SetMovementStateSync(nav->input_backend, {}, 0);
        return true;
    }

    LogDebug << "FIND: walking toward the target." << VAR_AS(find.steps, find->steps) << VAR(offset_px);
    MoveForward(nav->motion_controller);
    return true;
}

// 提示窗口可能夹在两拍之间, 所以走路时要更密地探测停车判据
Result probe_stop_while_walking(Navigator* nav, const Waypoint& waypoint, MaaController* controller)
{
    FindState& find = nav->runtime_state.find;
    if (waypoint.find_stop.empty() || controller == nullptr || !nav->motion_controller->is_moving_forward) {
        return {};
    }

    LogDebug << "FIND: probing the stop node while walking." << VAR(find.steps) << VAR(waypoint.find_stop)
             << VAR(find.stop_probe.has_value());
    const auto deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(kFindStopProbeWindowMs);
    while (nav->motion_controller->is_moving_forward && std::chrono::steady_clock::now() < deadline) {
        ScopedImageBuffer probe;
        if (!CaptureFreshFrame(controller, probe.Get())) {
            break;
        }
        bool flagged = true;
        if (find.stop_probe.has_value()) {
            const PromptScanProfile& profile = *find.stop_probe;
            flagged = MatchTemplateOnFrame(frame_as_mat(probe.Get()), profile.base_roi, profile.icons, "find-stop");
        }
        if (!flagged) {
            SleepFor(kFindStopProbeIntervalMs);
            continue;
        }
        // 先站住再确认, 走着确认会多滑出去一段
        HoldPosition(nav->motion_controller);
        SleepFor(kStopWaitMs);
        NodeSighting sighting {};
        if (!RunRecognitionNode(nav->maa_context, waypoint.find_stop, "{}", probe.Get(), &sighting)) {
            return fail_find(nav, "find_recognition_failed", "FIND stop node failed to recognize.");
        }
        if (sighting.hit) {
            LogInfo << "FIND: stop node hit while walking, target reached." << VAR(waypoint.find_stop) << VAR(find.steps);
            return finish_find(nav, waypoint, "find_target_reached");
        }
        LogDebug << "FIND: stop probe did not confirm; resuming the walk." << VAR(find.steps);
        MoveForward(nav->motion_controller);
        SleepFor(kFindStopProbeIntervalMs);
    }
    return {};
}

std::optional<Result> finish_find_if_done(Navigator* nav, const Waypoint& waypoint, const MaaImageBuffer* image)
{
    const FindState& find = nav->runtime_state.find;
    if (!waypoint.find_stop.empty()) {
        NodeSighting stop_sighting {};
        if (!RunRecognitionNode(nav->maa_context, waypoint.find_stop, "{}", image, &stop_sighting)) {
            return fail_find(nav, "find_recognition_failed", "FIND stop node failed to recognize.");
        }
        if (stop_sighting.hit) {
            LogInfo << "FIND: stop node hit, target reached." << VAR(waypoint.find_stop) << VAR(find.steps) << VAR(waypoint.x)
                    << VAR(waypoint.y);
            return finish_find(nav, waypoint, "find_target_reached");
        }
    }

    if (capture_and_check_arrive_point(nav, waypoint)) {
        LogInfo << "FIND: arrive point reached." << VAR(find.steps) << VAR(waypoint.find_arrive->at(0)) << VAR(waypoint.find_arrive->at(1));
        return finish_find(nav, waypoint, "find_arrive_reached");
    }
    return std::nullopt;
}

Result handle_target_miss(Navigator* nav, FindState* find)
{
    HoldPosition(nav->motion_controller);
    ++find->miss_streak;
    // 见过的目标漏认几拍先原地等, 从没见过则直接搜
    if (find->last_seen_side != 0 && find->miss_streak < kFindMissGraceTicks) {
        LogDebug << "FIND: target lost for a beat, holding." << VAR_AS(find.steps, find->steps)
                 << VAR_AS(find.miss_streak, find->miss_streak);
        return hold_find_step();
    }
    if (!search_one_step(nav, *find)) {
        return fail_find(nav, "find_turn_failed", "FIND failed to issue the search turn.");
    }
    return hold_find_step();
}

Result pursue_target(Navigator* nav, const Waypoint& waypoint, MaaController* controller, const MaaImageBuffer* image)
{
    FindState& find = nav->runtime_state.find;
    const bool inline_text = waypoint.find_target.empty();
    if (inline_text && waypoint.find_text.empty()) {
        return fail_find(nav, "find_target_missing", "FIND point names neither a target node nor a text list.");
    }
    const std::string target_node = inline_text ? std::string(kFindInlineOcrNode) : waypoint.find_target;
    const std::string target_override = inline_text ? build_inline_text_override(waypoint.find_text) : std::string("{}");

    NodeSighting target_sighting {};
    if (!RunRecognitionNode(nav->maa_context, target_node, target_override, image, &target_sighting)) {
        return fail_find(nav, "find_recognition_failed", "FIND target node failed to recognize.");
    }
    if (!target_sighting.hit) {
        return handle_target_miss(nav, &find);
    }
    find.miss_streak = 0;

    // DirectHit 之类命中没有框, 不能拿来定方向
    if (target_sighting.box.width <= 0 || target_sighting.box.height <= 0) {
        return fail_find(nav, "find_empty_box", "FIND target node hit without a usable box.");
    }

    const int32_t frame_width = MaaImageBufferWidth(image);
    const int32_t frame_height = MaaImageBufferHeight(image);
    if (frame_width <= 0 || frame_height <= 0) {
        LogWarn << "FIND: the captured frame has no size." << VAR(frame_width) << VAR(frame_height);
        HoldPosition(nav->motion_controller);
        return hold_find_step();
    }

    if (!approach_sighting(nav, &find, target_sighting.box, frame_width, frame_height)) {
        return fail_find(nav, "find_turn_failed", "FIND failed to issue a view turn.");
    }

    const Result probe_result = probe_stop_while_walking(nav, waypoint, controller);
    if (probe_result.consumed) {
        return probe_result;
    }
    return hold_find_step();
}

} // namespace

Result ArriveFind(Navigator* nav, const Waypoint& waypoint, double actual_distance)
{
    LogInfo << "Action: FIND reached its anchor." << VAR(actual_distance) << VAR(waypoint.x) << VAR(waypoint.y);
    return begin_find(nav, "find_anchor_reached");
}

Result ConsumeFindNodes(Navigator* nav)
{
    if (!HasCurrentWaypoint(*nav->session)) {
        return {};
    }

    const Waypoint& waypoint = CurrentWaypoint(*nav->session);
    // 带坐标的点由 ArriveFind 接手, 这里接了会被提前吃掉
    if (!IsFindPoint(waypoint) || waypoint.has_position) {
        return {};
    }
    return begin_find(nav, "find_control_node_reached");
}

Result TickFindTarget(Navigator* nav)
{
    if (nav->maa_context == nullptr || nav->session == nullptr || !HasCurrentWaypoint(*nav->session)) {
        // fail_find 会解引用 session, 上下文不全时只能直接返回失败
        LogError << "FIND is running without a waypoint or a MaaContext." << VAR(nav->maa_context == nullptr)
                 << VAR(nav->session == nullptr);
        return ConsumedFailure("find_context_missing", "FIND is running without a waypoint or a MaaContext.");
    }

    const Waypoint& waypoint = CurrentWaypoint(*nav->session);
    if (!IsFindPoint(waypoint)) {
        stop_and_reset_find(nav);
        SelectPhaseOrFinishRoute(nav, "find_phase_mismatch");
        return Handled();
    }

    FindState& find = nav->runtime_state.find;
    const auto now = std::chrono::steady_clock::now();
    if (IsUnset(find.started_at)) {
        find.started_at = now;
    }
    if (find.steps >= kFindMaxSteps || ElapsedMs(find.started_at, now) >= kFindBudgetMs) {
        return fail_find(nav, "find_budget_exhausted", "FIND ran out of budget before the stop node hit or the arrive point was reached.");
    }
    ++find.steps;

    if (waypoint.find_stop.empty() && !waypoint.find_arrive.has_value()) {
        return fail_find(nav, "find_criteria_missing", "FIND point has neither find_stop nor find_arrive.");
    }

    MaaController* controller = nav->input_backend->ctrl;
    ScopedImageBuffer image;
    bool captured = false;
    if (controller != nullptr) {
        captured = CaptureFreshFrame(controller, image.Get());
    }
    if (!captured) {
        HoldPosition(nav->motion_controller);
        LogWarn << "FIND: screencap failed, holding this step empty." << VAR(find.steps);
        return hold_find_step();
    }

    if (const std::optional<Result> done = finish_find_if_done(nav, waypoint, image.Get())) {
        return *done;
    }
    return pursue_target(nav, waypoint, controller, image.Get());
}

} // namespace semantic_nodes

} // namespace mapnavigator
