#pragma once

#include <chrono>

#include "navi_domain_types.h"

namespace mapnavigator
{

struct NavigationRuntimeState
{
    std::chrono::steady_clock::time_point last_auto_sprint_time_ {};
    std::chrono::steady_clock::time_point transfer_wait_started_ {};
    NaviPosition transfer_anchor_pos_ {};
    int transfer_stable_hits_ = 0;
    bool post_zone_transition_reacquire_pending_ = false;
};

} // namespace mapnavigator