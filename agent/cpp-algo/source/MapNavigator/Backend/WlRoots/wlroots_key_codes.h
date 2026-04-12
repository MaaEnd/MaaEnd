#pragma once

#include <cstdint>

namespace mapnavigator::backend::wlroots
{

// Linux input-event-codes.h (EV_KEY)
constexpr int32_t kMoveForwardKey = 17;  // KEY_W
constexpr int32_t kMoveLeftKey = 30;     // KEY_A
constexpr int32_t kMoveBackwardKey = 31; // KEY_S
constexpr int32_t kMoveRightKey = 32;    // KEY_D
constexpr int32_t kInteractKey = 33;     // KEY_F
constexpr int32_t kJumpKey = 57;         // KEY_SPACE
constexpr int32_t kLeftAltKey = 56;      // KEY_LEFTALT

} // namespace mapnavigator::backend::wlroots
