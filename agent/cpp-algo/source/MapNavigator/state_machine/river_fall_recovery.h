#pragma once

#include "navigation_runtime_state.h"

namespace mapnavigator
{

struct Motion;
struct InputBackend;

namespace sensitivity
{

struct SensitivityRun;

} // namespace sensitivity

enum class RiverFallStep
{
    Cleared,
    Escaping,
};

RiverFallStep AdvanceRiverFallRecovery(
    RiverFallRecoveryState* state,
    InputBackend* input_backend,
    Motion* motion_controller,
    sensitivity::SensitivityRun* sensitivity,
    const NaviPosition& position,
    double current_heading);

} // namespace mapnavigator
