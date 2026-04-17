// Copyright (c) 2026 Harry Huang
package control

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// WlrootsControlAdaptor implements ControlAdaptor for Linux wlroots controllers.
//
// Key codes follow Win32 Virtual-Key values. The WlRoots controller is created
// with use_win32_vk_code=true in interface.json, and MaaFramework translates
// these to Linux evdev codes internally.
type WlrootsControlAdaptor struct {
	*desktopControlAdaptor
}

func newWlrootsControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *WlrootsControlAdaptor {
	return &WlrootsControlAdaptor{newDesktopControlAdaptor(ctx, ctrl, w, h, wlrootsKeyBindings())}
}

const (
	// Win32 Virtual-Key codes
	WLROOTS_KEY_W         = 0x57
	WLROOTS_KEY_A         = 0x41
	WLROOTS_KEY_S         = 0x53
	WLROOTS_KEY_D         = 0x44
	WLROOTS_KEY_LEFTSHIFT = 0xA0 // VK_LSHIFT
	WLROOTS_KEY_LEFTCTRL  = 0xA2 // VK_LCONTROL
	WLROOTS_KEY_LEFTALT   = 0xA4 // VK_LMENU
	WLROOTS_KEY_SPACE     = 0x20
)

func wlrootsKeyBindings() desktopKeyBindings {
	return desktopKeyBindings{
		W:     WLROOTS_KEY_W,
		A:     WLROOTS_KEY_A,
		S:     WLROOTS_KEY_S,
		D:     WLROOTS_KEY_D,
		Shift: WLROOTS_KEY_LEFTSHIFT,
		Ctrl:  WLROOTS_KEY_LEFTCTRL,
		Alt:   WLROOTS_KEY_LEFTALT,
		Space: WLROOTS_KEY_SPACE,
	}
}
