// Copyright (c) 2026 Harry Huang
package control

import (
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// wlrootsControlAdaptor reuses desktop key/movement behavior while overriding
// camera interaction to use relative mouse movement.
type wlrootsControlAdaptor struct {
	*desktopControlAdaptor
}

func newWlrootsControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *wlrootsControlAdaptor {
	return &wlrootsControlAdaptor{
		desktopControlAdaptor: newDefaultDesktopControlAdaptor(ctx, ctrl, w, h),
	}
}

func (wca *wlrootsControlAdaptor) RotateCamera(dx, dy int) {
	wca.ctrl.PostRelativeMove(int32(dx), int32(dy)).Wait()
	time.Sleep(defaultDesktopKeyActionDelayMillis * time.Millisecond)
}

func (wca *wlrootsControlAdaptor) ResetCursor(_ CursorResetPolicy) {
	// wlroots uses relative mouse move for camera rotation, no cursor reset needed.
}
