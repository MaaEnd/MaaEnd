//go:build windows

package gamesetting

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows/registry"
)

const (
	registryPathCN     = `Software\Hypergryph\Endfield`
	registryPathGlobal = `Software\Gryphline\Endfield`
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

// Apply 按 pretask 选项写入游戏显示相关注册表项。
func Apply(region, displayType, resolution string) bool {
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
