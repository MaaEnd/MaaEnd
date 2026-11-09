#pragma once

namespace mapnavigator
{

struct Navigator;
struct NavigateTick;
enum class TickStep;

bool CaptureTrackedFix(Navigator* nav);
bool CaptureGlobalFix(Navigator* nav);
TickStep CaptureNavigateFix(Navigator* nav, NavigateTick* tick);
TickStep CheckDwellWatchdog(Navigator* nav);
bool HandleLocalizationLoss(Navigator* nav);
bool ReturnToWorld(Navigator* nav);
TickStep RunRiverFallRecovery(Navigator* nav, const NavigateTick& tick);

} // namespace mapnavigator
