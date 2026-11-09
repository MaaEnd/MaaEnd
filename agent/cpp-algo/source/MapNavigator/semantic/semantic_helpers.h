#pragma once

#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

void HoldAndArmCameraAlign(const Context& ctx);
void SelectPhaseOrFinishRoute(const Context& ctx, const char* reason);
void CompleteArrival(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason);
void ConsumeMatchedZoneNodes(const Context& ctx);
Result WaitNextFix(int interval_ms);
void ResumeAfterRelocation(const Context& ctx, const char* reason);

} // namespace semantic_nodes

} // namespace mapnavigator
