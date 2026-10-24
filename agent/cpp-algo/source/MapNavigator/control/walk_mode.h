#pragma once

namespace mapnavigator
{
class ActionWrapper;
}

namespace mapnavigator::walkmode
{

bool Engaged();

// 冲刺会自动退出走路, 只更新记录不发键
void NoteSprintTriggered();

class Toggle
{
public:
    explicit Toggle(ActionWrapper* action_wrapper);
    ~Toggle();

    Toggle(const Toggle&) = delete;
    Toggle& operator=(const Toggle&) = delete;

    // 幂等, 后端无走路键时不生效, 记录只随真实按键变化
    void Request(bool walk);

    bool engaged() const { return Engaged(); }

private:
    ActionWrapper* action_wrapper_ = nullptr;
};

} // namespace mapnavigator::walkmode
