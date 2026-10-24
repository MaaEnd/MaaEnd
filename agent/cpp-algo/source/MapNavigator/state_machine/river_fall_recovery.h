#pragma once

#include "navigation_runtime_state.h"

namespace mapnavigator
{

class ActionWrapper;
class MotionController;

class RiverFallRecovery
{
public:
    enum class Step
    {
        Cleared,
        Escaping,
    };

    RiverFallRecovery(
        RiverFallRecoveryState& state,
        ActionWrapper* action_wrapper,
        MotionController* motion_controller,
        const NaviPosition* position);

    Step Advance(double current_heading);

private:
    RiverFallRecoveryState& state_;
    ActionWrapper* action_wrapper_;
    MotionController* motion_controller_;
    const NaviPosition* position_;
};

} // namespace mapnavigator
