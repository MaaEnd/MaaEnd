#pragma once

namespace mapnavigator
{
struct InputBackend;
}

namespace mapnavigator::walkmode
{

bool Engaged();

// 冲刺会自动退出走路, 只更新记录不发键
void NoteSprintTriggered();

struct Toggle
{
    InputBackend* input_backend = nullptr;
};

Toggle OpenToggle(InputBackend* input_backend);
void CloseToggle(const Toggle& toggle);
// 幂等, 后端无走路键时不生效, 记录只随真实按键变化
void RequestWalk(const Toggle& toggle, bool walk);

// 持有者构造中途失败或异常退出也须恢复跑步
struct RestoreJogOnExit
{
    const Toggle& toggle;

    ~RestoreJogOnExit() { CloseToggle(toggle); }
};

} // namespace mapnavigator::walkmode
