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

// 一次寻路里滑索的去向，逐条腿累加。没数据要用户去导坐标，有数据却没选中不需要任何操作，
// 两者分开记才说得清。
struct ZiplineOutcome
{
    bool used = false;       // 至少有一条腿走了滑索
    bool no_data = false;    // 有标定但没导入坐标，或这个区一根通电的都没记到
    bool not_chosen = false; // 有候选，但没有一条比走路划算
};

// 由寻路入口在请求开始时清零、结束时取用。账记在调用线程上，并发请求各算各的。
void ResetZiplineOutcome();
ZiplineOutcome CurrentZiplineOutcome();

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

// 在本区找一条滑索路线：纯走路可达时只返回显著更省的方案；纯走路不可达时返回能把
// 起终两侧可走面接起来的最低成本连续链。没有可用方案、该区没标定过、或请求没开滑索时返回 nullopt。
//
// walking_path 非空时是纯走路方案，它的折线长度同时充当收益门槛：只有省下 min_gain 以上的
// 方案才会被返回。传折线而不是传长度，是为了让两边的长度出自同一把尺子——换成调用方自己量，
// 量法一旦不同，差出来的就是一个没人看得见的错误决策。传 nullptr 表示纯走路不可达，此时
// 不设收益门槛，候选仍必须分别规划出“起点到上索点”和“下索点到终点”两段可走路径。
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
    const navmesh::WorldPath* walking_path,
    std::optional<double> goal_deck_y,
    const std::function<bool()>& should_stop);

} // namespace mapnavigator
