#pragma once

#include "MaaFramework/MaaAPI.h"

namespace worldmap
{

MaaBool MAA_CALL MapFindRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    const MaaRect* roi_param,
    void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail);

} // namespace worldmap
