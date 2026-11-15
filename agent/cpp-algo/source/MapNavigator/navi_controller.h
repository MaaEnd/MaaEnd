#pragma once

#include <MaaFramework/MaaAPI.h>

#include "navi_param.h"

namespace mapnavigator
{

struct NavProcessState;
struct ZiplineOutcome;

bool RunNavigation(NavProcessState* process, ZiplineOutcome* zipline_outcome, MaaContext* context, const NaviParam& requested_param);

} // namespace mapnavigator
