#pragma once

#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

Result ArriveJumpPad(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance);
Result TickJumpPad(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
