#include <array>
#include <chrono>
#include <memory>
#include <optional>
#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../semantic/async_prompt_action.h"
#include "../semantic/semantic_nodes.h"
#include "../sensing/position_provider.h"
#include "../sensing/prompt_scan_profile.h"
#include "../sensing/roi_template_scanner.h"
#include "../zipline/zipline_action.h"
#include "internal.h"

namespace mapnavigator
{

TickStep NavigationStateMachine::HandlePrompts(const NavigateTick& tick)
{
    const Waypoint& waypoint = tick.waypoint;
    const RouteTrackingState& route = tick.route;
    if (TryRunPromptSubtaskWhileWalking(route)) {
        return TickStep::EndTick;
    }

    if (TryZiplineMountPrompt(waypoint, route)) {
        runtime_state_.semantic.zipline_prompt_probe = true;
        const semantic_nodes::Result prompt_result = semantic_nodes::HandleArrival(SemanticContext(), waypoint, route.waypoint_distance);
        runtime_state_.semantic.zipline_prompt_probe = false;
        if (prompt_result.request_failure) {
            return EndTickWith(FailNavigation(
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

    if (waypoint.action == ActionType::ZIPLINE && !runtime_state_.IsZiplineMounted() && waypoint.zipline_hop) {
        const size_t cursor = runtime_state_.zipline_approach.MountSpotCursor(waypoint.zipline_hop->mount);
        if (cursor + 1 < waypoint.zipline_hop->mount_spots.size() && route.waypoint_distance <= kZiplineWalkEnterBandWu
            && session_->HardStalledMs(tick.now) >= kZiplineMountSpotStallMs) {
            const semantic_nodes::Result advanced =
                semantic_nodes::AdvanceMountSpot(SemanticContext(), waypoint, "zipline_mount_spot_stalled");
            if (advanced.consumed) {
                return TickStep::EndTick;
            }
        }
    }
    return TickStep::Next;
}

std::array<AsyncPromptAction*, 2> NavigationStateMachine::PromptActions()
{
    return { &collect_prompt_, &interact_prompt_ };
}

void NavigationStateMachine::StartScanners()
{
    StartPromptScanners();
    StartDeviceProbe();

    if (position_provider_ == nullptr) {
        return;
    }
    position_provider_->SetFrameObserver([this](const cv::Mat& frame) {
        collect_prompt_.SubmitFrame(frame);
        interact_prompt_.SubmitFrame(frame);
        device_recovery_.SubmitFrame(frame);
        if (zipline_mount_scanner_ != nullptr) {
            zipline_mount_scanner_->SubmitFrame(frame);
        }
    });
}

void NavigationStateMachine::StopScanners()
{
    if (position_provider_ != nullptr) {
        position_provider_->SetFrameObserver(nullptr);
    }
    if (motion_controller_ != nullptr) {
        motion_controller_->SetSprintSuppressed(false);
    }
    collect_prompt_.Stop();
    interact_prompt_.Stop();
    device_recovery_.Stop();
    zipline_mount_scanner_.reset();
}

void NavigationStateMachine::StartPromptScanners()
{
    for (AsyncPromptAction* prompt : PromptActions()) {
        if (prompt->armed() || !prompt->RouteHasPoint()) {
            continue;
        }

        cv::Rect base_roi;
        if (!TryReadNodeRoi(maa_context_, prompt->spec().recognition_node, &base_roi)) {
            LogWarn << "Async prompt scanner not started: could not read its ROI from the pipeline." << VAR(prompt->spec().tag)
                    << VAR(prompt->spec().recognition_node);
            continue;
        }
        prompt->Start(base_roi, action_wrapper_->controller_type());
    }
}

void NavigationStateMachine::StartDeviceProbe()
{
    cv::Rect base_roi;
    if (!TryReadNodeRoi(maa_context_, kObstacleDeviceProbeNode, &base_roi)) {
        LogWarn << "Blocking-device probe not started: could not read its ROI from the pipeline." << VAR(kObstacleDeviceProbeNode);
        return;
    }
    device_recovery_.Start(base_roi);
}

void NavigationStateMachine::PreWarmPromptRecognition()
{
    for (AsyncPromptAction* prompt : PromptActions()) {
        prompt->PreWarmRecognition();
    }
}

// 判定圈外的检测结果也要读掉, 免得进圈那一拍拿着旧结果触发
bool NavigationStateMachine::TryZiplineMountPrompt(const Waypoint& waypoint, const RouteTrackingState& route)
{
    if (waypoint.action != ActionType::ZIPLINE) {
        zipline_mount_scanner_.reset();
        return false;
    }
    if (runtime_state_.IsZiplineMounted()) {
        return false;
    }
    if (zipline_mount_scanner_ == nullptr && maa_context_ != nullptr) {
        PromptScanProfile profile;
        if (!TryLoadPromptScanProfile(maa_context_, kZiplineMountScanNode, action_wrapper_->controller_type(), &profile)) {
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
    const bool flagged = zipline_mount_scanner_->ConsumeDetection();
    return flagged && route.startup_motion_confirmed && route.waypoint_distance <= kPromptTriggerBandWu;
}

NavigationStateMachine::PromptDistance NavigationStateMachine::NearestPromptDistance() const
{
    PromptDistance nearest;
    for (const AsyncPromptAction* prompt : { &collect_prompt_, &interact_prompt_ }) {
        const double distance_sq = prompt->NearestDistanceSq();
        if (distance_sq >= 0.0 && (nearest.distance_sq < 0.0 || distance_sq < nearest.distance_sq)) {
            nearest.distance_sq = distance_sq;
            nearest.is_zipline = false;
        }
    }
    const std::vector<Waypoint>& path = session_->current_path();
    for (size_t index = session_->current_node_idx(); index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        const bool is_zipline = waypoint.action == ActionType::ZIPLINE;
        if (!waypoint.HasPosition() || (waypoint.action != ActionType::DIG && !is_zipline)
            || (is_zipline && runtime_state_.IsZiplineMounted())) {
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

void NavigationStateMachine::UpdatePromptSprintSuppression()
{
    if (motion_controller_ == nullptr) {
        return;
    }

    const double nearest_sq = NearestPromptDistance().distance_sq;
    const bool approaching_prompt = nearest_sq >= 0.0 && nearest_sq <= kCollectSprintSuppressBandWu * kCollectSprintSuppressBandWu;
    motion_controller_->SetSprintSuppressed(approaching_prompt);
}

bool NavigationStateMachine::TryRunPromptSubtaskWhileWalking(const RouteTrackingState& route)
{
    if (!route.startup_motion_confirmed) {
        return false;
    }

    const std::array<AsyncPromptAction*, 2> prompts = PromptActions();
    for (size_t index = 0; index < prompts.size(); ++index) {
        const PromptTriggerOutcome outcome =
            prompts[index]->TryTriggerWhileWalking(motion_controller_, route.waypoint_distance, session_->current_node_idx());
        if (outcome == PromptTriggerOutcome::NotTriggered) {
            continue;
        }
        // 屏幕上一次只弹一个提示, 清掉另一类的检测结果, 免得为同一个提示停两次车
        for (size_t other = 0; other < prompts.size(); ++other) {
            if (other != index) {
                prompts[other]->ForgetDetection();
            }
        }
        if (outcome == PromptTriggerOutcome::Recognized && prompts[index]->spec().CompletesWaypointOnTrigger()) {
            CompleteWaypointAfterPromptTrigger();
        }
        return true;
    }
    return false;
}

// 交互一开角色就不动、小地图也没了, 所以提示命中即算走完
void NavigationStateMachine::CompleteWaypointAfterPromptTrigger()
{
    if (!session_->HasCurrentWaypoint()) {
        return;
    }

    const ActionType action = session_->CurrentWaypoint().action;
    const std::optional<RopeRideInfo> rope_ride = session_->CurrentWaypoint().rope_ride;
    const std::optional<size_t> consumed_absolute_node_idx = session_->CurrentAbsoluteNodeIndex();
    session_->NoteCanonicalFinalGoalConsumed(consumed_absolute_node_idx, *position_, "async_prompt_completed");
    session_->AdvanceToNextWaypoint(action, "async_prompt_completed");
    runtime_state_.OnWaypointAdvance();
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return;
    }
    if (rope_ride) {
        semantic_nodes::StartRopeRide(SemanticContext(), *rope_ride);
        return;
    }
    SelectPhaseForCurrentWaypoint("async_prompt_completed");
}

// 走完最后一个点的那一拍扫描器就停了, 所以收尾单独再检测一次
void NavigationStateMachine::TryRunPromptSubtaskAtRouteTail()
{
    if (maa_context_ == nullptr || should_stop_() || session_->phase() != NaviPhase::Finished) {
        return;
    }

    AsyncPromptAction* tail_prompt = nullptr;
    for (AsyncPromptAction* prompt : PromptActions()) {
        if (prompt->armed() && !prompt->spec().CompletesWaypointOnTrigger() && prompt->RouteEndsWithPoint()) {
            tail_prompt = prompt;
            break;
        }
    }
    if (tail_prompt == nullptr) {
        return;
    }

    StopMotion();
    utils::SleepFor(kStopWaitMs); // 先站定, 还在滑行就动手容易白按一次
    const auto started_at = std::chrono::steady_clock::now();
    while (!should_stop_()) {
        if (tail_prompt->TryTriggerAtRouteTail()) {
            return;
        }

        const int64_t elapsed_ms =
            std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - started_at).count();
        if (elapsed_ms >= kCollectTailGraceMs) {
            LogInfo << "Route tail prompt grace expired with nothing flagged." << VAR(tail_prompt->spec().tag) << VAR(elapsed_ms);
            return;
        }

        CaptureCurrentPosition(false); // 只为把新画面喂给检测器
        utils::SleepFor(kTargetTickMs);
    }
}

} // namespace mapnavigator
