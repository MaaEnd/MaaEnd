#pragma once

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

bool IsTriggerPending(const NavigationSession& session);

Result ProbeTriggerWhileNavigating(const Context& ctx);

Result ArriveTrigger(const Context& ctx, const Waypoint& waypoint, double actual_distance);

Result TickTriggerWait(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
