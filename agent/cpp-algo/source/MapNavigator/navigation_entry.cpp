#include "navigation_entry.h"

namespace mapnavigator
{

bool TryEnterNavigation(NavigatingTaskers* navigating, MaaTasker* tasker)
{
    const std::lock_guard<std::mutex> lock(navigating->mutex);
    return navigating->taskers.insert(tasker).second;
}

void LeaveNavigation(NavigatingTaskers* navigating, MaaTasker* tasker)
{
    const std::lock_guard<std::mutex> lock(navigating->mutex);
    navigating->taskers.erase(tasker);
}

} // namespace mapnavigator
