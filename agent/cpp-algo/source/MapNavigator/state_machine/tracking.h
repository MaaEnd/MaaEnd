#pragma once

namespace mapnavigator
{

struct Navigator;
struct NavigateTick;
enum class TickStep;

void TrackRoute(Navigator* nav, NavigateTick* tick);
TickStep RunRouteProgress(Navigator* nav, NavigateTick* tick);
double ResolveArrivalDistance(const Navigator& nav, const NavigateTick& tick);
TickStep HandleWaypointArrival(Navigator* nav, NavigateTick* tick);

} // namespace mapnavigator
