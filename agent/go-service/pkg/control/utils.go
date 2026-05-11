// Copyright (c) 2026 Harry Huang
package control

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

/* ******** Controller Type ******** */

const (
	CONTROL_TYPE_WIN32   = "win32"
	CONTROL_TYPE_WLROOTS = "wlroots"
	CONTROL_TYPE_ADB     = "adb"
)

type controllerCacheEntry struct {
	ControlType string
	Win32HWnd   uintptr
}

var controllerCache sync.Map

func loadControllerCache(ctrl *maa.Controller) (controllerCacheEntry, bool) {
	if ctrl == nil {
		return controllerCacheEntry{}, false
	}
	v, ok := controllerCache.Load(ctrl)
	if !ok {
		return controllerCacheEntry{}, false
	}
	entry, ok := v.(controllerCacheEntry)
	return entry, ok
}

func storeControllerCache(ctrl *maa.Controller, entry controllerCacheEntry) {
	if ctrl == nil {
		return
	}
	controllerCache.Store(ctrl, entry)
}

// InvalidateControllerCache clears cached metadata for a specific controller
// instance. Cache scope is controller-lifetime, not process-lifetime.
func InvalidateControllerCache(ctrl *maa.Controller) {
	if ctrl == nil {
		return
	}
	controllerCache.Delete(ctrl)
}

// GetControlType retrieves the control type of the given controller by parsing its info string.
func GetControlType(ctrl *maa.Controller) (string, error) {
	if entry, ok := loadControllerCache(ctrl); ok && entry.ControlType != "" {
		return entry.ControlType, nil
	}
	if ctrl == nil {
		return "", fmt.Errorf("nil controller")
	}

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
		HWnd uint64 `json:"hwnd"`
	}

	var info maaControllerInfo
	if err := json.Unmarshal([]byte(infoStr), &info); err != nil {
		// Fallback
		if strings.Contains(infoStr, CONTROL_TYPE_WIN32) {
			storeControllerCache(ctrl, controllerCacheEntry{ControlType: CONTROL_TYPE_WIN32})
			return CONTROL_TYPE_WIN32, nil
		}
		if strings.Contains(infoStr, CONTROL_TYPE_WLROOTS) {
			storeControllerCache(ctrl, controllerCacheEntry{ControlType: CONTROL_TYPE_WLROOTS})
			return CONTROL_TYPE_WLROOTS, nil
		}
		if strings.Contains(infoStr, CONTROL_TYPE_ADB) {
			storeControllerCache(ctrl, controllerCacheEntry{ControlType: CONTROL_TYPE_ADB})
			return CONTROL_TYPE_ADB, nil
		}
		return "", fmt.Errorf("failed to parse controller info via JSON: %w, and fallback parsing also failed", err)
	}
	if info.Type == "" {
		return "", fmt.Errorf("controller type is empty in parsed info")
	}

	if info.Type == CONTROL_TYPE_WIN32 {
		storeControllerCache(ctrl, controllerCacheEntry{
			ControlType: CONTROL_TYPE_WIN32,
			Win32HWnd:   uintptr(info.HWnd),
		})
		return CONTROL_TYPE_WIN32, nil
	}
	if info.Type == CONTROL_TYPE_WLROOTS {
		storeControllerCache(ctrl, controllerCacheEntry{ControlType: CONTROL_TYPE_WLROOTS})
		return CONTROL_TYPE_WLROOTS, nil
	}
	if info.Type == CONTROL_TYPE_ADB {
		storeControllerCache(ctrl, controllerCacheEntry{ControlType: CONTROL_TYPE_ADB})
		return CONTROL_TYPE_ADB, nil
	}
	return "", fmt.Errorf("unsupported controller type: %s", info.Type)
}

// GetCachedControlType returns controller type cached for a specific
// controller instance when available, otherwise falls back to GetControlType.
func GetCachedControlType(ctrl *maa.Controller) (string, error) {
	if entry, ok := loadControllerCache(ctrl); ok && entry.ControlType != "" {
		return entry.ControlType, nil
	}
	return GetControlType(ctrl)
}

// GetWin32HWnd returns the HWND that a Win32 controller is attached to.
// Prefers controller-scoped cached metadata; otherwise parses ctrl.GetInfo()
// directly. See MaaFramework's Win32ControlUnitMgr::get_info, which serializes
// `{"type":"win32","hwnd":<uint64>,...}`.
func GetWin32HWnd(ctrl *maa.Controller) (uintptr, error) {
	if entry, ok := loadControllerCache(ctrl); ok && entry.Win32HWnd != 0 {
		return entry.Win32HWnd, nil
	}
	if ctrl == nil {
		return 0, fmt.Errorf("nil controller")
	}
	infoStr, err := ctrl.GetInfo()
	if err != nil {
		return 0, fmt.Errorf("failed to get controller info: %w", err)
	}
	if infoStr == "" {
		return 0, fmt.Errorf("empty controller info")
	}
	var info struct {
		Type string `json:"type"`
		HWnd uint64 `json:"hwnd"`
	}
	if err := json.Unmarshal([]byte(infoStr), &info); err != nil {
		return 0, fmt.Errorf("failed to parse controller info: %w", err)
	}
	if info.Type != CONTROL_TYPE_WIN32 {
		return 0, fmt.Errorf("controller type is %q, not win32", info.Type)
	}
	if info.HWnd == 0 {
		return 0, fmt.Errorf("controller info has no hwnd field or hwnd is zero")
	}
	storeControllerCache(ctrl, controllerCacheEntry{
		ControlType: CONTROL_TYPE_WIN32,
		Win32HWnd:   uintptr(info.HWnd),
	})
	return uintptr(info.HWnd), nil
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
