// Copyright (c) 2026 Harry Huang
package maptracker

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type MapTrackerMove struct{}

type MoveParam struct {
	MapName string   `json:"map_name"`
	Targets [][2]int `json:"targets"`
}

//go:embed messages/emergency_stop.html
var emergencyStopHTML string

//go:embed messages/navigation_moving.html
var navigationMovingHTML string

//go:embed messages/navigation_finished.html
var navigationFinishedHTML string

var _ maa.CustomActionRunner = &MapTrackerMove{}

// Run implements maa.CustomActionRunner
func (a *MapTrackerMove) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// Parse parameters
	var param MoveParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("param", arg.CustomActionParam).Msg("Failed to parse MoveParam")
		return false
	}

	if len(param.Targets) == 0 {
		log.Error().Msg("No targets provided")
		return false
	}

	// Prepare variables
	ctrl := ctx.GetTasker().GetController()
	aw := NewActionWrapper(ctx, ctrl)
	inferIntervalDuration := time.Duration(INFER_INTERVAL_MS) * time.Millisecond

	log.Info().Str("map", param.MapName).Int("targets_count", len(param.Targets)).Msg("Starting navigation to targets")

	// For each target point
	for i, target := range param.Targets {
		targetX, targetY := target[0], target[1]
		log.Info().Int("index", i).Int("targetX", targetX).Int("targetY", targetY).Msg("Navigating to next target point")

		// Show navigation UI
		if initRes, err := doInfer(ctx, ctrl, param); err == nil && initRes != nil {
			initDist := math.Hypot(float64(initRes.X-targetX), float64(initRes.Y-targetY))
			PrintUI(aw.ctx, fmt.Sprintf(navigationMovingHTML, targetX, targetY, int(initDist)))
		} else if err != nil {
			log.Debug().Err(err).Msg("Initial infer failed for moving UI")
		}

		var (
			lastInferTime          = time.Time{}
			lastRotationAdjustTime = time.Time{}
			lastArrivalTime        = time.Now()
			prevLocationTime       = time.Time{}
			prevLocation           *[2]int
		)

		for {
			// Calculate time since last check
			elapsed := time.Since(lastInferTime)
			if elapsed < inferIntervalDuration {
				time.Sleep(inferIntervalDuration - elapsed)
			}
			now := time.Now()
			lastInferTime = now

			// Check stopping signal
			if ctx.GetTasker().Stopping() {
				log.Warn().Msg("Task is stopping, exiting navigation loop")
				aw.KeyUpSync(KEY_W, 100)
				return false
			}

			// Check arrival timeout
			deltaArrivalMs := now.Sub(lastArrivalTime).Milliseconds()
			if deltaArrivalMs > FAILURE_ARRIVAL_MAX_DURATION_MS {
				log.Error().Msg("Arrival timeout, stopping task")
				doEmergencyStop(aw)
				return false
			}

			// Run inference to get current location and rotation
			result, err := doInfer(ctx, ctrl, param)
			if err != nil {
				log.Error().Err(err).Msg("Inference failed during navigation")
				aw.KeyUpSync(KEY_W, 100)
				continue
			}

			curX, curY := result.X, result.Y
			rot := result.Rot

			// Check Stuck
			if prevLocation != nil && prevLocation[0] == curX && prevLocation[1] == curY {
				deltaLocationMs := now.Sub(prevLocationTime).Milliseconds()
				if deltaLocationMs > FAILURE_STUCK_MAX_DURATION_MS {
					log.Error().Msg("Stuck for too long, stopping task")
					doEmergencyStop(aw)
					return false
				}
				if deltaLocationMs > STUCK_MIN_DURATION_MS {
					log.Info().Msg("Stuck detected, jumping...")
					aw.KeyTypeSync(KEY_SPACE, 100)
				}
			} else {
				prevLocation = &[2]int{curX, curY}
				prevLocationTime = now
			}

			// Check arrival
			dist := math.Hypot(float64(curX-targetX), float64(curY-targetY))
			if dist < ARRIVAL_TOLERANCE {
				log.Info().Int("x", curX).Int("y", curY).Int("index", i).Msg("Target point reached")
				break
			}

			log.Debug().Int("x", curX).Int("y", curY).Float64("dist", dist).Msg("Navigating to target")

			// Calculate & adjust rotation
			targetRot := calcTargetRotation(curX, curY, targetX, targetY)
			deltaRot := calcDeltaRotation(rot, targetRot)

			// Check rotation and adjust if needed
			if math.Abs(float64(deltaRot)) > ROTATION_LOW_TOLERANCE {
				if lastRotationAdjustTime.IsZero() {
					lastRotationAdjustTime = now
				}
				deltaRotationAdjustMs := now.Sub(lastRotationAdjustTime).Milliseconds()
				if deltaRotationAdjustMs > FAILURE_ROTATION_MAX_DURATION_MS {
					log.Error().Msg("Rotation adjustment timeout, stopping task")
					doEmergencyStop(aw)
					return false
				}

				log.Debug().Int("cur", rot).Int("target", targetRot).Int("delta", deltaRot).Msg("Adjusting rotation")

				if math.Abs(float64(deltaRot)) > ROTATION_HIGH_TOLERANCE {
					// Stop and rotate for large misalignment
					aw.KeyUpSync(KEY_W, 0)
					aw.RotateCamera(int(float64(deltaRot)*ROTATION_SENSITIVITY), 100, 100)
					aw.KeyDownSync(KEY_W, 0)
				} else {
					// Just rotate for small misalignment
					aw.KeyDownSync(KEY_W, 0)
					aw.RotateCamera(int(float64(deltaRot)*ROTATION_SENSITIVITY), 100, 100)
				}
			} else {
				aw.KeyDownSync(KEY_W, 0)
				if dist > SPRINT_MIN_DISTANCE {
					// Sprint if target is far enough
					aw.KeyTypeSync(KEY_SHIFT, 100)
				}
				lastRotationAdjustTime = time.Time{} // Reset
			}
		}

		// End of loop, one target reached
		aw.KeyUpSync(KEY_W, 100)
	}

	// Show finished UI summary
	PrintUI(aw.ctx, fmt.Sprintf(navigationFinishedHTML, len(param.Targets)))

	return true
}

func doEmergencyStop(aw *ActionWrapper) {
	log.Warn().Msg("Emergency stop triggered")
	PrintUI(aw.ctx, emergencyStopHTML)
	aw.KeyUpSync(KEY_W, 100)
	aw.ctx.GetTasker().PostStop()
}

func doInfer(ctx *maa.Context, ctrl *maa.Controller, param MoveParam) (*InferResult, error) {
	// Capture Screen
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get cached image")
		return nil, err
	}
	if img == nil {
		log.Error().Msg("Cached image is nil")
		return nil, fmt.Errorf("cached image is nil")
	}

	// Run recognition
	nodeName := "MapTrackerMove_Infer"
	config := map[string]any{
		nodeName: map[string]any{
			"recognition":        "Custom",
			"custom_recognition": "MapTrackerInfer",
			"custom_recognition_param": map[string]any{
				"precision":      0.6,
				"map_name_regex": "^" + regexp.QuoteMeta(param.MapName) + "$",
			},
		},
	}

	res, err := ctx.RunRecognition(nodeName, img, config)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run MapTrackerInfer")
		return nil, err
	}
	if res == nil || res.DetailJson == "" {
		log.Error().Msg("Inference result is empty")
		return nil, fmt.Errorf("inference result is empty")
	}

	// Extract result
	var result InferResult
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}

	if err := json.Unmarshal([]byte(res.DetailJson), &wrapped); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal wrapped result")
		return nil, err
	}
	if err := json.Unmarshal(wrapped.Best.Detail, &result); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal InferResult")
		return nil, err
	}
	if result.MapName == "None" {
		log.Error().Msg("Map not recognized in inference result")
		return nil, fmt.Errorf("map not recognized in inference result")
	}

	return &result, nil
}

// calcTargetRotation calculates the angle from (fromX, fromY) to (toX, toY).
// 0 degrees is North (negative Y), increasing clockwise.
func calcTargetRotation(fromX, fromY, toX, toY int) int {
	dx := float64(toX - fromX)
	dy := float64(toY - fromY)
	angleRad := math.Atan2(dx, -dy)
	angleDeg := angleRad * 180.0 / math.Pi

	// Normalize to [0, 360)
	if angleDeg < 0 {
		angleDeg += 360
	}
	return int(math.Round(angleDeg)) % 360
}

// calcDeltaRotation calculates min difference between two angles [-180, 180]
func calcDeltaRotation(current, target int) int {
	diff := target - current
	for diff > 180 {
		diff -= 360
	}
	for diff < -180 {
		diff += 360
	}
	return diff
}
