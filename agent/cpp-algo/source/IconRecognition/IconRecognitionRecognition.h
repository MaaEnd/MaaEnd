#pragma once

#include <MaaFramework/MaaAPI.h>

namespace iconrecognition
{

MaaBool MAA_CALL IconRecognitionRun(
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

} // namespace iconrecognition
