#pragma once

#include <cstdint>
#include <functional>

#include <MaaFramework/MaaAPI.h>

namespace cloud
{
// The web game's relative-input units differ from native PC mouse units.
// Stationary bidirectional measurements give about 4 units per degree in the
// cloud host's fixed 1280x720 CSS viewport (DPR 1), not desktop/device pixels.
// The host scales only presentation; do not multiply this by window size or DPI.
// Navigation's existing sensitivity observer can still correct user settings.
inline constexpr double kDefaultTurnUnitsPerDegree = 4.0;

// An empty callback means this is an ordinary controller. Platform adapters
// can supply relative input without exposing native handles to navigation.
using ViewInput = std::function<bool(int32_t, int32_t)>;
ViewInput CreateViewInput(MaaController* controller);

void MAA_CALL TaskerEvent(void* handle, const char* message, const char* details_json, void* trans_arg);
MaaBool MAA_CALL CloseActionRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_action_name,
    const char* custom_action_param,
    MaaRecoId reco_id,
    const MaaRect* box,
    void* trans_arg);

MaaBool MAA_CALL RelativeMoveActionRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_action_name,
    const char* custom_action_param,
    MaaRecoId reco_id,
    const MaaRect* box,
    void* trans_arg);
} // namespace cloud
