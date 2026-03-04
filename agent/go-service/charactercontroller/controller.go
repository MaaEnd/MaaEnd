package charactercontroller

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func rotateView(ctx *maa.Context, dx, dy int) {
	cx, cy := 1280/2, 720/2
	override := map[string]any{
		"__CharacterControllerDeltaSwipeAction": map[string]any{
			"begin": maa.Rect{cx, cy, 4, 4},
			"end":   maa.Rect{cx + dx, cy + dy, 4, 4},
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

func moveAxis(ctx *maa.Context, duration int) {
	if duration > 0 {
		override := map[string]any{
			"__CharacterControllerAxisLongPressForwardAction": map[string]any{
				"duration": duration,
			},
		}
		ctx.RunAction("__CharacterControllerAxisLongPressForwardAction",
			maa.Rect{0, 0, 0, 0}, "", override)
	} else if duration < 0 {
		override := map[string]any{
			"__CharacterControllerAxisLongPressBackwardAction": map[string]any{
				"duration": -duration,
			},
		}
		ctx.RunAction("__CharacterControllerAxisLongPressBackwardAction",
			maa.Rect{0, 0, 0, 0}, "", override)
	}
}

type CharacterControllerYawDeltaAction struct{}

func (a *CharacterControllerYawDeltaAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Delta int `json:"delta"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}
	delta := params.Delta % 360
	dx := delta * 2 // mapTracker RotationSpeed默认2
	rotateView(ctx, dx, 0)
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
	delta := params.Delta % 360
	dy := delta * 2
	rotateView(ctx, 0, dy)
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
	moveAxis(ctx, 100*params.Axis)
	return true
}

func moveToTargetNeuralNetworkDetect(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg.RecognitionDetail == nil || !arg.RecognitionDetail.Hit {
		// Target not visible — rotate right to search for it.
		log.Debug().Msg("target not detected, rotating to search")
		return true
	}

	box := arg.Box
	targetCenterX := box.X() + box.Width()/2
	targetCenterY := box.Y() + box.Height()/2
	screenCenterX := 1280 / 2

	offsetX := targetCenterX - screenCenterX

	const alignThreshold = 120 // pixels; within this range the target is considered centered horizontally
	const lowerThreshold = 480 // pixels; below this Y the target is considered already passed

	switch {
	case offsetX < -alignThreshold:
		// Target is to the left — turn left.
		dx := offsetX / 3
		rotateView(ctx, dx, 0)
		log.Debug().Int("offsetX", offsetX).Int("dx", dx).Msg("turning left toward target")

	case offsetX > alignThreshold:
		// Target is to the right — turn right.
		dx := offsetX / 3
		rotateView(ctx, dx, 0)
		log.Debug().Int("offsetX", offsetX).Int("dx", dx).Msg("turning right toward target")

	case targetCenterY > lowerThreshold:
		// Target is centered but in the lower half — already walked past, step backward.
		moveAxis(ctx, -200)
		log.Debug().Int("targetCenterY", targetCenterY).Msg("target behind — stepping backward")

	default:
		// Target is centered and in the upper half — step forward.
		moveAxis(ctx, 200)
		log.Debug().Int("offsetX", offsetX).Int("targetCenterY", targetCenterY).Msg("moving forward toward target")
	}

	return true
}

type CharacterMoveToTargetAction struct{}

func (a *CharacterMoveToTargetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Recognition string `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}
	recognition := params.Recognition
	if recognition == "NeuralNetworkDetect" {
		return moveToTargetNeuralNetworkDetect(ctx, arg)
	}
	log.Warn().Str("recognition", recognition).Msg("Unsupported recognition method")
	return false
}
