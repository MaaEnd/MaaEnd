#pragma once

#include <mutex>
#include <set>

#include <MaaFramework/MaaAPI.h>

namespace mapnavigator
{

struct NavigatingTaskers
{
    std::mutex mutex;
    std::set<MaaTasker*> taskers;
};

bool TryEnterNavigation(NavigatingTaskers* navigating, MaaTasker* tasker);
void LeaveNavigation(NavigatingTaskers* navigating, MaaTasker* tasker);

struct LeaveNavigationOnExit
{
    NavigatingTaskers* navigating;
    MaaTasker* tasker;

    ~LeaveNavigationOnExit() { LeaveNavigation(navigating, tasker); }
};

} // namespace mapnavigator
