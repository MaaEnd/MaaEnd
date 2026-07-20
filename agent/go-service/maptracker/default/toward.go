// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"time"

	internal "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerToward adjusts the player's orientation to face a given angle or map point.
type MapTrackerToward struct{}

// MapTrackerTowardParam represents the custom_action_param for MapTrackerToward.
type MapTrackerTowardParam struct {
	// Angle is the target orientation in degrees (0 is North, increasing clockwise).
	// At least one of Angle and Target must be provided; Angle takes precedence when both are set.
	Angle *float64 `json:"angle,omitempty"`
	// MapName has the same definition as [MapTrackerMoveParam.MapName].
	// It is only required in target mode.
	MapName string `json:"map_name,omitempty"`
	// Target is the map coordinate the player should face toward.
	// At least one of Angle and Target must be provided.
	Target *internal.Point `json:"target,omitempty"`
	// RotationThreshold is the maximum allowed angle difference in degrees to treat the player as
	// already facing the target orientation.
	RotationThreshold float64 `json:"rotation_threshold,omitempty"`
	// MapNameMatchRule has the same definition as [MapTrackerMoveParam.MapNameMatchRule].
	MapNameMatchRule string `json:"map_name_match_rule,omitempty"`
}

const TOWARD_TIMEOUT = 5000 // ms

const TOWARD_ROT_STEP = 2

// Orientation-adjustment nudge durations. After rotating the camera, the player briefly
// walks backward and then forward so that the body orientation snaps to the new camera
// facing without drifting away from the original position.
const (
	TOWARD_NUDGE_BACK_MS    = 250
	TOWARD_NUDGE_FORWARD_MS = 75
)

var mapTrackerTowardDefaultParam = MapTrackerTowardParam{
	RotationThreshold: 12.0,
}

var _ maa.CustomActionRunner = &MapTrackerToward{}

// Run implements maa.CustomActionRunner.
func (a *MapTrackerToward) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerToward")
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	ca, err := control.NewControlAdaptor(ctx, ctrl, WORK_W, WORK_H)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create control adaptor for MapTrackerToward")
		return false
	}

	loopInterval := time.Duration(INFER_INTERVAL_MS) * time.Millisecond
	deadline := time.Now().Add(time.Duration(TOWARD_TIMEOUT) * time.Millisecond)

	// Pure angle mode (no target): infer only the rotation, without any map dependency.
	isPureAngle := param.Target == nil
	ctrlType, _ := control.GetControlType(ctrl)
	rotInfer := &MapTrackerInfer{}

	// settleWait is the fixed time to wait after each turn for the body orientation to
	// settle, sized as the time to rotate 180 degrees at the walk rotation speed.
	settleWait := control.MovementWalk.EtaOfRotation(180)

	// Ensure the player is still before the first measurement.
	ca.ResetCursor(control.CursorResetActive)
	ca.SetPlayerMovement(control.MovementStop, control.PolicyDefault)

	inferParam := &MapTrackerMoveParam{MapName: param.MapName, MapNameMatchRule: param.MapNameMatchRule}

	lastLoopTime := time.Time{}

	for {
		// Throttle the loop to INFER_INTERVAL_MS
		loopElapsed := time.Since(lastLoopTime)
		if loopElapsed < loopInterval {
			time.Sleep(loopInterval - loopElapsed)
		}
		loopStartTime := time.Now()
		lastLoopTime = loopStartTime

		// Check timeout (not a failure, treated as a normal end)
		if loopStartTime.After(deadline) {
			log.Warn().Int("timeout", TOWARD_TIMEOUT).Msg("Toward timeout, ending orientation adjustment")
			break
		}

		// Check stopping signal
		if ctx.GetTasker().Stopping() {
			log.Warn().Msg("Task is stopping, exiting orientation adjustment")
			doPlayerStop(ca)
			return false
		}

		// Read current rotation and (in target mode) location, then determine target rotation
		var curRot int
		var targetRot int
		if isPureAngle {
			// Pure angle mode: infer rotation only, no location judgement or map dependency
			screenImg, err := captureFullScreen(ctrl)
			if err != nil {
				log.Error().Err(err).Msg("Failed to capture screen during orientation adjustment")
				continue
			}
			rot := rotInfer.inferRotation(ctrlType, screenImg, TOWARD_ROT_STEP)
			if rot == nil {
				log.Error().Msg("Rotation inference failed during orientation adjustment")
				continue
			}
			curRot = rot.Rot
			targetRot = ((int(math.Round(*param.Angle)) % 360) + 360) % 360
		} else {
			// Target mode: infer location and rotation, then face toward the target point
			result, err := doInfer(ctx, ctrl, inferParam)
			if err != nil {
				log.Error().Err(err).Msg("Inference failed during orientation adjustment")
				continue
			}
			curRot = result.Rot
			if param.Angle != nil {
				targetRot = ((int(math.Round(*param.Angle)) % 360) + 360) % 360
			} else {
				targetRot = int(math.Round(result.Loc.AngleTo(*param.Target)))
			}
		}

		deltaRot := internal.DeltaRotation(curRot, targetRot)
		absDeltaRot := math.Abs(float64(deltaRot))
		log.Debug().Int("curRot", curRot).Int("targetRot", targetRot).Float64("deltaRot", float64(deltaRot)).Msg("Adjusting orientation")

		// Check if already facing the target orientation
		if absDeltaRot <= param.RotationThreshold {
			log.Info().Int("rot", curRot).Int("targetRot", targetRot).Msg("Reached target orientation")
			break
		}

		// Rotate the camera while the player is still.
		ca.RotateCamera(int(float64(deltaRot)*ROTATION_DEFAULT_SPEED), 0)
		ca.ResetCursor(control.CursorResetLazy)

		// Snap the body orientation to the new camera facing by briefly walking backward
		// and then forward, so the player turns in place without drifting.
		ca.SetPlayerDirection(control.DirectionB)
		ca.SetPlayerMovement(control.MovementWalk, control.PolicyDefault)
		time.Sleep(TOWARD_NUDGE_BACK_MS * time.Millisecond)
		ca.SetPlayerDirection(control.DirectionF)
		time.Sleep(TOWARD_NUDGE_FORWARD_MS * time.Millisecond)
		ca.SetPlayerMovement(control.MovementStop, control.PolicyDefault)

		// Wait a fixed time for the orientation to settle before the next measurement.
		time.Sleep(settleWait)
	}

	// End of adjustment (reached or timed out), stop movement
	doPlayerStop(ca)
	return true
}

func (a *MapTrackerToward) parseParam(paramStr string) (*MapTrackerTowardParam, error) {
	var param MapTrackerTowardParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}
	if param.Angle == nil && param.Target == nil {
		return nil, fmt.Errorf("at least one of angle and target is required in parameters")
	}
	if param.Target != nil && param.MapName == "" {
		return nil, fmt.Errorf("map_name is required in target mode, got empty")
	}
	if param.Angle != nil && (math.IsNaN(*param.Angle) || math.IsInf(*param.Angle, 0)) {
		return nil, fmt.Errorf("angle contains invalid value")
	}
	if param.Target != nil {
		if !param.Target.IsValid() {
			return nil, fmt.Errorf("target contains invalid coordinate")
		}
	}
	if param.RotationThreshold == 0 {
		param.RotationThreshold = mapTrackerTowardDefaultParam.RotationThreshold
	} else if param.RotationThreshold <= 0 || param.RotationThreshold >= 180 {
		return nil, fmt.Errorf("threshold must be between 0 and 180 degrees (exclusive)")
	}
	if param.MapNameMatchRule == "" {
		param.MapNameMatchRule = mapTrackerMoveDefaultParam.MapNameMatchRule
	}
	if param.MapName != "" {
		mapNameRegex := buildMapNameRegex(param.MapNameMatchRule, param.MapName)
		if _, err := regexp.Compile(mapNameRegex); err != nil {
			return nil, fmt.Errorf("map_name_match_rule produced invalid regex %q: %w", mapNameRegex, err)
		}
	}
	return &param, nil
}
