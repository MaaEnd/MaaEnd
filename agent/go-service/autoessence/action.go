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

// ApplyLocationEngraveOverrideAction reads AutoEssenceLocationOptionsCheck attach in location mode
// and overrides condition 1 OCR with the combined three base attribute patterns.
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

	sourceNode := nodeLocationOptionsCheck
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

	attach, attachRaw, err := getLocationOptionsAttach(ctx, sourceNode)
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

	catalog, err := loadSkillCatalog()
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to load skill catalog")
		return false
	}

	selection, err := buildBaseEngraveSelection(catalog, attachRaw)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("location_id", attach.LocationID).
			Msg("failed to resolve base engrave selection from attach")
		return false
	}

	override := buildLocationEngraveOverride(selection)
	if err := ctx.OverridePipeline(override); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Interface("override", override).
			Msg("failed to override condition 1 OCR node")
		return false
	}

	baseIDs, _ := collectSelectedBaseAttributeIDs(attachRaw)
	log.Info().
		Str("component", componentName).
		Str("location_id", attach.LocationID).
		Ints("base_attribute_ids", baseIDs).
		Strs("condition1_expected", combinedBaseExpectedPatterns([3]skillEntry{selection.base1, selection.base2, selection.base3})).
		Msg("applied location mode condition 1 OCR override")

	return true
}
