package autoessence

import (
	"encoding/json"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const actionApplyLocationEngraveOverride = "AutoEssenceApplyLocationEngraveOverrideAction"

type applyLocationEngraveParam struct {
	SourceNode string `json:"source_node,omitempty"`
}

// ApplyLocationEngraveOverrideAction reads AutoEssenceSetupLocation attach in location mode
// and overrides pre-engrave OCR nodes with the selected base/bonus attribute texts.
type ApplyLocationEngraveOverrideAction struct{}

var _ maa.CustomActionRunner = &ApplyLocationEngraveOverrideAction{}

func (a *ApplyLocationEngraveOverrideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		log.Error().Str("component", componentName).Msg("apply location engrave override: context is nil")
		return false
	}
	if arg == nil {
		log.Error().Str("component", componentName).Msg("apply location engrave override: arg is nil")
		return false
	}

	sourceNode := nodeSetupLocation
	if strings.TrimSpace(arg.CustomActionParam) != "" {
		var param applyLocationEngraveParam
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
			log.Error().
				Err(err).
				Str("component", componentName).
				Str("custom_action_param", arg.CustomActionParam).
				Msg("failed to parse apply location engrave override param")
			return false
		}
		if trimmed := strings.TrimSpace(param.SourceNode); trimmed != "" {
			sourceNode = trimmed
		}
	}

	attach, attachRaw, err := getSetupLocationAttach(ctx, sourceNode)
	if err != nil {
		return false
	}
	if !attach.isLocationMode() {
		log.Info().
			Str("component", componentName).
			Str("menu_mode", attach.MenuMode).
			Msg("skip engrave override because menu mode is not location")
		return true
	}
	if err := validateLocationModeAttach(attachRaw, attach); err != nil {
		return stopTaskWithInvalidOptions(ctx, err)
	}
	if err := attach.validateForEngraveOverride(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("location_id", attach.LocationID).
			Msg("location mode attach is incomplete for engrave override")
		return stopTaskWithInvalidOptions(ctx, err)
	}

	catalog, err := loadSkillCatalog()
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to load skill catalog")
		return false
	}

	selection, err := buildEngraveSelection(catalog, attach)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("location_id", attach.LocationID).
			Msg("failed to resolve engrave selection from attach")
		return false
	}

	override := buildLocationEngraveOverride(selection)
	if err := ctx.OverridePipeline(override); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Interface("override", override).
			Msg("failed to override pre-engrave OCR nodes")
		return false
	}

	log.Info().
		Str("component", componentName).
		Str("location_id", attach.LocationID).
		Int("slot1_1_id", attach.Slot1_1ID).
		Int("slot1_2_id", attach.Slot1_2ID).
		Int("slot1_3_id", attach.Slot1_3ID).
		Int("slot2_id", attach.Slot2ID).
		Strs("condition1_expected", combinedBaseExpectedPatterns([3]skillEntry{selection.base1, selection.base2, selection.base3})).
		Strs("condition2_expected", skillExpectedTexts(selection.bonus)).
		Msg("applied location mode pre-engrave OCR overrides")

	return true
}
