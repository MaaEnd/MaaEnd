#pragma once

#include <array>
#include <filesystem>
#include <string>
#include <vector>

#include "ZiplineStore.h"

namespace zipline
{

// x/y 是 base 像素, height 是世界高度, world_* 是原始世界坐标, 量索长只能用它
struct ZiplineNode
{
    double x = 0.0;
    double y = 0.0;
    double height = 0.0;
    double world_x = 0.0;
    double world_y = 0.0;
    double world_z = 0.0;
    std::string template_id;
    std::string level_id;
};

struct ZiplineFrame
{
    // 留空表示不限地图
    std::string map_id;
    std::string zone_name;
    // nav_x = plane[0] * sx + plane[1] * sz + plane[2], nav_y = plane[3] * sx + plane[4] * sz + plane[5]
    std::array<double, 6> plane { 1.0, 0.0, 0.0, 0.0, 1.0, 0.0 };
    // nav_height = height_scale * sy + height_offset
    double height_scale = 1.0;
    double height_offset = 0.0;
    // 参与规划的 templateId, 留空表示全都算数
    std::vector<std::string> template_ids;

    ZiplineNode project(const ZiplineMark& mark) const;
    bool accepts(const ZiplineMark& mark) const;
};

// 一根索的长度上限取两端架子里较小的那个
struct ZiplineType
{
    std::string template_id;
    std::string name;
    double max_span = 0.0; // 索长上限, 世界距离
    // 工厂网格上的水平占地 [x, z], 单位格
    std::array<int, 2> footprint { 1, 1 };
};

struct ZiplinePowerSource
{
    std::string template_id;
    std::string name;
    // 工厂网格上的水平占地 [x, z], 单位格
    std::array<int, 2> footprint { 1, 1 };
    // 圆形供电半径, 世界距离, 只量水平, 与 coverage_size 二选一
    double radius = 0.0;
    // 以中心格为中心的覆盖尺寸 [x, z], 只接受奇数, 与 radius 二选一
    std::array<int, 2> coverage_size { 0, 0 };
};

// 各项都已折算成步行距离, 单位与 navmesh 路径长度相同
struct ZiplineCostModel
{
    // 滑索耗时与步行同样距离耗时之比
    double speed_ratio = 0.35;
    double mount_penalty = 25.0;
    double transfer_penalty = 10.0;
    double min_gain = 25.0;
    // 架子离可走面超过这个距离就不当上下索点, 中途落点不受此限
    double reach_radius = 6.0;
};

class ZiplineFrames
{
public:
    // <exe>/../data/MapNavigator/zipline_frames.json
    static std::filesystem::path DefaultPath();

    // 文件不存在视为没有任何标定并返回 true
    bool load(const std::filesystem::path& path);

    // 返回 nullptr 表示该区不能用滑索
    const ZiplineFrame* findByZone(const std::string& zone_name) const;

    // 没登记过的类型返回 0, 配不成任何一对
    double maxSpan(const std::string& template_id) const;

    // 没登记过的类型按一个中心格处理
    std::array<int, 2> footprint(const std::string& template_id) const;

    const ZiplinePowerSource* powerSource(const std::string& template_id) const;

    // 为 false 时不做通电筛选
    bool hasPowerSources() const { return !power_sources_.empty(); }

    const ZiplineCostModel& cost() const { return cost_; }

    bool empty() const { return frames_.empty(); }

    // 去重, 不含留空的不限地图条目
    std::vector<std::string> mapIds() const;

private:
    std::vector<ZiplineFrame> frames_;
    std::vector<ZiplineType> types_;
    std::vector<ZiplinePowerSource> power_sources_;
    ZiplineCostModel cost_;
};

} // namespace zipline
