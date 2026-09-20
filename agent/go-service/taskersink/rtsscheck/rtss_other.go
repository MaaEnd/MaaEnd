//go:build !windows

package rtsscheck

// isEnableOSDEnabled always reports OSD disabled on non-Windows platforms.
func isEnableOSDEnabled() (enabled bool, profile string, err error) {
	return false, "", nil
}
