#pragma once

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

Result TickPortalTransit(const Context& ctx);
Result TickTransferWait(const Context& ctx);
Result TickRopeRide(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
