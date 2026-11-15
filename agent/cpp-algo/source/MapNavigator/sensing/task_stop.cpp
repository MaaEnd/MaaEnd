#include "task_stop.h"

#include <MaaFramework/MaaAPI.h>

namespace mapnavigator
{

bool TaskStopping(MaaContext* context)
{
    return MaaTaskerStopping(MaaContextGetTasker(context));
}

} // namespace mapnavigator
