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

	pm               PlayerMovement
	lastMotionIsWalk bool
}

func newWindowsControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *WindowsControlAdaptor {
	return &WindowsControlAdaptor{ctx, ctrl, w, h, MovementStop, false}
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

func (wca *WindowsControlAdaptor) Swipe(contact, x, y, dx, dy int, durationMillis, delayMillis int) {
	stepDurationMillis := durationMillis / 2
	wca.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	wca.ctrl.PostTouchMove(int32(contact), int32(x+dx), int32(y+dy), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	wca.ctrl.PostTouchUp(int32(contact)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (wca *WindowsControlAdaptor) SwipeHover(contact, x, y, dx, dy int, durationMillis, delayMillis int) {
	wca.ctrl.PostTouchMove(int32(contact), int32(x), int32(y), 0).Wait()
	time.Sleep(time.Duration(durationMillis) * time.Millisecond)
	wca.ctrl.PostTouchMove(int32(contact), int32(x+dx), int32(y+dy), 0).Wait()
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

func (wca *WindowsControlAdaptor) RotateCamera(dx, dy int) {
	cx, cy := wca.w/2, wca.h/2
	wca.SwipeHover(0, cx, cy, dx, dy, defaultKeyActionDelayMillis*2, defaultKeyActionDelayMillis)
}

func (wca *WindowsControlAdaptor) GetPlayerMovement() PlayerMovement {
	return wca.pm
}

func (wca *WindowsControlAdaptor) SetPlayerMovement(movement PlayerMovement) {
	if movement.Equals(wca.pm) {
		return
	}

	if movement.speed <= MovementStop.speed {
		// Stop moving forward
		wca.KeyUp(KEY_W, defaultKeyActionDelayMillis)
	} else {
		if wca.lastMotionIsWalk {
			if movement.speed >= MovementSprint.speed {
				// Set to "sprint"
				wca.KeyType(KEY_SHIFT, defaultKeyActionDelayMillis)
				wca.lastMotionIsWalk = false
			} else if movement.speed >= MovementRun.speed {
				// Set to "run"
				wca.KeyType(KEY_CTRL, defaultKeyActionDelayMillis)
				wca.lastMotionIsWalk = false
			} else {
				// Already in "walk", do nothing
			}
		} else {
			if movement.speed < MovementRun.speed {
				// Set to "walk"
				wca.KeyType(KEY_CTRL, defaultKeyActionDelayMillis)
				wca.lastMotionIsWalk = true
			} else if movement.speed < MovementSprint.speed {
				if wca.pm.speed >= MovementSprint.speed {
					// Set to "walk" temporarily to terminate the "sprint" state, then set to "run"
					wca.KeyType(KEY_CTRL, defaultKeyActionDelayMillis)
					wca.KeyType(KEY_CTRL, defaultKeyActionDelayMillis)
				} else {
					// Already in "run", do nothing
				}
			} else {
				// Set to "sprint"
				wca.KeyType(KEY_SHIFT, defaultKeyActionDelayMillis)
			}
		}
		// Ensure moving forward
		wca.KeyDown(KEY_W, defaultKeyActionDelayMillis/4)
	}
	wca.pm = movement
}

func (wca *WindowsControlAdaptor) PlayerJump() {
	wca.KeyType(KEY_SPACE, defaultKeyActionDelayMillis*4)
}

func (wca *WindowsControlAdaptor) PlayerSprint() {
	wca.KeyType(KEY_SHIFT, defaultKeyActionDelayMillis)
	wca.pm = MovementSprint
	wca.lastMotionIsWalk = false
}

func (wca *WindowsControlAdaptor) PlayerStop() {
	wca.KeyUp(KEY_W, defaultKeyActionDelayMillis)
	wca.pm = MovementStop
}

func (wca *WindowsControlAdaptor) AggressivelyResetCamera() {
	// Policy: use ALT key to release mouse cursor and reset its position using a click, then release ALT key
	cx, cy := wca.w/2, wca.h/2
	stepDelayMillis := defaultKeyActionDelayMillis / 3
	wca.KeyDown(KEY_ALT, stepDelayMillis)
	wca.TouchClick(0, cx, cy, stepDelayMillis, 0)
	wca.KeyUp(KEY_ALT, stepDelayMillis)
}

func (wca *WindowsControlAdaptor) AggressivelyResetPlayerMovement() {
	// Policy: sprint backward and immediately move forward to ensure the initial motional state is "run"
	wca.KeyDown(KEY_S, defaultKeyActionDelayMillis*2)
	wca.KeyType(KEY_SHIFT, defaultKeyActionDelayMillis*2)
	wca.KeyUp(KEY_S, defaultKeyActionDelayMillis*2)
	wca.KeyType(KEY_W, defaultKeyActionDelayMillis*2)
	wca.pm = MovementRun
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

const defaultKeyActionDelayMillis = 25
