#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "action_executor.h"

namespace mapnavigator
{

ActionExecutor::ActionExecutor(ActionWrapper* action_wrapper, MotionController* motion_controller)
    : action_wrapper_(action_wrapper)
    , motion_controller_(motion_controller)
{
}

void ActionExecutor::sprint()
{
    if (motion_controller_->triggerSprint()) {
        LogInfo << "Action: SPRINT triggered.";
    }
    else {
        LogInfo << "Action: SPRINT skipped.";
    }
}

void ActionExecutor::jump()
{
    motion_controller_->setForwardState(false);
    action_wrapper_->triggerJumpSync(kActionJumpHoldMs);
    LogInfo << "Action: JUMP triggered.";
    SleepFor(kActionJumpSettleMs);
}

void ActionExecutor::interact()
{
    motion_controller_->setForwardState(false);
    for (int i = 0; i < kActionInteractAttempts; ++i) {
        action_wrapper_->triggerInteractSync(kActionInteractHoldMs);
    }
    LogInfo << "Action: INTERACT completed.";
}

void ActionExecutor::fight()
{
    motion_controller_->setForwardState(false);
    action_wrapper_->clickMouseLeftSync();
    LogInfo << "Action: FIGHT triggered.";
}

} // namespace mapnavigator
