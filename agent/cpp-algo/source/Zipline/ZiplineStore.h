#pragma once

#include <filesystem>
#include <string>
#include <vector>

namespace zipline
{

// 一条滑索的落点，字段沿用森空岛接口的原义：x/z 张成水平面，y 是高度。
// 与 navmesh 的 WorldPoint 不是同一个坐标系，换算见 ZiplineFrames。
struct ZiplineMark
{
    std::string template_id;
    // 森空岛的楼层编号。同一张图的不同层之间挂不上索，配对时要靠它把跨层的对排除掉。
    std::string level_id;
    double x = 0.0;
    double y = 0.0;
    double z = 0.0;
};

// 一张森空岛地图上抓到的全部滑索。map_id 是森空岛的地图编号，与 navmesh 的 zone_name
// 是两套互不相干的编号，对应关系由 ZiplineFrames 给出。
struct ZiplineMapRecord
{
    std::string map_id;
    std::string fetched_at;
    std::vector<ZiplineMark> marks;
};

// debug/record/Ziplines.json 的读写。
class ZiplineStore
{
public:
    // 锚在 exe 上（<exe>/../debug/record/Ziplines.json），不随工作目录漂移。
    static std::filesystem::path DefaultPath();

    // 文件不存在按空库处理并返回 true，只有内容坏掉才返回 false：首次导入不该被当成故障。
    bool load(const std::filesystem::path& path);
    bool save(const std::filesystem::path& path) const;

    // 按 map_id 整张替换，不做逐条合并。拆掉的滑索必须随之消失，否则会在图里留下一条
    // 走不通的边——那比缺这条滑索更糟，规划会反复选中它再失败。
    void replaceMap(ZiplineMapRecord record);

    const std::vector<ZiplineMapRecord>& maps() const { return maps_; }

private:
    std::vector<ZiplineMapRecord> maps_;
};

// 当前 UTC 时刻的 ISO8601 串，落盘时间戳用。
std::string CurrentTimestamp();

} // namespace zipline
