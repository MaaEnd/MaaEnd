#pragma once

#include <cstddef>
#include <filesystem>
#include <string>
#include <vector>

namespace zipline
{

// 原始世界坐标, x/z 张成水平面, y 是高度, 换算见 ZiplineFrames
struct ZiplineMark
{
    std::string template_id;
    std::string level_id;
    double x = 0.0;
    double y = 0.0;
    double z = 0.0;
};

// map_id 与 navmesh 的 zone_name 无关, 对应关系由 ZiplineFrames 给出
struct ZiplineMapRecord
{
    // 由 HashUidForAccount 生成, 为空表示待 claimLegacyRecords 认领的无主记录
    std::string account_id;
    std::string map_id;
    std::string fetched_at;
    std::vector<ZiplineMark> marks;
};

class ZiplineStore
{
public:
    // 锚在启动时的工作目录, 不随后续工作目录变化
    static std::filesystem::path DefaultPath();

    // 文件不存在视为空库并返回 true, 只有内容坏掉才返回 false
    bool load(const std::filesystem::path& path);
    bool save(const std::filesystem::path& path) const;

    // 按 (account_id, map_id) 整张替换, 拆掉的滑索必须随之消失
    void replaceMap(ZiplineMapRecord record);

    // 只在该账号名下没有任何记录时认领无主记录并落盘, 返回认领条数
    size_t claimLegacyRecords(const std::string& account_id, const std::filesystem::path& path);

    // 仅供离线预览兜底, 运行时只认 CaptureUid 发布的当前游戏账号
    std::string latestAccountId() const;

    const std::vector<ZiplineMapRecord>& maps() const { return maps_; }

private:
    std::vector<ZiplineMapRecord> maps_;
};

// 当前 UTC 时刻的 ISO8601 串
std::string CurrentTimestamp();

} // namespace zipline
