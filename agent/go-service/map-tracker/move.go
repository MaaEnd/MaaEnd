// Copyright (c) 2026 Harry Huang
package maptracker

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type MapTrackerMove struct{}

// MapTrackerMoveParam represents the custom_action_param for MapTrackerMove
type MapTrackerMoveParam struct {
	// MapName is the name of the map to navigate (required).
	MapName string `json:"map_name"`
	// Path is a sequence of [x, y] coordinate points to follow (required).
	Path [][2]int `json:"path"`
	// PathTrim trims the path to start from the nearest point to the current location when enabled.
	PathTrim bool `json:"path_trim,omitempty"`
	// NoPrint controls whether to suppress printing navigation status to the GUI.
	NoPrint bool `json:"no_print,omitempty"`
	// ArrivalThreshold is the minimum distance to consider a target reached.
	ArrivalThreshold float64 `json:"arrival_threshold,omitempty"`
	// ArrivalTimeout is the maximum allowed time in milliseconds to reach each target point.
	ArrivalTimeout int64 `json:"arrival_timeout,omitempty"`
	// RotationLowerThreshold is the minimum angular difference in degrees to trigger rotation adjustment.
	RotationLowerThreshold float64 `json:"rotation_lower_threshold,omitempty"`
	// RotationUpperThreshold is the angular difference in degrees above which a more aggressive correction is applied.
	RotationUpperThreshold float64 `json:"rotation_upper_threshold,omitempty"`
	// RotationSpeed is the multiplier applied to the delta rotation when rotating the camera.
	RotationSpeed float64 `json:"rotation_speed,omitempty"`
	// RotationTimeout is the maximum time in milliseconds allowed for rotation adjustment.
	RotationTimeout int64 `json:"rotation_timeout,omitempty"`
	// SprintThreshold is the minimum distance beyond which sprinting is used.
	SprintThreshold float64 `json:"sprint_threshold,omitempty"`
	// StuckThreshold is the duration in milliseconds after which lack of movement is considered a stuck condition.
	StuckThreshold int64 `json:"stuck_threshold,omitempty"`
	// StuckTimeout is the maximum time in milliseconds to tolerate being stuck.
	StuckTimeout int64 `json:"stuck_timeout,omitempty"`
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
	// Prepare variables
	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerMove")
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	aw := NewActionWrapper(ctx, ctrl)
	inferIntervalDuration := time.Duration(INFER_INTERVAL_MS) * time.Millisecond

	if param.PathTrim && len(param.Path) > 1 {
		if initRes, err := doInfer(ctx, ctrl, param); err == nil && initRes != nil {
			closestIdx := 0
			minDist := math.MaxFloat64
			for i, p := range param.Path {
				dist := math.Hypot(float64(initRes.X-p[0]), float64(initRes.Y-p[1]))
				if dist < minDist {
					minDist = dist
					closestIdx = i
				}
			}
			if closestIdx > 0 {
				log.Info().Int("closest_index", closestIdx).Float64("closest_dist", minDist).Msg("Path trim enabled, skipping earlier targets")
				param.Path = param.Path[closestIdx:]
			}
		} else {
			log.Warn().Err(err).Msg("Path trim enabled but failed to infer current location; using full path")
		}
	}

	log.Info().Str("map", param.MapName).Int("targets_count", len(param.Path)).Msg("Starting navigation to targets")

	// For each target point
	for i, target := range param.Path {
		targetX, targetY := target[0], target[1]
		log.Info().Int("index", i).Int("targetX", targetX).Int("targetY", targetY).Msg("Navigating to next target point")

		// Show navigation UI
		if initRes, err := doInfer(ctx, ctrl, param); err == nil && initRes != nil {
			initDist := math.Hypot(float64(initRes.X-targetX), float64(initRes.Y-targetY))
			if !param.NoPrint {
				maafocus.NodeActionStarting(
					aw.ctx,
					fmt.Sprintf(navigationMovingHTML, targetX, targetY, int(initDist)),
				)
			}
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
			if deltaArrivalMs > param.ArrivalTimeout {
				log.Error().Msg("Arrival timeout, stopping task")
				doEmergencyStop(aw, param.NoPrint)
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
				if deltaLocationMs > param.StuckTimeout {
					log.Error().Msg("Stuck for too long, stopping task")
					doEmergencyStop(aw, param.NoPrint)
					return false
				}
				if deltaLocationMs > param.StuckThreshold {
					log.Info().Msg("Stuck detected, jumping...")
					aw.KeyTypeSync(KEY_SPACE, 100)
				}
			} else {
				prevLocation = &[2]int{curX, curY}
				prevLocationTime = now
			}

			// Check arrival
			dist := math.Hypot(float64(curX-targetX), float64(curY-targetY))
			if dist < param.ArrivalThreshold {
				log.Info().Int("x", curX).Int("y", curY).Int("index", i).Msg("Target point reached")
				break
			}

			log.Debug().Int("x", curX).Int("y", curY).Float64("dist", dist).Msg("Navigating to target")

			// Calculate & adjust rotation
			targetRot := calcTargetRotation(curX, curY, targetX, targetY)
			deltaRot := calcDeltaRotation(rot, targetRot)

			// Check rotation and adjust if needed
			if math.Abs(float64(deltaRot)) > param.RotationLowerThreshold {
				if lastRotationAdjustTime.IsZero() {
					lastRotationAdjustTime = now
				}
				deltaRotationAdjustMs := now.Sub(lastRotationAdjustTime).Milliseconds()
				if deltaRotationAdjustMs > param.RotationTimeout {
					log.Error().Msg("Rotation adjustment timeout, stopping task")
					doEmergencyStop(aw, param.NoPrint)
					return false
				}

				log.Debug().Int("cur", rot).Int("target", targetRot).Int("delta", deltaRot).Msg("Adjusting rotation")

				if math.Abs(float64(deltaRot)) > param.RotationUpperThreshold {
					// Stop and rotate for large misalignment
					aw.KeyUpSync(KEY_W, 0)
					aw.RotateCamera(int(float64(deltaRot)*param.RotationSpeed), 100, 100)
					aw.KeyDownSync(KEY_W, 100)
				} else {
					// Just rotate for small misalignment
					aw.RotateCamera(int(float64(deltaRot)*param.RotationSpeed), 100, 100)
					aw.KeyDownSync(KEY_W, 100)
				}
			} else {
				aw.KeyDownSync(KEY_W, 100)
				if dist > param.SprintThreshold {
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
	if !param.NoPrint {
		maafocus.NodeActionStarting(
			aw.ctx,
			fmt.Sprintf(navigationFinishedHTML, len(param.Path)),
		)
	}

	return true
}

func (a *MapTrackerMove) parseParam(paramStr string) (*MapTrackerMoveParam, error) {
	log.Debug().Msg("Parsing and validating parameters")

	// Parse parameters
	var param MapTrackerMoveParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}
	if len(param.MapName) == 0 {
		return nil, fmt.Errorf("map_name is required in parameters, got empty")
	}
	if len(param.Path) == 0 {
		return nil, fmt.Errorf("path is required in parameters, got empty")
	}

	// Validate parameters and set defaults
	if param.ArrivalThreshold < 0 {
		return nil, fmt.Errorf("arrival_threshold must be non-negative")
	} else if param.ArrivalThreshold == 0 {
		param.ArrivalThreshold = DEFAULT_MOVING_PARAM.ArrivalThreshold
	}

	if param.ArrivalTimeout < 0 {
		return nil, fmt.Errorf("arrival_timeout must be non-negative")
	} else if param.ArrivalTimeout == 0 {
		param.ArrivalTimeout = DEFAULT_MOVING_PARAM.ArrivalTimeout
	}

	if param.RotationLowerThreshold < 0 {
		return nil, fmt.Errorf("rotation_lower_threshold must be non-negative")
	} else if param.RotationLowerThreshold > 180 {
		return nil, fmt.Errorf("rotation_lower_threshold must be between 0 and 180 degrees")
	} else if param.RotationLowerThreshold == 0 {
		param.RotationLowerThreshold = DEFAULT_MOVING_PARAM.RotationLowerThreshold
	}

	if param.RotationUpperThreshold < 0 {
		return nil, fmt.Errorf("rotation_upper_threshold must be non-negative")
	} else if param.RotationUpperThreshold > 180 {
		return nil, fmt.Errorf("rotation_upper_threshold must be between 0 and 180 degrees")
	} else if param.RotationUpperThreshold == 0 {
		param.RotationUpperThreshold = DEFAULT_MOVING_PARAM.RotationUpperThreshold
	}

	if param.RotationSpeed < 0 {
		return nil, fmt.Errorf("rotation_speed must be non-negative")
	} else if param.RotationSpeed == 0 {
		param.RotationSpeed = DEFAULT_MOVING_PARAM.RotationSpeed
	}

	if param.RotationTimeout < 0 {
		return nil, fmt.Errorf("rotation_timeout must be non-negative")
	} else if param.RotationTimeout == 0 {
		param.RotationTimeout = DEFAULT_MOVING_PARAM.RotationTimeout
	}

	if param.SprintThreshold < 0 {
		return nil, fmt.Errorf("sprint_threshold must be non-negative")
	} else if param.SprintThreshold == 0 {
		param.SprintThreshold = DEFAULT_MOVING_PARAM.SprintThreshold
	}

	if param.StuckThreshold < 0 {
		return nil, fmt.Errorf("stuck_threshold must be non-negative")
	} else if param.StuckThreshold == 0 {
		param.StuckThreshold = DEFAULT_MOVING_PARAM.StuckThreshold
	}

	if param.StuckTimeout < 0 {
		return nil, fmt.Errorf("stuck_timeout must be non-negative")
	} else if param.StuckTimeout == 0 {
		param.StuckTimeout = DEFAULT_MOVING_PARAM.StuckTimeout
	}

	return &param, nil
}

func doEmergencyStop(aw *ActionWrapper, noPrint bool) {
	log.Warn().Msg("Emergency stop triggered")
	if !noPrint {
		maafocus.NodeActionStarting(aw.ctx, emergencyStopHTML)
	}
	aw.KeyUpSync(KEY_W, 100)
	aw.ctx.GetTasker().PostStop()
}

func doInfer(ctx *maa.Context, ctrl *maa.Controller, param *MapTrackerMoveParam) (*MapTrackerInferResult, error) {
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
				"map_name_regex": "^" + regexp.QuoteMeta(param.MapName) + "$",
				"precision":      DEFAULT_INFERENCE_PARAM_FOR_MOVE.Precision,
				"threshold":      DEFAULT_INFERENCE_PARAM_FOR_MOVE.Threshold,
			},
		},
	}

	res, err := ctx.RunRecognition(nodeName, img, config)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run MapTrackerInfer")
		return nil, err
	}
	if res == nil || res.DetailJson == "" || res.Hit == false {
		log.Error().Msg("Location inference not hit or result is empty")
		return nil, fmt.Errorf("location inference not hit or result is empty")
	}

	// Extract result
	var result MapTrackerInferResult
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
		log.Error().Err(err).Msg("Failed to unmarshal MapTrackerInferResult")
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
