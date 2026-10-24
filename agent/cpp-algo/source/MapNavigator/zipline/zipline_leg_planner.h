#pragma once

#include <functional>
#include <optional>
#include <string>
#include <vector>

#include "../../Navmesh/BaseNavPlanner.h"
#include "../../Zipline/ZiplineFrames.h"
#include "../planning/navmesh_diagnostics.h"
#include "zipline_types.h"

namespace mapnavigator
{

struct NaviParam;

struct ZiplineOutcome
{
    bool used = false;
    bool account_unknown = false;
    bool no_data = false; // 没导入坐标, 或本区一根通电的架子都没有
    bool not_chosen = false;
};

// 按调用线程记账, 寻路入口开始时清零、结束时取用
void ResetZiplineOutcome();
ZiplineOutcome CurrentZiplineOutcome();

struct ZiplineRoute
{
    navmesh::WorldPath approach;  // 起点 → 上索点
    navmesh::WorldPath departure; // 下索点 → 终点
    // 至少两根, 中间的既是上一跳落点也是下一跳上索点
    std::vector<zipline::ZiplineNode> towers;
    // 与 towers 逐跳对应(最后一根没有), 列出这根架子上下一跳以外的索通向的架子
    std::vector<std::vector<zipline::ZiplineNode>> hop_alternates;
    // 折算成等效走路距离
    double cost = 0.0;
    // 链首依次试的上索站位, 为空表示只能按架子坐标走
    std::vector<navmesh::WorldPoint> mount_spots;
    std::vector<NavmeshRouteDiagnostic> diagnostics;
};

// SameTower 靠世界坐标认架子, 漏一个分量就退化成按像素认, 所以两侧共用这份映射
ZiplineNodeRef ToNodeRef(const zipline::ZiplineNode& node);

// walking_path 为空表示步行不可达, goal_deck_y 须与步行方案一致, 两个区名不可互换
std::optional<ZiplineRoute> PlanZiplineRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const std::string& navmesh_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    const navmesh::WorldPath* walking_path,
    std::optional<double> goal_deck_y,
    std::optional<double> start_floor_y,
    std::optional<double> start_deck_y,
    const std::function<bool()>& should_stop,
    bool capture_diagnostics = false);

} // namespace mapnavigator
