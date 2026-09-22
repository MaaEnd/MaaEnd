//go:build windows

package gamesetting

import (
	"errors"
	"fmt"
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
)

var registryPath = registryPathCN

var ErrUnsupported = errors.New("gamesetting: only supported on windows")

const (
	valuePrefixScreenmanagerFullscreenMode         = `Screenmanager Fullscreen mode_h`
	valuePrefixScreenmanagerResolutionHeight       = `Screenmanager Resolution Height_h`
	valuePrefixScreenmanagerResolutionWidth        = `Screenmanager Resolution Width_h`
	valuePrefixScreenmanagerResolutionWindowHeight = `Screenmanager Resolution Window Height_h`
	valuePrefixScreenmanagerResolutionWindowWidth  = `Screenmanager Resolution Window Width_h`
	valuePrefixScreenmanagerWindowPositionX        = `Screenmanager Window Position X_h`
	valuePrefixScreenmanagerWindowPositionY        = `Screenmanager Window Position Y_h`
	valuePrefixLanguageTextChange                  = `language_text_change_h`
	valuePrefixVideoCustomQuality                  = `video_custom_quality_h`
	valuePrefixVideoFrameRate8                     = `video_frame_rate_8_h`
	valuePrefixVideoFullScreen                     = `video_full_screen_h`
	valuePrefixVideoQualityAnisoLevel1             = `video_quality_anisoLevel_1_h`
	valuePrefixVideoQualityContactShadow           = `video_quality_contactshadow_h`
	valuePrefixVideoQualityDLSSMode1               = `video_quality_dlss_mode_1_h`
	valuePrefixVideoQualityMain                    = `video_quality_main_h`
	valuePrefixVideoQualityReflex                  = `video_quality_reflex_h`
	valuePrefixVideoQualitySharpness               = `video_quality_sharpness_h`
	valuePrefixVideoQualityUpscaler                = `video_quality_upscaler_h`
	valuePrefixVideoResolution                     = `video_resolution_h`
	valuePrefixVideoResolutionHeight               = `video_resolution_height_h`
	valuePrefixVideoResolutionWidth                = `video_resolution_width_h`
	valuePrefixVideoTextureQuality1                = `video_texture_quality_1_h`
	// PLDK_cachedRoleId 是游戏角色 UID；不要与 u8sdk_cached_uid 混淆。
	valuePrefixPLDKCachedRoleId = `PLDK_cachedRoleId`
)

func GetScreenmanagerFullscreenMode() (uint32, error) {
	return getDWord(valuePrefixScreenmanagerFullscreenMode)
}

func SetScreenmanagerFullscreenMode(value uint32) error {
	return setDWord(valuePrefixScreenmanagerFullscreenMode, value)
}

func GetScreenmanagerResolutionHeight() (uint32, error) {
	return getDWord(valuePrefixScreenmanagerResolutionHeight)
}

func SetScreenmanagerResolutionHeight(value uint32) error {
	return setDWord(valuePrefixScreenmanagerResolutionHeight, value)
}

func GetScreenmanagerResolutionWidth() (uint32, error) {
	return getDWord(valuePrefixScreenmanagerResolutionWidth)
}

func SetScreenmanagerResolutionWidth(value uint32) error {
	return setDWord(valuePrefixScreenmanagerResolutionWidth, value)
}

func GetScreenmanagerResolutionWindowHeight() (uint32, error) {
	return getDWord(valuePrefixScreenmanagerResolutionWindowHeight)
}

func SetScreenmanagerResolutionWindowHeight(value uint32) error {
	return setDWord(valuePrefixScreenmanagerResolutionWindowHeight, value)
}

func GetScreenmanagerResolutionWindowWidth() (uint32, error) {
	return getDWord(valuePrefixScreenmanagerResolutionWindowWidth)
}

func SetScreenmanagerResolutionWindowWidth(value uint32) error {
	return setDWord(valuePrefixScreenmanagerResolutionWindowWidth, value)
}

func GetScreenmanagerWindowPositionX() (uint32, error) {
	return getDWord(valuePrefixScreenmanagerWindowPositionX)
}

func SetScreenmanagerWindowPositionX(value uint32) error {
	return setDWord(valuePrefixScreenmanagerWindowPositionX, value)
}

func GetScreenmanagerWindowPositionY() (uint32, error) {
	return getDWord(valuePrefixScreenmanagerWindowPositionY)
}

func SetScreenmanagerWindowPositionY(value uint32) error {
	return setDWord(valuePrefixScreenmanagerWindowPositionY, value)
}

// GetLanguageTextChange reads Endfield's text language.
func GetLanguageTextChange() (uint32, error) {
	return getDWord(valuePrefixLanguageTextChange)
}

// SetLanguageTextChange writes Endfield's text language.
// 语音语言存放在独立的注册表项，不受此项影响。
func SetLanguageTextChange(value uint32) error {
	return setDWord(valuePrefixLanguageTextChange, value)
}

func GetVideoCustomQuality() (uint32, error) {
	return getDWord(valuePrefixVideoCustomQuality)
}

func SetVideoCustomQuality(value uint32) error {
	return setDWord(valuePrefixVideoCustomQuality, value)
}

func GetVideoFrameRate8() (uint32, error) {
	return getDWord(valuePrefixVideoFrameRate8)
}

func SetVideoFrameRate8(value uint32) error {
	return setDWord(valuePrefixVideoFrameRate8, value)
}

func GetVideoFullScreen() (uint32, error) {
	return getDWord(valuePrefixVideoFullScreen)
}

func SetVideoFullScreen(value uint32) error {
	return setDWord(valuePrefixVideoFullScreen, value)
}

func GetVideoQualityAnisoLevel1() (uint32, error) {
	return getDWord(valuePrefixVideoQualityAnisoLevel1)
}

func SetVideoQualityAnisoLevel1(value uint32) error {
	return setDWord(valuePrefixVideoQualityAnisoLevel1, value)
}

func GetVideoQualityContactShadow() (uint32, error) {
	return getDWord(valuePrefixVideoQualityContactShadow)
}

func SetVideoQualityContactShadow(value uint32) error {
	return setDWord(valuePrefixVideoQualityContactShadow, value)
}

func GetVideoQualityDLSSMode1() (uint32, error) {
	return getDWord(valuePrefixVideoQualityDLSSMode1)
}

func SetVideoQualityDLSSMode1(value uint32) error {
	return setDWord(valuePrefixVideoQualityDLSSMode1, value)
}

func GetVideoQualityMain() (uint32, error) {
	return getDWord(valuePrefixVideoQualityMain)
}

func SetVideoQualityMain(value uint32) error {
	return setDWord(valuePrefixVideoQualityMain, value)
}

func GetVideoQualityReflex() (uint32, error) {
	return getDWord(valuePrefixVideoQualityReflex)
}

func SetVideoQualityReflex(value uint32) error {
	return setDWord(valuePrefixVideoQualityReflex, value)
}

func GetVideoQualitySharpness() (uint32, error) {
	return getDWord(valuePrefixVideoQualitySharpness)
}

func SetVideoQualitySharpness(value uint32) error {
	return setDWord(valuePrefixVideoQualitySharpness, value)
}

func GetVideoQualityUpscaler() (uint32, error) {
	return getDWord(valuePrefixVideoQualityUpscaler)
}

func SetVideoQualityUpscaler(value uint32) error {
	return setDWord(valuePrefixVideoQualityUpscaler, value)
}

func GetVideoResolution() (uint32, error) {
	return getDWord(valuePrefixVideoResolution)
}

func SetVideoResolution(value uint32) error {
	return setDWord(valuePrefixVideoResolution, value)
}

func GetVideoResolutionHeight() (uint32, error) {
	return getDWord(valuePrefixVideoResolutionHeight)
}

func SetVideoResolutionHeight(value uint32) error {
	return setDWord(valuePrefixVideoResolutionHeight, value)
}

func GetVideoResolutionWidth() (uint32, error) {
	return getDWord(valuePrefixVideoResolutionWidth)
}

func SetVideoResolutionWidth(value uint32) error {
	return setDWord(valuePrefixVideoResolutionWidth, value)
}

func GetVideoTextureQuality1() (uint32, error) {
	return getDWord(valuePrefixVideoTextureQuality1)
}

func SetVideoTextureQuality1(value uint32) error {
	return setDWord(valuePrefixVideoTextureQuality1, value)
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
