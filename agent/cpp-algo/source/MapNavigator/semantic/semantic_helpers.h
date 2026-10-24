#pragma once

#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

void StopMotionAndCommitment(const Context& ctx);
void SelectPhaseForCurrentWaypoint(const Context& ctx, const char* reason);
Result CompleteArrival(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason);
// 吃掉当前位置起连续的、未写区或区与当前区相同的 ZONE 声明
void ConsumeMatchedZoneNodes(const Context& ctx);
Result WaitNextFix(int interval_ms);
// 传送、跳板、升降索把人挪到新位置后, 按落点所在区接回剩余路线
Result ResumeAfterRelocation(const Context& ctx, const char* reason);

} // namespace semantic_nodes

} // namespace mapnavigator
