#pragma once

#include <atomic>
#include <cstddef>
#include <string>

#include <MaaFramework/MaaDef.h>

namespace mapnavigator
{

struct ZiplineOutcome;

struct ZiplineNoticeLatch
{
    std::atomic_bool told_no_data { false };
    std::atomic_bool told_account_unknown { false };
};

// 前端设置的三态覆盖请求里的 zip, 读不到设置时返回 requested
bool ResolveZiplineEnabled(MaaContext* context, bool requested);

// 取不到时返回空串
std::string ReadOrCaptureZiplineAccountId(MaaContext* context);

// 把不带账号字段的坐标记录认领给当前账号
size_t ClaimLegacyZiplineRecords(const std::string& account_id);

void NoticeClaimedLegacyZiplineRecords(MaaContext* context, size_t claimed);

void NoticeZiplineOutcome(ZiplineNoticeLatch* latch, MaaContext* context, const ZiplineOutcome& outcome);

} // namespace mapnavigator
