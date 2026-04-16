//go:build !windows

package foregroundwindow

// activateForegroundWindow 在非 Windows 平台下为空操作。
func activateForegroundWindow(_ string, _ string) error {
	return nil
}
