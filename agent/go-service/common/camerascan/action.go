package camerascan

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName           = "CameraScanAction"
	cameraSwipeNode         = "__CameraScanActionSwipe"
	cameraSwipeStartX       = 520
	cameraSwipeStartY       = 360
	screenCenterX           = 640
	screenCenterY           = 360
	maxPitchStepPixels      = 359
	defaultYawStepPixels    = 240
	defaultPitchStepPixels  = 120
	defaultFallbackYawSteps = 12
)

type cameraScanParam struct {
	WaitNodes        []string `json:"wait_nodes"`
	AimTarget        bool     `json:"aim_target,omitempty"`
	YawStepPixels    int      `json:"yaw_step_px,omitempty"`
	PitchStepPixels  int      `json:"pitch_step_px,omitempty"`
	FallbackYawSteps int      `json:"fallback_yaw_steps,omitempty"`
}

// CameraScanAction scans the camera view and recognizes target Pipeline nodes after every movement.
type CameraScanAction struct{}

var _ maa.CustomActionRunner = &CameraScanAction{}

func (a *CameraScanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, ok := parseParam(arg.CustomActionParam)
	if !ok {
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	if detail := recognizeWaitNodes(ctx, ctrl, param.WaitNodes, "initial", 0); detail != nil {
		return finishHit(ctx, detail, param.AimTarget)
	}

	for index, step := range buildCameraScanPath(param.FallbackYawSteps) {
		if ctx.GetTasker().Stopping() {
			return false
		}
		if !runCameraSwipe(ctx, step, param) {
			return false
		}
		if ctx.GetTasker().Stopping() {
			return false
		}
		if detail := recognizeWaitNodes(ctx, ctrl, param.WaitNodes, step.phase, index+1); detail != nil {
			return finishHit(ctx, detail, param.AimTarget)
		}
	}

	log.Info().
		Str("component", componentName).
		Msg("target not found after camera scan")
	return false
}

func parseParam(raw string) (cameraScanParam, bool) {
	var param cameraScanParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to parse params")
		return cameraScanParam{}, false
	}
	if len(param.WaitNodes) == 0 {
		log.Error().
			Str("component", componentName).
			Msg("wait_nodes is required")
		return cameraScanParam{}, false
	}

	if param.YawStepPixels == 0 {
		param.YawStepPixels = defaultYawStepPixels
	}
	if param.PitchStepPixels == 0 {
		param.PitchStepPixels = defaultPitchStepPixels
	}
	if param.FallbackYawSteps == 0 {
		param.FallbackYawSteps = defaultFallbackYawSteps
	}
	if param.YawStepPixels < 1 ||
		param.YawStepPixels > cameraSwipeStartX ||
		param.PitchStepPixels < 1 ||
		param.PitchStepPixels > maxPitchStepPixels ||
		param.FallbackYawSteps < 4 ||
		param.FallbackYawSteps > 72 {
		log.Error().
			Str("component", componentName).
			Int("yaw_step_px", param.YawStepPixels).
			Int("pitch_step_px", param.PitchStepPixels).
			Int("fallback_yaw_steps", param.FallbackYawSteps).
			Msg("camera scan step values are out of range")
		return cameraScanParam{}, false
	}
	return param, true
}

func runCameraSwipe(ctx *maa.Context, step cameraScanStep, param cameraScanParam) bool {
	dx := step.yawDelta * param.YawStepPixels
	dy := step.pitchDelta * param.PitchStepPixels
	return runCameraSwipePixels(ctx, cameraSwipeStartX, cameraSwipeStartY, dx, dy, step.phase)
}

func runCameraSwipePixels(ctx *maa.Context, beginX, beginY, dx, dy int, phase string) bool {
	if ctx.GetTasker().Stopping() {
		return false
	}
	override := map[string]any{
		cameraSwipeNode: map[string]any{
			"begin": maa.Rect{beginX, beginY, 1, 1},
			"end":   maa.Rect{beginX + dx, beginY + dy, 1, 1},
		},
	}
	if _, err := ctx.RunAction(cameraSwipeNode, maa.Rect{}, "", override); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("phase", phase).
			Int("dx", dx).
			Int("dy", dy).
			Msg("camera swipe failed")
		return false
	}
	return true
}

func finishHit(ctx *maa.Context, detail *maa.RecognitionDetail, aimTarget bool) bool {
	if !aimTarget {
		return true
	}
	dx, dy, ok := aimDelta(detail.Box)
	if !ok {
		log.Error().
			Str("component", componentName).
			Msg("target recognition returned an empty box")
		return false
	}
	if dx == 0 && dy == 0 {
		return true
	}
	return runCameraSwipePixels(ctx, screenCenterX, screenCenterY, dx, dy, "aim_target")
}

func aimDelta(box maa.Rect) (int, int, bool) {
	if box.Width() <= 0 || box.Height() <= 0 {
		return 0, 0, false
	}
	targetCenterX := box.X() + box.Width()/2
	targetCenterY := box.Y() + box.Height()/2

	// Photo mode uses drag gestures: drag opposite to the target's screen
	// offset so the camera turns toward the target.
	dx := clamp(screenCenterX-targetCenterX, -screenCenterX, screenCenterX-1)
	dy := clamp(screenCenterY-targetCenterY, -screenCenterY, screenCenterY-1)
	return dx, dy, true
}

func clamp(value, minimum, maximum int) int {
	return min(max(value, minimum), maximum)
}

func recognizeWaitNodes(
	ctx *maa.Context,
	ctrl *maa.Controller,
	waitNodes []string,
	phase string,
	step int,
) *maa.RecognitionDetail {
	if ctx.GetTasker().Stopping() {
		return nil
	}
	ctrl.PostScreencap().Wait()
	if ctx.GetTasker().Stopping() {
		return nil
	}
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		log.Warn().
			Err(err).
			Str("component", componentName).
			Str("phase", phase).
			Int("step", step).
			Msg("cache image failed")
		return nil
	}

	for _, node := range waitNodes {
		detail, recognitionErr := ctx.RunRecognition(node, img)
		if recognitionErr != nil {
			log.Warn().
				Err(recognitionErr).
				Str("component", componentName).
				Str("phase", phase).
				Int("step", step).
				Str("node", node).
				Msg("target recognition failed")
			continue
		}
		if detail != nil && detail.Hit {
			log.Info().
				Str("component", componentName).
				Str("phase", phase).
				Int("step", step).
				Str("node", node).
				Msg("target found")
			return detail
		}
	}
	return nil
}
