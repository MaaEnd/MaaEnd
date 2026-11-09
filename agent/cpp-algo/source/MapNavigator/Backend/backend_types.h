#pragma once

namespace mapnavigator
{

enum class BackendKind
{
    Desktop,
    Linux,
    Adb,
};

struct SteeringTransportProfile
{
    bool supports_concurrent_move_and_look = true;
    int min_send_interval_ms = 0;
    double min_emit_delta_deg = 1.0;
    double max_batch_delta_deg = 18.0;
    int action_quiet_period_ms = 0;
    bool drops_turn_sends = false;
};

struct MovementState
{
    bool forward = false;
    bool left = false;
    bool backward = false;
    bool right = false;
};

} // namespace mapnavigator
