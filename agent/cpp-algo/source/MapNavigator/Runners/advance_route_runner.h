#pragma once

#include <chrono>
#include <functional>

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

class AdvanceRouteRunner
{
public:
    AdvanceRouteRunner(
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
    bool TryTurnInPlace(
        double sensor_yaw_error,
        double actual_distance,
        int64_t stalled_ms,
        bool post_turn_forward_commit_active,
        uint64_t frame_seq);
    bool TryMovingTrim(double guidance_yaw_error, bool strict_arrival, uint64_t frame_seq);
    bool ShouldFailForDivergence(double sensor_yaw_error, double actual_distance, int64_t stalled_ms) const;
    bool FailNavigation(const char* reason, double actual_distance, double sensor_yaw_error, int64_t stalled_ms, const char* log_message);
    double EstimateSprintSegmentDistance() const;
    void MaybeTriggerAutoSprint(double sprint_segment_distance, bool near_portal_transition);
    void SleepFor(int millis) const;

    const NaviParam& param_;
    ActionWrapper* action_wrapper_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    IActionExecutor* action_executor_;
    NaviPosition* position_;
    NavigationRuntimeState* runtime_state_;
    ZoneTransitionRunner* zone_transition_runner_;
    WaypointArrivalHandler waypoint_arrival_handler_;
    std::function<bool()> should_stop_;
    uint64_t next_navigation_frame_seq_ = 0;
};

} // namespace mapnavigator
