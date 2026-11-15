#pragma once

namespace mapnavigator
{
struct InputBackend;
}

namespace mapnavigator::walkmode
{

struct WalkBelief
{
    bool walking = false;
};

bool Engaged(const WalkBelief& belief);

// 冲刺会自动退出走路, 只更新记录不发键
void NoteSprintTriggered(WalkBelief* belief);

// 幂等, 后端无走路键时不生效, 记录只随真实按键变化
void RequestWalk(WalkBelief* belief, InputBackend* input_backend, bool walk);

void RestoreJogIfBeliefSurvived(WalkBelief* belief, InputBackend* input_backend);

// 导航中途异常退出也须恢复跑步
struct RestoreJogOnExit
{
    WalkBelief* belief;
    InputBackend* input_backend;

    ~RestoreJogOnExit() { RequestWalk(belief, input_backend, false); }
};

} // namespace mapnavigator::walkmode
