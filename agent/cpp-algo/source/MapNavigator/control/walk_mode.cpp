#include "walk_mode.h"

#include <MaaUtils/Logger.h>

#include "action_wrapper.h"

namespace mapnavigator::walkmode
{

namespace
{
// 游戏的走路切换状态跨导航保留, 所以放在进程级
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
    if (g_walking) {
        LogWarn << "Walk mode belief survived the previous run; restoring jogging before navigating.";
        request(false);
    }
}

Toggle::~Toggle()
{
    request(false);
}

void Toggle::request(bool walk)
{
    if (walk == g_walking) {
        return;
    }
    if (action_wrapper_ == nullptr || !action_wrapper_->supportsWalkToggle()) {
        return;
    }

    action_wrapper_->toggleWalkModeSync();
    g_walking = walk;
    LogInfo << (walk ? "Walk mode engaged (game walking speed)." : "Walk mode released (back to jogging).");
}

} // namespace mapnavigator::walkmode
