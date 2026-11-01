#pragma once

#include <MaaFramework/MaaDef.h>

namespace mapnavmesh
{

// param.op 选查询类型, 识别恒命中, 结果与失败原因都写进 out_detail
MaaBool MAA_CALL MapNavmeshQueryRun(
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

}
