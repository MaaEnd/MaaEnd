//go:build !windows

package launchgamepc

import "time"

// waitGameReady 仅在 Windows 上生效；其他平台不会进入 LaunchGamePC 流程。
func waitGameReady(timeout time.Duration) bool {
	return false
}

// closeLauncher 仅在 Windows 上生效。
func closeLauncher(installDir string, launcherPID uint32) {}
