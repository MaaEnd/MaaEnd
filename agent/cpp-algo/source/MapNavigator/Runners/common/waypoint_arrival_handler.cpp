#include <chrono>
#include <thread>

#include <MaaUtils/Logger.h>

#include "../../action_executor.h"
#include "../../motion_controller.h"
#include "../../navi_config.h"
#include "../../navigation_session.h"
#include "../../position_provider.h"
#include "runner_phase_utils.h"
#include "waypoint_arrival_handler.h"

namespace mapnavigator
{

namespace
{

void SleepFor(int millis)
{
    if (millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(millis));
}

} // namespace

WaypointArrivalHandler::WaypointArrivalHandler(
    PositionProvider* position_provider,
    NavigationSession* session,
    MotionController* motion_controller,
    IActionExecutor* action_executor,
    NaviPosition* position,
    NavigationRuntimeState* runtime_state)
    : position_provider_(position_provider)
    , session_(session)
    , motion_controller_(motion_controller)
    , action_executor_(action_executor)
    , position_(position)
    , runtime_state_(runtime_state)
{
}

bool WaypointArrivalHandler::HandleArrival(
    double guidance_pos_x,
    double guidance_pos_y,
    double actual_distance,
    double portal_distance,
    bool advanced_by_pass_through,
    bool portal_commit_ready)
{
    (void)guidance_pos_x;
    (void)guidance_pos_y;
    (void)portal_distance;

    const Waypoint arrived_waypoint = session_->CurrentWaypoint();
    const ActionType current_action = arrived_waypoint.action;
    const size_t arrived_absolute_node_idx = session_->CurrentAbsoluteNodeIndex();

    if (advanced_by_pass_through) {
        LogInfo << "Advance waypoint by pass-through." << VAR(actual_distance) << VAR(position_->x) << VAR(position_->y);
    }
    if (portal_commit_ready) {
        LogInfo << "Commit PORTAL approach early." << VAR(actual_distance) << VAR(portal_distance);
    }

    if (arrived_waypoint.RequiresStrictArrival() && motion_controller_->IsMoving()) {
        StopMotionAndCommitment(motion_controller_);
        SleepFor(kStopWaitMs);
    }

    if (current_action == ActionType::TRANSFER) {
        StopMotionAndCommitment(motion_controller_);
        session_->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *position_, "transfer_wait_started");
        session_->AdvanceToNextWaypoint(ActionType::TRANSFER, "transfer_wait_started");
        runtime_state_->last_auto_sprint_time_ = {};
        session_->ResetDriverProgressTracking();
        runtime_state_->transfer_anchor_pos_ = *position_;
        runtime_state_->transfer_wait_started_ = std::chrono::steady_clock::now();
        runtime_state_->transfer_stable_hits_ = 0;
        position_provider_->ResetTracking();

        if (!session_->HasCurrentWaypoint()) {
            session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
            return true;
        }

        session_->UpdatePhase(NaviPhase::WaitTransfer, "transfer_wait_started");
        LogInfo << "Action: TRANSFER reached. Waiting for external movement to the next waypoint.";
        return true;
    }

    const ActionExecutionResult execution = action_executor_->Execute(current_action);
    if (execution.entered_portal_mode) {
        session_->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *position_, "portal_wait_zone_switch");
        session_->AdvanceToNextWaypoint(ActionType::PORTAL, "portal_wait_zone_switch");
        session_->SetWaitingForZoneSwitch(true);
        position_provider_->ResetTracking();
        session_->UpdatePhase(NaviPhase::WaitZoneTransition, "portal_wait_zone_switch");
        return true;
    }

    session_->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *position_, "waypoint_action_completed");
    session_->AdvanceToNextWaypoint(current_action, "waypoint_action_completed");
    runtime_state_->last_auto_sprint_time_ = {};
    session_->ResetDriverProgressTracking();

    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        return true;
    }

    SelectPhaseForCurrentWaypoint(session_, position_, "waypoint_action_completed");
    return true;
}

} // namespace mapnavigator
