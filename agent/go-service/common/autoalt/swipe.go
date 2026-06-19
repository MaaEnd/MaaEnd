package autoalt

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const autoAltSwipeSubNode = "__AutoAltSwipeMouseSwipeAction"

type AutoAltSwipeAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &AutoAltSwipeAction{}

func rectToSlice(box maa.Rect) []int {
	return []int{box[0], box[1], box[2], box[3]}
}

func (a *AutoAltSwipeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	box := rectToSlice(arg.Box)
	swipeOverride := map[string]any{
		"begin": box,
		"end":   box,
	}
	if param := arg.CustomActionParam; param != "" {
		var customParam map[string]any
		if err := json.Unmarshal([]byte(param), &customParam); err != nil {
			log.Error().
				Err(err).
				Str("component", "AutoAltSwipeAction").
				Str("custom_action_param", param).
				Msg("failed to parse custom action param")
			return false
		}
		for k, v := range customParam {
			swipeOverride[k] = v
		}
	}

	if _, err := ctx.RunAction("__AutoAltClickAltKeyDownAction",
		maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoAltSwipeAction").
			Msg("failed to run __AutoAltClickAltKeyDownAction")
		return false
	}

	_, swipeErr := ctx.RunAction(autoAltSwipeSubNode,
		arg.Box, "", map[string]any{
			autoAltSwipeSubNode: swipeOverride,
		})
	if swipeErr != nil {
		log.Error().
			Err(swipeErr).
			Str("component", "AutoAltSwipeAction").
			Interface("box", arg.Box).
			Interface("swipe_override", swipeOverride).
			Msg("failed to run __AutoAltSwipeMouseSwipeAction")
	}

	if _, err := ctx.RunAction("__AutoAltClickAltKeyUpAction",
		maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoAltSwipeAction").
			Msg("failed to run __AutoAltClickAltKeyUpAction")
	}

	return swipeErr == nil
}
