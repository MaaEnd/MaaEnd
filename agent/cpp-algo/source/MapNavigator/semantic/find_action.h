#pragma once

#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

// 停车并切进 WaitFind 相位, 之后每拍由 TickFindTarget 驱动
Result ArriveFind(const Context& ctx, const Waypoint& waypoint, double actual_distance);

// 只处理不带坐标的 FIND 控制节点
Result ConsumeFindNodes(const Context& ctx);

Result TickFindTarget(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
