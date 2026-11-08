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
#include "../semantic/semantic_nodes.h"
#include "../sensing/pipeline_node_reader.h"
#include "../sensing/position_provider.h"
#include "../sensing/prompt_scan_profile.h"
#include "../sensing/roi_template_scanner.h"
#include "../zipline/zipline_action.h"
#include "internal.h"

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

} // namespace

TickStep NavigationStateMachine::handlePrompts(const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteMeasurement& route = tick.route;
    if (tryRunPromptSubtaskWhileWalking(route)) {
        return TickStep::EndTick;
    }

    if (tryZiplineMountPrompt(waypoint, route)) {
        runtime_state_.semantic.zipline_prompt_probe = true;
        const semantic_nodes::Result prompt_result = semantic_nodes::HandleArrival(semanticContext(), waypoint, route.waypoint_distance);
        runtime_state_.semantic.zipline_prompt_probe = false;
        if (prompt_result.request_failure) {
            return EndTickWith(failNavigation(
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

    if (waypoint.action == ActionType::ZIPLINE && !IsZiplineMounted(runtime_state_) && waypoint.zipline_hop) {
        FollowMountTower(&runtime_state_.zipline_approach, waypoint.zipline_hop->mount);
        const size_t cursor = runtime_state_.zipline_approach.spot_index;
        if (cursor + 1 < waypoint.zipline_hop->mount_spots.size() && route.waypoint_distance <= kZiplineWalkEnterBandWu
            && HardStalledMs(*session_, tick.now) >= kZiplineMountSpotStallMs) {
            const semantic_nodes::Result advanced =
                semantic_nodes::AdvanceMountSpot(semanticContext(), waypoint, "zipline_mount_spot_stalled");
            if (advanced.consumed) {
                return TickStep::EndTick;
            }
        }
    }
    return TickStep::Next;
}

std::array<AsyncPromptAction*, 2> NavigationStateMachine::promptActions()
{
    return { &collect_prompt_, &interact_prompt_ };
}

void NavigationStateMachine::startScanners()
{
    startPromptScanners();
    startDeviceProbe();

    if (position_provider_ == nullptr) {
        return;
    }
    position_provider_->frame_sinks = FrameSinks {
        .collect = &collect_prompt_,
        .interact = &interact_prompt_,
        .device = &device_recovery_,
        .zipline_mount = &zipline_mount_scanner_,
    };
}

void NavigationStateMachine::stopScanners()
{
    if (position_provider_ != nullptr) {
        position_provider_->frame_sinks.reset();
    }
    if (motion_controller_ != nullptr) {
        motion_controller_->sprint_suppressed = false;
    }
    StopPromptAction(&collect_prompt_);
    StopPromptAction(&interact_prompt_);
    StopDeviceRecovery(&device_recovery_);
    zipline_mount_scanner_.reset();
}

void NavigationStateMachine::startPromptScanners()
{
    for (AsyncPromptAction* prompt : promptActions()) {
        if (prompt->started || !PromptRouteHasPoint(*prompt)) {
            continue;
        }

        cv::Rect base_roi;
        if (!TryReadNodeRoi(maa_context_, prompt->spec.recognition_node, &base_roi)) {
            LogWarn << "Async prompt scanner not started: could not read its ROI from the pipeline." << VAR(prompt->spec.tag)
                    << VAR(prompt->spec.recognition_node);
            continue;
        }
        StartPromptAction(prompt, base_roi, input_backend_->controller_type.c_str());
    }
}

void NavigationStateMachine::startDeviceProbe()
{
    cv::Rect base_roi;
    if (!TryReadNodeRoi(maa_context_, kObstacleDeviceProbeNode, &base_roi)) {
        LogWarn << "Blocking-device probe not started: could not read its ROI from the pipeline." << VAR(kObstacleDeviceProbeNode);
        return;
    }
    StartDeviceRecovery(&device_recovery_, base_roi, input_backend_->controller_type.c_str());
}

void NavigationStateMachine::preWarmPromptRecognition()
{
    for (AsyncPromptAction* prompt : promptActions()) {
        PreWarmPromptRecognition(*prompt);
    }
}

// 判定圈外的检测结果也要读掉, 免得进圈那一拍拿着旧结果触发
bool NavigationStateMachine::tryZiplineMountPrompt(const Waypoint& waypoint, const RouteMeasurement& route)
{
    if (waypoint.action != ActionType::ZIPLINE) {
        zipline_mount_scanner_.reset();
        return false;
    }
    if (IsZiplineMounted(runtime_state_)) {
        return false;
    }
    if (zipline_mount_scanner_ == nullptr && maa_context_ != nullptr) {
        PromptScanProfile profile;
        if (!TryLoadPromptScanProfile(maa_context_, kZiplineMountScanNode, input_backend_->controller_type.c_str(), &profile)) {
            LogWarn << "Zipline mount pre-filter not started; the chain stops on arrival instead." << VAR(kZiplineMountScanNode);
            return false;
        }
        LogInfo << "Zipline mount pre-filter started." << VAR(kZiplineMountScanNode) << VAR(profile.base_roi.x) << VAR(profile.base_roi.y)
                << VAR(profile.icons.size());
        zipline_mount_scanner_ = std::make_unique<RoiTemplateScanner>("zipline", profile.base_roi, std::move(profile.icons));
    }
    if (zipline_mount_scanner_ == nullptr) {
        return false;
    }
    const bool flagged = zipline_mount_scanner_->consumeDetection();
    return flagged && route.startup_motion_confirmed && route.waypoint_distance <= kPromptTriggerBandWu;
}

NavigationStateMachine::PromptDistance NavigationStateMachine::nearestPromptDistance() const
{
    PromptDistance nearest;
    for (const AsyncPromptAction* prompt : { &collect_prompt_, &interact_prompt_ }) {
        const double distance_sq = NearestPromptDistanceSq(*prompt);
        if (distance_sq >= 0.0 && (nearest.distance_sq < 0.0 || distance_sq < nearest.distance_sq)) {
            nearest.distance_sq = distance_sq;
            nearest.is_zipline = false;
        }
    }
    const std::vector<Waypoint>& path = session_->current_path;
    for (size_t index = session_->current_node_idx; index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        const bool is_zipline = waypoint.action == ActionType::ZIPLINE;
        if (!waypoint.has_position || (waypoint.action != ActionType::DIG && !is_zipline)
            || (is_zipline && IsZiplineMounted(runtime_state_))) {
            continue;
        }
        const double dx = waypoint.x - position_->x;
        const double dy = waypoint.y - position_->y;
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

void NavigationStateMachine::updatePromptSprintSuppression()
{
    if (motion_controller_ == nullptr) {
        return;
    }

    const double nearest_sq = nearestPromptDistance().distance_sq;
    const bool approaching_prompt = nearest_sq >= 0.0 && nearest_sq <= kCollectSprintSuppressBandWu * kCollectSprintSuppressBandWu;
    if (approaching_prompt && SprintCancellable(*motion_controller_)) {
        CancelSprint(motion_controller_);
    }
    motion_controller_->sprint_suppressed = approaching_prompt;
}

bool NavigationStateMachine::tryRunPromptSubtaskWhileWalking(const RouteMeasurement& route)
{
    if (!route.startup_motion_confirmed) {
        return false;
    }
    const std::optional<size_t> node = next_authored_node(*session_);
    const std::array<AsyncPromptAction*, 2> prompts = promptActions();
    for (size_t index = 0; index < prompts.size(); ++index) {
        if (prompt_repeats_exhausted(runtime_state_.prompt_repeat, node, index)) {
            ForgetPromptDetection(prompts[index]);
            continue;
        }
        const auto started_at = std::chrono::steady_clock::now();
        const PromptTriggerOutcome outcome =
            TryTriggerPromptWhileWalking(prompts[index], motion_controller_, route.waypoint_distance, session_->current_node_idx);
        if (outcome == PromptTriggerOutcome::NotTriggered) {
            continue;
        }
        NoteTimedFailure(
            &failure_trail_,
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
        NoteInteraction(session_);
        if (CompletesWaypointOnTrigger(prompts[index]->spec)) {
            completeWaypointAfterPromptTrigger();
            return true;
        }
        count_prompt_repeat(&runtime_state_.prompt_repeat, node, index);
        if (prompt_repeats_exhausted(runtime_state_.prompt_repeat, node, index)) {
            LogWarn << "Prompt keeps being recognized without advancing; ignoring it at this waypoint." << VAR(prompts[index]->spec.tag)
                    << VAR(node);
            NoteFailure(&failure_trail_, std::chrono::steady_clock::now(), "prompt muted");
        }
        return true;
    }
    return false;
}

// 交互一开角色就不动、小地图也没了, 所以提示命中即算走完
void NavigationStateMachine::completeWaypointAfterPromptTrigger()
{
    if (!HasCurrentWaypoint(*session_)) {
        return;
    }

    const ActionType action = CurrentWaypoint(*session_).action;
    const std::optional<RopeRideInfo> rope_ride = CurrentWaypoint(*session_).rope_ride;
    const std::optional<size_t> consumed_absolute_node_idx = CurrentAbsoluteNodeIndex(*session_);
    NoteCanonicalFinalGoalConsumed(session_, consumed_absolute_node_idx, *position_, "async_prompt_completed");
    AdvanceToNextWaypoint(session_, action, "async_prompt_completed");
    ResetOnWaypointAdvance(&runtime_state_);
    if (!HasCurrentWaypoint(*session_)) {
        NoteRouteTailConsumed(session_, *position_, "route_tail_consumed");
        return;
    }
    if (rope_ride) {
        semantic_nodes::StartRopeRide(semanticContext(), *rope_ride);
        return;
    }
    selectPhaseForCurrentWaypoint("async_prompt_completed");
}

// 走完最后一个点的那一拍扫描器就停了, 所以收尾单独再检测一次
void NavigationStateMachine::tryRunPromptSubtaskAtRouteTail()
{
    if (maa_context_ == nullptr || should_stop_() || session_->phase != NaviPhase::Finished) {
        return;
    }

    AsyncPromptAction* tail_prompt = nullptr;
    for (AsyncPromptAction* prompt : promptActions()) {
        if (prompt->started && !CompletesWaypointOnTrigger(prompt->spec) && PromptRouteEndsWithPoint(*prompt)) {
            tail_prompt = prompt;
            break;
        }
    }
    if (tail_prompt == nullptr) {
        return;
    }

    stopMotion();
    SleepFor(kStopWaitMs); // 先站定, 还在滑行就动手容易白按一次
    const auto started_at = std::chrono::steady_clock::now();
    while (!should_stop_()) {
        if (TryTriggerPromptAtRouteTail(tail_prompt)) {
            return;
        }

        const int64_t elapsed_ms =
            std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - started_at).count();
        if (elapsed_ms >= kCollectTailGraceMs) {
            LogInfo << "Route tail prompt grace expired with nothing flagged." << VAR(tail_prompt->spec.tag) << VAR(elapsed_ms);
            return;
        }

        captureCurrentPosition(false); // 只为把新画面喂给检测器
        SleepFor(kTargetTickMs);
    }
}

} // namespace mapnavigator
