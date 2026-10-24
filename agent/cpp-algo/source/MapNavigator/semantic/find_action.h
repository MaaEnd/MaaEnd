#pragma once

#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

// 带坐标的 FIND 点: 走到锚点后接手, 停车并切进 WaitFind 相位, 之后的每一拍由 TickFindTarget 驱动
Result ArriveFind(const Context& ctx, const Waypoint& waypoint, double actual_distance);

// 不带坐标的 FIND 控制节点: 走到它就地开找, 不参与到达判定, 所以在这条就地消费的路上处理
Result ConsumeFindNodes(const Context& ctx);

// FIND 相位的每一拍: 截图、判 stop、认目标、按框转视角或走一步。全程不读小地图, 也不改路线进度
Result TickFindTarget(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
