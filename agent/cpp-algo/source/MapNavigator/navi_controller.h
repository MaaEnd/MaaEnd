#pragma once

#include <cstdint>
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
    bool path_trim = false;
    int64_t arrival_timeout = 60000;
    double sprint_threshold = 25.0;
    bool enable_rejoin = true;
    double rejoin_abort_distance = 18.0;
    int32_t rejoin_candidate_limit = 4;
    int32_t rejoin_backtrack_window = 6;
    int64_t rejoin_retry_timeout = 2200;
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
