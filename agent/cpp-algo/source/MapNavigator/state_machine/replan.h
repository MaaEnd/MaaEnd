#pragma once

#include <cstddef>
#include <cstdint>

namespace mapnavigator
{

struct Navigator;
struct NaviPosition;
struct Waypoint;

bool TryApplyDynamicOverlayToAnchor(Navigator* nav, const char* reason, size_t continue_index, const Waypoint& anchor);
bool TryApplyDynamicOverlayToNextAnchor(Navigator* nav, const char* reason);
// 在 origin 前方放一块禁区再规划到锚点; 这里是唯一通路时把禁区标成 push_through 并返回 false
bool TryVirtualNoGoReplan(
    Navigator* nav,
    const char* reason,
    size_t continue_index,
    const Waypoint& anchor,
    const NaviPosition& origin,
    double stuck_heading);
bool HandleZiplineRecoveryReplan(Navigator* nav);
bool AbandonRope(Navigator* nav, uint32_t uid);
bool HandleDynamicReplanRequest(Navigator* nav, const char* reason);

} // namespace mapnavigator
