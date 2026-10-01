// Copyright (c) 2026 Harry Huang
package control

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

/* ******** Controller Type ******** */

const (
	CONTROL_TYPE_WIN32 = "win32"
	CONTROL_TYPE_MACOS = "macos"
	CONTROL_TYPE_LINUX = "linux"
	CONTROL_TYPE_ADB   = "adb"
)

// controlTypeNativeAndroid 是 MaaFramework Android 原生控制器（MaaFwApp 在手机上直接跑）上报的类型。
// 走触控、画面是移动端 UI，与 ADB 一致，只是不经过 adb，所以统一归到 CONTROL_TYPE_ADB。
const controlTypeNativeAndroid = "native_android"

type maaControllerInfoDto struct {
	Type string `json:"type"`
	HWnd uint64 `json:"hwnd"`
}

func controlTypeFromPI() string {
	return controlTypeFromPIType(pienv.ControllerType())
}

func controlTypeFromPIType(controllerType string) string {
	switch strings.ToLower(strings.TrimSpace(controllerType)) {
	case CONTROL_TYPE_ADB, controlTypeNativeAndroid, "playcover":
		return CONTROL_TYPE_ADB
	case CONTROL_TYPE_WIN32:
		return CONTROL_TYPE_WIN32
	case CONTROL_TYPE_MACOS:
		return CONTROL_TYPE_MACOS
	case CONTROL_TYPE_LINUX:
		return CONTROL_TYPE_LINUX
	default:
		return ""
	}
}

// ResolveControlType returns PI_CONTROLLER type when MXU injects it; otherwise parses controller.GetInfo().
func ResolveControlType(ctrl *maa.Controller) (string, error) {
	if t := controlTypeFromPI(); t != "" {
		return t, nil
	}
	return GetControlType(ctrl)
}

// GetControlType retrieves the control type of the given controller by parsing its info string.
func GetControlType(ctrl *maa.Controller) (string, error) {
	if ctrl == nil {
		return "", fmt.Errorf("nil controller")
	}

	infoStr, err := ctrl.GetInfo()
	if err != nil {
		return "", err
	}
	return controlTypeFromInfo(infoStr)
}

// controlTypeFromInfo 从控制器 info 字符串解析出控制类型；与 GetControlType 分开是为了能脱离真实控制器测试。
func controlTypeFromInfo(infoStr string) (string, error) {
	if infoStr == "" {
		return "", fmt.Errorf("empty controller info")
	}

	var info maaControllerInfoDto
	if err := json.Unmarshal([]byte(infoStr), &info); err != nil {
		// Fallback
		if strings.Contains(infoStr, CONTROL_TYPE_WIN32) {
			return CONTROL_TYPE_WIN32, nil
		}
		if strings.Contains(infoStr, CONTROL_TYPE_MACOS) {
			return CONTROL_TYPE_MACOS, nil
		}
		if strings.Contains(infoStr, CONTROL_TYPE_LINUX) {
			return CONTROL_TYPE_LINUX, nil
		}
		if strings.Contains(infoStr, CONTROL_TYPE_ADB) || strings.Contains(infoStr, controlTypeNativeAndroid) {
			return CONTROL_TYPE_ADB, nil
		}
		return "", fmt.Errorf("failed to parse controller info via JSON: %w, and fallback parsing also failed", err)
	}
	if info.Type == "" {
		return "", fmt.Errorf("controller type is empty in parsed info")
	}

	if info.Type == CONTROL_TYPE_WIN32 {
		return CONTROL_TYPE_WIN32, nil
	}
	if info.Type == CONTROL_TYPE_MACOS {
		return CONTROL_TYPE_MACOS, nil
	}
	if info.Type == CONTROL_TYPE_LINUX {
		return CONTROL_TYPE_LINUX, nil
	}
	if info.Type == CONTROL_TYPE_ADB || info.Type == controlTypeNativeAndroid {
		return CONTROL_TYPE_ADB, nil
	}
	return "", fmt.Errorf("unsupported controller type: %s", info.Type)
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
