#pragma once

#include <cstdint>

namespace mapnavigator::backend::wlroots
{

// Win32 Virtual-Key codes。
// WlRoots 控制器已启用 use_win32_vk_code，MaaFramework 内部会将这些值翻译为 Linux evdev 码。
constexpr int32_t kMoveForwardKey = 'W';  // 0x57
constexpr int32_t kMoveLeftKey = 'A';     // 0x41
constexpr int32_t kMoveBackwardKey = 'S'; // 0x53
constexpr int32_t kMoveRightKey = 'D';    // 0x44
constexpr int32_t kInteractKey = 'F';     // 0x46
constexpr int32_t kJumpKey = 0x20;        // VK_SPACE
constexpr int32_t kLeftAltKey = 0xA4;     // VK_LMENU

} // namespace mapnavigator::backend::wlroots
