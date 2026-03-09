#pragma once

#include "MaaFramework/MaaAPI.h"

namespace mapnavigator
{

MaaBool MAA_CALL MapNavigateActionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg);

} // namespace mapnavigator
