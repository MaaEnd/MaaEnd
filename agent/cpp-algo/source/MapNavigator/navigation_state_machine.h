#pragma once

#include <chrono>
#include <functional>
#include <limits>

#include "navi_controller.h"
#include "navigation_session.h"

namespace mapnavigator
{

class IActionExecutor;
class ActionWrapper;
class LocalDriverLite;
class MotionController;
class PositionProvider;
class RouteRejoinPlanner;
class ZoneTransitionController;
struct RouteRejoinCandidate;

enum class RelocationCompletionPolicy
{
    ResumeRoute,
    RejoinOrFail,
};

class NavigationStateMachine
{
public:
    NavigationStateMachine(
        const NaviParam& param,
        ActionWrapper* action_wrapper,
        PositionProvider* position_provider,
        NavigationSession* session,
        RouteRejoinPlanner* rejoin_planner,
        MotionController* motion_controller,
        ZoneTransitionController* zone_transition_controller,
        LocalDriverLite* local_driver,
        IActionExecutor* action_executor,
        NaviPosition* position,
        std::function<bool()> should_stop);

    bool Run();

private:
    bool Bootstrap();
    bool TickPhase(NaviPhase phase);
    bool TickAdvanceOnRoute();
    bool TickWaitZoneTransition();
    bool TickWaitRelocation();
    bool TickAlignHeading();
    bool TickExactTargetRefine();
    bool ConsumeHeadingNodes(bool sync_with_sensor_yaw);
    bool HandleImplicitZoneTransition(const std::string& expected_zone_id);
    bool HandleWaypointArrival(
        double real_pos_x,
        double real_pos_y,
        double distance,
        double actual_distance,
        double portal_distance,
        bool advanced_by_pass_through,
        bool portal_commit_ready);
    bool ApplyRejoinCandidate(const RouteRejoinCandidate& candidate, const char* reason);
    bool AttemptRouteRejoin(const char* reason, bool require_forward_candidate);
    bool HasSevereTurnDrift(double yaw_mismatch, double actual_distance, int64_t stalled_ms) const;
    bool CanArriveSamePointAction(const Waypoint& waypoint) const;
    bool CanChainSamePointAction(const Waypoint& from_waypoint, const Waypoint& to_waypoint) const;
    void EnterRelocationWait(const char* reason, int min_pause_ms, bool require_movement, RelocationCompletionPolicy completion_policy);
    void MaybeTriggerAutoSprint(double actual_distance, double sensor_yaw_error, const std::chrono::steady_clock::time_point& now);
    void ResumeMotionTowardsCurrentWaypoint(double origin_x, double origin_y, const char* reason, int settle_wait_ms);
    void SelectPhaseForCurrentWaypoint(const char* reason);
    void SleepFor(int millis) const;

    const NaviParam& param_;
    ActionWrapper* action_wrapper_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    RouteRejoinPlanner* rejoin_planner_;
    MotionController* motion_controller_;
    ZoneTransitionController* zone_transition_controller_;
    LocalDriverLite* local_driver_;
    IActionExecutor* action_executor_;
    NaviPosition* position_;
    std::function<bool()> should_stop_;
    std::chrono::steady_clock::time_point last_auto_sprint_time_ {};
    std::chrono::steady_clock::time_point relocation_wait_started_ {};
    NaviPosition relocation_anchor_pos_ {};
    int relocation_min_pause_ms_ = 0;
    int relocation_stable_hits_ = 0;
    bool relocation_requires_movement_ = false;
    const char* relocation_reason_ = "";
    RelocationCompletionPolicy relocation_completion_policy_ = RelocationCompletionPolicy::ResumeRoute;
    bool pending_transfer_relocation_wait_ = false;
    bool relocation_finish_when_complete_ = false;
    size_t strict_arrival_walk_reset_node_idx_ = std::numeric_limits<size_t>::max();
};

} // namespace mapnavigator
