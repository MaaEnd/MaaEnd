#include "obstacle_device_recovery.h"

#include <cmath>
#include <filesystem>
#include <optional>
#include <string>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../navi_math.h"
#include "../planning/navigation_session.h"
#include "../planning/route_tracker.h"
#include "../semantic/semantic_helpers.h"
#include "../sensing/position_provider.h"
#include "../sensing/roi_template_scanner.h"
#include "motion_controller.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace
{

const char* WalkOutEntryFor(const std::vector<std::string>& handled)
{
    for (const std::string& node : handled) {
        if (node == kObstacleDeviceMoveNode) {
            return kObstacleDeviceWalkOutEntry;
        }
        if (node == kObstacleAicCoreMoveNode) {
            return kObstacleAicCoreWalkOutEntry;
        }
    }
    return nullptr;
}

} // namespace

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

    const std::filesystem::path template_path = std::filesystem::absolute(get_install_dir() / kObstacleDeviceTemplateRelativePath);
    const cv::Mat button_template = MAA_NS::imread(template_path, cv::IMREAD_GRAYSCALE);
    if (button_template.empty()) {
        LogWarn << "Blocking-device probe not started: interact button template not loaded."
                << VAR(MAA_NS::path_to_utf8_string(template_path));
        return;
    }

    scanner_ = std::make_unique<RoiTemplateScanner>(
        "device",
        base_roi,
        std::vector<ScanIcon> { ScanIcon { .templ = button_template, .threshold = kObstacleDeviceMatchThreshold } });
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
        scanner_->ConsumeDetection(); // 重新开喂时丢掉停喂期间的命中
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
        return DeviceRemovalOutcome::NotAttempted;
    }

    const NaviPosition move_start = *position_;
    LogInfo << "Dynamic recovery carrying off the blocking device." << VAR(route.waypoint_distance);
    // 边走边点会点到滚到下面的另一行提示, 移动点击前须站定
    motion_controller_->SetForwardState(false);
    const std::optional<std::vector<std::string>> handled =
        semantic_nodes::RunTaskForCompletedNodes(maa_context_, kObstacleDeviceEntry, "{}");
    if (!handled) {
        LogWarn << "Blocking-device subtask result unavailable.";
        return DeviceRemovalOutcome::StillPinned;
    }
    if (const char* walk_out = WalkOutEntryFor(*handled); walk_out != nullptr) {
        // 搬起的设备会放回原处, 放下前须先走出来
        motion_controller_->ReassertForward();
        if (MaaContextRunTask(maa_context_, walk_out, "{}") == MaaInvalidId) {
            LogWarn << "Blocking-device walk-out did not run." << VAR(walk_out);
            return DeviceRemovalOutcome::StillPinned;
        }
    }

    if (!position_provider_->Capture(position_, false, session_->current_zone_id()) || position_provider_->LastCaptureWasBlackScreen()
        || !position_->valid) {
        // 旧定位会让随后的跳跃从旧点量位移, 误报已脱困
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
