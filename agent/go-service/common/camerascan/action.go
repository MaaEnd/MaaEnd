package camerascan

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName           = "CameraScanAction"
	cameraAimSwipeNode      = "__CameraScanAimSwipe"
	defaultMoveUpNode       = "__CameraScanMoveUp"
	defaultMoveDownNode     = "__CameraScanMoveDown"
	defaultMoveLeftNode     = "__CameraScanMoveLeft"
	defaultMoveRightNode    = "__CameraScanMoveRight"
	screenCenterX           = 640
	screenCenterY           = 360
	defaultFallbackYawSteps = 8
)

type cameraScanParam struct {
	WaitNodes        []string `json:"wait_nodes"`
	AimTarget        bool     `json:"aim_target,omitempty"`
	MoveUp           string   `json:"move_up,omitempty"`
	MoveDown         string   `json:"move_down,omitempty"`
	MoveLeft         string   `json:"move_left,omitempty"`
	MoveRight        string   `json:"move_right,omitempty"`
	FallbackYawSteps int      `json:"fallback_yaw_steps,omitempty"`
}

// CameraScanAction scans the camera view and recognizes target Pipeline nodes
// before and after every movement except reset.
type CameraScanAction struct{}

var _ maa.CustomActionRunner = &CameraScanAction{}

func (a *CameraScanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, ok := parseParam(arg.CustomActionParam)
	if !ok {
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	for index, step := range buildCameraScanPath(param.FallbackYawSteps) {
		if ctx.GetTasker().Stopping() {
			return false
		}
		detail, ok := runScanStep(ctx, ctrl, step, param, index+1)
		if !ok {
			return false
		}
		if detail != nil {
			return finishHit(ctx, detail, param.AimTarget)
		}
	}

	log.Info().
		Str("component", componentName).
		Msg("target not found after camera scan")
	return false
}

func runScanStep(
	ctx *maa.Context,
	ctrl *maa.Controller,
	step cameraScanStep,
	param cameraScanParam,
	stepIndex int,
) (*maa.RecognitionDetail, bool) {
	if step.needsRecognition() {
		if detail := recognizeWaitNodes(ctx, ctrl, param.WaitNodes, step.phase, "before", stepIndex); detail != nil {
			return detail, true
		}
	}
	if !runCameraSwipe(ctx, step, param) {
		return nil, false
	}
	if step.needsRecognition() {
		if detail := recognizeWaitNodes(ctx, ctrl, param.WaitNodes, step.phase, "after", stepIndex); detail != nil {
			return detail, true
		}
	}
	return nil, true
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

	if param.MoveUp == "" {
		param.MoveUp = defaultMoveUpNode
	}
	if param.MoveDown == "" {
		param.MoveDown = defaultMoveDownNode
	}
	if param.MoveLeft == "" {
		param.MoveLeft = defaultMoveLeftNode
	}
	if param.MoveRight == "" {
		param.MoveRight = defaultMoveRightNode
	}
	if param.FallbackYawSteps == 0 {
		param.FallbackYawSteps = defaultFallbackYawSteps
	}
	if param.FallbackYawSteps < 4 || param.FallbackYawSteps > 72 {
		log.Error().
			Str("component", componentName).
			Int("fallback_yaw_steps", param.FallbackYawSteps).
			Msg("camera scan step values are out of range")
		return cameraScanParam{}, false
	}
	return param, true
}

func runCameraSwipe(ctx *maa.Context, step cameraScanStep, param cameraScanParam) bool {
	for range absInt(step.pitchDelta) {
		node := param.MoveDown
		if step.pitchDelta < 0 {
			node = param.MoveUp
		}
		if !runMoveNode(ctx, node, step.phase) {
			return false
		}
	}
	for range absInt(step.yawDelta) {
		node := param.MoveRight
		if step.yawDelta < 0 {
			node = param.MoveLeft
		}
		if !runMoveNode(ctx, node, step.phase) {
			return false
		}
	}
	return true
}

func runMoveNode(ctx *maa.Context, node, phase string) bool {
	if ctx.GetTasker().Stopping() {
		return false
	}
	detail, err := ctx.RunTask(node)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("phase", phase).
			Str("node", node).
			Msg("camera move node failed")
		return false
	}
	if detail == nil || !detail.Status.Success() {
		log.Error().
			Str("component", componentName).
			Str("phase", phase).
			Str("node", node).
			Msg("camera move node did not succeed")
		return false
	}
	return true
}

func runCameraSwipePixels(ctx *maa.Context, beginX, beginY, dx, dy int, phase string) bool {
	if ctx.GetTasker().Stopping() {
		return false
	}
	override := map[string]any{
		cameraAimSwipeNode: map[string]any{
			"begin": maa.Rect{beginX, beginY, 1, 1},
			"end":   maa.Rect{beginX + dx, beginY + dy, 1, 1},
		},
	}
	detail, err := ctx.RunTask(cameraAimSwipeNode, override)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("phase", phase).
			Int("dx", dx).
			Int("dy", dy).
			Msg("camera aim swipe failed")
		return false
	}
	if detail == nil || !detail.Status.Success() {
		log.Error().
			Str("component", componentName).
			Str("phase", phase).
			Int("dx", dx).
			Int("dy", dy).
			Msg("camera aim swipe did not succeed")
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

	// Aim swipe starts at screen center and ends at the target box center.
	dx := clamp(targetCenterX-screenCenterX, -screenCenterX, screenCenterX-1)
	dy := clamp(targetCenterY-screenCenterY, -screenCenterY, screenCenterY-1)
	return dx, dy, true
}

func clamp(value, minimum, maximum int) int {
	return min(max(value, minimum), maximum)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func recognizeWaitNodes(
	ctx *maa.Context,
	ctrl *maa.Controller,
	waitNodes []string,
	phase string,
	timing string,
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
			Str("timing", timing).
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
				Str("timing", timing).
				Int("step", step).
				Str("node", node).
				Msg("target recognition failed")
			continue
		}
		if detail != nil && detail.Hit {
			log.Info().
				Str("component", componentName).
				Str("phase", phase).
				Str("timing", timing).
				Int("step", step).
				Str("node", node).
				Msg("target found")
			return detail
		}
	}
	return nil
}
