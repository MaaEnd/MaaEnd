#pragma once

#include <chrono>
#include <functional>
#include <limits>
#include <string>

#include "../navigation_runtime_state.h"
#include "../navi_controller.h"
#include "common/waypoint_arrival_handler.h"

namespace mapnavigator
{

class ActionWrapper;
class IActionExecutor;
class MotionController;
class PositionProvider;
class ZoneTransitionRunner;
struct NavigationSession;
struct NaviPosition;

class SerialRouteRunner
{
public:
    SerialRouteRunner(
        const NaviParam& param,
        ActionWrapper* action_wrapper,
        PositionProvider* position_provider,
        NavigationSession* session,
        MotionController* motion_controller,
        IActionExecutor* action_executor,
        NaviPosition* position,
        NavigationRuntimeState* runtime_state,
        ZoneTransitionRunner* zone_transition_runner,
        std::function<bool()> should_stop);

    bool Tick();

private:
    bool IsWaypointArrived(double actual_distance, const Waypoint& waypoint) const;
    bool TryAlignTowardsWaypoint(const Waypoint& waypoint, const std::string& expected_zone_id, uint64_t frame_seq, double sensor_yaw_error);
    bool TryCompensateDeviation(
        const Waypoint& waypoint,
        const std::string& expected_zone_id,
        uint64_t frame_seq,
        double sensor_yaw_error,
        double actual_distance,
        double lateral_distance);
    bool TryRecoverFromSevereDeviation(
        const Waypoint& waypoint,
        const std::string& expected_zone_id,
        uint64_t frame_seq,
        double sensor_yaw_error,
        double actual_distance,
        double lateral_distance,
        int64_t stalled_ms);
    bool TryRecoverFromStallByJump(const Waypoint& waypoint, double actual_distance, int64_t stalled_ms);
    bool ShouldFailForDeviation(double lateral_distance, int64_t stalled_ms) const;
    bool FailNavigation(const char* reason, double actual_distance, double sensor_yaw_error, int64_t stalled_ms, const char* log_message);
    void RefreshSegmentState();
    void SleepFor(int millis) const;
    double ComputePathFollowYawError(const Waypoint& waypoint) const;
    static double ComputeLateralDistance(double ax, double ay, double bx, double by, double px, double py);

    const NaviParam& param_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    NaviPosition* position_;
    NavigationRuntimeState* runtime_state_;
    ZoneTransitionRunner* zone_transition_runner_;
    WaypointArrivalHandler waypoint_arrival_handler_;
    uint64_t next_navigation_frame_seq_ = 0;
    size_t tracking_waypoint_absolute_idx_ = std::numeric_limits<size_t>::max();
    bool segment_aligned_ = false;
    bool segment_compensated_ = false;
    bool segment_return_attempted_ = false;
    bool segment_jump_attempted_ = false;
    double segment_origin_x_ = 0.0;
    double segment_origin_y_ = 0.0;
    std::chrono::steady_clock::time_point segment_realign_not_before_ {};
    std::chrono::steady_clock::time_point segment_jump_grace_until_ {};
};

} // namespace mapnavigator
