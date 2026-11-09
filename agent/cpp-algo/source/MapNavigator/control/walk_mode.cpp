#include "walk_mode.h"

#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"

namespace mapnavigator::walkmode
{

bool Engaged(const WalkBelief& belief)
{
    return belief.walking;
}

void NoteSprintTriggered(WalkBelief* belief)
{
    if (!belief->walking) {
        return;
    }
    belief->walking = false;
    LogInfo << "Walk mode cleared by sprint (the game leaves walking on its own).";
}

void RequestWalk(WalkBelief* belief, InputBackend* input_backend, bool walk)
{
    if (walk == belief->walking) {
        return;
    }
    if (input_backend == nullptr || !SupportsWalkToggle(*input_backend)) {
        return;
    }

    ToggleWalkModeSync(input_backend);
    belief->walking = walk;
    LogInfo << (walk ? "Walk mode engaged (game walking speed)." : "Walk mode released (back to jogging).");
}

void RestoreJogIfBeliefSurvived(WalkBelief* belief, InputBackend* input_backend)
{
    if (!belief->walking) {
        return;
    }
    LogWarn << "Walk mode belief survived the previous run; restoring jogging before navigating.";
    RequestWalk(belief, input_backend, false);
}

} // namespace mapnavigator::walkmode
