#include "obstacle_device_recovery.h"

#include <cmath>
#include <filesystem>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>

#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "navigation_session.h"
#include "position_provider.h"
#include "roi_template_scanner.h"
#include "route_tracker.h"

#include "../utils.h"

namespace mapnavigator
{

ObstacleDeviceRecovery::ObstacleDeviceRecovery(
    MaaContext* maa_context,
    MotionController* motion_controller,
    PositionProvider* position_provider,
    NavigationSession* session,
    NaviPosition* position)
    : maa_context_(maa_context)
    , motion_controller_(motion_controller)
    , position_provider_(position_provider)
    , session_(session)
    , position_(position)
{
}

ObstacleDeviceRecovery::~ObstacleDeviceRecovery() = default;

void ObstacleDeviceRecovery::Start(const cv::Rect& base_roi)
{
    if (scanner_ != nullptr) {
        return;
    }

    const std::filesystem::path template_path = std::filesystem::absolute(get_exe_dir() / ".." / kObstacleDeviceTemplateRelativePath);
    const cv::Mat button_template = MAA_NS::imread(template_path, cv::IMREAD_GRAYSCALE);
    if (button_template.empty()) {
        LogWarn << "Blocking-device probe not started: interact button template not loaded."
                << VAR(MAA_NS::path_to_utf8_string(template_path));
        return;
    }

    scanner_ = std::make_unique<RoiTemplateScanner>("device", base_roi, button_template, cv::Mat(), kObstacleDeviceMatchThreshold);
    LogInfo << "Blocking-device probe started." << VAR(base_roi.x) << VAR(base_roi.y) << VAR(base_roi.width) << VAR(base_roi.height);
}

void ObstacleDeviceRecovery::Stop()
{
    scanner_.reset();
    feeding_ = false;
}

void ObstacleDeviceRecovery::SubmitFrame(const cv::Mat& frame)
{
    if (scanner_ == nullptr || !feeding_) {
        return;
    }
    scanner_->SubmitFrame(frame);
}

void ObstacleDeviceRecovery::UpdateFeeding(int64_t stalled_ms, bool attempt_available)
{
    if (scanner_ == nullptr) {
        return;
    }
    const bool feed = stalled_ms > 0 && attempt_available;
    if (feed && !feeding_) {
        scanner_->ConsumeDetection(); // the restart edge drops whatever the worker flagged while unattended
    }
    feeding_ = feed;
}

void ObstacleDeviceRecovery::ForgetObservation()
{
    if (scanner_ != nullptr) {
        scanner_->ConsumeDetection();
    }
    feeding_ = false;
}

DeviceRemovalOutcome ObstacleDeviceRecovery::TryRemove(const RouteTrackingState& route, const Waypoint& waypoint, bool attempt_available)
{
    if (maa_context_ == nullptr || scanner_ == nullptr || !attempt_available) {
        return DeviceRemovalOutcome::NotAttempted;
    }
    if (!scanner_->ConsumeDetection()) {
        return DeviceRemovalOutcome::NotAttempted; // nothing interactable in front of the agent, so a jump is the faster answer
    }

    const NaviPosition move_start = *position_;
    LogInfo << "Dynamic recovery carrying off the blocking device." << VAR(route.waypoint_distance);
    // Forward has to be held across the subtask: it picks the device up, and the agent must walk out from
    // under it before the subtask drops it again, or the device lands back in the way.
    motion_controller_->ReassertForward();
    if (MaaContextRunTask(maa_context_, kObstacleDeviceEntry, "{}") == MaaInvalidId) {
        LogWarn << "Blocking-device subtask did not run.";
        return DeviceRemovalOutcome::StillPinned;
    }

    if (!position_provider_->Capture(position_, false, session_->current_zone_id()) || position_provider_->LastCaptureWasHeld()
        || position_provider_->LastCaptureWasBlackScreen() || !position_->valid) {
        // A stale fix would let the jump that follows measure its displacement from an old point and report an
        // escape that never happened, so wait for a real one instead.
        LogWarn << "Dynamic recovery waiting for a local tracking fix after the device move.";
        utils::SleepFor(kTargetTickMs);
        return DeviceRemovalOutcome::NeedsFreshFix;
    }

    const bool zone_changed = !move_start.zone_id.empty() && !position_->zone_id.empty() && move_start.zone_id != position_->zone_id;
    const double displacement = std::hypot(position_->x - move_start.x, position_->y - move_start.y);
    const double move_waypoint_distance = std::hypot(waypoint.x - position_->x, waypoint.y - position_->y);
    const bool made_progress = move_waypoint_distance + kNoProgressDistanceEpsilon < route.waypoint_distance;
    const bool moved_forward = displacement >= kDynamicRecoveryResetDistance * 0.5 && made_progress;
    if (zone_changed || displacement >= kDynamicRecoveryResetDistance || moved_forward) {
        LogInfo << "Dynamic recovery device move escaped obstacle." << VAR(zone_changed) << VAR(displacement) << VAR(moved_forward);
        return DeviceRemovalOutcome::Escaped;
    }

    LogInfo << "Device handled but the agent is still pinned; continuing to the jump." << VAR(displacement);
    return DeviceRemovalOutcome::StillPinned;
}

} // namespace mapnavigator
