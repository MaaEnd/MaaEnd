package autoecofarm

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type autoEcoFarmOverrideTargetTemplateParam struct {
	Template string `json:"template"`
}

type autoEcoFarmOverrideTargetTemplate struct{}

// overrideTargetTemplatePath updates target-recognition template paths for AutoEcoFarm swipe-to-target flow.
func overrideTargetTemplatePath(ctx *maa.Context, targetTemplatePath string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	trimmedPath := strings.TrimSpace(targetTemplatePath)
	if trimmedPath == "" {
		return fmt.Errorf("target template path is empty")
	}

	return ctx.OverridePipeline(map[string]any{
		"_AutoEcoFarmTargeRecognitionFullScreen": map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"template": []string{trimmedPath},
				},
			},
		},
		"_AutoEcoFarmTargeInCenter": map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"template": []string{trimmedPath},
				},
			},
		},
		"_AutoEcoFarmTargeInBack": map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"template": []string{trimmedPath},
				},
			},
		},
	})
}

// Run applies template to AutoEcoFarm target-recognition nodes.
func (a *autoEcoFarmOverrideTargetTemplate) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", "AutoEcoFarm").
			Msg("override target template: nil arg")
		return false
	}

	var params autoEcoFarmOverrideTargetTemplateParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoEcoFarm").
			Str("param", arg.CustomActionParam).
			Msg("override target template: parse param failed")
		return false
	}

	if err := overrideTargetTemplatePath(ctx, params.Template); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoEcoFarm").
			Str("template", params.Template).
			Msg("override target template: apply pipeline override failed")
		return false
	}

	log.Debug().
		Str("component", "AutoEcoFarm").
		Str("template", strings.TrimSpace(params.Template)).
		Msg("override target template: pipeline override applied")
	return true
}
