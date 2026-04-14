#include <chrono>
#include <thread>

#include <MaaUtils/Logger.h>

#include "action_executor.h"
#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"

namespace mapnavigator
{

ActionExecutor::ActionExecutor(ActionWrapper* action_wrapper, MotionController* motion_controller, bool enable_local_driver)
    : action_wrapper_(action_wrapper)
    , motion_controller_(motion_controller)
{
    (void)enable_local_driver;
}

ActionExecutionResult ActionExecutor::Execute(ActionType action)
{
    ActionExecutionResult result;

    switch (action) {
    case ActionType::SPRINT:
        if (motion_controller_->TriggerSprint()) {
            LogInfo << "Action: SPRINT triggered.";
        }
        else {
            LogInfo << "Action: SPRINT skipped because backend does not support sprint.";
        }
        break;

    case ActionType::JUMP:
        motion_controller_->SetForwardState(false);
        action_wrapper_->TriggerJumpSync(kActionJumpHoldMs);
        LogInfo << "Action: JUMP triggered.";
        utils::SleepFor(kActionJumpSettleMs);
        break;

    case ActionType::INTERACT:
        motion_controller_->SetForwardState(false);
        for (int i = 0; i < kActionInteractAttempts; ++i) {
            action_wrapper_->TriggerInteractSync(kActionInteractHoldMs);
        }
        LogInfo << "Action: INTERACT completed.";
        break;

    case ActionType::FIGHT:
        motion_controller_->SetForwardState(false);
        action_wrapper_->ClickMouseLeftSync();
        LogInfo << "Action: FIGHT triggered.";
        break;

    case ActionType::TRANSFER:
        LogWarn << "TRANSFER action dispatched to ActionExecutor unexpectedly.";
        break;

    case ActionType::PORTAL:
        motion_controller_->SetForwardState(false);
        result.entered_portal_mode = true;
        LogInfo << "Action: PORTAL triggered. Entering semantic transit flow...";
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
