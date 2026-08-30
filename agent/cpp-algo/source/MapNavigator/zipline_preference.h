#pragma once

#include <string>

#include <MaaFramework/MaaDef.h>

namespace mapnavigator
{

// 前端设置里的滑索三态,覆盖单条请求里写的 zip。读不到设置就返回 requested,
// 也就是老行为:路线自己说要用才用。
bool ResolveZiplineEnabled(MaaContext* context, bool requested);

// 读取 CaptureUid 写入 Resource 通用状态节点的伪匿名账号标识。节点缺失、为空或格式异常时返回空串。
std::string ResolveZiplineAccountId(MaaContext* context);

// 寻路结束时调,把滑索没用上的原因讲给用户听。两种原因各自只讲一次,直到滑索真的用上过
// 才复位:一条任务链里几十次寻路,同一句重复几十遍就成了噪声。
void NoticeZiplineOutcome(MaaContext* context);

} // namespace mapnavigator
