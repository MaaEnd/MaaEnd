#pragma once

#include "../semantic/semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

Result StartZiplineHop(const Context& ctx, const Waypoint& waypoint, double actual_distance);

Result TickZiplineRide(const Context& ctx);

Result AdvanceMountSpot(const Context& ctx, const Waypoint& waypoint, const char* reason);

Result AbandonZipline(const Context& ctx, const char* reason, const char* detail);

// 没站在架子上时什么也不发
void LeaveZiplineTower(const Context& ctx);

// 链首从脚下这根架子起滑时跳过它前面的接近点, 返回 true
bool SkipToHopUnderfoot(const Context& ctx, const char* reason);

} // namespace semantic_nodes

} // namespace mapnavigator
