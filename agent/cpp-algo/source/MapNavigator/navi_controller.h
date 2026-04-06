#pragma once

#include <string>
#include <vector>

#include "MaaFramework/MaaAPI.h"

#include "navi_domain_types.h"

namespace mapnavigator
{

struct NaviParam
{
    std::string map_name;
    std::vector<Waypoint> path;
    int64_t arrival_timeout = 60000;
    double sprint_threshold = 25.0;
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
