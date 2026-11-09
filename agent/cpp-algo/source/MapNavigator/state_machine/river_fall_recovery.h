#pragma once

#include "navigation_runtime_state.h"

namespace mapnavigator
{

class ActionWrapper;
class MotionController;

enum class RiverFallStep
{
    Cleared,
    Escaping,
};

RiverFallStep AdvanceRiverFallRecovery(
    RiverFallRecoveryState* state,
    ActionWrapper* action_wrapper,
    MotionController* motion_controller,
    const NaviPosition& position,
    double current_heading);

} // namespace mapnavigator
