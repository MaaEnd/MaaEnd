//go:build !windows && !linux

package gamesetting

func getDWord(string) (uint32, error) {
	return 0, ErrUnsupported
}

func setDWord(string, uint32) error {
	return ErrUnsupported
}

// GetCachedUID 在非 Windows/Linux 平台不可用
func GetCachedUID() (string, error) {
	return "", ErrUnsupported
}

// ApplyAutoHDR 在非 Windows/Linux 平台不可用
func ApplyAutoHDR(_ string) error {
	return ErrUnsupported
}

// IsAutoHDREnabled 在非 Windows 平台始终视为未开启。
func IsAutoHDREnabled() (bool, error) {
	return false, nil
}

func FindLauncherInstallDir(_ string) (string, error) {
	return "", ErrUnsupported
}

func FindLauncherInstallDirByProtocol(_ string) (string, error) {
	return "", ErrUnsupported
}
