// Copyright (c) 2026 Harry Huang
package control

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
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
	SetPlayerMovement(movement PlayerMovement)

	// PlayerJump performs the player jump action once.
	// This will not change the player movement state.
	PlayerJump()

	// PlayerSprint performs the player sprint action once.
	// This will set the player movement state to at least sprint.
	PlayerSprint()

	// PlayerStop lets the player stop moving forward.
	// This will set the player movement state to stop.
	PlayerStop()

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
		return newWindowsControlAdaptor(ctx, ctrl, w, h), nil
	case CONTROL_TYPE_ADB:
		return newADBControlAdaptor(ctx, ctrl, w, h), nil
	default:
		return nil, fmt.Errorf("unsupported control type: %s", controlType)
	}
}

func GetControlType(ctrl *maa.Controller) (string, error) {
	infoStr, err := ctrl.GetInfo()
	if err != nil {
		return "", err
	}
	log.Info().Str("controllerInfo", infoStr).Msg("Fetched controller info")
	if infoStr == "" {
		return "", fmt.Errorf("empty controller info")
	}

	type maaControllerInfo struct {
		Type string `json:"type"`
	}

	var info maaControllerInfo
	if err := json.Unmarshal([]byte(infoStr), &info); err != nil {
		// Fallback
		if strings.Contains(infoStr, CONTROL_TYPE_WIN32) {
			CachedControlType = CONTROL_TYPE_WIN32
			return CONTROL_TYPE_WIN32, nil
		}
		if strings.Contains(infoStr, CONTROL_TYPE_ADB) {
			CachedControlType = CONTROL_TYPE_ADB
			return CONTROL_TYPE_ADB, nil
		}
		return "", fmt.Errorf("failed to parse controller info via JSON: %w, and fallback parsing also failed", err)
	}
	if info.Type == "" {
		return "", fmt.Errorf("controller type is empty in parsed info")
	}

	if info.Type == CONTROL_TYPE_WIN32 {
		CachedControlType = CONTROL_TYPE_WIN32
		return CONTROL_TYPE_WIN32, nil
	}
	if info.Type == CONTROL_TYPE_ADB {
		CachedControlType = CONTROL_TYPE_ADB
		return CONTROL_TYPE_ADB, nil
	}
	return "", fmt.Errorf("unsupported controller type: %s", info.Type)
}

const (
	CONTROL_TYPE_WIN32 = "win32"
	CONTROL_TYPE_ADB   = "adb"
)

var CachedControlType string = ""

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
