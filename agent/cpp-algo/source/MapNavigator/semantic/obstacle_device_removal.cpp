#include "obstacle_device_removal.h"

#include <cmath>
#include <optional>
#include <string>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../control/obstacle_device_recovery.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "../planning/navigation_session.h"
#include "../planning/route_tracker.h"
#include "../sensing/maa_task.h"
#include "../sensing/position_provider.h"
#include "../state_machine/navigation_state_machine.h"

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

std::optional<DeviceRemovalOutcome> run_device_subtasks(Navigator* nav)
{
    // 边走边点会点到滚到下面的另一行提示, 移动点击前须站定
    HoldPosition(nav->motion_controller);
    NoteInteraction(nav->session);
    const std::optional<std::vector<std::string>> handled = RunTaskForCompletedNodes(nav->maa_context, kObstacleDeviceEntry, "{}");
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
        ReassertForward(nav->motion_controller);
        if (MaaContextRunTask(nav->maa_context, walk_out, "{}") == MaaInvalidId) {
            LogWarn << "Blocking-device walk-out did not run." << VAR(walk_out);
            return DeviceRemovalOutcome::StillPinned;
        }
    }
    return std::nullopt;
}

bool capture_fix_after_device_move(Navigator* nav)
{
    const bool captured = CaptureTracked(nav->position_provider, nav->position, nav->session->current_zone_id);
    return captured && !nav->position_provider->last_capture_was_black_screen && nav->position->valid;
}

DeviceRemovalOutcome classify_device_move(
    const NaviPosition& move_start,
    const NaviPosition& position,
    const RouteMeasurement& route,
    const Waypoint& waypoint)
{
    const bool zone_changed = !move_start.zone_id.empty() && !position.zone_id.empty() && move_start.zone_id != position.zone_id;
    const double displacement = std::hypot(position.x - move_start.x, position.y - move_start.y);
    const double move_waypoint_distance = std::hypot(waypoint.x - position.x, waypoint.y - position.y);
    const bool made_progress = move_waypoint_distance + kNoProgressDistanceEpsilon < route.waypoint_distance;
    const bool moved_forward = displacement >= kDynamicRecoveryResetDistance * 0.5 && made_progress;
    if (zone_changed || displacement >= kDynamicRecoveryResetDistance || moved_forward) {
        LogInfo << "Dynamic recovery device move escaped obstacle." << VAR(zone_changed) << VAR(displacement) << VAR(moved_forward);
        return DeviceRemovalOutcome::Escaped;
    }

    LogInfo << "Device handled but the agent is still pinned; continuing to the jump." << VAR(displacement);
    return DeviceRemovalOutcome::StillPinned;
}

} // namespace

DeviceRemovalOutcome TryRemoveDevice(Navigator* nav, const RouteMeasurement& route, const Waypoint& waypoint)
{
    if (nav->maa_context == nullptr) {
        return DeviceRemovalOutcome::NotAttempted;
    }
    if (!DeviceProbeRunning(nav->device_recovery)) {
        return DeviceRemovalOutcome::NotAttempted;
    }
    if (!ConsumeDeviceDetection(&nav->device_recovery)) {
        return DeviceRemovalOutcome::NotAttempted;
    }

    const NaviPosition move_start = *nav->position;
    LogInfo << "Dynamic recovery carrying off the blocking device." << VAR(route.waypoint_distance);
    const std::optional<DeviceRemovalOutcome> subtask_outcome = run_device_subtasks(nav);
    if (subtask_outcome) {
        return *subtask_outcome;
    }

    if (!capture_fix_after_device_move(nav)) {
        // 旧定位会让随后的跳跃从旧点量位移, 误报已脱困
        LogWarn << "Dynamic recovery waiting for a local tracking fix after the device move.";
        SleepFor(kTargetTickMs);
        return DeviceRemovalOutcome::NeedsFreshFix;
    }

    return classify_device_move(move_start, *nav->position, route, waypoint);
}

} // namespace mapnavigator
