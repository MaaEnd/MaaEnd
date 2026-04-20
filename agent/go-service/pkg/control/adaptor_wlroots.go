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

// SwipeHover on wlroots is implemented via relative mouse movement, so the
// absolute anchor (contact/x/y) is ignored and only dx/dy are honored.
func (wca *wlrootsControlAdaptor) SwipeHover(_ /*contact*/, _ /*x*/, _ /*y*/, dx, dy int, durationMillis, delayMillis int) {
	wca.ctrl.PostRelativeMove(int32(dx), int32(dy)).Wait()
	time.Sleep(time.Duration(durationMillis+delayMillis) * time.Millisecond)
}

func (wca *wlrootsControlAdaptor) RotateCamera(dx, dy int) {
	// No screen-center anchor: wlroots SwipeHover ignores x/y and sends a relative move.
	wca.SwipeHover(0, 0, 0, dx, dy, defaultDesktopKeyActionDelayMillis*3, defaultDesktopKeyActionDelayMillis)
}

func (wca *wlrootsControlAdaptor) AggressivelyResetCamera() {
	// wlroots uses relative mouse move for camera rotation, no cursor reset needed.
}
