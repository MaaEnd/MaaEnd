//go:build !windows

package gamesetting

import (
	"errors"

	"github.com/rs/zerolog/log"
)

var ErrUnsupported = errors.New("gamesetting: only supported on windows")

func GetScreenmanagerFullscreenMode() (uint32, error) {
	return 0, ErrUnsupported
}

func SetScreenmanagerFullscreenMode(_ uint32) error {
	return ErrUnsupported
}

func GetScreenmanagerResolutionHeight() (uint32, error) {
	return 0, ErrUnsupported
}

func SetScreenmanagerResolutionHeight(_ uint32) error {
	return ErrUnsupported
}

func GetScreenmanagerResolutionWidth() (uint32, error) {
	return 0, ErrUnsupported
}

func SetScreenmanagerResolutionWidth(_ uint32) error {
	return ErrUnsupported
}

func GetScreenmanagerResolutionWindowHeight() (uint32, error) {
	return 0, ErrUnsupported
}

func SetScreenmanagerResolutionWindowHeight(_ uint32) error {
	return ErrUnsupported
}

func GetScreenmanagerResolutionWindowWidth() (uint32, error) {
	return 0, ErrUnsupported
}

func SetScreenmanagerResolutionWindowWidth(_ uint32) error {
	return ErrUnsupported
}

func GetScreenmanagerWindowPositionX() (uint32, error) {
	return 0, ErrUnsupported
}

func SetScreenmanagerWindowPositionX(_ uint32) error {
	return ErrUnsupported
}

func GetScreenmanagerWindowPositionY() (uint32, error) {
	return 0, ErrUnsupported
}

func SetScreenmanagerWindowPositionY(_ uint32) error {
	return ErrUnsupported
}

func GetVideoCustomQuality() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoCustomQuality(_ uint32) error {
	return ErrUnsupported
}

func GetVideoFrameRate8() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoFrameRate8(_ uint32) error {
	return ErrUnsupported
}

func GetVideoFullScreen() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoFullScreen(_ uint32) error {
	return ErrUnsupported
}

func GetVideoQualityAnisoLevel1() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoQualityAnisoLevel1(_ uint32) error {
	return ErrUnsupported
}

func GetVideoQualityContactShadow() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoQualityContactShadow(_ uint32) error {
	return ErrUnsupported
}

func GetVideoQualityDLSSMode1() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoQualityDLSSMode1(_ uint32) error {
	return ErrUnsupported
}

func GetVideoQualityMain() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoQualityMain(_ uint32) error {
	return ErrUnsupported
}

func GetVideoQualityReflex() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoQualityReflex(_ uint32) error {
	return ErrUnsupported
}

func GetVideoQualitySharpness() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoQualitySharpness(_ uint32) error {
	return ErrUnsupported
}

func GetVideoQualityUpscaler() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoQualityUpscaler(_ uint32) error {
	return ErrUnsupported
}

func GetVideoResolution() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoResolution(_ uint32) error {
	return ErrUnsupported
}

func GetVideoResolutionHeight() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoResolutionHeight(_ uint32) error {
	return ErrUnsupported
}

func GetVideoResolutionWidth() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoResolutionWidth(_ uint32) error {
	return ErrUnsupported
}

func GetVideoTextureQuality1() (uint32, error) {
	return 0, ErrUnsupported
}

func SetVideoTextureQuality1(_ uint32) error {
	return ErrUnsupported
}

// Apply 在非 Windows 平台不可用。
func Apply(region, displayType, resolution string) bool {
	log.Error().
		Str("component", "gamesetting").
		Str("region", region).
		Str("display_type", displayType).
		Str("resolution", resolution).
		Msg("apply is only supported on windows")
	return false
}
