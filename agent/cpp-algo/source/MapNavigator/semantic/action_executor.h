#pragma once

namespace mapnavigator
{

class MotionController;
class ActionWrapper;

class ActionExecutor
{
public:
    ActionExecutor(ActionWrapper* action_wrapper, MotionController* motion_controller, bool enable_local_driver);

    void Sprint();
    void Jump();
    void Interact();
    void Fight();

private:
    ActionWrapper* action_wrapper_;
    MotionController* motion_controller_;
};

} // namespace mapnavigator
