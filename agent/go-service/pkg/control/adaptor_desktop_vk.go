// Copyright (c) 2026 Harry Huang
package control

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Win32 Virtual-Key codes。Win32 控制器原生使用 VK 码；WlRoots 控制器在
// interface.json 中开启 use_win32_vk_code 后，MaaFramework 会将这些码翻译为
// Linux evdev 码，因此两者共享同一套键码绑定与控制逻辑。
const (
	DESKTOP_VK_W     = 0x57
	DESKTOP_VK_A     = 0x41
	DESKTOP_VK_S     = 0x53
	DESKTOP_VK_D     = 0x44
	DESKTOP_VK_SHIFT = 0x10 // VK_SHIFT
	DESKTOP_VK_CTRL  = 0x11 // VK_CONTROL
	DESKTOP_VK_ALT   = 0x12 // VK_MENU
	DESKTOP_VK_SPACE = 0x20 // VK_SPACE
)

func desktopVKKeyBindings() desktopKeyBindings {
	return desktopKeyBindings{
		W:     DESKTOP_VK_W,
		A:     DESKTOP_VK_A,
		S:     DESKTOP_VK_S,
		D:     DESKTOP_VK_D,
		Shift: DESKTOP_VK_SHIFT,
		Ctrl:  DESKTOP_VK_CTRL,
		Alt:   DESKTOP_VK_ALT,
		Space: DESKTOP_VK_SPACE,
	}
}

// newDesktopVKControlAdaptor constructs a ControlAdaptor for desktop controllers
// (Win32 and WlRoots) that speak Win32 Virtual-Key codes.
func newDesktopVKControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *desktopControlAdaptor {
	return newDesktopControlAdaptor(ctx, ctrl, w, h, desktopVKKeyBindings())
}
