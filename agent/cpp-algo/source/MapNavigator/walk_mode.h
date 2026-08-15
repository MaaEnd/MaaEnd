#pragma once

namespace mapnavigator
{
class ActionWrapper;
}

namespace mapnavigator::walkmode
{

// The game's walk toggle is a press-to-flip state that outlives a single run, so the belief is process-global
// and the restore is a destructor.
bool Engaged();

// Sprint leaves walking on its own — adopt that instead of sending a key.
void NoteSprintTriggered();

class Toggle
{
public:
    explicit Toggle(ActionWrapper* action_wrapper);
    // Restores jogging on every exit path: success, failure, user stop, exception unwind.
    ~Toggle();

    Toggle(const Toggle&) = delete;
    Toggle& operator=(const Toggle&) = delete;

    // Idempotent; a no-op on backends with no binding — the belief only moves with a real press.
    void Request(bool walk);
    bool engaged() const { return Engaged(); }

private:
    ActionWrapper* action_wrapper_ = nullptr;
};

} // namespace mapnavigator::walkmode
