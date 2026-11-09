#pragma once

namespace mapnavigator
{

struct Navigator;
struct NavigateTick;
enum class TickStep;

struct PromptDistance
{
    double distance_sq = -1.0;
    bool is_zipline = false;
    double dig_distance_sq = -1.0;
};

TickStep HandlePrompts(Navigator* nav, const NavigateTick& tick);
void StartScanners(Navigator* nav);
void PreWarmPrompts(Navigator* nav);
// 路线上没有需要减速接近的点时 distance_sq 为 -1
PromptDistance NearestPromptDistance(const Navigator& nav);
void UpdatePromptSprintSuppression(Navigator* nav);
void TryRunPromptSubtaskAtRouteTail(Navigator* nav);

} // namespace mapnavigator
