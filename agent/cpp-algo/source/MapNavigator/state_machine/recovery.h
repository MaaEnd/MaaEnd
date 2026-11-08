#pragma once

namespace mapnavigator
{

struct Navigator;
struct NavigateTick;
enum class TickStep;

TickStep CheckOffRouteWedge(Navigator* nav, const NavigateTick& tick);
TickStep RunObstacleRecovery(Navigator* nav, const NavigateTick& tick);

} // namespace mapnavigator
