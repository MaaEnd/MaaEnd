#pragma once

#include <MaaFramework/MaaAPI.h>

namespace weaponinventoryscan
{

MaaBool MAA_CALL WeaponInventoryScanRecognitionRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    const MaaRect* roi,
    void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail);

} // namespace weaponinventoryscan
