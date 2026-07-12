package charactercontroller

import (
	"encoding/json"
	"math"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func rotateView(ctx *maa.Context, dx, dy int) {
	cx, cy := 1280/2, 720/2
	override := map[string]any{
		"__CharacterControllerDeltaSwipeAction": map[string]any{
			"begin": maa.Rect{cx, cy, 4, 4},
			"end":   maa.Rect{cx + dx, cy + dy, 4, 4},
			"custom_action_param": map[string]any{
				"dx": dx,
				"dy": dy,
			},
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

type characterControllerRelativeMoveParam struct {
	Dx    int   `json:"dx"`
	Dy    int   `json:"dy"`
	Begin []int `json:"begin"`
}

// on wlroots, dx/dy are compensated by control.WlrootsRelativeMoveScale.
// When "begin" is specified, dx/dy are computed from begin to arg.Box
// (resolved from pipeline "target") instead of the explicit dx/dy fields.
type CharacterControllerRelativeMoveAction struct{}

func (a *CharacterControllerRelativeMoveAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params characterControllerRelativeMoveParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "CharacterController").
			Str("action", "CharacterControllerRelativeMove").
			Msg("failed to parse CustomActionParam")
		return false
	}

	dx := params.Dx
	dy := params.Dy

	if len(params.Begin) >= 2 {
		if arg.RecognitionDetail == nil || !arg.RecognitionDetail.Hit {
			log.Debug().
				Str("component", "CharacterController").
				Str("action", "CharacterControllerRelativeMove").
				Msg("target recognition not hit, skipping relative move")
			return true
		}
		dx = arg.Box.X() - params.Begin[0]
		dy = arg.Box.Y() - params.Begin[1]
	}

	scaledDX := int32(dx)
	scaledDY := int32(dy)
	controlType, _ := control.GetControlType(ctx.GetTasker().GetController())
	if controlType == control.CONTROL_TYPE_WLROOTS {
		scaledDX = int32(math.Round(float64(dx) * control.WlrootsRelativeMoveScale))
		scaledDY = int32(math.Round(float64(dy) * control.WlrootsRelativeMoveScale))
	}
	ctx.GetTasker().GetController().PostRelativeMove(scaledDX, scaledDY).Wait()
	return true
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

func moveToTarget(ctx *maa.Context, arg *maa.CustomActionArg, alignThreshold int, farTargetWidth *int) bool {
	if arg.RecognitionDetail == nil || !arg.RecognitionDetail.Hit {
		log.Debug().Msg("recognition detail missing or not a hit")
		return false
	}

	box := arg.Box
	if farTargetWidth != nil && box.Width() < *farTargetWidth {
		moveAxis(ctx, 200)
		log.Debug().
			Int("width", box.Width()).
			Int("far_target_width", *farTargetWidth).
			Msg("target too far — moving forward")
		return true
	}

	targetCenterX := box.X() + box.Width()/2
	targetCenterY := box.Y() + box.Height()/2
	screenCenterX := 1280 / 2

	offsetX := targetCenterX - screenCenterX

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

var (
	targetNotFoundCounter = 0
)

type CharacterMoveToTargetAction struct{}

func (a *CharacterMoveToTargetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	targetNotFoundCounter = 0
	var params struct {
		AlignThreshold *int `json:"align_threshold"`
		FarTargetWidth *int `json:"far_target_width"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "CharacterController").
			Str("action", "CharacterMoveToTarget").
			Msg("failed to parse CustomActionParam")
		return false
	}
	alignThreshold := 120 // pixels; within this range the target is considered centered horizontally
	if params.AlignThreshold != nil {
		alignThreshold = *params.AlignThreshold
	}
	return moveToTarget(ctx, arg, alignThreshold, params.FarTargetWidth)
}

type CharacterMoveToTargetNotFoundAction struct{}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner = &CharacterControllerYawDeltaAction{}
	_ maa.CustomActionRunner = &CharacterControllerPitchDeltaAction{}
	_ maa.CustomActionRunner = &CharacterControllerForwardAxisAction{}
	_ maa.CustomActionRunner = &CharacterControllerRelativeMoveAction{}
	_ maa.CustomActionRunner = &CharacterMoveToTargetAction{}
	_ maa.CustomActionRunner = &CharacterMoveToTargetNotFoundAction{}
)

func (a *CharacterMoveToTargetNotFoundAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	targetNotFoundCounter++
	if targetNotFoundCounter > 15 {
		log.Warn().
			Int("counter", targetNotFoundCounter).
			Str("component", "CharacterController").
			Str("action", "CharacterMoveToTargetNotFound").
			Msg("target not found for too many times, stopping task")
		targetNotFoundCounter = 0
		return false
	}

	log.Debug().
		Int("counter", targetNotFoundCounter).
		Str("component", "CharacterController").
		Str("action", "CharacterMoveToTargetNotFound").
		Msg("target not found, attempting to adjust view to find target")

	var params struct {
		Delta int `json:"delta"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "CharacterController").
			Str("action", "CharacterMoveToTargetNotFound").
			Msg("failed to parse CustomActionParam")
		return false
	}
	delta := params.Delta % 360
	dx := delta * 2 // mapTracker RotationSpeed默认2
	rotateView(ctx, dx, 0)

	return true
}
