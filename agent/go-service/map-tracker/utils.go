// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

/* ******** Actions ******** */

// ActionWrapper provides synchronized touch/key operations with built-in delays
type ActionWrapper struct {
	ctx  *maa.Context
	ctrl *maa.Controller
}

// NewActionWrapper creates a new ActionWrapper from a context
func NewActionWrapper(ctx *maa.Context, ctrl *maa.Controller) *ActionWrapper {
	return &ActionWrapper{ctx, ctrl}
}

// ClickSync performs a touch down and up at (x, y)
func (aw *ActionWrapper) ClickSync(contact, x, y int, delayMillis int) {
	aw.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
	aw.ctrl.PostTouchUp(int32(contact)).Wait()
}

// SwipeSync performs a swipe from (x, y) to (x+dx, y+dy)
func (aw *ActionWrapper) SwipeSync(x, y, dx, dy int, durationMillis, delayMillis int) {
	aw.ctx.RunActionDirect("Swipe", maa.SwipeParam{
		Begin:     maa.NewTargetRect(maa.Rect{x, y, 4, 4}),
		End:       []maa.Target{maa.NewTargetRect(maa.Rect{x + dx, y + dy, 4, 4})},
		Duration:  []time.Duration{time.Duration(durationMillis) * time.Millisecond},
		OnlyHover: true,
	}, maa.Rect{0, 0, 0, 0}, nil)
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyDownSync sends a key press
func (aw *ActionWrapper) KeyDownSync(keyCode int, delayMillis int) {
	aw.ctrl.PostKeyDown(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyUpSync sends a key release
func (aw *ActionWrapper) KeyUpSync(keyCode int, delayMillis int) {
	aw.ctrl.PostKeyUp(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyTypeSync sends a key press-release and waits
func (aw *ActionWrapper) KeyTypeSync(keyCode int, delayMillis int) {
	aw.ctrl.PostClickKey(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// RotateCamera performs a camera rotation via series of mouse-keyboard operations
func (aw *ActionWrapper) RotateCamera(dx int, durationMillis, delayMillis int) {
	cx, cy := WORK_W/2, WORK_H/2
	aw.SwipeSync(cx, cy, dx, 0, durationMillis, delayMillis)
}

func (aw *ActionWrapper) ResetCamera(delayMillis int) {
	cx, cy := WORK_W/2, WORK_H/2
	stepDelayMillis := delayMillis / 3
	aw.KeyDownSync(KEY_ALT, stepDelayMillis)
	aw.ClickSync(0, cx, cy, stepDelayMillis)
	aw.KeyUpSync(KEY_ALT, stepDelayMillis)
}
