#include "walk_mode.h"

#include <MaaUtils/Logger.h>

#include "../Backend/backend.h"

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

Toggle OpenToggle(InputBackend* input_backend)
{
    Toggle toggle;
    toggle.input_backend = input_backend;
    if (g_walking) {
        LogWarn << "Walk mode belief survived the previous run; restoring jogging before navigating.";
        RequestWalk(toggle, false);
    }
    return toggle;
}

void CloseToggle(const Toggle& toggle)
{
    RequestWalk(toggle, false);
}

void RequestWalk(const Toggle& toggle, bool walk)
{
    if (walk == g_walking) {
        return;
    }
    if (toggle.input_backend == nullptr || !SupportsWalkToggle(*toggle.input_backend)) {
        return;
    }

    ToggleWalkModeSync(toggle.input_backend);
    g_walking = walk;
    LogInfo << (walk ? "Walk mode engaged (game walking speed)." : "Walk mode released (back to jogging).");
}

} // namespace mapnavigator::walkmode
