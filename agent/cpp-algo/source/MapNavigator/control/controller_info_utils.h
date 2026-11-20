#pragma once

#include <cstdint>
#include <optional>
#include <string>

#include <MaaFramework/MaaAPI.h>

namespace mapnavigator
{

std::string DetectControllerType(MaaController* controller);

// win32 控制器的窗口句柄；其它控制器类型拿不到时返回 false
bool TryGetWin32Hwnd(MaaController* controller, uint64_t* out_hwnd);

bool TryGetWin32MouseInputMethod(MaaController* controller, MaaWin32InputMethod* out_method);
bool IsMessageInputMethod(MaaWin32InputMethod method);

// Window handle from the controller info; only Win32 controllers carry it.
std::optional<uintptr_t> TryGetWin32WindowHandle(MaaController* controller);

} // namespace mapnavigator
