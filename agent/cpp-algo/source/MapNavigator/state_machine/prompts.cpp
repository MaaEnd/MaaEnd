#include "prompts.h"

#include <array>
#include <chrono>
#include <format>
#include <memory>
#include <optional>
#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "../semantic/async_prompt_action.h"
#include "../semantic/semantic_helpers.h"
#include "../semantic/semantic_nodes.h"
#include "../sensing/pipeline_node_reader.h"
#include "../sensing/position_provider.h"
#include "../sensing/prompt_scan_profile.h"
#include "../sensing/roi_template_scanner.h"
#include "../sensing/task_stop.h"
#include "../zipline/zipline_action.h"
#include "internal.h"
#include "localization.h"

namespace mapnavigator
{

namespace
{

std::optional<size_t> next_authored_node(const Session& session)
{
    const size_t path_size = session.current_path.size();
    for (size_t index = session.current_node_idx; index < path_size; ++index) {
        if (const std::optional<size_t> canonical = CanonicalIndexAtCurrentPath(session, index)) {
            return canonical;
        }
    }
    return std::nullopt;
}

bool prompt_repeats_exhausted(const PromptRepeatState& repeat, const std::optional<size_t>& node, size_t prompt)
{
    return node.has_value() && repeat.node == node && repeat.prompt == prompt && repeat.recognized >= kPromptRecognizedRepeatLimit;
}

void count_prompt_repeat(PromptRepeatState* repeat, const std::optional<size_t>& node, size_t prompt)
{
    if (repeat->node != node || repeat->prompt != prompt) {
        repeat->node = node;
        repeat->prompt = prompt;
        repeat->recognized = 0;
    }
    ++repeat->recognized;
}

std::array<AsyncPromptAction*, 2> prompt_actions(Navigator* nav)
{
    return { &nav->collect_prompt, &nav->interact_prompt };
}

void start_prompt_scanners(Navigator* nav)
{
    for (AsyncPromptAction* prompt : prompt_actions(nav)) {
        if (prompt->started || !PromptRouteHasPoint(prompt->spec, nav->session)) {
            continue;
        }

        cv::Rect base_roi;
        if (!TryReadNodeRoi(nav->maa_context, prompt->spec.recognition_node, &base_roi)) {
            LogWarn << "Async prompt scanner not started: could not read its ROI from the pipeline." << VAR(prompt->spec.tag)
                    << VAR(prompt->spec.recognition_node);
            continue;
        }
        StartPromptAction(nav, prompt, base_roi);
    }
}

void start_device_probe(Navigator* nav)
{
    cv::Rect base_roi;
    if (!TryReadNodeRoi(nav->maa_context, kObstacleDeviceProbeNode, &base_roi)) {
        LogWarn << "Blocking-device probe not started: could not read its ROI from the pipeline." << VAR(kObstacleDeviceProbeNode);
        return;
    }
    StartDeviceRecovery(&nav->device_recovery, base_roi, nav->input_backend->controller_type.c_str());
}

// 返回扫描器这一拍是否在为这个滑索点工作
bool sync_zipline_mount_scanner(Navigator* nav, const Waypoint& waypoint)
{
    if (waypoint.action != ActionType::ZIPLINE) {
        nav->zipline_mount_scanner.reset();
        return false;
    }
    if (IsZiplineMounted(nav->runtime_state)) {
        return false;
    }
    if (nav->zipline_mount_scanner == nullptr && nav->maa_context != nullptr) {
        PromptScanProfile profile;
        if (!TryLoadPromptScanProfile(nav->maa_context, kZiplineMountScanNode, nav->input_backend->controller_type.c_str(), &profile)) {
            LogWarn << "Zipline mount pre-filter not started; the chain stops on arrival instead." << VAR(kZiplineMountScanNode);
            return false;
        }
        LogInfo << "Zipline mount pre-filter started." << VAR(kZiplineMountScanNode) << VAR(profile.base_roi.x) << VAR(profile.base_roi.y)
                << VAR(profile.icons.size());
        nav->zipline_mount_scanner = std::make_unique<RoiTemplateScanner>("zipline", profile.base_roi, std::move(profile.icons));
    }
    return nav->zipline_mount_scanner != nullptr;
}

// 判定圈外的检测结果也要读掉, 免得进圈那一拍拿着旧结果触发
bool consume_zipline_mount_prompt(Navigator* nav, const RouteMeasurement& route)
{
    const bool flagged = nav->zipline_mount_scanner->consumeDetection();
    return flagged && route.startup_motion_confirmed && route.waypoint_distance <= kPromptTriggerBandWu;
}

// 交互一开角色就不动、小地图也没了, 所以提示命中即算走完
void complete_waypoint_after_prompt_trigger(Navigator* nav)
{
    if (!HasCurrentWaypoint(*nav->session)) {
        return;
    }

    const ActionType action = CurrentWaypoint(*nav->session).action;
    const std::optional<RopeRideInfo> rope_ride = CurrentWaypoint(*nav->session).rope_ride;
    const std::optional<size_t> consumed_absolute_node_idx = CurrentAbsoluteNodeIndex(*nav->session);
    NoteCanonicalFinalGoalConsumed(nav->session, consumed_absolute_node_idx, *nav->position, "async_prompt_completed");
    if (MayOpenUi(action)) {
        NoteInteraction(nav->session);
    }
    LeaveCurrentWaypoint(nav->session, action, "async_prompt_completed");
    ResetOnWaypointAdvance(&nav->runtime_state);
    if (!HasCurrentWaypoint(*nav->session)) {
        ConcludeRouteTail(nav->session, *nav->position, "route_tail_consumed");
        return;
    }
    if (rope_ride) {
        semantic_nodes::StartRopeRide(nav, *rope_ride);
        return;
    }
    semantic_nodes::SelectPhaseOrFinishRoute(nav, "async_prompt_completed");
}

bool try_run_prompt_subtask_while_walking(Navigator* nav, const RouteMeasurement& route)
{
    if (!route.startup_motion_confirmed) {
        return false;
    }
    const std::optional<size_t> node = next_authored_node(*nav->session);
    const std::array<AsyncPromptAction*, 2> prompts = prompt_actions(nav);
    for (size_t index = 0; index < prompts.size(); ++index) {
        if (prompt_repeats_exhausted(nav->runtime_state.prompt_repeat, node, index)) {
            ForgetPromptDetection(prompts[index]);
            continue;
        }
        const auto started_at = std::chrono::steady_clock::now();
        const PromptTriggerOutcome outcome = TryTriggerPromptWhileWalking(nav, prompts[index], route.waypoint_distance);
        if (outcome == PromptTriggerOutcome::NotTriggered) {
            continue;
        }
        NoteTimedFailure(
            &nav->failure_trail,
            std::chrono::steady_clock::now(),
            std::format("prompt {} {}", prompts[index]->spec.tag, outcome == PromptTriggerOutcome::Recognized ? "recognized" : "missed"),
            ElapsedMs(started_at));
        // 屏幕上一次只弹一个提示, 清掉另一类的检测结果, 免得为同一个提示停两次车
        for (size_t other = 0; other < prompts.size(); ++other) {
            if (other != index) {
                ForgetPromptDetection(prompts[other]);
            }
        }
        if (outcome != PromptTriggerOutcome::Recognized) {
            return true;
        }
        // 采集提示认中后不走完路点, 交互时刻须在这里记
        NoteInteraction(nav->session);
        if (CompletesWaypointOnTrigger(prompts[index]->spec)) {
            complete_waypoint_after_prompt_trigger(nav);
            return true;
        }
        count_prompt_repeat(&nav->runtime_state.prompt_repeat, node, index);
        if (prompt_repeats_exhausted(nav->runtime_state.prompt_repeat, node, index)) {
            LogWarn << "Prompt keeps being recognized without advancing; ignoring it at this waypoint." << VAR(prompts[index]->spec.tag)
                    << VAR(node);
            NoteFailure(&nav->failure_trail, std::chrono::steady_clock::now(), "prompt muted");
        }
        return true;
    }
    return false;
}

} // namespace

TickStep HandlePrompts(Navigator* nav, const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteMeasurement& route = tick.route;
    if (try_run_prompt_subtask_while_walking(nav, route)) {
        return TickStep::EndTick;
    }

    bool mount_prompt_due = false;
    if (sync_zipline_mount_scanner(nav, waypoint)) {
        mount_prompt_due = consume_zipline_mount_prompt(nav, route);
    }
    if (mount_prompt_due) {
        nav->runtime_state.semantic.zipline_prompt_probe = true;
        const semantic_nodes::Result prompt_result = semantic_nodes::HandleArrival(nav, waypoint, route.waypoint_distance);
        nav->runtime_state.semantic.zipline_prompt_probe = false;
        if (prompt_result.request_failure) {
            return EndTickWith(FailNavigation(
                nav,
                prompt_result.failure_reason,
                prompt_result.failure_log_message,
                route.waypoint_distance,
                0.0,
                tick.stalled_ms));
        }
        if (prompt_result.consumed) {
            return TickStep::EndTick;
        }
    }

    if (waypoint.action == ActionType::ZIPLINE && !IsZiplineMounted(nav->runtime_state) && waypoint.zipline_hop) {
        FollowMountTower(&nav->runtime_state.zipline_approach, waypoint.zipline_hop->mount);
        const size_t cursor = nav->runtime_state.zipline_approach.spot_index;
        if (cursor + 1 < waypoint.zipline_hop->mount_spots.size() && route.waypoint_distance <= kZiplineWalkEnterBandWu
            && HardStalledMs(*nav->session, tick.now) >= kZiplineMountSpotStallMs) {
            const semantic_nodes::Result advanced = semantic_nodes::AdvanceMountSpot(nav, waypoint, "zipline_mount_spot_stalled");
            if (advanced.consumed) {
                return TickStep::EndTick;
            }
        }
    }
    return TickStep::Next;
}

void StartScanners(Navigator* nav)
{
    start_prompt_scanners(nav);
    start_device_probe(nav);

    if (nav->position_provider == nullptr) {
        return;
    }
    nav->position_provider->frame_sinks = FrameSinks {
        .collect = &nav->collect_prompt,
        .interact = &nav->interact_prompt,
        .device = &nav->device_recovery,
        .zipline_mount = &nav->zipline_mount_scanner,
    };
}

void StopScanners(Navigator* nav)
{
    if (nav->position_provider != nullptr) {
        nav->position_provider->frame_sinks.reset();
    }
    StopPromptAction(&nav->collect_prompt);
    StopPromptAction(&nav->interact_prompt);
    StopDeviceRecovery(&nav->device_recovery);
    nav->zipline_mount_scanner.reset();
}

void PreWarmPrompts(Navigator* nav)
{
    for (AsyncPromptAction* prompt : prompt_actions(nav)) {
        PreWarmPromptRecognition(*nav, *prompt);
    }
}

PromptDistance NearestPromptDistance(const Navigator& nav)
{
    PromptDistance nearest;
    for (const AsyncPromptAction* prompt : { &nav.collect_prompt, &nav.interact_prompt }) {
        const double distance_sq = NearestPromptDistanceSq(*prompt, nav.session, nav.position);
        if (distance_sq >= 0.0 && (nearest.distance_sq < 0.0 || distance_sq < nearest.distance_sq)) {
            nearest.distance_sq = distance_sq;
            nearest.is_zipline = false;
        }
    }
    const std::vector<Waypoint>& path = nav.session->current_path;
    for (size_t index = nav.session->current_node_idx; index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        const bool is_zipline = waypoint.action == ActionType::ZIPLINE;
        if (!waypoint.has_position || (waypoint.action != ActionType::DIG && !is_zipline)
            || (is_zipline && IsZiplineMounted(nav.runtime_state))) {
            continue;
        }
        const double dx = waypoint.x - nav.position->x;
        const double dy = waypoint.y - nav.position->y;
        const double distance_sq = dx * dx + dy * dy;
        if (waypoint.action == ActionType::DIG && (nearest.dig_distance_sq < 0.0 || distance_sq < nearest.dig_distance_sq)) {
            nearest.dig_distance_sq = distance_sq;
        }
        if (nearest.distance_sq < 0.0 || distance_sq < nearest.distance_sq) {
            nearest.distance_sq = distance_sq;
            nearest.is_zipline = is_zipline;
        }
    }
    return nearest;
}

bool ApproachingPrompt(const Navigator& nav)
{
    const double nearest_sq = NearestPromptDistance(nav).distance_sq;
    return nearest_sq >= 0.0 && nearest_sq <= kCollectSprintSuppressBandWu * kCollectSprintSuppressBandWu;
}

// 走完最后一个点的那一拍扫描器就停了, 所以收尾单独再检测一次
void TryRunPromptSubtaskAtRouteTail(Navigator* nav)
{
    if (nav->maa_context == nullptr) {
        return;
    }
    if (TaskStopping(nav->maa_context)) {
        return;
    }
    if (nav->session->phase != NaviPhase::Finished) {
        return;
    }

    AsyncPromptAction* tail_prompt = nullptr;
    for (AsyncPromptAction* prompt : prompt_actions(nav)) {
        if (prompt->started && !CompletesWaypointOnTrigger(prompt->spec) && PromptRouteEndsWithPoint(prompt->spec, nav->session)) {
            tail_prompt = prompt;
            break;
        }
    }
    if (tail_prompt == nullptr) {
        return;
    }

    HoldPosition(nav->motion_controller);
    SleepFor(kStopWaitMs); // 先站定, 还在滑行就动手容易白按一次
    const auto started_at = std::chrono::steady_clock::now();
    while (!TaskStopping(nav->maa_context)) {
        if (TryTriggerPromptAtRouteTail(nav, tail_prompt)) {
            return;
        }

        const int64_t elapsed_ms =
            std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - started_at).count();
        if (elapsed_ms >= kCollectTailGraceMs) {
            LogInfo << "Route tail prompt grace expired with nothing flagged." << VAR(tail_prompt->spec.tag) << VAR(elapsed_ms);
            return;
        }

        CaptureTrackedFix(nav); // 只为把新画面喂给检测器
        SleepFor(kTargetTickMs);
    }
}

} // namespace mapnavigator
