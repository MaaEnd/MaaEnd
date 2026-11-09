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
#include "../navi_time.h"
#include "../planning/navigation_session.h"
#include "../planning/route_tracker.h"
#include "../sensing/maa_task.h"
#include "../sensing/position_provider.h"
#include "../sensing/roi_template_scanner.h"
#include "controller_type_utils.h"
#include "motion_controller.h"

namespace mapnavigator
{

namespace
{

const char* walk_out_entry_for(const std::vector<std::string>& handled)
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

ObstacleDeviceRecovery MakeDeviceRecovery(
    MaaContext* maa_context,
    Motion* motion_controller,
    PositionSensor* position_provider,
    NavigationSession* session,
    NaviPosition* position)
{
    ObstacleDeviceRecovery recovery;
    recovery.maa_context = maa_context;
    recovery.motion_controller = motion_controller;
    recovery.position_provider = position_provider;
    recovery.session = session;
    recovery.position = position;
    return recovery;
}

void StartDeviceRecovery(ObstacleDeviceRecovery* recovery, const cv::Rect& base_roi, std::string_view controller_type)
{
    if (recovery->scanner != nullptr) {
        return;
    }

    const std::vector<std::filesystem::path> roots = ResourceImageRoots(controller_type);
    cv::Mat button_template;
    if (const std::optional<std::filesystem::path> template_path = ResolveResourceImage(roots, kObstacleDeviceTemplateRelativePath)) {
        button_template = MAA_NS::imread(*template_path, cv::IMREAD_GRAYSCALE);
    }
    if (button_template.empty()) {
        LogWarn << "Blocking-device probe not started: interact button template not loaded." << VAR(kObstacleDeviceTemplateRelativePath)
                << VAR(DescribeRoots(roots));
        return;
    }

    recovery->scanner = std::make_unique<RoiTemplateScanner>(
        "device",
        base_roi,
        std::vector<ScanIcon> { ScanIcon { .templ = button_template, .threshold = kObstacleDeviceMatchThreshold } });
    LogInfo << "Blocking-device probe started." << VAR(base_roi.x) << VAR(base_roi.y) << VAR(base_roi.width) << VAR(base_roi.height);
}

void StopDeviceRecovery(ObstacleDeviceRecovery* recovery)
{
    recovery->scanner.reset();
    recovery->feeding = false;
}

void SubmitDeviceFrame(ObstacleDeviceRecovery* recovery, const cv::Mat& frame)
{
    if (recovery->scanner == nullptr || !recovery->feeding) {
        return;
    }
    recovery->scanner->submitFrame(frame);
}

void UpdateDeviceFeeding(ObstacleDeviceRecovery* recovery, int64_t stalled_ms, bool attempt_available)
{
    if (recovery->scanner == nullptr) {
        return;
    }
    const bool feed = stalled_ms > 0 && attempt_available;
    if (feed && !recovery->feeding) {
        recovery->scanner->consumeDetection(); // 重新开喂时丢掉停喂期间的命中
    }
    recovery->feeding = feed;
}

void ForgetDeviceObservation(ObstacleDeviceRecovery* recovery)
{
    if (recovery->scanner != nullptr) {
        recovery->scanner->consumeDetection();
    }
    recovery->feeding = false;
}

DeviceRemovalOutcome
    TryRemoveDevice(ObstacleDeviceRecovery* recovery, const RouteMeasurement& route, const Waypoint& waypoint, bool attempt_available)
{
    if (recovery->maa_context == nullptr || recovery->scanner == nullptr || !attempt_available) {
        return DeviceRemovalOutcome::NotAttempted;
    }
    if (!recovery->scanner->consumeDetection()) {
        return DeviceRemovalOutcome::NotAttempted;
    }

    const NaviPosition move_start = *recovery->position;
    LogInfo << "Dynamic recovery carrying off the blocking device." << VAR(route.waypoint_distance);
    // 边走边点会点到滚到下面的另一行提示, 移动点击前须站定
    HoldPosition(recovery->motion_controller);
    recovery->session->noteInteraction();
    const std::optional<std::vector<std::string>> handled = RunTaskForCompletedNodes(recovery->maa_context, kObstacleDeviceEntry, "{}");
    if (!handled) {
        LogWarn << "Blocking-device subtask result unavailable.";
        return DeviceRemovalOutcome::StillPinned;
    }
    // 认不出点开的界面时设备子任务停在 InteractPost
    if (!handled->empty() && handled->back() == kObstacleDeviceInteractPostNode) {
        LogWarn << "Blocking-device click opened an unrecognized screen.";
        return DeviceRemovalOutcome::UnknownUi;
    }
    if (const char* walk_out = walk_out_entry_for(*handled); walk_out != nullptr) {
        // 搬起的设备会放回原处, 放下前须先走出来
        ReassertForward(recovery->motion_controller);
        if (MaaContextRunTask(recovery->maa_context, walk_out, "{}") == MaaInvalidId) {
            LogWarn << "Blocking-device walk-out did not run." << VAR(walk_out);
            return DeviceRemovalOutcome::StillPinned;
        }
    }

    if (!CaptureTracked(recovery->position_provider, recovery->position, recovery->session->currentZoneId())
        || recovery->position_provider->last_capture_was_black_screen || !recovery->position->valid) {
        // 旧定位会让随后的跳跃从旧点量位移, 误报已脱困
        LogWarn << "Dynamic recovery waiting for a local tracking fix after the device move.";
        SleepFor(kTargetTickMs);
        return DeviceRemovalOutcome::NeedsFreshFix;
    }

    const bool zone_changed =
        !move_start.zone_id.empty() && !recovery->position->zone_id.empty() && move_start.zone_id != recovery->position->zone_id;
    const double displacement = std::hypot(recovery->position->x - move_start.x, recovery->position->y - move_start.y);
    const double move_waypoint_distance = std::hypot(waypoint.x - recovery->position->x, waypoint.y - recovery->position->y);
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
