package gamesetting

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

var ErrUnsupported = errors.New("gamesetting: only supported on windows and linux")

const (
	registryPathCN     = `Software\Hypergryph\Endfield`
	registryPathGlobal = `Software\Gryphline\Endfield`
)

var registryPath = registryPathCN

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
	valuePrefixPLDKCachedRoleId = `PLDK_cachedRoleId_h`
)

const (
	// Unity FullScreenMode：3 = Windowed，1 = FullScreenWindow。
	screenmanagerModeWindowed   uint32 = 3
	screenmanagerModeFullscreen uint32 = 1

	videoFullScreenOff uint32 = 0
	videoFullScreenOn  uint32 = 1
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
},
) bool {
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

// validateCachedUID 校验 PLDK_cachedRoleId：8–12 位纯数字
// 不记录原值：非法长度的 PLDK_cachedRoleId 仍可能含可识别的角色 ID 片段
func validateCachedUID(uid string) error {
	if uid == "" {
		return fmt.Errorf("gamesetting: PLDK_cachedRoleId is empty under %s", registryPath)
	}
	if len(uid) < 8 || len(uid) > 12 {
		return fmt.Errorf("gamesetting: PLDK_cachedRoleId length %d is not in 8-12", len(uid))
	}
	for i := 0; i < len(uid); i++ {
		if uid[i] < '0' || uid[i] > '9' {
			return fmt.Errorf("gamesetting: PLDK_cachedRoleId contains non-digit characters (len=%d)", len(uid))
		}
	}
	return nil
}

// winePrefix Linux 使用
var winePrefix string

// SetLauncherDir Linux 端调用
func SetLauncherDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "user.reg")); err != nil {
		return fmt.Errorf("gamesetting: %q is not a wine prefix: user.reg not found", dir)
	}
	winePrefix = dir
	return nil
}

var wineBin string

// SetWineBin Linux 端调用：wineprefix 所属环境自带的 wine 绝对路径
// 系统 wine 可以用但版本与 prefix 不同的时候会弹出 wineboot
func SetWineBin(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("gamesetting: wine path is empty")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("gamesetting: wine path %q must be absolute", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("gamesetting: wine path %q: %w", path, err)
	}
	if info.IsDir() || info.Mode()&0111 == 0 {
		return fmt.Errorf("gamesetting: wine path %q is not an executable file", path)
	}
	wineBin = path
	return nil
}
