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
	Path [][2]float64 `json:"path"`
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
	// SprintThreshold is the minimum distance beyond which sprinting is used.
	SprintThreshold float64 `json:"sprint_threshold,omitempty"`
	// StuckThreshold is the duration in milliseconds after which lack of movement is considered a stuck condition.
	StuckThreshold int64 `json:"stuck_threshold,omitempty"`
	// StuckTimeout is the maximum time in milliseconds to tolerate being stuck.
	StuckTimeout int64 `json:"stuck_timeout,omitempty"`
}

// PlayerMovement represents different movement state in the game
type PlayerMovement struct {
	Speed         float64 // Movement speed (px/s)
	RotationSpeed float64 // Rotation adjustment response speed (degrees/s)
}

var (
	MovementWalk   = PlayerMovement{2.0, 180.0}
	MovementRun    = PlayerMovement{8.0, 360.0}
	MovementSprint = PlayerMovement{12.0, 720.0}
)

// PlayerRotationAdjustmentState keeps track of one rotation adjustment
type PlayerRotationAdjustmentState struct {
	fromPos         [2]float64    // Last position where rotation adjustment started to apply
	fromRot         int           // Last rotation when rotation adjustment started to apply
	deltaRot        float64       // Last rotation difference to apply
	startTime       time.Time     // Last time when rotation adjustment started to apply
	expectedElapsed time.Duration // Expected time for this rotation adjustment to take effect
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
	loopInterval := time.Duration(INFER_INTERVAL_MS) * time.Millisecond

	if param.PathTrim && len(param.Path) > 1 {
		if initRes, err := doInfer(ctx, ctrl, param); err == nil && initRes != nil {
			closestIdx := 0
			minDist := math.MaxFloat64
			for i, p := range param.Path {
				dist := math.Hypot(initRes.X-p[0], initRes.Y-p[1])
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

	log.Info().Str("map", param.MapName).Int("targetsCount", len(param.Path)).Msg("Starting navigation to targets")

	// Reset player movement type by sprint once
	aw.KeyDownSync(KEY_S, 50)
	aw.KeyTypeSync(KEY_SHIFT, 50)
	aw.KeyUpSync(KEY_S, 50)
	aw.KeyTypeSync(KEY_W, 50)
	movement := &MovementRun

	// Adaptive rotation sensitivity local state
	rotationSpeed := ROTATION_DEFAULT_SPEED
	var rotAdjState, rotAdjStateCache *PlayerRotationAdjustmentState

	// For each target point
	for i, target := range param.Path {
		targetX, targetY := target[0], target[1]
		log.Info().Int("index", i).Float64("targetX", targetX).Float64("targetY", targetY).Msg("Navigating to next target point")

		// Show navigation UI
		var initDist float64
		var initRot int
		if initResult, err := doInfer(ctx, ctrl, param); err == nil && initResult != nil {
			initDist = math.Hypot(initResult.X-targetX, initResult.Y-targetY)
			initRot = calcTargetRotation(initResult.X, initResult.Y, targetX, targetY)
			if !param.NoPrint {
				maafocus.NodeActionStarting(
					aw.ctx,
					fmt.Sprintf(navigationMovingHTML, targetX, targetY, initDist),
				)
			}
		} else if err != nil {
			log.Debug().Err(err).Msg("Initial infer failed for moving UI")
		}

		var (
			lastLoopTime     = time.Time{}
			lastArrivalTime  = time.Now()
			prevLocationTime = time.Time{}
			prevLocation     *[2]float64
		)

		for {
			// Calculate time since last check
			loopElapsed := time.Since(lastLoopTime)
			if loopElapsed < loopInterval {
				time.Sleep(loopInterval - loopElapsed)
			}
			loopStartTime := time.Now()
			lastLoopTime = loopStartTime

			// Check stopping signal
			if ctx.GetTasker().Stopping() {
				log.Warn().Msg("Task is stopping, exiting navigation loop")
				aw.KeyUpSync(KEY_W, 25)
				return false
			}

			// Check arrival timeout
			deltaArrivalMs := loopStartTime.Sub(lastArrivalTime).Milliseconds()
			if deltaArrivalMs > param.ArrivalTimeout {
				log.Error().Msg("Arrival timeout, stopping task")
				doEmergencyStop(aw, param.NoPrint)
				return false
			}

			// Run inference to get current location and rotation
			result, err := doInfer(ctx, ctrl, param)
			if err != nil {
				log.Error().Err(err).Msg("Inference failed during navigation")
				aw.KeyUpSync(KEY_W, 25)
				continue
			}
			curX, curY := result.X, result.Y
			rot := result.Rot

			// Calculate rotation difference
			targetRot := calcTargetRotation(curX, curY, targetX, targetY)
			rawDeltaRot := calcDeltaRotation(rot, targetRot)
			absRawDeltaRot := math.Abs(float64(rawDeltaRot))

			// Check arrival
			dist := math.Hypot(curX-targetX, curY-targetY)
			isArrived := func() bool {
				if dist < param.ArrivalThreshold {
					log.Info().Float64("x", curX).Float64("y", curY).Int("index", i).Msg("Target point reached")
					return true
				}
				if math.Abs(float64(calcDeltaRotation(targetRot, initRot))) > 90.0 {
					log.Info().Int("targetRot", targetRot).Int("initRot", initRot).Int("index", i).Msg("Target point reached (guessed by rotation)")
					return true
				}
				return false
			}
			if isArrived() {
				// Peek next target's direction
				if i < len(param.Path)-1 {
					nextX, nextY := param.Path[i+1][0], param.Path[i+1][1]
					nextTargetRot := calcTargetRotation(curX, curY, nextX, nextY)
					nextDeltaRot := calcDeltaRotation(rot, nextTargetRot)
					// Pause slightly if next target is in a very different direction
					if math.Abs(float64(nextDeltaRot)) > param.RotationUpperThreshold {
						aw.KeyUpSync(KEY_W, 25)
					}
				}
				// Finish current target
				break
			}

			log.Debug().Float64("curX", curX).Float64("curY", curY).Int("curRot", rot).Float64("dist", dist).Int("targetRot", targetRot).Msg("Navigating to target")

			// Check Stuck
			if prevLocation != nil && prevLocation[0] == curX && prevLocation[1] == curY {
				deltaLocationMs := loopStartTime.Sub(prevLocationTime).Milliseconds()
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
				prevLocation = &[2]float64{curX, curY}
				prevLocationTime = loopStartTime
			}

			// Update adaptive rotation speed
			if rotAdjState != nil && (rotAdjStateCache == nil || rotAdjState.startTime.After(rotAdjStateCache.startTime)) {
				// Check if last rotation adjustment is completed
				if loopStartTime.Sub(rotAdjState.startTime) > rotAdjState.expectedElapsed {
					// Check if player is moving and rotating sufficiently to trust rotation measurement
					distTravel := math.Hypot(curX-rotAdjState.fromPos[0], curY-rotAdjState.fromPos[1])
					if distTravel > rotAdjState.expectedElapsed.Seconds()*MovementWalk.Speed {
						// Check if rotation difference is sufficient to consider adjusting rotation speed
						actualDeltaRot := calcDeltaRotation(rotAdjState.fromRot, rot)
						if math.Abs(float64(actualDeltaRot))+math.Abs(rotAdjState.deltaRot) > param.RotationLowerThreshold {
							idealRotSpeed := rotAdjState.deltaRot / (float64(actualDeltaRot) + 1e-6)
							if idealRotSpeed >= ROTATION_MIN_SPEED && idealRotSpeed <= ROTATION_MAX_SPEED {
								rotationSpeed = rotationSpeed*0.618 + idealRotSpeed*0.382
								rotAdjStateCache = rotAdjState
								log.Debug().
									Float64("idealRotSpeed", idealRotSpeed).
									Float64("newRotSpeed", rotationSpeed).
									Int("actualDeltaRot", actualDeltaRot).
									Float64("lastDeltaRot", rotAdjState.deltaRot).
									Msg("Adaptive rotation speed updated")
							}
						}
					}
				}
			}

			// Check if no active rotation adjustment
			if rotAdjState == nil || loopStartTime.Sub(rotAdjState.startTime) > rotAdjState.expectedElapsed {
				// Check if rotation is not good enough to sprint
				if absRawDeltaRot > param.RotationLowerThreshold {
					// Ensure no sprinting: forcibly set to 'walk'
					if movement.Speed > MovementRun.Speed {
						aw.KeyTypeSync(KEY_CTRL, 25)
						movement = &MovementWalk
					}
				} else {
					// Rotation is good: at least set to 'run'
					if movement.Speed < MovementRun.Speed {
						aw.KeyTypeSync(KEY_CTRL, 25)
						movement = &MovementRun
					}
					aw.KeyDownSync(KEY_W, 5)

					if dist > param.SprintThreshold {
						// Target is far enough: enable 'sprint'
						if movement.Speed < MovementSprint.Speed {
							aw.KeyTypeSync(KEY_SHIFT, 100)
							movement = &MovementSprint
						}
					}
				}

				// Start a new rotation adjustment
				if absRawDeltaRot > 1.0 {
					finalDeltaRot := float64(rawDeltaRot)

					// Select appropriate rotation method based on how bad the rotation is
					if absRawDeltaRot > param.RotationUpperThreshold {
						// Rotation is very bad: forcibly set to 'walk' for better control
						if movement.Speed > MovementWalk.Speed {
							aw.KeyTypeSync(KEY_CTRL, 25)
							movement = &MovementWalk
						}
						aw.RotateCamera(int(finalDeltaRot*rotationSpeed), 75, 25)
						aw.KeyDownSync(KEY_W, 25)
					} else {
						// Rotation is acceptable but can be improved: at least ensure 'run'
						if movement.Speed < MovementRun.Speed {
							aw.KeyTypeSync(KEY_CTRL, 25)
							movement = &MovementRun
						}
						aw.KeyDownSync(KEY_W, 25)
						aw.RotateCamera(int(finalDeltaRot*rotationSpeed), 75, 25)
					}

					// Update adaptive rotation state
					rotAdjState = &PlayerRotationAdjustmentState{
						fromPos:         [2]float64{curX, curY},
						fromRot:         rot,
						deltaRot:        finalDeltaRot,
						startTime:       time.Now(),
						expectedElapsed: time.Duration(float64(time.Second) * math.Abs(finalDeltaRot) / movement.RotationSpeed),
					}
					aw.ResetCamera(25)
				}
			}
		}
		// End of loop, one target reached
	}

	// End of all targets reached, stop movement
	aw.KeyUpSync(KEY_W, 25)

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
	for i, point := range param.Path {
		if math.IsNaN(point[0]) || math.IsInf(point[0], 0) || math.IsNaN(point[1]) || math.IsInf(point[1], 0) {
			return nil, fmt.Errorf("path[%d] contains invalid coordinate", i)
		}
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
	// Capture screen
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
	inferConfig := map[string]any{
		"map_name_regex": "^" + regexp.QuoteMeta(param.MapName) + "$",
		"precision":      DEFAULT_INFERENCE_PARAM_FOR_MOVE.Precision,
		"threshold":      DEFAULT_INFERENCE_PARAM_FOR_MOVE.Threshold,
	}

	inferConfigBytes, err := json.Marshal(inferConfig)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal inference config")
		return nil, err
	}

	taskDetail, err := ctx.GetTaskJob().GetDetail()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get task detail")
		return nil, err
	}

	resultJson, hit := mapTrackerInferRunner.Run(ctx, &maa.CustomRecognitionArg{
		TaskID:                 taskDetail.ID,
		CurrentTaskName:        taskDetail.Entry,
		CustomRecognitionName:  "MapTrackerInfer",
		CustomRecognitionParam: string(inferConfigBytes),
		Img:                    img,
		Roi:                    maa.Rect{0, 0, img.Bounds().Dx(), img.Bounds().Dy()},
	})

	if !hit {
		log.Error().Msg("Location inference not hit")
		return nil, fmt.Errorf("location inference not hit")
	}
	if resultJson == nil || resultJson.Detail == "" {
		log.Error().Msg("Location inference result is empty")
		return nil, fmt.Errorf("location inference result is empty")
	}

	// Extract result
	var result MapTrackerInferResult
	if err := json.Unmarshal([]byte(resultJson.Detail), &result); err != nil {
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
func calcTargetRotation(fromX, fromY, toX, toY float64) int {
	dx := toX - fromX
	dy := toY - fromY
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
