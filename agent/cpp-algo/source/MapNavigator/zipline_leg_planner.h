#pragma once

#include <functional>
#include <optional>
#include <string>
#include <vector>

#include "../Navmesh/BaseNavPlanner.h"
#include "../Zipline/ZiplineFrames.h"

namespace mapnavigator
{

struct NaviParam;

// 一条借滑索的路线：走到 towers 的头一根上索，顺着索一跳跳滑到最后一根，再走完剩下的路。
struct ZiplineRoute
{
    navmesh::WorldPath approach;  // 起点 → 上索点
    navmesh::WorldPath departure; // 下索点 → 终点
    // 依次经过的架子，至少两根。中间那些既是上一跳的落点也是下一跳的上索点，
    // 人落下来就站在下一根上，所以跳与跳之间不需要走路。
    std::vector<zipline::ZiplineNode> towers;
    // 折算成等效走路距离的总代价，与 baseline_length 可直接比大小。
    double cost = 0.0;
    // 上索点旁边贴着供电结构时给的备用站位，执行侧认不出上索提示才改瞄它。
    // 接近段仍然走到架子本身：让开量再小也是往外推，把它当常规落脚点会把人推出够得着的那圈。
    std::optional<navmesh::WorldPoint> mount_restand;
};

// 在本区找一条比纯走路更省的滑索路线；没有更省的、该区没标定过、或请求没开滑索时返回 nullopt。
//
// walking_path 是纯走路方案，它的折线长度同时充当收益门槛：只有省下 min_gain 以上的方案才会被返回。
// 传折线而不是传长度，是为了让两边的长度出自同一把尺子——换成调用方自己量，量法一旦不同，
// 差出来的就是一个没人看得见的错误决策。
// 候选先按欧氏下界筛，欧氏距离恒不大于真实路径长度，所以被剪掉的分支不可能更优——
// 结果与穷举所有滑索对逐位一致，剪枝只省时间不改答案。
//
// goal_deck_y 是终点所在的面，必须与纯走路方案用的是同一个：滑索只换走法不换目的地，
// 少传一层声明就会让终点段悄悄落到别的层上。
//
// 两个区名不是一回事，不能互相顶替：navmesh_zone 是标定的键（标定产出的是 base 像素，
// 所以只能按 base 区名索引），locator_zone 是角色当下所在的区，决定起点吸到哪一层。
std::optional<ZiplineRoute> PlanZiplineRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const std::string& navmesh_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    const navmesh::WorldPath& walking_path,
    std::optional<double> goal_deck_y,
    const std::function<bool()>& should_stop);

} // namespace mapnavigator
