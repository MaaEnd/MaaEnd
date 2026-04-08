#pragma once

#include <cstdint>

namespace mapnavigator::backend::win32
{

constexpr int32_t kMoveForwardKey = 'W';
constexpr int32_t kMoveLeftKey = 'A';
constexpr int32_t kMoveBackwardKey = 'S';
constexpr int32_t kMoveRightKey = 'D';
constexpr int32_t kInteractKey = 'F';
constexpr int32_t kJumpKey = 0x20;

} // namespace mapnavigator::backend::win32
