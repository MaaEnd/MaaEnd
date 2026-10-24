#pragma once

#include <string>

#include <MaaFramework/MaaDef.h>

namespace mapnavigator
{

// 前端设置的三态覆盖请求里的 zip, 读不到设置时返回 requested
bool ResolveZiplineEnabled(MaaContext* context, bool requested);

// 取不到时返回空串, 取到后顺带把不带账号字段的坐标记录认领给当前账号
std::string ResolveZiplineAccountId(MaaContext* context);

// 每种原因只提示一次, 滑索真正用上后才复位
void NoticeZiplineOutcome(MaaContext* context);

} // namespace mapnavigator
