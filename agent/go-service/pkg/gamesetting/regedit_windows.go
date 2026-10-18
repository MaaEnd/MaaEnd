//go:build windows

package gamesetting

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows/registry"
)

const (
	userGpuPreferencesPath    = `Software\Microsoft\DirectX\UserGpuPreferences`
	directXUserGlobalSettings = "DirectXUserGlobalSettings"
	autoHDREnableKey          = "AutoHDREnable"
	autoHDRDisabledValue      = "2096"
	autoHDREnabledValue       = "2097"
	autoHDRGlobalEnabledValue = "1"
)

// GetCachedUID 读取 Unity PlayerPrefs 中的 PLDK_cachedRoleId（游戏角色 UID）。
// 不是 u8sdk_cached_uid。返回 8–12 位纯数字字符串。
// 区服由 ResolveRegion 决定；不修改包级 registryPath。
func GetCachedUID() (string, error) {
	region, err := ResolveRegion()
	if err != nil {
		return "", err
	}

	path := registryPathCN
	if region == regionGlobal {
		path = registryPathGlobal
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("gamesetting: open %q failed: %w", path, err)
	}
	defer k.Close()

	name, err := findValueNameByPrefixUnder(k, path, valuePrefixPLDKCachedRoleId)
	if err != nil {
		return "", err
	}

	val, _, err := k.GetBinaryValue(name)
	if err != nil {
		return "", fmt.Errorf("gamesetting: read binary value %q failed: %w", name, err)
	}

	uid := strings.TrimSpace(strings.TrimRight(string(val), "\x00"))
	if err := validateCachedUID(uid); err != nil {
		return "", err
	}
	return uid, nil
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
	return findValueNameByPrefixUnder(k, registryPath, prefix)
}

func findValueNameByPrefixUnder(k registry.Key, path, prefix string) (string, error) {
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return "", fmt.Errorf("gamesetting: enumerate values under %q failed: %w", path, err)
	}

	var matches []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			matches = append(matches, n)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("gamesetting: no value with prefix %q under HKCU\\%s", prefix, path)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("gamesetting: ambiguous prefix %q under HKCU\\%s, matched %v", prefix, path, matches)
	}
}

// IsAutoHDREnabled 判断终末地是否会实际开启自动 HDR。
// 按应用 *Endfield.exe 的 AutoHDREnable 优先；未显式配置时回退到 DirectXUserGlobalSettings。
func IsAutoHDREnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, userGpuPreferencesPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("gamesetting: open %q failed: %w", userGpuPreferencesPath, err)
	}
	defer k.Close()

	names, err := k.ReadValueNames(-1)
	if err != nil {
		return false, fmt.Errorf("gamesetting: enumerate values under %q failed: %w", userGpuPreferencesPath, err)
	}

	var (
		globalValue string
		hasGlobal   bool
		perAppSeen  bool
		perAppOn    bool
	)

	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		isGlobal := strings.EqualFold(name, directXUserGlobalSettings)
		isPerApp := strings.EqualFold(filepath.Base(name), endfieldProcessName)
		if !isGlobal && !isPerApp {
			continue
		}

		raw, _, err := k.GetStringValue(name)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) {
				continue
			}
			return false, fmt.Errorf("gamesetting: read UserGpuPreferences %q failed: %w", name, err)
		}

		if isGlobal {
			if v, ok := parseAutoHDRValue(raw); ok {
				globalValue = v
				hasGlobal = true
			}
			continue
		}

		v, ok := parseAutoHDRValue(raw)
		if !ok {
			continue
		}
		perAppSeen = true
		if isAutoHDREnabledValue(v, true) {
			perAppOn = true
		}
	}

	if perAppSeen {
		return perAppOn, nil
	}
	if hasGlobal {
		return isAutoHDREnabledValue(globalValue, false), nil
	}
	return false, nil
}

// ApplyAutoHDR 按 mode 写入按应用自动 HDR：Unchanged 不改；Disable=2096；Enable=2097。
func ApplyAutoHDR(mode string) error {
	var value string
	switch strings.TrimSpace(mode) {
	case "", optionUnchanged:
		return nil
	case "Disable":
		value = autoHDRDisabledValue
	case "Enable":
		value = autoHDREnabledValue
	default:
		return fmt.Errorf("gamesetting: unknown Auto HDR mode %q", mode)
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, userGpuPreferencesPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			log.Info().
				Str("component", "gamesetting").
				Str("mode", mode).
				Msg("skip Auto HDR: UserGpuPreferences key not found")
			return nil
		}
		return fmt.Errorf("gamesetting: open %q failed: %w", userGpuPreferencesPath, err)
	}
	defer k.Close()

	names, err := k.ReadValueNames(-1)
	if err != nil {
		return fmt.Errorf("gamesetting: enumerate values under %q failed: %w", userGpuPreferencesPath, err)
	}

	target := autoHDREnableKey + "=" + value
	matched := 0
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || !strings.EqualFold(filepath.Base(name), endfieldProcessName) {
			continue
		}

		existing, _, err := k.GetStringValue(name)
		if err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("gamesetting: read UserGpuPreferences %q failed: %w", name, err)
		}

		if err := k.SetStringValue(name, mergeAutoHDRValue(existing, target)); err != nil {
			return fmt.Errorf("gamesetting: write UserGpuPreferences %q failed: %w", name, err)
		}
		matched++
		log.Info().
			Str("component", "gamesetting").
			Str("exe_path", name).
			Str("mode", mode).
			Str("value", value).
			Msg("applied Auto HDR for Endfield.exe")
	}
	if matched == 0 {
		log.Info().
			Str("component", "gamesetting").
			Str("mode", mode).
			Msg("skip Auto HDR: no Endfield.exe value under UserGpuPreferences")
	}
	return nil
}

func mergeAutoHDRValue(existing, target string) string {
	parts := strings.Split(existing, ";")
	var out []string
	found := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq > 0 && strings.EqualFold(part[:eq], autoHDREnableKey) {
			out = append(out, target)
			found = true
			continue
		}
		out = append(out, part)
	}
	if !found {
		out = append(out, target)
	}
	return strings.Join(out, ";") + ";"
}

// parseAutoHDRValue 从 UserGpuPreferences 字符串中解析 AutoHDREnable 的值。
func parseAutoHDRValue(raw string) (string, bool) {
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq <= 0 || !strings.EqualFold(part[:eq], autoHDREnableKey) {
			continue
		}
		return strings.TrimSpace(part[eq+1:]), true
	}
	return "", false
}

// isAutoHDREnabledValue 判断 AutoHDREnable 取值是否表示开启。
// perApp=true 时兼容按应用取值（2097/1/4147）；false 时仅全局 1 为开启。
func isAutoHDREnabledValue(value string, perApp bool) bool {
	switch strings.TrimSpace(value) {
	case autoHDRGlobalEnabledValue:
		return true
	case autoHDREnabledValue, "4147":
		return perApp
	default:
		return false
	}
}

const launcherUninstallPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\`

// FindLauncherInstallDir 读取启动器卸载项中的 InstallLocation。启动器为 32 位安装程序，
// 卸载项位于 WOW6432Node，故先查 32 位视图，再兼容 64 位视图与当前用户。
func FindLauncherInstallDir(uninstallKey string) (string, error) {
	lookups := []struct {
		root   registry.Key
		access uint32
	}{
		{registry.LOCAL_MACHINE, registry.QUERY_VALUE | registry.WOW64_32KEY},
		{registry.LOCAL_MACHINE, registry.QUERY_VALUE | registry.WOW64_64KEY},
		{registry.CURRENT_USER, registry.QUERY_VALUE},
	}

	for _, l := range lookups {
		k, err := registry.OpenKey(l.root, launcherUninstallPath+uninstallKey, l.access)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) {
				continue
			}
			return "", err
		}
		dir, _, err := k.GetStringValue("InstallLocation")
		k.Close()
		if err != nil || dir == "" {
			continue
		}
		// 启动器被移动或删除后卸载项可能残留，确认 Launcher.exe 仍存在
		if _, err := os.Stat(filepath.Join(dir, "Launcher.exe")); err != nil {
			log.Warn().
				Err(err).
				Str("uninstall_key", uninstallKey).
				Str("install_location", dir).
				Msg("launcher not found in uninstall InstallLocation")
			continue
		}
		return dir, nil
	}
	return "", fmt.Errorf("uninstall key %s not found", uninstallKey)
}

// FindLauncherInstallDirByProtocol 读取启动器注册的 URL 协议（HKCR\<protocolKey>\shell\open\command）
// 中的 Launcher.exe 路径。TapTap 版不写 Uninstall 卸载项，只能由此找到。
func FindLauncherInstallDirByProtocol(protocolKey string) (string, error) {
	k, err := registry.OpenKey(registry.CLASSES_ROOT, protocolKey+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("protocol key %s not found: %w", protocolKey, err)
	}
	defer k.Close()

	command, _, err := k.GetStringValue("")
	if err != nil {
		return "", fmt.Errorf("protocol key %s has no command: %w", protocolKey, err)
	}

	// 形如 "D:\...\Launcher.exe" "%1"，取第一个引号内的路径
	exePath := strings.TrimSpace(command)
	if strings.HasPrefix(exePath, `"`) {
		exePath = exePath[1:]
		if i := strings.Index(exePath, `"`); i >= 0 {
			exePath = exePath[:i]
		}
	} else if i := strings.Index(exePath, " "); i >= 0 {
		exePath = exePath[:i]
	}
	if !strings.EqualFold(filepath.Base(exePath), "Launcher.exe") {
		return "", fmt.Errorf("protocol key %s command is not Launcher.exe: %s", protocolKey, command)
	}
	// 卸载后协议键可能残留，确认文件仍存在
	if _, err := os.Stat(exePath); err != nil {
		return "", fmt.Errorf("protocol key %s points to missing launcher: %w", protocolKey, err)
	}
	return filepath.Dir(exePath), nil
}
