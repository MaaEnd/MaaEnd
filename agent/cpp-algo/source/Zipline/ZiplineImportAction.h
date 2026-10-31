#pragma once

#include <MaaFramework/MaaAPI.h>

namespace zipline
{

// 被动复制官方地图页取回的滑索标记, 不读也不存任何凭据, attach.clear_login 为真时清空浏览器登录态
MaaBool MAA_CALL ZiplineImportActionRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_action_name,
    const char* custom_action_param,
    MaaRecoId reco_id,
    const MaaRect* box,
    void* trans_arg);

} // namespace zipline
