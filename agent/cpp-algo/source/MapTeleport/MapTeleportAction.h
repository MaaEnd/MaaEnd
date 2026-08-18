#pragma once

#include "MaaFramework/MaaAPI.h"

namespace mapteleport
{

MaaBool MAA_CALL MapTeleportSelectRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_action_name,
    const char* custom_action_param,
    MaaRecoId reco_id,
    const MaaRect* box,
    void* trans_arg);

} // namespace mapteleport
