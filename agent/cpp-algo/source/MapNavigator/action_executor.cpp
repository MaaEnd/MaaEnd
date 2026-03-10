#include <chrono>
#include <thread>

#include <MaaUtils/Logger.h>

#include "action_executor.h"
#include "action_wrapper.h"
#include "local_driver_lite.h"
#include "motion_controller.h"
#include "navi_config.h"

namespace mapnavigator
{

ActionExecutor::ActionExecutor(
    ActionWrapper* action_wrapper,
    MotionController* motion_controller,
    LocalDriverLite* local_driver,
    bool enable_local_driver)
    : action_wrapper_(action_wrapper)
    , motion_controller_(motion_controller)
    , local_driver_(local_driver)
    , enable_local_driver_(enable_local_driver)
{
}

ActionExecutionResult ActionExecutor::Execute(ActionType action)
{
    ActionExecutionResult result;

    switch (action) {
    case ActionType::SPRINT:
        action_wrapper_->TriggerSprintSync();
        motion_controller_->NotifySprintTriggered();
        LogInfo << "Action: SPRINT triggered.";
        break;

    case ActionType::JUMP:
        motion_controller_->CancelSprintIfActive(kWalkResetReleaseMs);
        action_wrapper_->ClickKeySync(kKeySpace, kActionJumpHoldMs);
        LogInfo << "Action: JUMP triggered.";
        std::this_thread::sleep_for(std::chrono::milliseconds(kActionJumpSettleMs));
        break;

    case ActionType::INTERACT:
        motion_controller_->CancelSprintIfActive(kWalkResetReleaseMs);
        motion_controller_->Stop();
        for (int i = 0; i < kActionInteractAttempts; ++i) {
            action_wrapper_->ClickKeySync(kKeyF, kActionInteractHoldMs);
        }
        LogInfo << "Action: INTERACT completed.";
        break;

    case ActionType::FIGHT:
        motion_controller_->CancelSprintIfActive(kWalkResetReleaseMs);
        action_wrapper_->ClickMouseLeftSync();
        LogInfo << "Action: FIGHT triggered.";
        break;

    case ActionType::TRANSFER:
        LogInfo << "Action: TRANSFER reached. Waiting for relocation trigger.";
        break;

    case ActionType::PORTAL:
        motion_controller_->CancelSprintIfActive(kWalkResetReleaseMs);
        local_driver_->Reset();
        if (enable_local_driver_) {
            motion_controller_->SetAction(LocalDriverAction::Forward, true);
        }
        else if (!motion_controller_->IsMoving()) {
            motion_controller_->EnsureForwardMotion(false);
        }
        result.entered_portal_mode = true;
        LogInfo << "Action: PORTAL triggered. Entering blind-walk state...";
        break;

    case ActionType::RUN:
        break;

    case ActionType::HEADING:
        LogWarn << "HEADING action dispatched to ActionExecutor unexpectedly.";
        break;

    case ActionType::ZONE:
        LogWarn << "ZONE action dispatched to ActionExecutor unexpectedly.";
        break;
    }

    return result;
}

} // namespace mapnavigator
