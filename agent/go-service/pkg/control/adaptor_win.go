// Copyright (c) 2026 Harry Huang
package control

import maa "github.com/MaaXYZ/maa-framework-go/v4"

type WindowsControlAdaptor struct {
	*desktopControlAdaptor
}

func newWindowsControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *WindowsControlAdaptor {
	return &WindowsControlAdaptor{newDesktopControlAdaptor(ctx, ctrl, w, h, windowsKeyBindings())}
}

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
