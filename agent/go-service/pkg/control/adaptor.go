// Copyright (c) 2026 Harry Huang
package control

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// ControlAdaptor defines an interface for abstracting control actions, allowing different implementations for different platforms.
type ControlAdaptor interface {
	// Ctx returns the wrapped Maa Framework context.
	Ctx() *maa.Context

	// TouchDown performs a touch down at (x, y) with the given contact ID and delay after the action.
	TouchDown(contact, x, y int, delayMillis int)

	// TouchUp performs a touch up of the given contact ID with delay after the action.
	TouchUp(contact int, delayMillis int)

	// TouchClick performs a touch down and up at (x, y) with the given contact ID, duration of the touch, and delay after the action.
	TouchClick(contact, x, y int, durationMillis, delayMillis int)

	// Swipe performs an actual swipe from (x, y) to (x+dx, y+dy) with the given duration and delay after the action.
	Swipe(x, y, dx, dy int, durationMillis, delayMillis int)

	// SwipeHover performs an only-hover swipe from (x, y) to (x+dx, y+dy) with the given duration and delay after the action.
	SwipeHover(x, y, dx, dy int, durationMillis, delayMillis int)

	// KeyDown performs a key down of the given key code with delay after the action.
	KeyDown(keyCode int, delayMillis int)

	// KeyUp performs a key up of the given key code with delay after the action.
	KeyUp(keyCode int, delayMillis int)

	// KeyType performs a key type of the given key code with delay after the action.
	KeyType(keyCode int, delayMillis int)

	// RotateCamera performs a camera rotation by only-hover swipe starting from the center of the screen
	// with the given delta, duration and delay after the action.
	RotateCamera(dx, dy int, durationMillis, delayMillis int)

	// RotateCameraEliminateSideEffect eliminates the side effect of camera rotation with delay after the action.
	// Different implementations may have different ways to achieve this.
	RotateCameraEliminateSideEffect(delayMillis int)
}

// NewControlAdaptor creates a new ControlAdaptor instance.
func NewControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) ControlAdaptor {
	// Currently only Windows is supported
	return newWindowsControlAdaptor(ctx, ctrl, w, h)
}
