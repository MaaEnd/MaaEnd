//go:build !windows

package hdrcheck

// IsHDREnabled always returns false on non-Windows platforms
// HDR detection is only supported on Windows
func IsHDREnabled() (bool, error) {
	return false, nil
}
