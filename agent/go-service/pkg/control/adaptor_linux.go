// Copyright (c) 2026 Harry Huang
package control

import (
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// linuxControlAdaptor reuses desktop key/movement behavior while overriding
// camera interaction to use relative mouse movement.
type linuxControlAdaptor struct {
	*desktopControlAdaptor
}

func init() {
	newLinuxControlAdaptor = func(ctx *maa.Context, ctrl *maa.Controller, w, h int) (ControlAdaptor, error) {
		return &linuxControlAdaptor{
			desktopControlAdaptor: newDefaultDesktopControlAdaptor(ctx, ctrl, w, h),
		}, nil
	}
}

func (wca *linuxControlAdaptor) RotateCamera(dx, dy int) {
	wca.ctrl.PostRelativeMove(int32(dx), int32(dy)).Wait()
	time.Sleep(defaultDesktopKeyActionDelayMillis * time.Millisecond)
}

func (wca *linuxControlAdaptor) ResetCursor(_ CursorResetPolicy) {
	// linux uses relative mouse move for camera rotation, no cursor reset needed.
}
