// Copyright (c) 2026 Harry Huang
package control

import (
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type WindowsControlAdaptor struct {
	ctx  *maa.Context
	ctrl *maa.Controller
	w    int
	h    int
}

func newWindowsControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *WindowsControlAdaptor {
	return &WindowsControlAdaptor{ctx, ctrl, w, h}
}

func (wca *WindowsControlAdaptor) Ctx() *maa.Context {
	return wca.ctx
}

func (wca *WindowsControlAdaptor) TouchDown(contact, x, y int, delayMillis int) {
	wca.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (wca *WindowsControlAdaptor) TouchUp(contact int, delayMillis int) {
	wca.ctrl.PostTouchUp(int32(contact)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (wca *WindowsControlAdaptor) TouchClick(contact, x, y int, durationMillis, delayMillis int) {
	wca.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(durationMillis) * time.Millisecond)
	wca.ctrl.PostTouchUp(int32(contact)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (wca *WindowsControlAdaptor) Swipe(x, y, dx, dy int, durationMillis, delayMillis int) {
	stepDurationMillis := durationMillis / 2
	wca.ctrl.PostTouchDown(0, int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	wca.ctrl.PostTouchMove(0, int32(x+dx), int32(y+dy), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	wca.ctrl.PostTouchUp(0).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (wca *WindowsControlAdaptor) SwipeHover(x, y, dx, dy int, durationMillis, delayMillis int) {
	wca.ctrl.PostTouchMove(0, int32(x), int32(y), 0).Wait()
	time.Sleep(time.Duration(durationMillis) * time.Millisecond)
	wca.ctrl.PostTouchMove(0, int32(x+dx), int32(y+dy), 0).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (wca *WindowsControlAdaptor) KeyDown(keyCode int, delayMillis int) {
	wca.ctrl.PostKeyDown(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (wca *WindowsControlAdaptor) KeyUp(keyCode int, delayMillis int) {
	wca.ctrl.PostKeyUp(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (wca *WindowsControlAdaptor) KeyType(keyCode int, delayMillis int) {
	wca.ctrl.PostClickKey(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (wca *WindowsControlAdaptor) RotateCamera(dx, dy int, durationMillis, delayMillis int) {
	cx, cy := wca.w/2, wca.h/2
	wca.SwipeHover(cx, cy, dx, dy, durationMillis, delayMillis)
}

func (wca *WindowsControlAdaptor) RotateCameraEliminateSideEffect(delayMillis int) {
	cx, cy := wca.w/2, wca.h/2
	stepDelayMillis := delayMillis / 3
	wca.KeyDown(KEY_ALT, stepDelayMillis)
	wca.TouchClick(0, cx, cy, stepDelayMillis, 0)
	wca.KeyUp(KEY_ALT, stepDelayMillis)
}

const (
	KEY_W     = 0x57
	KEY_A     = 0x41
	KEY_S     = 0x53
	KEY_D     = 0x44
	KEY_SHIFT = 0x10
	KEY_CTRL  = 0x11
	KEY_ALT   = 0x12
	KEY_SPACE = 0x20
)
