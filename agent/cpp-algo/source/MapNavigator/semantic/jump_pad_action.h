#pragma once

#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

Result ArriveJumpPad(Navigator* nav, const Waypoint& waypoint, const std::optional<size_t>& node_idx, double actual_distance);
Result TickJumpPad(Navigator* nav);

} // namespace semantic_nodes

} // namespace mapnavigator
