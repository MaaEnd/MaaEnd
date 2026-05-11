//go:build !windows

package gamesetting

import "errors"

var ErrUnsupported = errors.New("gamesetting: only supported on windows")

func GetVideoFullScreen() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoFullScreen(_ uint32) error {
	return ErrUnsupported
}
