//go:build !windows

package aspectratio

import "errors"

// SendAltEnter is not supported on non-Windows platforms.
func SendAltEnter(_ uintptr) error {
	return errors.New("Alt+Enter is only supported on Windows")
}
