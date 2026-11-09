#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"
#include "../control/motion_controller.h"
#include "../control/walk_mode.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "action_executor.h"

namespace mapnavigator
{

void ExecuteSprint(Motion* motion_controller)
{
    if (SprintAllowed(*motion_controller)) {
        ArmSteeringQuietPeriod(motion_controller);
        TriggerSprintSync(motion_controller->input_backend);
        walkmode::NoteSprintTriggered();
        MarkSprintActive(motion_controller);
        LogInfo << "Action: SPRINT triggered.";
    }
    else {
        LogInfo << "Action: SPRINT skipped.";
    }
}

void ExecuteJump(InputBackend* input_backend)
{
    TriggerJumpSync(input_backend, kActionJumpHoldMs);
    LogInfo << "Action: JUMP triggered.";
    SleepFor(kActionJumpSettleMs);
}

void ExecuteInteract(InputBackend* input_backend)
{
    for (int i = 0; i < kActionInteractAttempts; ++i) {
        TriggerInteractSync(input_backend, kActionInteractHoldMs);
    }
    LogInfo << "Action: INTERACT completed.";
}

void ExecuteFight(InputBackend* input_backend)
{
    ClickMouseLeftSync(input_backend);
    LogInfo << "Action: FIGHT triggered.";
}

} // namespace mapnavigator
