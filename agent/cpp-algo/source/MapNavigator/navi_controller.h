#pragma once

#include <MaaFramework/MaaAPI.h>

#include "navi_param.h"

namespace mapnavigator
{

bool RunNavigation(MaaContext* context, const NaviParam& requested_param);

} // namespace mapnavigator
