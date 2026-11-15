#pragma once

#include "pulse_history.h"
#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

int PlanPulseHoldMs(PulseHistory* history, double residual, int max_hold_ms);
// 返回 false 是非致命的未达标, 调用方照常做到点动作, 取消时不得再做
bool SettleAtStrictGoal(Navigator* nav, const Waypoint& waypoint);

} // namespace semantic_nodes

} // namespace mapnavigator
