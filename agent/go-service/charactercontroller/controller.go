package charactercontroller

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type CharacterControllerYawDeltaAction struct{}

func (a *CharacterControllerYawDeltaAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Delta int `json:"delta"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}
	delta := params.Delta
	delta = delta % 360
	dx := delta * 2 // mapTracker RotationSpeed默认2
	cx, cy := 1280/2, 720/2
	{
		override := map[string]any{
			"__CharacterControllerDeltaSwipeAction": map[string]any{
				"begin": maa.Rect{cx, cy, 4, 4},
				"end":   maa.Rect{cx + dx, cy, 4, 4},
			},
		}
		ctx.RunAction("__CharacterControllerDeltaSwipeAction",
			maa.Rect{0, 0, 0, 0}, "", override)
		ctx.RunAction("__CharacterControllerDeltaAltKeyDownAction",
			maa.Rect{0, 0, 0, 0}, "", nil)
		ctx.RunAction("__CharacterControllerDeltaClickCenterAction",
			maa.Rect{0, 0, 0, 0}, "", nil)
		ctx.RunAction("__CharacterControllerDeltaAltKeyUpAction",
			maa.Rect{0, 0, 0, 0}, "", nil)
	}
	return true
}

type CharacterControllerPitchDeltaAction struct{}

func (a *CharacterControllerPitchDeltaAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Delta int `json:"delta"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}
	delta := params.Delta
	delta = delta % 360
	dx := delta * 2
	cx, cy := 1280/2, 720/2
	{
		override := map[string]any{
			"__CharacterControllerDeltaSwipeAction": map[string]any{
				"begin": maa.Rect{cx, cy, 4, 4},
				"end":   maa.Rect{cx, cy + dx, 4, 4},
			},
		}
		ctx.RunAction("__CharacterControllerDeltaSwipeAction",
			maa.Rect{0, 0, 0, 0}, "", override)
		ctx.RunAction("__CharacterControllerDeltaAltKeyDownAction",
			maa.Rect{0, 0, 0, 0}, "", nil)
		ctx.RunAction("__CharacterControllerDeltaClickCenterAction",
			maa.Rect{0, 0, 0, 0}, "", nil)
		ctx.RunAction("__CharacterControllerDeltaAltKeyUpAction",
			maa.Rect{0, 0, 0, 0}, "", nil)
	}
	return true
}

type CharacterControllerForwardAxisAction struct{}

func (a *CharacterControllerForwardAxisAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Axis int `json:"axis"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}
	axis := params.Axis
	if axis > 0 {
		override := map[string]any{
			"__CharacterControllerAxisLongPressForwardAction": map[string]any{
				"duration": 100 * axis,
			},
		}
		ctx.RunAction("__CharacterControllerAxisLongPressForwardAction",
			maa.Rect{0, 0, 0, 0}, "", override)
	} else if axis < 0 {
		override := map[string]any{
			"__CharacterControllerAxisLongPressBackwardAction": map[string]any{
				"duration": 100 * axis,
			},
		}
		ctx.RunAction("__CharacterControllerAxisLongPressBackwardAction",
			maa.Rect{0, 0, 0, 0}, "", override)
	}
	return true
}
