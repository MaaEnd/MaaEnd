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
    // When set, live fixes are projected onto the navmesh base-pixel frame via the navmesh's own baked
    // tier affine (see NormalizeLivePositionToBase). Native MapNavigator turns this on; the Compatible
    // entry leaves it off so its MapTracker-base-px frame is preserved byte-for-byte.
    bool normalize_position_via_navmesh = false;
    // 这条路线允许不允许借滑索。默认关：没有显式写 zip 的请求一律纯走路，既不去找最近的
    // 滑索也不做加速，规划结果与没有滑索这件事时逐位相同。
    bool zipline_enabled = false;
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
