#pragma once

#include "control/walk_mode.h"
#include "sensing/sensitivity_observer.h"

namespace mapnavigator
{

struct NavProcessState
{
    sensitivity::SensitivityRun sensitivity;
    // 游戏的走路切换状态跨导航保留
    walkmode::WalkBelief walk;
};

} // namespace mapnavigator
