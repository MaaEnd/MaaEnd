#pragma once

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

// 不在门户传送途中时什么都不做
Result TickPortalTransit(const Context& ctx);
Result TickTransferWait(const Context& ctx);
Result TickRopeRide(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
