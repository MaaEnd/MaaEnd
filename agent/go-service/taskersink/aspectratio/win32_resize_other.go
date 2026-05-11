//go:build !windows

package aspectratio

import "errors"

// ResizeClientArea is not supported on non-Windows platforms.
func ResizeClientArea(_ uintptr, _, _ int32) (int32, int32, int32, int32, error) {
	return 0, 0, 0, 0, errors.New("window resize is only supported on Windows")
}

// RestoreWindowRect is not supported on non-Windows platforms.
func RestoreWindowRect(_ uintptr, _, _, _, _ int32) error {
	return errors.New("window restore is only supported on Windows")
}

// SendAltEnter is not supported on non-Windows platforms.
func SendAltEnter(_ uintptr) error {
	return errors.New("Alt+Enter is only supported on Windows")
}
