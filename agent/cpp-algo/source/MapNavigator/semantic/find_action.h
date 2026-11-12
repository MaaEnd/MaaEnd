#pragma once

#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

// 停车并切进 WaitFind 相位, 之后每拍由 TickFindTarget 驱动
Result ArriveFind(Navigator* nav, const Waypoint& waypoint, double actual_distance);

// 只处理不带坐标的 FIND 控制节点
Result ConsumeFindNodes(Navigator* nav);

Result TickFindTarget(Navigator* nav);

} // namespace semantic_nodes

} // namespace mapnavigator
