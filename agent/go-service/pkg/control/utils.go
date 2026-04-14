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

// GetControlType retrieves the control type of the given controller by parsing its info string.
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
		if strings.Contains(infoStr, CONTROL_TYPE_WLROOTS) {
			CachedControlType = CONTROL_TYPE_WLROOTS
			return CONTROL_TYPE_WLROOTS, nil
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
	if info.Type == CONTROL_TYPE_WLROOTS {
		CachedControlType = CONTROL_TYPE_WLROOTS
		return CONTROL_TYPE_WLROOTS, nil
	}
	if info.Type == CONTROL_TYPE_ADB {
		CachedControlType = CONTROL_TYPE_ADB
		return CONTROL_TYPE_ADB, nil
	}
	return "", fmt.Errorf("unsupported controller type: %s", info.Type)
}

const (
	CONTROL_TYPE_WIN32   = "win32"
	CONTROL_TYPE_WLROOTS = "wlroots"
	CONTROL_TYPE_ADB     = "adb"
)

var CachedControlType string = ""

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
