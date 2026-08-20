#pragma once

#include <array>
#include <filesystem>
#include <string>
#include <vector>

#include "ZiplineStore.h"

namespace zipline
{

// 一根滑索架落在 navmesh 坐标系里的样子：x/y 是 base 像素，height 是世界高度，
// 与 BaseNavRouteRequest 的 floor_y 同一把尺子。
// 森空岛那边的原始坐标一并留着：判两根架子之间挂没挂索要量索长，而索长是物理量，
// 换算成像素会随地图的像素比例变，只能在世界坐标里量。
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

// 森空岛世界坐标 → navmesh 坐标的换算。两者是彼此独立的两套系，必须标定后才能互换；
// 没有标定过的地图一律不参与规划——宁可不走滑索，也不能照着错的坐标规划出一条假路。
//
// 平面部分是一个完整仿射，容得下旋转 / 翻转 / 缩放 / 平移：
//     nav_x = plane[0] * sx + plane[1] * sz + plane[2]
//     nav_y = plane[3] * sx + plane[4] * sz + plane[5]
// 高度部分是一条直线：nav_height = height_scale * sy + height_offset。
struct ZiplineFrame
{
    // 森空岛的地图编号。留空表示不限地图：标记照样参与规划，只是会多出拿别的图的标记
    // 去试算的那部分开销，被规划预算兜住。
    std::string map_id;
    std::string zone_name;
    std::array<double, 6> plane { 1.0, 0.0, 0.0, 0.0, 1.0, 0.0 };
    double height_scale = 1.0;
    double height_offset = 0.0;
    // 参与规划的 templateId，用来把供电桩、中继器之类的非滑索标记挡在外面。
    // 留空表示该地图下的标记全都算数。
    std::vector<std::string> template_ids;

    ZiplineNode project(const ZiplineMark& mark) const;
    bool accepts(const ZiplineMark& mark) const;
};

// 一种滑索架的物理属性。两根架子之间挂没挂索按几何判，而不同架子能拉多长的索不一样，
// 判的时候必须按类型分开看。
struct ZiplineType
{
    std::string template_id;
    // 给人看的名字，取自接口的 markTemplates，判定不读它。
    std::string name;
    // 同型两根架子之间的索长上限，单位是世界距离。
    double max_span = 0.0;
};

// 一种供电结构的供电范围。不通电的滑索架在游戏里走不了，靠它把这些架子挡在规划之外。
struct ZiplinePowerSource
{
    std::string template_id;
    // 给人看的名字，取自接口的 markTemplates，判定不读它。
    std::string name;
    // 供电半径，单位是世界距离，只量水平距离。
    double radius = 0.0;
};

// 滑索相对走路的代价折算。判据两边同乘走路速度后全是像素量纲，与 navmesh 的
// 路径长度同单位，因此不需要知道走路的绝对速度。
struct ZiplineCostModel
{
    // 滑索速度的倒数比：走同样距离，滑索花的时间相当于走路的几分之几。
    // 0.35 约等于「滑索比跑步快三倍」，是个待实测收紧的保守估计。
    double speed_ratio = 0.35;
    // 上索一次的固定开销，折算成「这段时间用走的能走多远」。
    // 含转向对准、交互、起步，同样待实测。
    double mount_penalty = 25.0;
    // 一条链里换乘一次的固定开销。落点就是下一根架子，不必走过去也不必重新起步，
    // 只剩重新对准和交互，所以比上索便宜。同样待实测。
    double transfer_penalty = 10.0;
    // 至少要比走路省下这么多才值得上索。省下的还不够一次上索的开销时，这点便宜是上面几个
    // 估计值的误差范围，换来的却是实打实的多一次交互和多一处会失败的地方。
    double min_gain = 25.0;
    // 架子的标记点离可走面超过这个距离就当摸不到，不再当上索点和下索点。
    // 中途落点不受此限：人是从索上落到下一根架子上的，脚下有没有可走面都站得住。
    double reach_radius = 6.0;
};

// zipline_frames.json 的读取。没有这份配置，滑索只会被导入和记录，不会参与规划。
class ZiplineFrames
{
public:
    // 锚在 exe 上（<exe>/../data/MapNavigator/zipline_frames.json）。
    static std::filesystem::path DefaultPath();

    // 文件不存在按「没有任何地图标定过」处理并返回 true。
    bool load(const std::filesystem::path& path);

    // 按 navmesh 的 zone_name 找标定。找不到表示该区不能用滑索。
    const ZiplineFrame* findByZone(const std::string& zone_name) const;

    // 这种架子的索长上限。没登记过的类型返回 0，配不成任何一对——
    // 忘了登记的后果是这类索用不上，而不是照着猜出来的长度规划出一条不存在的索。
    double maxSpan(const std::string& template_id) const;

    // 这种标记的供电半径。不是供电结构就返回 0。
    double supplyRadius(const std::string& template_id) const;

    // 一个供电结构都没登记时为 false，此时不做通电筛选，全部架子照单参与规划。
    bool hasPowerSources() const { return !power_sources_.empty(); }

    const ZiplineCostModel& cost() const { return cost_; }

    bool empty() const { return frames_.empty(); }

    // 标定过的地图编号，去重。留空的「不限地图」条目不算在内。
    std::vector<std::string> mapIds() const;

private:
    std::vector<ZiplineFrame> frames_;
    std::vector<ZiplineType> types_;
    std::vector<ZiplinePowerSource> power_sources_;
    ZiplineCostModel cost_;
};

} // namespace zipline
