// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	actionWorkW = 1280
	actionWorkH = 720

	keyAlt = 0x12
)

// ActionWrapper provides synchronized touch/key operations with built-in delays.
type ActionWrapper struct {
	ctx  *maa.Context
	ctrl *maa.Controller
}

// NewActionWrapper creates a new ActionWrapper from context/controller.
func NewActionWrapper(ctx *maa.Context, ctrl *maa.Controller) *ActionWrapper {
	return &ActionWrapper{ctx, ctrl}
}

// Ctx returns the wrapped Maa context.
func (aw *ActionWrapper) Ctx() *maa.Context {
	return aw.ctx
}

// Controller returns the wrapped Maa controller.
func (aw *ActionWrapper) Controller() *maa.Controller {
	return aw.ctrl
}

// ClickSync performs a touch down and up at (x, y).
func (aw *ActionWrapper) ClickSync(contact, x, y int, delayMillis int) {
	aw.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
	aw.ctrl.PostTouchUp(int32(contact)).Wait()
}

// SwipeSync performs an actual swipe from (x, y) to (x+dx, y+dy).
func (aw *ActionWrapper) SwipeSync(x, y, dx, dy int, durationMillis, delayMillis int) {
	stepDurationMillis := durationMillis / 2
	aw.ctrl.PostTouchDown(0, int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	aw.ctrl.PostTouchMove(0, int32(x+dx), int32(y+dy), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	aw.ctrl.PostTouchUp(0).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// SwipeHoverSync performs an only-hover swipe from (x, y) to (x+dx, y+dy).
func (aw *ActionWrapper) SwipeHoverSync(x, y, dx, dy int, durationMillis, delayMillis int) {
	aw.ctrl.PostTouchMove(0, int32(x), int32(y), 0).Wait()
	time.Sleep(time.Duration(durationMillis) * time.Millisecond)
	aw.ctrl.PostTouchMove(0, int32(x+dx), int32(y+dy), 0).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyDownSync sends a key press.
func (aw *ActionWrapper) KeyDownSync(keyCode int, delayMillis int) {
	aw.ctrl.PostKeyDown(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyUpSync sends a key release.
func (aw *ActionWrapper) KeyUpSync(keyCode int, delayMillis int) {
	aw.ctrl.PostKeyUp(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyTypeSync sends a key press-release and waits.
func (aw *ActionWrapper) KeyTypeSync(keyCode int, delayMillis int) {
	aw.ctrl.PostClickKey(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// RotateCamera performs a camera rotation via mouse-hover dragging.
func (aw *ActionWrapper) RotateCamera(dx int, durationMillis, delayMillis int) {
	cx, cy := actionWorkW/2, actionWorkH/2
	aw.SwipeHoverSync(cx, cy, dx, 0, durationMillis, delayMillis)
}

// ResetCamera performs a centered alt-click to reset camera orientation.
func (aw *ActionWrapper) ResetCamera(delayMillis int) {
	cx, cy := actionWorkW/2, actionWorkH/2
	stepDelayMillis := delayMillis / 3
	aw.KeyDownSync(keyAlt, stepDelayMillis)
	aw.ClickSync(0, cx, cy, stepDelayMillis)
	aw.KeyUpSync(keyAlt, stepDelayMillis)
}
