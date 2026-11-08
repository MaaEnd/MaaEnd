#pragma once

#include "navigation_runtime_state.h"

namespace mapnavigator
{

struct Motion;
struct InputBackend;

enum class RiverFallStep
{
    Cleared,
    Escaping,
};

RiverFallStep AdvanceRiverFallRecovery(
    RiverFallRecoveryState* state,
    InputBackend* input_backend,
    Motion* motion_controller,
    const NaviPosition& position,
    double current_heading);

} // namespace mapnavigator
