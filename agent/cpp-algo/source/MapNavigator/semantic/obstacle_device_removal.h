#pragma once

#include "../navi_domain_types.h"

namespace mapnavigator
{

struct Navigator;
struct RouteMeasurement;

// 除 NotAttempted 外都已跑过子任务并用掉这次机会
enum class DeviceRemovalOutcome
{
    NotAttempted,
    NeedsFreshFix, // 已处理但定位不可用, 本拍须到此结束
    Escaped,
    StillPinned,
    UnknownUi,
};

DeviceRemovalOutcome TryRemoveDevice(Navigator* nav, const RouteMeasurement& route, const Waypoint& waypoint);

} // namespace mapnavigator
