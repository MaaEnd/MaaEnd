package autoessence

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const actionLocationOptionsCheck = "AutoEssenceLocationOptionsCheckAction"

// LocationOptionsCheckAction validates task option attach on AutoEssenceLocationOptionsCheck.
// Non-location modes skip validation.
type LocationOptionsCheckAction struct{}

var _ maa.CustomActionRunner = &LocationOptionsCheckAction{}

func (a *LocationOptionsCheckAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		log.Error().Str("component", componentName).Msg("location options check: context is nil")
		return false
	}

	attach, attachRaw, err := getLocationOptionsAttach(ctx, nodeLocationOptionsCheck)
	if err != nil {
		return false
	}
	if !attach.isLocationMode() {
		log.Info().
			Str("component", componentName).
			Str("menu_mode", attach.MenuMode).
			Msg("skip location options check because menu mode is not location")
		return true
	}

	if err := validateLocationOptionsAttach(attachRaw, attach); err != nil {
		return stopTaskWithInvalidOptions(ctx, err)
	}

	log.Info().
		Str("component", componentName).
		Str("location_id", attach.LocationID).
		Int("slot2_id", attach.Slot2ID).
		Msg("location mode options check passed")

	return true
}
