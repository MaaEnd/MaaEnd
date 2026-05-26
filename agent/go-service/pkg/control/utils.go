// Copyright (c) 2026 Harry Huang
package control

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

/* ******** Controller Type ******** */

const (
	CONTROL_TYPE_WIN32   = "win32"
	CONTROL_TYPE_WLROOTS = "wlroots"
	CONTROL_TYPE_ADB     = "adb"
)

const (
	win32InputMethodSendMessage              = 1 << 1
	win32InputMethodPostMessage              = 1 << 2
	win32InputMethodSendMessageWithCursorPos = 1 << 5
	win32InputMethodPostMessageWithCursorPos = 1 << 6
	win32InputMethodSendMessageWithWindowPos = 1 << 7
	win32InputMethodPostMessageWithWindowPos = 1 << 8
)

type maaControllerInfoDto struct {
	Type           string `json:"type"`
	HWnd           uint64 `json:"hwnd"`
	MouseMethod    *int   `json:"mouse_method"`
	KeyboardMethod *int   `json:"keyboard_method"`
}

func getControllerInfo(ctrl *maa.Controller) (maaControllerInfoDto, string, error) {
	if ctrl == nil {
		return maaControllerInfoDto{}, "", fmt.Errorf("nil controller")
	}

	infoStr, err := ctrl.GetInfo()
	if err != nil {
		return maaControllerInfoDto{}, "", err
	}
	if infoStr == "" {
		return maaControllerInfoDto{}, "", fmt.Errorf("empty controller info")
	}

	var info maaControllerInfoDto
	if err := json.Unmarshal([]byte(infoStr), &info); err != nil {
		return maaControllerInfoDto{}, infoStr, fmt.Errorf("failed to parse controller info via JSON: %w", err)
	}
	return info, infoStr, nil
}

// GetControlType retrieves the control type of the given controller by parsing its info string.
func GetControlType(ctrl *maa.Controller) (string, error) {
	info, infoStr, err := getControllerInfo(ctrl)
	if err != nil {
		if strings.Contains(infoStr, CONTROL_TYPE_WIN32) {
			return CONTROL_TYPE_WIN32, nil
		}
		if strings.Contains(infoStr, CONTROL_TYPE_WLROOTS) {
			return CONTROL_TYPE_WLROOTS, nil
		}
		if strings.Contains(infoStr, CONTROL_TYPE_ADB) {
			return CONTROL_TYPE_ADB, nil
		}
		return "", err
	}
	if info.Type == "" {
		return "", fmt.Errorf("controller type is empty in parsed info")
	}

	if info.Type == CONTROL_TYPE_WIN32 {
		return CONTROL_TYPE_WIN32, nil
	}
	if info.Type == CONTROL_TYPE_WLROOTS {
		return CONTROL_TYPE_WLROOTS, nil
	}
	if info.Type == CONTROL_TYPE_ADB {
		return CONTROL_TYPE_ADB, nil
	}
	return "", fmt.Errorf("unsupported controller type: %s", info.Type)
}

func IsWin32MessageInputMethod(method int) bool {
	switch method {
	case win32InputMethodSendMessage,
		win32InputMethodPostMessage,
		win32InputMethodSendMessageWithCursorPos,
		win32InputMethodPostMessageWithCursorPos,
		win32InputMethodSendMessageWithWindowPos,
		win32InputMethodPostMessageWithWindowPos:
		return true
	default:
		return false
	}
}

func IsMessageInputWin32(ctrl *maa.Controller) bool {
	info, _, err := getControllerInfo(ctrl)
	if err != nil {
		log.Debug().Err(err).Str("component", "Control").Msg("failed to read controller info")
		return false
	}
	if info.Type != CONTROL_TYPE_WIN32 || info.MouseMethod == nil {
		return false
	}
	return IsWin32MessageInputMethod(*info.MouseMethod)
}

func TrySetMouseLockFollow(ctrl *maa.Controller, enabled bool) bool {
	if !IsMessageInputWin32(ctrl) {
		return false
	}
	if err := ctrl.SetMouseLockFollow(enabled); err != nil {
		log.Warn().Err(err).Bool("enabled", enabled).Str("component", "Control").Msg("failed to set mouse lock follow")
		return false
	}
	return true
}

func TrySetBackgroundManagedKeys(ctrl *maa.Controller, keys []int32) bool {
	controlType, err := GetControlType(ctrl)
	if err != nil || controlType != CONTROL_TYPE_WIN32 {
		if err != nil {
			log.Debug().Err(err).Str("component", "Control").Msg("failed to read controller type")
		}
		return false
	}
	if err := setBackgroundManagedKeys(ctrl, keys); err != nil {
		log.Warn().Err(err).Ints32("keys", keys).Str("component", "Control").Msg("failed to set background managed keys")
		return false
	}
	return true
}

func TryPostRelativeMove(ctrl *maa.Controller, dx, dy int32) bool {
	if ctrl == nil {
		return false
	}
	job := ctrl.PostRelativeMove(dx, dy).Wait()
	if !job.Success() {
		log.Debug().Int32("dx", dx).Int32("dy", dy).Str("component", "Control").Msg("relative move failed")
		return false
	}
	return true
}

/* ******** Screen Diagonal Size ******** */

// GetScreenDiagonalSize calculates the diagonal size of the screen based on the controller's raw resolution,
// which can be used for dynamic adjustments in control logic.
//
// When failed to get the diagonal size, or the diagonal size is less than 800.0,
// it will fallback to the default value 800.0 (640x480).
func GetScreenDiagonalSize(ctrl *maa.Controller) float64 {
	const FALLBACK = 800.0

	if ctrl == nil {
		return FALLBACK
	}

	rawWidth, rawHeight, err := ctrl.GetResolution()
	if err != nil || rawWidth <= 0 || rawHeight <= 0 {
		return FALLBACK
	}

	diagonal := math.Hypot(float64(rawWidth), float64(rawHeight))
	return max(diagonal, FALLBACK)
}
