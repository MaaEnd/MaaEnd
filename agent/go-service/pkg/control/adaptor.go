// Copyright (c) 2026 Harry Huang
package control

import (
	"fmt"
	"math"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

/* ******** Control Adaptor Base Interface ******** */

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
	Swipe(contact, x, y, dx, dy int, durationMillis, delayMillis int)

	// SwipeHover performs an only-hover swipe from (x, y) to (x+dx, y+dy) with the given duration and delay after the action.
	SwipeHover(contact, x, y, dx, dy int, durationMillis, delayMillis int)

	// KeyDown performs a key down of the given key code with delay after the action.
	KeyDown(keyCode int, delayMillis int)

	// KeyUp performs a key up of the given key code with delay after the action.
	KeyUp(keyCode int, delayMillis int)

	// KeyType performs a key type of the given key code with delay after the action.
	KeyType(keyCode int, delayMillis int)

	// RotateCamera performs a camera rotation by only-hover swipe starting from
	// the center of the screen with the given delta.
	RotateCamera(dx, dy int)

	// GetPlayerMovement returns the current player movement state.
	GetPlayerMovement() PlayerMovement

	// SetPlayerMovement sets the player movement state to the given value,
	// and performs necessary control actions to achieve that state.
	SetPlayerMovement(movement PlayerMovement, policy PlayerMovementPolicy)

	// PlayerJump performs the player jump action once.
	// This will not change the player movement state.
	PlayerJump()

	// AggressivelyResetCamera eliminates the side effect of camera rotation.
	// Different implementations may have different ways to achieve this.
	AggressivelyResetCamera()

	// AggressivelyResetPlayerMovement provides an aggressive way to reset player movement for initialization purpose.
	// Different implementations may have different ways to achieve this.
	AggressivelyResetPlayerMovement()
}

// NewControlAdaptor creates a new ControlAdaptor instance.
// The implementation type is determined by the controller info obtained from the Maa Controller.
func NewControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) (ControlAdaptor, error) {
	controlType, err := GetControlType(ctrl)
	if err != nil {
		return nil, fmt.Errorf("failed to get control type: %w", err)
	}

	switch controlType {
	case CONTROL_TYPE_WIN32:
		return newDefaultDesktopControlAdaptor(ctx, ctrl, w, h), nil
	case CONTROL_TYPE_WLROOTS:
		return newWlrootsControlAdaptor(ctx, ctrl, w, h), nil
	case CONTROL_TYPE_ADB:
		return newADBControlAdaptor(ctx, ctrl, w, h), nil
	default:
		return nil, fmt.Errorf("unsupported control type: %s", controlType)
	}
}

/* ******** Player Movement Enumeration ******** */

// PlayerMovement represents different movement state in the game
type PlayerMovement struct {
	speed         float64 // Movement speed (px/s)
	rotationSpeed float64 // Rotation adjustment response speed (degrees/s)
}

// Equals checks if this PlayerMovement is approximately equal to another one.
func (pm PlayerMovement) Equals(other PlayerMovement) bool {
	return math.Abs(pm.speed-other.speed) <= 1e-6 && math.Abs(pm.rotationSpeed-other.rotationSpeed) <= 1e-6
}

// EtaOfDistance returns the minimal estimated time to cover the given distance at this movement speed.
func (pm PlayerMovement) EtaOfDistance(dist float64) time.Duration {
	if pm.speed <= 1e-6 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(float64(time.Second) * dist / pm.speed)
}

// EtaOfRotation returns the minimal estimated time to adjust the given rotation at this rotation speed.
func (pm PlayerMovement) EtaOfRotation(rot float64) time.Duration {
	if pm.rotationSpeed <= 1e-6 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(float64(time.Second) * math.Abs(rot) / pm.rotationSpeed)
}

// DistanceDuring returns the maximal distance that can be covered during the given duration at this movement speed.
func (pm PlayerMovement) DistanceDuring(duration time.Duration) float64 {
	return pm.speed * duration.Seconds()
}

// RotationDuring returns the maximal rotation adjustment that can be achieved during the given duration at this rotation speed.
func (pm PlayerMovement) RotationDuring(duration time.Duration) float64 {
	return pm.rotationSpeed * duration.Seconds()
}

var (
	MovementStop   = PlayerMovement{0.0, 0.0}
	MovementWalk   = PlayerMovement{2.0, 270.0}
	MovementRun    = PlayerMovement{8.0, 540.0}
	MovementSprint = PlayerMovement{12.0, 1080.0}
)

/* ******** Player Movement Policy Enumeration ******** */

type PlayerMovementPolicy int

const (
	// PolicyLazy avoids any unnecessary key action if the new movement state is already achieved,
	// which may cause less latency but also less robustness.
	PolicyLazy PlayerMovementPolicy = 0
	// PolicyDefault balances between [PolicyLazy] and [PolicyActive] policy.
	PolicyDefault PlayerMovementPolicy = 1
	// PolicyActive performs extra key actions to actively ensure the new movement state is achieved,
	// which may cause more latency but also more robustness.
	PolicyActive PlayerMovementPolicy = 2
)
