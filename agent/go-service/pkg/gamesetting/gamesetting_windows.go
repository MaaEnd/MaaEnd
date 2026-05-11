//go:build windows

package gamesetting

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const registryPath = `Software\Hypergryph\Endfield`

const valuePrefixVideoFullScreen = `video_full_screen_h`

var ErrUnsupported = errors.New("gamesetting: only supported on windows")

func GetVideoFullScreen() (uint32, error) {
	return getDWord(valuePrefixVideoFullScreen)
}

func SetVideoFullScreen(value uint32) error {
	return setDWord(valuePrefixVideoFullScreen, value)
}

func getDWord(prefix string) (uint32, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, registryPath, registry.QUERY_VALUE)
	if err != nil {
		return 0, fmt.Errorf("gamesetting: open %q failed: %w", registryPath, err)
	}
	defer k.Close()

	name, err := findValueNameByPrefix(k, prefix)
	if err != nil {
		return 0, err
	}

	val, _, err := k.GetIntegerValue(name)
	if err != nil {
		return 0, fmt.Errorf("gamesetting: read value %q failed: %w", name, err)
	}
	return uint32(val), nil
}

func setDWord(prefix string, value uint32) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, registryPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("gamesetting: open %q failed: %w", registryPath, err)
	}
	defer k.Close()

	name, err := findValueNameByPrefix(k, prefix)
	if err != nil {
		return err
	}

	if err := k.SetDWordValue(name, value); err != nil {
		return fmt.Errorf("gamesetting: write value %q failed: %w", name, err)
	}
	return nil
}

func findValueNameByPrefix(k registry.Key, prefix string) (string, error) {
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return "", fmt.Errorf("gamesetting: enumerate values under %q failed: %w", registryPath, err)
	}

	var matches []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			matches = append(matches, n)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("gamesetting: no value with prefix %q under HKCU\\%s", prefix, registryPath)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("gamesetting: ambiguous prefix %q under HKCU\\%s, matched %v", prefix, registryPath, matches)
	}
}
