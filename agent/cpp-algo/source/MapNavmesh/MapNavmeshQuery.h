#pragma once

#include <MaaFramework/MaaDef.h>

namespace mapnavmesh
{

// 把 navmesh 的几何、吸附与路线查询作为一次识别暴露出去：param 里的 op 选查询类型，
// 结果整个写进 out_detail。识别恒命中，规划不出来也要把原因带回调用方。
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
