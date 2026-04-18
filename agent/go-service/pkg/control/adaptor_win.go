// Copyright (c) 2026 Harry Huang
package control

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Win32 Virtual-Key 键码绑定。WlRoots 控制器在 interface.json 中开启
// use_win32_vk_code 后，MaaFramework 会将相同 VK 码翻译为 Linux evdev 码，
// 因此与 Win32 共用本文件中的绑定与 desktopControlAdaptor 实现。
const (
	WINDOWS_KEY_W     = 0x57
	WINDOWS_KEY_A     = 0x41
	WINDOWS_KEY_S     = 0x53
	WINDOWS_KEY_D     = 0x44
	WINDOWS_KEY_SHIFT = 0x10
	WINDOWS_KEY_CTRL  = 0x11
	WINDOWS_KEY_ALT   = 0x12
	WINDOWS_KEY_SPACE = 0x20
)

func windowsKeyBindings() desktopKeyBindings {
	return desktopKeyBindings{
		W:     WINDOWS_KEY_W,
		A:     WINDOWS_KEY_A,
		S:     WINDOWS_KEY_S,
		D:     WINDOWS_KEY_D,
		Shift: WINDOWS_KEY_SHIFT,
		Ctrl:  WINDOWS_KEY_CTRL,
		Alt:   WINDOWS_KEY_ALT,
		Space: WINDOWS_KEY_SPACE,
	}
}

func newWindowsControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *desktopControlAdaptor {
	return newDesktopControlAdaptor(ctx, ctrl, w, h, windowsKeyBindings())
}
