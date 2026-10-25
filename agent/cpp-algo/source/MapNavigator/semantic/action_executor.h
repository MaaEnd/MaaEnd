#pragma once

namespace mapnavigator
{

class MotionController;
class ActionWrapper;

class ActionExecutor
{
public:
    ActionExecutor(ActionWrapper* action_wrapper, MotionController* motion_controller);

    void sprint();
    void jump();
    void interact();
    void fight();

private:
    ActionWrapper* action_wrapper_;
    MotionController* motion_controller_;
};

} // namespace mapnavigator
