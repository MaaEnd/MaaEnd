package approachocr

import (
	"encoding/json"
	"image"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "ApproachOCRTargetAction"
	screenW       = 1280
	screenH       = 720
)

type approachOCRParam struct {
	TargetNodes      []string `json:"target_nodes"`
	StopNodes        []string `json:"stop_nodes"`
	AlignThreshold   int      `json:"align_threshold,omitempty"`
	PulseForwardMs   int      `json:"pulse_forward_ms,omitempty"`
	PulseBackwardMs  int      `json:"pulse_backward_ms,omitempty"`
	SearchYawDeg     int      `json:"search_yaw_deg,omitempty"`
	MaxSteps         int      `json:"max_steps,omitempty"`
	Movement         string   `json:"movement,omitempty"`
	PassedYThreshold int      `json:"passed_y_threshold,omitempty"`
	YawPxPerDeg      int      `json:"yaw_px_per_deg,omitempty"`
}

// ApproachOCRTargetAction finds target recognition boxes (usually OCR nameplates),
// turns the camera toward them, and walks forward with free pulse distances via
// pkg/control until any stop node hits (usually an interact button).
type ApproachOCRTargetAction struct{}

var _ maa.CustomActionRunner = &ApproachOCRTargetAction{}

func (a *ApproachOCRTargetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, ok := parseParam(arg.CustomActionParam)
	if !ok {
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	ca, err := control.NewControlAdaptor(ctx, ctrl, screenW, screenH)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to create control adaptor")
		return false
	}

	ca.AggressivelyResetPlayerMovement()
	defer ca.SetPlayerMovement(control.MovementStop, control.PolicyActive)

	movement := resolveMovement(param.Movement)

	for step := 1; step <= param.MaxSteps; step++ {
		if ctx.GetTasker().Stopping() {
			return false
		}

		img, ok := cacheImage(ctrl, step)
		if !ok {
			return false
		}

		if hit, node := recognizeAny(ctx, img, param.StopNodes, step, "stop"); hit {
			log.Info().
				Str("component", componentName).
				Int("step", step).
				Str("node", node).
				Msg("stop node hit, approach complete")
			ca.ResetCursor(control.CursorResetActive)
			return true
		}

		if detail, node := recognizeDetail(ctx, img, param.TargetNodes, step, "target"); detail != nil {
			if !approachHit(ca, detail, param, movement, step, node) {
				return false
			}
			continue
		}

		dx := param.SearchYawDeg * param.YawPxPerDeg
		log.Info().
			Str("component", componentName).
			Int("step", step).
			Int("dx", dx).
			Msg("target not found, searching by yaw")
		ca.RotateCamera(dx, 0)
		ca.ResetCursor(control.CursorResetLazy)
	}

	log.Warn().
		Str("component", componentName).
		Int("max_steps", param.MaxSteps).
		Msg("approach failed: stop node never appeared")
	return false
}

func parseParam(raw string) (approachOCRParam, bool) {
	var param approachOCRParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to parse params")
		return approachOCRParam{}, false
	}
	if len(param.TargetNodes) == 0 {
		log.Error().
			Str("component", componentName).
			Msg("target_nodes is required")
		return approachOCRParam{}, false
	}
	if len(param.StopNodes) == 0 {
		log.Error().
			Str("component", componentName).
			Msg("stop_nodes is required")
		return approachOCRParam{}, false
	}
	if param.AlignThreshold == 0 {
		param.AlignThreshold = 80
	}
	if param.PulseForwardMs == 0 {
		param.PulseForwardMs = 250
	}
	if param.PulseBackwardMs == 0 {
		param.PulseBackwardMs = 200
	}
	if param.SearchYawDeg == 0 {
		param.SearchYawDeg = 30
	}
	if param.MaxSteps == 0 {
		param.MaxSteps = 48
	}
	if param.PassedYThreshold == 0 {
		param.PassedYThreshold = 480
	}
	if param.YawPxPerDeg == 0 {
		param.YawPxPerDeg = 2
	}
	if param.MaxSteps < 1 || param.MaxSteps > 200 {
		log.Error().
			Str("component", componentName).
			Int("max_steps", param.MaxSteps).
			Msg("max_steps out of range")
		return approachOCRParam{}, false
	}
	return param, true
}

func resolveMovement(name string) control.PlayerMovement {
	switch name {
	case "run":
		return control.MovementRun
	case "sprint":
		return control.MovementSprint
	default:
		return control.MovementWalk
	}
}

func approachHit(
	ca control.ControlAdaptor,
	detail *maa.RecognitionDetail,
	param approachOCRParam,
	movement control.PlayerMovement,
	step int,
	node string,
) bool {
	box := detail.Box
	if box.Width() <= 0 || box.Height() <= 0 {
		log.Error().
			Str("component", componentName).
			Int("step", step).
			Str("node", node).
			Msg("target recognition returned an empty box")
		return false
	}

	centerX := box.X() + box.Width()/2
	centerY := box.Y() + box.Height()/2
	offsetX := centerX - screenW/2

	switch {
	case offsetX < -param.AlignThreshold || offsetX > param.AlignThreshold:
		dx := offsetX / 3
		log.Info().
			Str("component", componentName).
			Int("step", step).
			Str("node", node).
			Int("offset_x", offsetX).
			Int("dx", dx).
			Msg("turning toward target")
		ca.RotateCamera(dx, 0)
		ca.ResetCursor(control.CursorResetLazy)

	case centerY > param.PassedYThreshold:
		back := time.Duration(param.PulseBackwardMs) * time.Millisecond
		log.Info().
			Str("component", componentName).
			Int("step", step).
			Str("node", node).
			Int("center_y", centerY).
			Dur("pulse_ms", back).
			Msg("target passed, stepping backward")
		ca.PlayerPulseMove(-back, 0, movement)

	default:
		// Higher on screen ≈ farther: stretch pulse duration a bit.
		farFactor := float64(screenH/2-centerY) / float64(screenH/2)
		if farFactor < 0 {
			farFactor = 0
		}
		ms := float64(param.PulseForwardMs) * (0.7 + 0.6*farFactor)
		forward := time.Duration(ms) * time.Millisecond
		log.Info().
			Str("component", componentName).
			Int("step", step).
			Str("node", node).
			Int("center_y", centerY).
			Float64("far_factor", farFactor).
			Dur("pulse_ms", forward).
			Msg("moving forward toward target")
		ca.PlayerPulseMove(forward, 0, movement)
	}
	return true
}

func cacheImage(ctrl *maa.Controller, step int) (image.Image, bool) {
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		log.Warn().
			Err(err).
			Str("component", componentName).
			Int("step", step).
			Msg("cache image failed")
		return nil, false
	}
	return img, true
}

func recognizeAny(
	ctx *maa.Context,
	img image.Image,
	nodes []string,
	step int,
	kind string,
) (bool, string) {
	detail, node := recognizeDetail(ctx, img, nodes, step, kind)
	return detail != nil, node
}

func recognizeDetail(
	ctx *maa.Context,
	img image.Image,
	nodes []string,
	step int,
	kind string,
) (*maa.RecognitionDetail, string) {
	for _, node := range nodes {
		detail, err := ctx.RunRecognition(node, img)
		if err != nil {
			log.Warn().
				Err(err).
				Str("component", componentName).
				Int("step", step).
				Str("kind", kind).
				Str("node", node).
				Msg("recognition failed")
			continue
		}
		if detail != nil && detail.Hit {
			return detail, node
		}
	}
	return nil, ""
}
