#pragma once

#include "control/walk_mode.h"
#include "navigation_entry.h"
#include "semantic/pulse_history.h"
#include "sensing/latency_observer.h"
#include "sensing/sensitivity_observer.h"
#include "zipline/zipline_preference.h"

namespace mapnavigator
{

struct NavProcessState
{
    sensitivity::SensitivityRun sensitivity;
    latency::LatencyRun latency;
    // 游戏的走路切换状态跨导航保留
    walkmode::WalkBelief walk;
    // 同一进程即同一台机器与按键链路, 量到的关系跨点跨任务都成立
    semantic_nodes::PulseHistory pulses;
    // 每种原因只提示一次, 滑索真正用上后才复位
    ZiplineNoticeLatch zipline_notices;
    // 子任务 pipeline 可能重入导航动作, 按 tasker 把导航嵌套深度限制为 1
    NavigatingTaskers navigating;
};

} // namespace mapnavigator
