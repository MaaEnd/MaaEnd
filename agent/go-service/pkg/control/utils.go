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

type maaControllerInfoDto struct {
	Type string `json:"type"`
	HWnd uint64 `json:"hwnd"`
}

// ResolveControlType 返回归一化后的控制器类别（取值见 pienv.ControllerKind*）。
// 优先采用 PI 注入的类型；PI 环境缺失或类型未知时，回退到解析 controller.GetInfo()。
func ResolveControlType(ctrl *maa.Controller) (string, error) {
	if kind := pienv.ControllerKind(); kind != pienv.ControllerKindUnknown {
		return kind, nil
	}
	return GetControlType(ctrl)
}

// GetControlType 解析 controller.GetInfo()，返回归一化后的控制器类别。
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
		// Fallback：info 不是合法 JSON 时按子串探测。
		lower := strings.ToLower(infoStr)
		for _, candidate := range []string{
			pienv.ControllerKindWin32,
			pienv.ControllerKindMacOS,
			pienv.ControllerKindLinux,
			pienv.ControllerKindPlayCover,
			pienv.ControllerKindNativeAndroid,
			pienv.ControllerKindAdb,
		} {
			if strings.Contains(lower, candidate) {
				return candidate, nil
			}
		}
		return "", fmt.Errorf("failed to parse controller info via JSON: %w, and fallback parsing also failed", err)
	}
	if kind := pienv.NormalizeControllerKind(info.Type); kind != pienv.ControllerKindUnknown {
		return kind, nil
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
