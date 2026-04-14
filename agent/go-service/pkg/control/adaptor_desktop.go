// Copyright (c) 2026 Harry Huang
package control

import (
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type desktopKeyBindings struct {
	W     int
	A     int
	S     int
	D     int
	Shift int
	Ctrl  int
	Alt   int
	Space int
}

type desktopControlAdaptor struct {
	ctx  *maa.Context
	ctrl *maa.Controller
	w    int
	h    int

	keys             desktopKeyBindings
	pm               PlayerMovement
	lastMotionIsWalk bool
}

func newDesktopControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int, keys desktopKeyBindings) *desktopControlAdaptor {
	return &desktopControlAdaptor{ctx: ctx, ctrl: ctrl, w: w, h: h, keys: keys, pm: MovementStop}
}

func (dca *desktopControlAdaptor) Ctx() *maa.Context {
	return dca.ctx
}

func (dca *desktopControlAdaptor) TouchDown(contact, x, y int, delayMillis int) {
	dca.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (dca *desktopControlAdaptor) TouchUp(contact int, delayMillis int) {
	dca.ctrl.PostTouchUp(int32(contact)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (dca *desktopControlAdaptor) TouchClick(contact, x, y int, durationMillis, delayMillis int) {
	dca.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(durationMillis) * time.Millisecond)
	dca.ctrl.PostTouchUp(int32(contact)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (dca *desktopControlAdaptor) Swipe(contact, x, y, dx, dy int, durationMillis, delayMillis int) {
	stepDurationMillis := durationMillis / 2
	dca.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	dca.ctrl.PostTouchMove(int32(contact), int32(x+dx), int32(y+dy), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	dca.ctrl.PostTouchUp(int32(contact)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (dca *desktopControlAdaptor) SwipeHover(contact, x, y, dx, dy int, durationMillis, delayMillis int) {
	dca.ctrl.PostTouchMove(int32(contact), int32(x), int32(y), 0).Wait()
	time.Sleep(time.Duration(durationMillis) * time.Millisecond)
	dca.ctrl.PostTouchMove(int32(contact), int32(x+dx), int32(y+dy), 0).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (dca *desktopControlAdaptor) KeyDown(keyCode int, delayMillis int) {
	dca.ctrl.PostKeyDown(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (dca *desktopControlAdaptor) KeyUp(keyCode int, delayMillis int) {
	dca.ctrl.PostKeyUp(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (dca *desktopControlAdaptor) KeyType(keyCode int, delayMillis int) {
	dca.ctrl.PostClickKey(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (dca *desktopControlAdaptor) RotateCamera(dx, dy int) {
	cx, cy := dca.w/2, dca.h/2
	dca.SwipeHover(0, cx, cy, dx, dy, defaultDesktopKeyActionDelayMillis*3, defaultDesktopKeyActionDelayMillis)
}

func (dca *desktopControlAdaptor) GetPlayerMovement() PlayerMovement {
	return dca.pm
}

func (dca *desktopControlAdaptor) SetPlayerMovement(movement PlayerMovement, policy PlayerMovementPolicy) {
	if movement.Equals(dca.pm) {
		if policy >= PolicyActive {
			// Actively ensure moving state
			if movement.speed > MovementStop.speed {
				dca.KeyDown(dca.keys.W, defaultDesktopKeyActionDelayMillis/4)
			} else {
				dca.KeyUp(dca.keys.W, defaultDesktopKeyActionDelayMillis/4)
			}
		}
		return
	}

	if movement.speed <= MovementStop.speed {
		// Stop moving forward
		dca.KeyUp(dca.keys.W, defaultDesktopKeyActionDelayMillis)
	} else {
		if dca.lastMotionIsWalk {
			if movement.speed >= MovementSprint.speed {
				// Set to "sprint"
				dca.KeyType(dca.keys.Shift, defaultDesktopKeyActionDelayMillis)
				dca.lastMotionIsWalk = false
			} else if movement.speed >= MovementRun.speed {
				// Set to "run"
				dca.KeyType(dca.keys.Ctrl, defaultDesktopKeyActionDelayMillis)
				dca.lastMotionIsWalk = false
			} else {
				// Already in "walk", do nothing
			}
		} else {
			if movement.speed < MovementRun.speed {
				// Set to "walk"
				dca.KeyType(dca.keys.Ctrl, defaultDesktopKeyActionDelayMillis)
				dca.lastMotionIsWalk = true
			} else if movement.speed < MovementSprint.speed {
				if dca.pm.speed >= MovementSprint.speed {
					// Set to "walk" temporarily to terminate the "sprint" state, then set to "run"
					dca.KeyType(dca.keys.Ctrl, defaultDesktopKeyActionDelayMillis)
					dca.KeyType(dca.keys.Ctrl, defaultDesktopKeyActionDelayMillis)
				} else {
					// Already in "run", do nothing
				}
			} else {
				// Set to "sprint"
				dca.KeyType(dca.keys.Shift, defaultDesktopKeyActionDelayMillis)
			}
		}
		if policy >= PolicyDefault {
			// Ensure moving forward
			dca.KeyDown(dca.keys.W, defaultDesktopKeyActionDelayMillis/4)
		}
	}
	dca.pm = movement
}

func (dca *desktopControlAdaptor) PlayerJump() {
	dca.KeyType(dca.keys.Space, defaultDesktopKeyActionDelayMillis*4)
}

func (dca *desktopControlAdaptor) AggressivelyResetCamera() {
	// Policy: use ALT key to release mouse cursor and reset its position using a click, then release ALT key
	cx, cy := dca.w/2, dca.h/2
	stepDelayMillis := defaultDesktopKeyActionDelayMillis / 3
	dca.KeyDown(dca.keys.Alt, stepDelayMillis)
	dca.TouchClick(0, cx, cy, stepDelayMillis, 0)
	dca.KeyUp(dca.keys.Alt, stepDelayMillis)
}

func (dca *desktopControlAdaptor) AggressivelyResetPlayerMovement() {
	// Policy: sprint backward and immediately move forward to ensure the initial motional state is not walk
	dca.KeyDown(dca.keys.S, defaultDesktopKeyActionDelayMillis*2)
	dca.KeyType(dca.keys.Shift, defaultDesktopKeyActionDelayMillis*2)
	dca.KeyUp(dca.keys.S, defaultDesktopKeyActionDelayMillis*2)
	dca.KeyType(dca.keys.W, defaultDesktopKeyActionDelayMillis*2)
	dca.pm = MovementStop
	dca.lastMotionIsWalk = false
}

const defaultDesktopKeyActionDelayMillis = 25
