// Copyright (c) 2026 Harry Huang
package control

import (
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type ADBControlAdaptor struct {
	ctx  *maa.Context
	ctrl *maa.Controller
	w    int
	h    int

	pm               PlayerMovement
	lastMotionIsWalk bool
}

func newADBControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *ADBControlAdaptor {
	return &ADBControlAdaptor{ctx, ctrl, w, h, MovementStop, false}
}

func (aca *ADBControlAdaptor) Ctx() *maa.Context {
	return aca.ctx
}

func (aca *ADBControlAdaptor) TouchDown(contact, x, y int, delayMillis int) {
	aca.ctrl.PostTouchMove(int32(contact), int32(x), int32(y), 1).Wait()
	aca.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (aca *ADBControlAdaptor) TouchUp(contact int, delayMillis int) {
	aca.ctrl.PostTouchUp(int32(contact)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (aca *ADBControlAdaptor) TouchClick(contact, x, y int, durationMillis, delayMillis int) {
	aca.ctrl.PostTouchMove(int32(contact), int32(x), int32(y), 1).Wait()
	aca.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(durationMillis) * time.Millisecond)
	aca.ctrl.PostTouchUp(int32(contact)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (aca *ADBControlAdaptor) Swipe(contact, x, y, dx, dy int, durationMillis, delayMillis int) {
	aca.ctrl.PostSwipeV2(int32(x), int32(y), int32(x+dx), int32(y+dy), time.Duration(durationMillis)*time.Millisecond, int32(contact), 1).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (aca *ADBControlAdaptor) SwipeHover(contact, x, y, dx, dy int, durationMillis, delayMillis int) {
	aca.ctrl.PostSwipeV2(int32(x), int32(y), int32(x+dx), int32(y+dy), time.Duration(durationMillis)*time.Millisecond, int32(contact), 0).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (aca *ADBControlAdaptor) KeyDown(keyCode int, delayMillis int) {
	aca.ctrl.PostKeyDown(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (aca *ADBControlAdaptor) KeyUp(keyCode int, delayMillis int) {
	aca.ctrl.PostKeyUp(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (aca *ADBControlAdaptor) KeyType(keyCode int, delayMillis int) {
	aca.ctrl.PostClickKey(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (aca *ADBControlAdaptor) RotateCamera(dx, dy int) {
	cx, cy := aca.w/4*3, aca.h/2
	aca.Swipe(cameraContact, cx, cy, dx, dy, defaultTouchActionDelayMillis*5/2, 0)
}

func (aca *ADBControlAdaptor) GetPlayerMovement() PlayerMovement {
	return aca.pm
}

func (aca *ADBControlAdaptor) SetPlayerMovement(movement PlayerMovement) {
	if movement.Equals(aca.pm) {
		return
	}

	// Note: Currently "sprint" is temporarily disabled in ADB
	if movement.speed >= MovementSprint.speed {
		movement = MovementRun
	}

	if movement.speed <= MovementStop.speed {
		// Stop moving forward
		aca.TouchUp(joystickContact, defaultTouchActionDelayMillis)
	} else {
		if aca.lastMotionIsWalk {
			if movement.speed >= MovementSprint.speed {
				// Set to "sprint"
				aca.TouchClick(sprintButtonContact, SPRINT_BUTTON_X, SPRINT_BUTTON_Y, defaultTouchActionDelayMillis, 0)
				aca.lastMotionIsWalk = false
			} else if movement.speed >= MovementRun.speed {
				// Set to "run"
				aca.TouchDown(joystickContact, JOYSTICK_CENTER_X, JOYSTICK_CENTER_Y+JOYSTICK_RUN_DY, 0)
				aca.lastMotionIsWalk = false
			} else {
				// Already in "walk", do nothing
			}
		} else {
			if movement.speed < MovementRun.speed {
				// Set to "walk"
				aca.TouchDown(joystickContact, JOYSTICK_CENTER_X, JOYSTICK_CENTER_Y+JOYSTICK_WALK_DY, 0)
				aca.lastMotionIsWalk = true
			} else if movement.speed < MovementSprint.speed {
				if aca.pm.speed >= MovementSprint.speed {
					// Set to "stop" temporarily to terminate the "sprint" state, then set to "run"
					aca.TouchUp(joystickContact, defaultTouchActionDelayMillis)
					aca.TouchDown(joystickContact, JOYSTICK_CENTER_X, JOYSTICK_CENTER_Y+JOYSTICK_RUN_DY, 0)
				} else {
					// Already in "run", do nothing else
				}
				aca.TouchDown(joystickContact, JOYSTICK_CENTER_X, JOYSTICK_CENTER_Y+JOYSTICK_RUN_DY, 0)
			} else {
				// Set to "sprint"
				aca.TouchClick(sprintButtonContact, SPRINT_BUTTON_X, SPRINT_BUTTON_Y, defaultTouchActionDelayMillis, 0)
				aca.TouchDown(joystickContact, JOYSTICK_CENTER_X, JOYSTICK_CENTER_Y+JOYSTICK_RUN_DY, 0)
			}
		}
	}
	aca.pm = movement
}

func (aca *ADBControlAdaptor) PlayerJump() {
	aca.TouchClick(jumpButtonContact, JUMP_BUTTON_X, JUMP_BUTTON_Y, defaultTouchActionDelayMillis*4, 0)
}

func (aca *ADBControlAdaptor) PlayerSprint() {
	aca.TouchClick(sprintButtonContact, SPRINT_BUTTON_X, SPRINT_BUTTON_Y, defaultTouchActionDelayMillis, 0)
	aca.pm = MovementSprint
	aca.lastMotionIsWalk = false
}

func (aca *ADBControlAdaptor) PlayerStop() {
	aca.TouchUp(joystickContact, defaultTouchActionDelayMillis)
	aca.pm = MovementStop
}

func (aca *ADBControlAdaptor) AggressivelyResetCamera() {
	// ADB has no need to reset camera aggressively
}

func (aca *ADBControlAdaptor) AggressivelyResetPlayerMovement() {
	// ADB has no need to reset player movement aggressively
}

const (
	JOYSTICK_CENTER_X = 195
	JOYSTICK_CENTER_Y = 551
	JOYSTICK_WALK_DY  = -15
	JOYSTICK_RUN_DY   = -90

	JUMP_BUTTON_X = 1166
	JUMP_BUTTON_Y = 475

	SPRINT_BUTTON_X = 1166
	SPRINT_BUTTON_Y = 620
)

const (
	joystickContact               = 0
	cameraContact                 = 1
	sprintButtonContact           = 2
	jumpButtonContact             = 3
	defaultTouchActionDelayMillis = 50
)
