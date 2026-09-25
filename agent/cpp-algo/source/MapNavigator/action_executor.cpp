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

void ActionExecutor::Sprint()
{
    if (motion_controller_->TriggerSprint()) {
        LogInfo << "Action: SPRINT triggered.";
    }
    else {
        LogInfo << "Action: SPRINT skipped because backend does not support sprint.";
    }
}

void ActionExecutor::Jump()
{
    motion_controller_->SetForwardState(false);
    action_wrapper_->TriggerJumpSync(kActionJumpHoldMs);
    LogInfo << "Action: JUMP triggered.";
    utils::SleepFor(kActionJumpSettleMs);
}

void ActionExecutor::Interact()
{
    motion_controller_->SetForwardState(false);
    for (int i = 0; i < kActionInteractAttempts; ++i) {
        action_wrapper_->TriggerInteractSync(kActionInteractHoldMs);
    }
    LogInfo << "Action: INTERACT completed.";
}

void ActionExecutor::Fight()
{
    motion_controller_->SetForwardState(false);
    action_wrapper_->ClickMouseLeftSync();
    LogInfo << "Action: FIGHT triggered.";
}

} // namespace mapnavigator
