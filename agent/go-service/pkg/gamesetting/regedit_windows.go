//go:build windows

package gamesetting

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows/registry"
)

const (
	registryPathCN     = `Software\Hypergryph\Endfield`
	registryPathGlobal = `Software\Gryphline\Endfield`

	userGpuPreferencesPath    = `Software\Microsoft\DirectX\UserGpuPreferences`
	directXUserGlobalSettings = "DirectXUserGlobalSettings"
	autoHDREnableKey          = "AutoHDREnable"
	autoHDRDisabledValue      = "2096"
	autoHDREnabledValue       = "2097"
	autoHDRGlobalEnabledValue = "1"
	autoHDRUserOverrideFlag   = 0x1000
	autoHDREnabledFlag        = 0x1
)

var registryPath = registryPathCN

var ErrUnsupported = errors.New("gamesetting: only supported on windows")

// Unity PlayerPrefs 值名为「键名_h + djb2-xor(键名)」，哈希只取决于键名，各机器一致，故直接写死完整值名。
// 游戏未保存过的设置不会出现在注册表中，按完整值名写入即可新建。
const (
	valueNameScreenmanagerFullscreenMode         = `Screenmanager Fullscreen mode_h3630240806`
	valueNameScreenmanagerResolutionHeight       = `Screenmanager Resolution Height_h2627697771`
	valueNameScreenmanagerResolutionWidth        = `Screenmanager Resolution Width_h182942802`
	valueNameScreenmanagerResolutionWindowHeight = `Screenmanager Resolution Window Height_h1684712807`
	valueNameScreenmanagerResolutionWindowWidth  = `Screenmanager Resolution Window Width_h2524650974`
	valueNameScreenmanagerWindowPositionX        = `Screenmanager Window Position X_h4088080503`
	valueNameScreenmanagerWindowPositionY        = `Screenmanager Window Position Y_h4088080502`
	valueNameLanguageTextChange                  = `language_text_change_h1448997164`
	valueNameVideoCustomQuality                  = `video_custom_quality_h2367254986`
	valueNameVideoFrameRate8                     = `video_frame_rate_8_h697513772`
	valueNameVideoFullScreen                     = `video_full_screen_h1998742411`
	valueNameVideoQualityAnisoLevel1             = `video_quality_anisoLevel_1_h3121203451`
	valueNameVideoQualityContactShadow           = `video_quality_contactshadow_h2467683423`
	valueNameVideoQualityDLSSMode1               = `video_quality_dlss_mode_1_h833809667`
	valueNameVideoQualityMain                    = `video_quality_main_h2648490162`
	valueNameVideoQualityReflex                  = `video_quality_reflex_h2003858649`
	valueNameVideoQualitySharpness               = `video_quality_sharpness_h269037322`
	valueNameVideoQualityUpscaler                = `video_quality_upscaler_h1386130102`
	valueNameVideoResolution                     = `video_resolution_h1805082501`
	valueNameVideoResolutionHeight               = `video_resolution_height_h2517654917`
	valueNameVideoResolutionWidth                = `video_resolution_width_h583690364`
	valueNameVideoTextureQuality1                = `video_texture_quality_1_h3061223496`
	// PLDK_cachedRoleId 是游戏角色 UID；不要与 u8sdk_cached_uid 混淆。
	valuePrefixPLDKCachedRoleId = `PLDK_cachedRoleId`
)

func GetScreenmanagerFullscreenMode() (uint32, error) {
	return getDWord(valueNameScreenmanagerFullscreenMode)
}

func SetScreenmanagerFullscreenMode(value uint32) error {
	return setDWord(valueNameScreenmanagerFullscreenMode, value)
}

func GetScreenmanagerResolutionHeight() (uint32, error) {
	return getDWord(valueNameScreenmanagerResolutionHeight)
}

func SetScreenmanagerResolutionHeight(value uint32) error {
	return setDWord(valueNameScreenmanagerResolutionHeight, value)
}

func GetScreenmanagerResolutionWidth() (uint32, error) {
	return getDWord(valueNameScreenmanagerResolutionWidth)
}

func SetScreenmanagerResolutionWidth(value uint32) error {
	return setDWord(valueNameScreenmanagerResolutionWidth, value)
}

func GetScreenmanagerResolutionWindowHeight() (uint32, error) {
	return getDWord(valueNameScreenmanagerResolutionWindowHeight)
}

func SetScreenmanagerResolutionWindowHeight(value uint32) error {
	return setDWord(valueNameScreenmanagerResolutionWindowHeight, value)
}

func GetScreenmanagerResolutionWindowWidth() (uint32, error) {
	return getDWord(valueNameScreenmanagerResolutionWindowWidth)
}

func SetScreenmanagerResolutionWindowWidth(value uint32) error {
	return setDWord(valueNameScreenmanagerResolutionWindowWidth, value)
}

func GetScreenmanagerWindowPositionX() (uint32, error) {
	return getDWord(valueNameScreenmanagerWindowPositionX)
}

func SetScreenmanagerWindowPositionX(value uint32) error {
	return setDWord(valueNameScreenmanagerWindowPositionX, value)
}

func GetScreenmanagerWindowPositionY() (uint32, error) {
	return getDWord(valueNameScreenmanagerWindowPositionY)
}

func SetScreenmanagerWindowPositionY(value uint32) error {
	return setDWord(valueNameScreenmanagerWindowPositionY, value)
}

// GetLanguageTextChange reads Endfield's text language.
func GetLanguageTextChange() (uint32, error) {
	return getDWord(valueNameLanguageTextChange)
}

// SetLanguageTextChange writes Endfield's text language.
// 语音语言存放在独立的注册表项，不受此项影响。
func SetLanguageTextChange(value uint32) error {
	return setDWord(valueNameLanguageTextChange, value)
}

func GetVideoCustomQuality() (uint32, error) {
	return getDWord(valueNameVideoCustomQuality)
}

func SetVideoCustomQuality(value uint32) error {
	return setDWord(valueNameVideoCustomQuality, value)
}

func GetVideoFrameRate8() (uint32, error) {
	return getDWord(valueNameVideoFrameRate8)
}

func SetVideoFrameRate8(value uint32) error {
	return setDWord(valueNameVideoFrameRate8, value)
}

func GetVideoFullScreen() (uint32, error) {
	return getDWord(valueNameVideoFullScreen)
}

func SetVideoFullScreen(value uint32) error {
	return setDWord(valueNameVideoFullScreen, value)
}

func GetVideoQualityAnisoLevel1() (uint32, error) {
	return getDWord(valueNameVideoQualityAnisoLevel1)
}

func SetVideoQualityAnisoLevel1(value uint32) error {
	return setDWord(valueNameVideoQualityAnisoLevel1, value)
}

func GetVideoQualityContactShadow() (uint32, error) {
	return getDWord(valueNameVideoQualityContactShadow)
}

func SetVideoQualityContactShadow(value uint32) error {
	return setDWord(valueNameVideoQualityContactShadow, value)
}

func GetVideoQualityDLSSMode1() (uint32, error) {
	return getDWord(valueNameVideoQualityDLSSMode1)
}

func SetVideoQualityDLSSMode1(value uint32) error {
	return setDWord(valueNameVideoQualityDLSSMode1, value)
}

func GetVideoQualityMain() (uint32, error) {
	return getDWord(valueNameVideoQualityMain)
}

func SetVideoQualityMain(value uint32) error {
	return setDWord(valueNameVideoQualityMain, value)
}

func GetVideoQualityReflex() (uint32, error) {
	return getDWord(valueNameVideoQualityReflex)
}

func SetVideoQualityReflex(value uint32) error {
	return setDWord(valueNameVideoQualityReflex, value)
}

func GetVideoQualitySharpness() (uint32, error) {
	return getDWord(valueNameVideoQualitySharpness)
}

func SetVideoQualitySharpness(value uint32) error {
	return setDWord(valueNameVideoQualitySharpness, value)
}

func GetVideoQualityUpscaler() (uint32, error) {
	return getDWord(valueNameVideoQualityUpscaler)
}

func SetVideoQualityUpscaler(value uint32) error {
	return setDWord(valueNameVideoQualityUpscaler, value)
}

func GetVideoResolution() (uint32, error) {
	return getDWord(valueNameVideoResolution)
}

func SetVideoResolution(value uint32) error {
	return setDWord(valueNameVideoResolution, value)
}

func GetVideoResolutionHeight() (uint32, error) {
	return getDWord(valueNameVideoResolutionHeight)
}

func SetVideoResolutionHeight(value uint32) error {
	return setDWord(valueNameVideoResolutionHeight, value)
}

func GetVideoResolutionWidth() (uint32, error) {
	return getDWord(valueNameVideoResolutionWidth)
}

func SetVideoResolutionWidth(value uint32) error {
	return setDWord(valueNameVideoResolutionWidth, value)
}

func GetVideoTextureQuality1() (uint32, error) {
	return getDWord(valueNameVideoTextureQuality1)
}

func SetVideoTextureQuality1(value uint32) error {
	return setDWord(valueNameVideoTextureQuality1, value)
}

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
	if uid == "" {
		return "", fmt.Errorf("gamesetting: PLDK_cachedRoleId is empty under HKCU\\%s", path)
	}
	if len(uid) < 8 || len(uid) > 12 {
		// 不记录原值：非法长度的 PLDK_cachedRoleId 仍可能含可识别的角色 ID 片段。
		return "", fmt.Errorf("gamesetting: PLDK_cachedRoleId length %d is not in 8-12", len(uid))
	}
	for i := 0; i < len(uid); i++ {
		if uid[i] < '0' || uid[i] > '9' {
			return "", fmt.Errorf("gamesetting: PLDK_cachedRoleId contains non-digit characters (len=%d)", len(uid))
		}
	}
	return uid, nil
}

func getDWord(name string) (uint32, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, registryPath, registry.QUERY_VALUE)
	if err != nil {
		return 0, fmt.Errorf("gamesetting: open %q failed: %w", registryPath, err)
	}
	defer k.Close()

	val, _, err := k.GetIntegerValue(name)
	if err != nil {
		return 0, fmt.Errorf("gamesetting: read value %q failed: %w", name, err)
	}
	return uint32(val), nil
}

func setDWord(name string, value uint32) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, registryPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("gamesetting: open %q failed: %w", registryPath, err)
	}
	defer k.Close()

	if err := k.SetDWordValue(name, value); err != nil {
		return fmt.Errorf("gamesetting: write value %q failed: %w", name, err)
	}
	return nil
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

const (
	// Unity FullScreenMode：3 = Windowed，1 = FullScreenWindow。
	screenmanagerModeWindowed   uint32 = 3
	screenmanagerModeFullscreen uint32 = 1

	videoFullScreenOff uint32 = 0
	videoFullScreenOn  uint32 = 1
)

func setRegistryPath(region string) error {
	switch region {
	case regionCN:
		registryPath = registryPathCN
	case regionGlobal:
		registryPath = registryPathGlobal
	default:
		return fmt.Errorf("gamesetting: unknown region %q", region)
	}
	return nil
}

func useResolvedRegistryPath() error {
	region, err := ResolveRegion()
	if err != nil {
		return err
	}
	return setRegistryPath(region)
}

// IsVideoFullScreen 按当前区服读取是否全屏。video_full_screen 为 1 时返回 true。
func IsVideoFullScreen() (bool, error) {
	if err := useResolvedRegistryPath(); err != nil {
		return false, err
	}
	fullScreen, err := GetVideoFullScreen()
	if err != nil {
		return false, err
	}
	return fullScreen == videoFullScreenOn, nil
}

// GetResolvedVideoResolutionWidth 按当前区服读取游戏设置分辨率宽度。
func GetResolvedVideoResolutionWidth() (uint32, error) {
	if err := useResolvedRegistryPath(); err != nil {
		return 0, err
	}
	return GetVideoResolutionWidth()
}

// GetResolvedVideoResolutionHeight 按当前区服读取游戏设置分辨率高度。
func GetResolvedVideoResolutionHeight() (uint32, error) {
	if err := useResolvedRegistryPath(); err != nil {
		return 0, err
	}
	return GetVideoResolutionHeight()
}

// Apply 按 ResolveRegion 选定注册表路径，并写入游戏显示相关项。
// 调用前若游戏未运行，须先 SetRegion；否则无法自动判区。
func Apply(displayType, resolution string) bool {
	region, err := ResolveRegion()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Msg("failed to resolve game region")
		return false
	}
	if err := setRegistryPath(region); err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("region", region).
			Msg("invalid game region")
		return false
	}

	width, height, err := parseResolution(resolution)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("resolution", resolution).
			Msg("invalid resolution")
		return false
	}

	switch displayType {
	case displayTypeWindow:
		return applyWindowed(width, height)
	case displayTypeFullscreen:
		return applyFullscreen(width, height)
	default:
		log.Error().
			Str("component", "gamesetting").
			Str("display_type", displayType).
			Msg("unknown display type")
		return false
	}
}

func applyWindowed(width, height uint32) bool {
	ok := applyResolution(width, height)
	displaySetters := []struct {
		key string
		fn  func(uint32) error
		val uint32
	}{
		{"Screenmanager Fullscreen mode", SetScreenmanagerFullscreenMode, screenmanagerModeWindowed},
		{"video_full_screen", SetVideoFullScreen, videoFullScreenOff},
	}
	return applySetters(displaySetters) && ok
}

func applyFullscreen(width, height uint32) bool {
	ok := applyResolution(width, height)
	displaySetters := []struct {
		key string
		fn  func(uint32) error
		val uint32
	}{
		{"Screenmanager Fullscreen mode", SetScreenmanagerFullscreenMode, screenmanagerModeFullscreen},
		{"video_full_screen", SetVideoFullScreen, videoFullScreenOn},
	}
	return applySetters(displaySetters) && ok
}

func applyResolution(width, height uint32) bool {
	setters := []struct {
		key string
		fn  func(uint32) error
		val uint32
	}{
		{"video_resolution_width", SetVideoResolutionWidth, width},
		{"video_resolution_height", SetVideoResolutionHeight, height},
		{"Screenmanager Resolution Width", SetScreenmanagerResolutionWidth, width},
		{"Screenmanager Resolution Height", SetScreenmanagerResolutionHeight, height},
	}
	return applySetters(setters)
}

func applySetters(setters []struct {
	key string
	fn  func(uint32) error
	val uint32
}) bool {
	ok := true
	for _, item := range setters {
		if err := item.fn(item.val); err != nil {
			log.Error().
				Err(err).
				Str("component", "gamesetting").
				Str("key", item.key).
				Uint32("value", item.val).
				Msg("failed to apply setting")
			ok = false
		}
	}
	return ok
}

func parseResolution(resolution string) (uint32, uint32, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(resolution)), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("gamesetting: resolution %q must be WIDTHxHEIGHT", resolution)
	}

	width, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("gamesetting: parse width from %q: %w", resolution, err)
	}
	height, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("gamesetting: parse height from %q: %w", resolution, err)
	}
	if width == 0 || height == 0 {
		return 0, 0, fmt.Errorf("gamesetting: resolution %q is invalid", resolution)
	}
	return uint32(width), uint32(height), nil
}

// IsAutoHDREnabled 判断终末地是否会实际开启自动 HDR。
// 按应用 *Endfield.exe 的 AutoHDREnable 带用户覆盖位时以按应用为准；
// 否则跟随 DirectXUserGlobalSettings（按应用覆盖全局，见 DirectX 博客「Navigating the redesigned Graphics settings page」）。
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
		globalRaw      string
		globalValue    string
		perAppSeen     bool
		perAppOverride bool
		perAppOn       bool
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
			globalRaw = raw
			globalValue, _ = parseAutoHDRValue(raw)
			continue
		}

		v, _ := parseAutoHDRValue(raw)
		log.Debug().
			Str("component", "gamesetting").
			Str("exe_path", name).
			Str("raw", raw).
			Str("auto_hdr", v).
			Msg("found Endfield.exe in UserGpuPreferences")
		if v == "" {
			continue
		}
		perAppSeen = true
		override, on := parsePerAppAutoHDRFlags(v)
		if override {
			perAppOverride = true
			if on {
				perAppOn = true
			}
		}
	}

	var (
		result bool
		reason string
	)
	if perAppOverride {
		result = perAppOn
		reason = "per_app_override"
	} else {
		result = globalValue == autoHDRGlobalEnabledValue
		reason = "follow_global"
	}
	log.Debug().
		Str("component", "gamesetting").
		Str("global_raw", globalRaw).
		Str("global_value", globalValue).
		Bool("per_app_seen", perAppSeen).
		Bool("per_app_override", perAppOverride).
		Bool("per_app_on", perAppOn).
		Bool("result", result).
		Str("reason", reason).
		Msg("Endfield Auto HDR registry check")
	return result, nil
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

// parsePerAppAutoHDRFlags 解析按应用 AutoHDREnable 位标志：
// 0x1000 表示用户在该应用上手动设置过（覆盖全局），0x1 表示开启。
// 如 2097(0x0831) 跟随全局，6193(0x1831) 手动开启；无法解析时视为跟随全局。
func parsePerAppAutoHDRFlags(value string) (override, on bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return false, false
	}
	return n&autoHDRUserOverrideFlag != 0, n&autoHDREnabledFlag != 0
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
