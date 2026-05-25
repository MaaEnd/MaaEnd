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
    double navmesh_max_cost = 0.0;
    int64_t arrival_timeout = 60000;
    double sprint_threshold = 16.0;
    bool enable_local_driver = true;
};

class NaviController
{
public:
    explicit NaviController(MaaContext* ctx);
    ~NaviController() = default;

    bool Navigate(const NaviParam& param);

private:
    MaaContext* ctx_;
};

} // namespace mapnavigator
