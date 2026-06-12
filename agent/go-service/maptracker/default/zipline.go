// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"regexp"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerZipline tries one zipline fast travel while the player is already on a zipline.
type MapTrackerZipline struct{}

// MapTrackerZiplineParam represents the custom_action_param for MapTrackerZipline.
type MapTrackerZiplineParam struct {
	// MapName has the same definition as [MapTrackerMoveParam.MapName].
	MapName string `json:"map_name"`
	// Target is the map coordinate of the destination zipline.
	Target *[2]float64 `json:"target"`
	// RotationThreshold is the maximum allowed angle difference to treat the player as
	// already facing the right direction to the destination zipline.
	RotationThreshold float64 `json:"rotation_threshold,omitempty"`
	// Timeout is the maximum time in milliseconds to wait for the entire zipline process to complete.
	Timeout int64 `json:"timeout,omitempty"`
	// MapNameMatchRule has the same definition as [MapTrackerMoveParam.MapNameMatchRule].
	MapNameMatchRule string `json:"map_name_match_rule,omitempty"`
}

var mapTrackerZiplineDefaultParam = MapTrackerZiplineParam{
	RotationThreshold: 9.0,
	Timeout:           15000,
}

const (
	MINIMAP_SIMILARITY_THRESHOLD      = 0.9
	ZIPLINE_ACTION_DURATION_MS        = 50
	ZIPLINE_ACTION_POST_DELAY_MS      = 300
	ZIPLINE_ROTATION_LOOP_INTERVAL_MS = 450

	ZIPLINE_STILL_CHECK_INTERVAL_MS = 300
	ZIPLINE_STILL_THRESHOLD_INIT    = 0.985
	ZIPLINE_STILL_THRESHOLD_FINAL   = 0.875
	ZIPLINE_STILL_THRESHOLD_SPEED   = 0.005 // reduced per second
)

var _ maa.CustomActionRunner = &MapTrackerZipline{}

// Run implements maa.CustomActionRunner.
func (a *MapTrackerZipline) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerZipline")
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	ca, err := control.NewControlAdaptor(ctx, ctrl, WORK_W, WORK_H)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create control adaptor for MapTrackerZipline")
		return false
	}
	ca.SetPlayerMovement(control.MovementStop, control.PolicyLazy)

	deadline := time.Now().Add(time.Duration(param.Timeout) * time.Millisecond)
	result, err := doInfer(ctx, ctrl, &MapTrackerMoveParam{MapName: param.MapName, MapNameMatchRule: param.MapNameMatchRule})
	if err != nil {
		log.Error().Err(err).Msg("Failed to infer current location before zipline")
		return false
	}

	distance := math.Hypot(result.X-param.Target[0], result.Y-param.Target[1])
	log.Info().
		Float64("curX", result.X).
		Float64("curY", result.Y).
		Float64("targetX", param.Target[0]).
		Float64("targetY", param.Target[1]).
		Float64("distance", distance).
		Msg("Current location and target for MapTrackerZipline")

	maafocus.Print(ctx, "准备对准滑索架") // TODO: i18n
	if !a.rotateTowardTarget(ctx, ctrl, ca, param, deadline) {
		log.Warn().Msg("Failed to rotate toward target zipline")
		return false
	}

	before, err := captureMiniMapImage(ctrl)
	if err != nil {
		log.Error().Err(err).Msg("Failed to capture minimap before zipline fast travel")
		return false
	}

	maafocus.Print(ctx, "尝试进行移动")
	ca.TouchClick(0, WORK_W/2, WORK_H/2, ZIPLINE_ACTION_DURATION_MS, ZIPLINE_ACTION_POST_DELAY_MS)

	after, err := captureMiniMapImage(ctrl)
	if err != nil {
		log.Error().Err(err).Msg("Failed to capture minimap after zipline fast travel")
		return false
	}
	similarity := minicv.ImageSimilarity(before, after, [2]int{50, 50})
	if similarity > MINIMAP_SIMILARITY_THRESHOLD {
		log.Warn().Float64("similarity", similarity).Msg("Zipline fast travel did not start")
		maafocus.Print(ctx, "未检测到移动")
		return false
	}

	log.Info().Float64("similarity", similarity).Msg("Zipline fast travel started")
	maafocus.Print(ctx, "检测到成功移动")

	prevFrame, err := captureFullScreen(ctrl)
	if err != nil {
		log.Error().Err(err).Msg("Failed to capture initial frame for stillness detection")
		return false
	}

	deadline = time.Now().Add(time.Duration(param.Timeout) * time.Millisecond) // Refresh deadline
	stillCheckInterval := time.Duration(ZIPLINE_STILL_CHECK_INTERVAL_MS) * time.Millisecond
	stillStartTime := time.Now()
	for time.Now().Before(deadline) {
		if ctx.GetTasker().Stopping() {
			log.Warn().Msg("Task is stopping while waiting for zipline")
			return false
		}
		time.Sleep(stillCheckInterval)
		currFrame, err := captureFullScreen(ctrl)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to capture frame for stillness detection")
			continue
		}
		stillSimilarity := minicv.ImageSimilarity(prevFrame, currFrame, [2]int{200, 200})
		elapsed := time.Since(stillStartTime).Seconds()
		threshold := max(ZIPLINE_STILL_THRESHOLD_FINAL, ZIPLINE_STILL_THRESHOLD_INIT-elapsed*ZIPLINE_STILL_THRESHOLD_SPEED)
		log.Debug().Float64("similarity", stillSimilarity).Float64("threshold", threshold).Msg("Zipline stillness check")
		if stillSimilarity >= threshold {
			log.Info().Float64("similarity", stillSimilarity).Msg("Zipline fast travel completed (screen still)")
			maafocus.Print(ctx, "滑索移动完毕")
			return true
		}
		prevFrame = currFrame
	}
	log.Warn().Int64("timeout", param.Timeout).Msg("Zipline fast travel timed out")
	return false
}

func (a *MapTrackerZipline) parseParam(paramStr string) (*MapTrackerZiplineParam, error) {
	var param MapTrackerZiplineParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}
	if param.MapName == "" {
		return nil, fmt.Errorf("map_name is required in parameters, got empty")
	}
	if param.Target == nil {
		return nil, fmt.Errorf("target is required in parameters")
	}
	if math.IsNaN(param.Target[0]) || math.IsInf(param.Target[0], 0) || math.IsNaN(param.Target[1]) || math.IsInf(param.Target[1], 0) {
		return nil, fmt.Errorf("target contains invalid coordinate")
	}
	if param.RotationThreshold == 0 {
		param.RotationThreshold = mapTrackerZiplineDefaultParam.RotationThreshold
	} else if param.RotationThreshold <= 0 || param.RotationThreshold >= 180 {
		return nil, fmt.Errorf("rotation_threshold must be between 0 and 180 degrees (exclusive)")
	}
	if param.Timeout == 0 {
		param.Timeout = mapTrackerZiplineDefaultParam.Timeout
	} else if param.Timeout < 0 {
		return nil, fmt.Errorf("timeout must be a positive integer")
	}
	if param.MapNameMatchRule == "" {
		param.MapNameMatchRule = mapTrackerMoveDefaultParam.MapNameMatchRule
	}
	mapNameRegex := buildMapNameRegex(param.MapNameMatchRule, param.MapName)
	if _, err := regexp.Compile(mapNameRegex); err != nil {
		return nil, fmt.Errorf("map_name_match_rule produced invalid regex %q: %w", mapNameRegex, err)
	}
	return &param, nil
}

func (a *MapTrackerZipline) rotateTowardTarget(ctx *maa.Context, ctrl *maa.Controller, ca control.ControlAdaptor, param *MapTrackerZiplineParam, deadline time.Time) bool {
	rotationInterval := time.Duration(ZIPLINE_ROTATION_LOOP_INTERVAL_MS) * time.Millisecond
	for time.Now().Before(deadline) {
		if ctx.GetTasker().Stopping() {
			log.Warn().Msg("Task is stopping while rotating toward zipline")
			return false
		}
		result, err := doInfer(ctx, ctrl, &MapTrackerMoveParam{MapName: param.MapName, MapNameMatchRule: param.MapNameMatchRule})
		if err != nil {
			log.Warn().Err(err).Msg("Inference failed while rotating toward zipline")
			time.Sleep(rotationInterval)
			continue
		}
		targetRot := calcTargetRotation(result.X, result.Y, param.Target[0], param.Target[1])
		deltaRot := calcDeltaRotation(result.Rot, targetRot)
		absDeltaRot := math.Abs(float64(deltaRot))
		log.Debug().Int("curRot", result.Rot).Int("targetRot", targetRot).Float64("deltaRot", float64(deltaRot)).Msg("Rotating toward zipline")
		if absDeltaRot <= param.RotationThreshold {
			return true
		}
		ca.RotateCamera(int(float64(deltaRot)*ROTATION_DEFAULT_SPEED), 0)
		ca.ResetCamera()
		time.Sleep(rotationInterval)
	}
	return false
}

func captureMiniMapImage(ctrl *maa.Controller) (*image.RGBA, error) {
	screen, err := captureFullScreen(ctrl)
	if err != nil {
		return nil, err
	}
	ctrlType, _ := control.GetControlType(ctrl)
	switch ctrlType {
	case control.CONTROL_TYPE_ADB:
		return minicv.ImageScale(minicv.ImageCropSquareByRadius(screen, 136, 131, 50), 0.8), nil
	default:
		return minicv.ImageCropSquareByRadius(screen, 108, 111, 40), nil
	}
}

func captureFullScreen(ctrl *maa.Controller) (*image.RGBA, error) {
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}
	return minicv.ImageConvertRGBA(img), nil
}
