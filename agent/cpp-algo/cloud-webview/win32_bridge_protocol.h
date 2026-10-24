#pragma once

#include <cstdint>

namespace cloud::win32bridge
{
inline constexpr wchar_t kClass[] = L"MaaEnd.CloudWeb.Controller.v1";
inline constexpr wchar_t kTitle[] = L"MaaEnd - Cloud Web Controller";
// Scalars only: no pointers cross process boundaries. Each delta keeps all 32 bits.
inline constexpr uint32_t kRelativeMove = 0x8000 + 0x430;
inline constexpr uint32_t kReleaseInput = kRelativeMove + 1;
// Task messages carry the Agent PID in WPARAM and Framework task ID in LPARAM.
inline constexpr uint32_t kBeginTask = kRelativeMove + 2;
inline constexpr uint32_t kEndTask = kRelativeMove + 3;
inline constexpr uint32_t kPrepareFrame = kRelativeMove + 4;
inline constexpr uint32_t kWatchOwner = kRelativeMove + 5;
inline constexpr uint32_t kCloseHost = kRelativeMove + 6;
inline constexpr int kWidth = 1280;
inline constexpr int kHeight = 720;
} // namespace cloud::win32bridge
