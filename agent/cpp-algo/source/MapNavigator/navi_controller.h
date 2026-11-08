#pragma once

#include <MaaFramework/MaaAPI.h>

#include "navi_param.h"

namespace mapnavigator
{

struct NavProcessState;

bool RunNavigation(NavProcessState* process, MaaContext* context, const NaviParam& requested_param);

} // namespace mapnavigator
