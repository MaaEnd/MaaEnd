#pragma once

#include "../semantic/semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

Result StartZiplineHop(Navigator* nav, const Waypoint& waypoint, double actual_distance);

Result TickZiplineRide(Navigator* nav);

Result AdvanceMountSpot(Navigator* nav, const Waypoint& waypoint, const char* reason);

Result AbandonZipline(Navigator* nav, const char* reason, const char* detail);

bool ScreenShowsZiplineTower(Navigator* nav);

void StepOffZiplineTower(Navigator* nav);

// 没站在架子上时什么也不发
void LeaveZiplineTower(Navigator* nav);

// 链首从脚下这根架子起滑时跳过它前面的接近点, 返回 true
bool SkipToHopUnderfoot(Navigator* nav, const char* reason);

} // namespace semantic_nodes

} // namespace mapnavigator
