//go:build !windows

package launchgamepc

import "unsafe"

// moveLauncherToPrimaryMonitor 仅在 Windows 上生效
func moveLauncherToPrimaryMonitor(hWnd unsafe.Pointer) (bool, error) {
	return false, nil
}

// restoreLauncherWindow 仅在 Windows 上生效
func restoreLauncherWindow(hWnd unsafe.Pointer) bool {
	return false
}

// hasActiveMonitor 仅在 Windows 上生效，其他平台视为有显示器
func hasActiveMonitor() bool {
	return true
}
