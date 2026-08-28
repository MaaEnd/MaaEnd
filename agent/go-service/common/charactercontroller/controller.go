package charactercontroller

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	screenW = 1280
	screenH = 720
)

// Accumulated hover-cursor offset since last Alt reset (Win32 path).
// Always restarting from screen center would reverse-delta and bounce the camera.
var cursorDX, cursorDY int

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func rotateView(ctx *maa.Context, dx, dy int) {
	cx, cy := screenW/2, screenH/2
	if absInt(cursorDX) > screenW/4 || absInt(cursorDY) > screenH/4 {
		ctx.RunAction("__CharacterControllerDeltaAltKeyDownAction",
			maa.Rect{0, 0, 0, 0}, "", nil)
		ctx.RunAction("__CharacterControllerDeltaClickCenterAction",
			maa.Rect{0, 0, 0, 0}, "", nil)
		ctx.RunAction("__CharacterControllerDeltaAltKeyUpAction",
			maa.Rect{0, 0, 0, 0}, "", nil)
		cursorDX, cursorDY = 0, 0
	}

	fromX, fromY := cx+cursorDX, cy+cursorDY
	override := map[string]any{
		"__CharacterControllerDeltaSwipeAction": map[string]any{
			"begin": maa.Rect{fromX, fromY, 1, 1},
			"end":   maa.Rect{fromX + dx, fromY + dy, 1, 1},
			// linux resource remaps this node to RelativeMove; keep dx/dy for that path.
			"custom_action_param": map[string]any{
				"dx": dx,
				"dy": dy,
			},
		},
	}
	ctx.RunAction("__CharacterControllerDeltaSwipeAction",
		maa.Rect{0, 0, 0, 0}, "", override)
	cursorDX += dx
	cursorDY += dy
}

type characterControllerRelativeMoveParam struct {
	Dx    int   `json:"dx"`
	Dy    int   `json:"dy"`
	Begin []int `json:"begin"`
}

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

	ctx.GetTasker().GetController().PostRelativeMove(int32(dx), int32(dy)).Wait()
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

// sprintForward taps Shift once to sprint.
func sprintForward(ctx *maa.Context) {
	ctx.RunAction("__CharacterControllerShiftClickAction",
		maa.Rect{0, 0, 0, 0}, "", nil)
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
	dx := delta * 2 // 视角旋转灵敏度：1 度偏航对应 2 像素横向拖动
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
	rememberLastTarget(box, farTargetWidth)

	if farTargetWidth != nil && box.Width() < *farTargetWidth {
		// Keep original single W press, then Shift-sprint three times with gaps.
		moveAxis(ctx, 200)
		for i := 0; i < 3; i++ {
			time.Sleep(300 * time.Millisecond)
			sprintForward(ctx)
		}
		log.Debug().
			Int("width", box.Width()).
			Int("far_target_width", *farTargetWidth).
			Msg("target too far — walk, then shift-sprint x3")
		return true
	}

	targetCenterX := box.X() + box.Width()/2
	targetCenterY := box.Y() + box.Height()/2
	screenCenterX := screenW / 2

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

type lastTargetBox struct {
	x, y, w, h int
}

var (
	targetNotFoundCounter = 0
	lastTarget            *lastTargetBox
	lastFarTargetWidth    *int
)

func rememberLastTarget(box maa.Rect, farTargetWidth *int) {
	lastTarget = &lastTargetBox{
		x: box.X(),
		y: box.Y(),
		w: box.Width(),
		h: box.Height(),
	}
	if farTargetWidth == nil {
		lastFarTargetWidth = nil
		return
	}
	w := *farTargetWidth
	lastFarTargetWidth = &w
}

func clearLastTarget() {
	lastTarget = nil
	lastFarTargetWidth = nil
}

// signedDeltaFromLastBox picks NotFound yaw sign from the last MoveToTarget hit.
// Returns +absDelta (turn right) when there is no usable last box.
// A box is usable when farW is nil, or when box width >= *farW (not too far).
// Left-of-center usable boxes return -absDelta; right-of-center return +absDelta.
func signedDeltaFromLastBox(box *lastTargetBox, farW *int, absDelta int) (signedDelta int, side string, usedLast bool) {
	absDelta = absInt(absDelta)
	if box == nil {
		return absDelta, "right", false
	}
	if farW != nil && box.w < *farW {
		return absDelta, "right", false
	}
	centerX := box.x + box.w/2
	if centerX < screenW/2 {
		return -absDelta, "left", true
	}
	return absDelta, "right", true
}

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
	if targetNotFoundCounter > 30 {
		log.Warn().
			Int("counter", targetNotFoundCounter).
			Str("component", "CharacterController").
			Str("action", "CharacterMoveToTargetNotFound").
			Msg("target not found for too many times, stopping task")
		targetNotFoundCounter = 0
		clearLastTarget()
		return false
	}

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

	absDelta := absInt(params.Delta % 360)
	signedDelta, side, usedLast := signedDeltaFromLastBox(lastTarget, lastFarTargetWidth, absDelta)
	clearLastTarget()

	evt := log.Debug().
		Int("counter", targetNotFoundCounter).
		Str("component", "CharacterController").
		Str("action", "CharacterMoveToTargetNotFound").
		Str("side", side).
		Int("signed_delta", signedDelta).
		Bool("used_last", usedLast)
	if usedLast {
		evt.Msg("target not found, turning toward last recognition side")
	} else {
		evt.Str("fallback", "right").
			Msg("target not found, attempting to adjust view to find target")
	}

	dx := signedDelta * 2 // 视角旋转灵敏度：1 度偏航对应 2 像素横向拖动
	rotateView(ctx, dx, 0)

	return true
}
