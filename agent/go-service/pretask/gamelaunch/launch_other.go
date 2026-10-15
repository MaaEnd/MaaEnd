//go:build !windows

package gamelaunch

import "errors"

func launch(string) error {
	return errors.New("official CN launcher startup is only supported on Windows")
}
