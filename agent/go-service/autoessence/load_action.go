package autoessence

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// PrepareInventoryAction chooses between reuse and the existing read-only traversal.
type PrepareInventoryAction struct{}

func (*PrepareInventoryAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	raw, err := ctx.GetNodeJSON(arg.CurrentTaskName)
	if err != nil {
		return false
	}
	var node struct {
		Attach struct {
			ForceRefresh bool `json:"force_refresh"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return false
	}
	if !node.Attach.ForceRefresh {
		if err := loadAccountStock(ctx, arg.TaskID); err == nil {
			log.Info().Str("component", componentInventory).Msg("reusing verified account inventory")
			return ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "AutoEssenceTargetPlan"}}) == nil
		} else {
			log.Info().Err(err).Str("component", componentInventory).Msg("account inventory requires a warehouse scan")
		}
	}
	return ctx.OverridePipeline(map[string]any{
		"EssenceFilterEntry":          map[string]any{"enabled": false},
		"EssenceFilterInventoryEntry": map[string]any{"enabled": true},
		arg.CurrentTaskName:           map[string]any{"next": []string{"[JumpBack]AutoEssenceRunInventoryFilter", "AutoEssenceTargetLoadInventory"}},
	}) == nil
}

type LoadInventoryAction struct{}

func (*LoadInventoryAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if err := loadAccountStock(ctx, arg.TaskID); err != nil {
		log.Error().Err(err).Str("component", componentInventory).Msg("failed to load account inventory")
		return false
	}
	return true
}
