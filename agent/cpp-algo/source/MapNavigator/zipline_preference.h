#pragma once

#include <MaaFramework/MaaDef.h>

namespace mapnavigator
{

// 前端设置里的滑索三态,覆盖单条请求里写的 zip。读不到设置就返回 requested,
// 也就是老行为:路线自己说要用才用。
bool ResolveZiplineEnabled(MaaContext* context, bool requested);

} // namespace mapnavigator
