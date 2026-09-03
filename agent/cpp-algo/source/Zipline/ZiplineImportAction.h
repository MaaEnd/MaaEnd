#pragma once

#include <MaaFramework/MaaAPI.h>

namespace zipline
{

// 打开官方地图页（国服森空岛、国际服 SKPORT），把页面自己拉到的滑索标记记一份进
// debug/record/Ziplines.json。开哪个站由 param 里的 url 决定。
//
// 取数是被动的：不构造请求、不复刻签名、不读取也不保存任何凭据，只订阅 CDP 的
// Network 事件，等页面把标记列表取回来之后复制一份响应体。登录态由 WebView2 自己的
// 用户数据目录持有，本模块从头到尾没有接触过 token。
//
// 因此覆盖范围取决于用户在页面上浏览过哪些地图：浏览到哪张，就得到哪张的滑索。
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
