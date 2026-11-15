#pragma once

namespace mapnavigator
{

struct Navigator;
struct NavigateTick;
enum class NaviPhase;

// 返回本拍的朝向误差
double SteerAlongCorridor(Navigator* nav, const NavigateTick& tick);
void UpdateAutoSprint(Navigator* nav, const NavigateTick& tick);
void UpdateWalkMode(Navigator* nav, NaviPhase phase);

} // namespace mapnavigator
