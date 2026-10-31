#pragma once

#include <functional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

int PlanPulseHoldMs(double residual, int max_hold_ms);
// 返回 false 是非致命的未达标, 调用方照常做到点动作, 取消时不得再做
bool SettleAtStrictGoal(const Context& ctx, const Waypoint& waypoint, const std::function<bool()>& should_stop);

} // namespace semantic_nodes

} // namespace mapnavigator
