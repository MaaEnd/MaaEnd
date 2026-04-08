#include <cmath>
#include <thread>

#include <MaaUtils/Logger.h>

#include "../action_wrapper.h"
#include "../motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navigation_session.h"
#include "common/runner_phase_utils.h"
#include "heading_align_runner.h"

namespace mapnavigator
{

HeadingAlignRunner::HeadingAlignRunner(
    ActionWrapper* action_wrapper,
    NavigationSession* session,
    MotionController* motion_controller,
    NaviPosition* position,
    std::function<bool()> should_stop)
    : action_wrapper_(action_wrapper)
    , session_(session)
    , motion_controller_(motion_controller)
    , position_(position)
    , should_stop_(std::move(should_stop))
{
}

bool HeadingAlignRunner::Tick()
{
    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        LogInfo << "Heading-only route tail consumed; final success will use stable session evidence.";
        return true;
    }

    if (!session_->CurrentWaypoint().IsHeadingOnly()) {
        mapnavigator::SelectPhaseForCurrentWaypoint(session_, position_, "align_phase_exit");
        return true;
    }

    ConsumeHeadingNodes(true);

    if (!session_->HasCurrentWaypoint()) {
        session_->NoteRouteTailConsumed(*position_, "route_tail_consumed");
        LogInfo << "Heading-only route tail consumed; final success will use stable session evidence.";
        return true;
    }

    mapnavigator::SelectPhaseForCurrentWaypoint(session_, position_, "heading_nodes_consumed");
    return true;
}

bool HeadingAlignRunner::ConsumeHeadingNodes(bool sync_with_sensor_yaw)
{
    if (sync_with_sensor_yaw && session_->HasCurrentWaypoint() && session_->CurrentWaypoint().IsHeadingOnly()) {
        session_->SyncVirtualYaw(position_->angle);
    }

    bool consumed = false;
    while (session_->HasCurrentWaypoint() && session_->CurrentWaypoint().IsHeadingOnly()) {
        const Waypoint& heading_node = session_->CurrentWaypoint();
        double target_heading = heading_node.heading_angle;
        target_heading = std::fmod(target_heading, 360.0);
        if (target_heading < 0.0) {
            target_heading += 360.0;
        }
        const double required_turn = NaviMath::NormalizeAngle(target_heading - session_->virtual_yaw());
        const size_t current_node_idx = session_->current_node_idx();
        const double virtual_yaw = session_->virtual_yaw();

        LogInfo << "HEADING node triggered." << VAR(current_node_idx) << VAR(target_heading) << VAR(virtual_yaw) << VAR(required_turn);

        if (motion_controller_->IsMoving()) {
            motion_controller_->Stop();
            SleepFor(kStopWaitMs);
        }

        const TurnCommandResult turn_result =
            motion_controller_
                ->InjectMouseAndTrack(required_turn, false, heading_node.zone_id, kWaitAfterFirstTurnMs, TurnActionKind::HeadingAlign);
        if (!turn_result.issued) {
            LogWarn << "Heading node turn injection failed." << VAR(current_node_idx) << VAR(required_turn);
            return consumed;
        }
        motion_controller_->ArmForwardCommit(required_turn, "heading");

        session_->AdvanceToNextWaypoint(ActionType::HEADING, "heading_consumed");
        session_->ResetStraightStableFrames();
        session_->ResetDriverProgressTracking();
        consumed = true;

        if (session_->HasCurrentWaypoint()) {
            motion_controller_->EnsureForwardMotion(true);
        }
        else {
            action_wrapper_->PulseForwardSync(kPostHeadingForwardPulseMs);
        }
    }

    return consumed;
}

void HeadingAlignRunner::SleepFor(int millis) const
{
    if (millis > 0) {
        std::this_thread::sleep_for(std::chrono::milliseconds(millis));
    }
}

} // namespace mapnavigator
