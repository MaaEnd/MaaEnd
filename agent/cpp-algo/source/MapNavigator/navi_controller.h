#pragma once

#include <cstdint>
#include <string>
#include <vector>

#include <MaaFramework/MaaAPI.h>

#include "navi_domain_types.h"

namespace mapnavigator
{

struct NaviParam
{
    std::string map_name;
    std::vector<Waypoint> path;
    std::string navmesh_file;
    double navmesh_snap_radius = 5.0;
    int64_t arrival_timeout = 60000;
    double sprint_threshold = 16.0;
    bool enable_local_driver = true;
    // 起步 A* 开关。叠层地区的 tier floor 烘在空气里时，起步规划会绕出几百格；关掉它就落到
    // Bootstrap 里现成的两档兜底（串行续接 / 直接朝路线头走），照录制点走。默认开，不设置的线路行为不变。
    bool enable_bootstrap_navmesh = true;
    // When set, live fixes are projected onto the navmesh base-pixel frame via the navmesh's own baked
    // tier affine (see NormalizeLivePositionToBase). MapNavigator turns this on.
    bool normalize_position_via_navmesh = false;
    // 这条路线允许不允许借滑索。默认关：没有显式写 zip 的请求一律纯走路，既不去找最近的
    // 滑索也不做加速，规划结果与没有滑索这件事时逐位相同。
    bool zipline_enabled = false;
    // 使用全候选动态规划 Slim，追求精确最短距离；默认使用生产用的稀疏近似。
    bool exact_slim = false;
    // 展开前的原始作者路线。执行侧拿到的 path 是全局展开后的；滑索链半路失败时要靠它重新展开
    // 剩余路线，而不是沿着按链尾落点规划的旧展开走。
    std::vector<Waypoint> authored_path;
    // 执行侧判死过的跳。滑索恢复重展开时带上, 规划从连通图里拿掉这些索, 其余滑索照常参与。
    std::vector<ZiplineHopBan> banned_zipline_hops;
};

class NaviController
{
public:
    explicit NaviController(MaaContext* ctx);
    ~NaviController() = default;

    bool Navigate(const NaviParam& requested_param);

private:
    MaaContext* ctx_;
};

} // namespace mapnavigator
