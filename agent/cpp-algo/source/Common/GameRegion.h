#pragma once

namespace gamesetting
{

enum class Region
{
    Unknown,
    CN,
    Global,
};

// DetectGameRegion 查找运行中的 Endfield.exe，按其目录下 SDK DLL 判区：
// hgsdk.dll（官服）或 PCGameSDK.dll（Bilibili 服）→ 国服；仅有 gfsdk.dll → 国际服。
// Windows 使用 PROCESS_QUERY_LIMITED_INFORMATION + QueryFullProcessImageNameW，避免过高进程权限。
// 成功结果会缓存；无法判定时返回 Region::Unknown。
Region DetectGameRegion();

} // namespace gamesetting
