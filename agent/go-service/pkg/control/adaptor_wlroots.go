// Copyright (c) 2026 Harry Huang
package control

import (
	"math"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// WlrootsRelativeMoveScale compensates for the sensitivity difference between
// the wlroots relative-move path and the default desktop hover-swipe path.
// Empirically derived from the previous per-call ratio 2.6 / 2.0 = 1.3.
const WlrootsRelativeMoveScale = 1.3

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
// dx/dy are scaled by WlrootsRelativeMoveScale so callers can share the same
// sensitivity baseline with desktop controllers.
func (wca *wlrootsControlAdaptor) SwipeHover(_ /*contact*/, _ /*x*/, _ /*y*/, dx, dy int, durationMillis, delayMillis int) {
	scaledDX := int32(math.Round(float64(dx) * WlrootsRelativeMoveScale))
	scaledDY := int32(math.Round(float64(dy) * WlrootsRelativeMoveScale))
	wca.ctrl.PostRelativeMove(scaledDX, scaledDY).Wait()
	time.Sleep(time.Duration(durationMillis+delayMillis) * time.Millisecond)
}

func (wca *wlrootsControlAdaptor) RotateCamera(dx, dy int) {
	// No screen-center anchor: wlroots SwipeHover ignores x/y and sends a relative move.
	wca.SwipeHover(0, 0, 0, dx, dy, defaultDesktopKeyActionDelayMillis*3, defaultDesktopKeyActionDelayMillis)
}

func (wca *wlrootsControlAdaptor) AggressivelyResetCamera() {
	// wlroots uses relative mouse move for camera rotation, no cursor reset needed.
}
