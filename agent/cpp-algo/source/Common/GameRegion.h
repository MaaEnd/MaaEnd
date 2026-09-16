#pragma once

namespace gamesetting
{

enum class Region
{
    Unknown,
    CN,
    Global,
};

// DetectGameRegion 查找运行中的 Endfield.exe，按其目录下仅有的 hgsdk.dll / gfsdk.dll 判区。
// 成功结果会缓存；无法判定时返回 Region::Unknown。
Region DetectGameRegion();

} // namespace gamesetting
