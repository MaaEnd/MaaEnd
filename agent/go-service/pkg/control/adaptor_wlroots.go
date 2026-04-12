// Copyright (c) 2026 Harry Huang
package control

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// WlrootsControlAdaptor implements ControlAdaptor for Linux wlroots controllers.
//
// Key codes follow Linux input-event-codes.h values (EV_KEY), which differ from
// Win32 virtual key codes.
type WlrootsControlAdaptor struct {
	*desktopControlAdaptor
}

func newWlrootsControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *WlrootsControlAdaptor {
	return &WlrootsControlAdaptor{newDesktopControlAdaptor(ctx, ctrl, w, h, wlrootsKeyBindings())}
}

const (
	// Linux input-event-codes.h (EV_KEY)
	WLROOTS_KEY_W         = 17
	WLROOTS_KEY_A         = 30
	WLROOTS_KEY_S         = 31
	WLROOTS_KEY_D         = 32
	WLROOTS_KEY_LEFTSHIFT = 42
	WLROOTS_KEY_LEFTCTRL  = 29
	WLROOTS_KEY_LEFTALT   = 56
	WLROOTS_KEY_SPACE     = 57
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
