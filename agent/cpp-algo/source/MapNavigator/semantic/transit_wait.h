#pragma once

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

Result TickPortalTransit(Navigator* nav);
Result TickTransferWait(Navigator* nav);
Result TickRopeRide(Navigator* nav);

} // namespace semantic_nodes

} // namespace mapnavigator
