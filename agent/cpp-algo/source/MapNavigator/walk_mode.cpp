#include "walk_mode.h"

#include <MaaUtils/Logger.h>

#include "action_wrapper.h"

namespace mapnavigator::walkmode
{

namespace
{
// The game's toggle state, not a run's — hence process-global.
bool g_walking = false;
}

bool Engaged()
{
    return g_walking;
}

void NoteSprintTriggered()
{
    if (!g_walking) {
        return;
    }
    g_walking = false;
    LogInfo << "Walk mode cleared by sprint (the game leaves walking on its own).";
}

Toggle::Toggle(ActionWrapper* action_wrapper)
    : action_wrapper_(action_wrapper)
{
    // Survived into a new run = the last one failed to unwind; put the game back before navigating.
    if (g_walking) {
        LogWarn << "Walk mode belief survived the previous run; restoring jogging before navigating.";
        Request(false);
    }
}

Toggle::~Toggle()
{
    Request(false);
}

void Toggle::Request(bool walk)
{
    if (walk == g_walking) {
        return;
    }
    if (action_wrapper_ == nullptr || !action_wrapper_->SupportsWalkToggle()) {
        return; // no binding on this backend
    }

    action_wrapper_->ToggleWalkModeSync();
    g_walking = walk;
    LogInfo << (walk ? "Walk mode engaged (game walking speed)." : "Walk mode released (back to jogging).");
}

} // namespace mapnavigator::walkmode
