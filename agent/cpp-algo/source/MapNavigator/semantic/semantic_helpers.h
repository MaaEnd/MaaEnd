#pragma once

#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

void HoldAndArmCameraAlign(Navigator* nav);
void SelectPhaseOrFinishRoute(Navigator* nav, const char* reason);
void CompleteArrival(Navigator* nav, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason);
void ConsumeMatchedZoneNodes(Navigator* nav);
Result WaitNextFix(int interval_ms);
void ResumeAfterRelocation(Navigator* nav, const char* reason);

} // namespace semantic_nodes

} // namespace mapnavigator
