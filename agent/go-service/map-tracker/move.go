// Copyright (c) 2026 Harry Huang
package maptracker

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	mt "github.com/MaaXYZ/MaaEnd/agent/go-service/map-tracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
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
	// FineApproach controls when to enable fine approaching behavior. Valid values: "FinalTarget", "AllTargets", "Never".
	FineApproach string `json:"fine_approach,omitempty"`
	// MapNameMatchRule is the regex template used to match recognized map names. Use %s as map_name placeholder.
	MapNameMatchRule string `json:"map_name_match_rule,omitempty"`
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

const (
	FINE_APPROACH_FINAL_TARGET = "FinalTarget"
	FINE_APPROACH_ALL_TARGETS  = "AllTargets"
	FINE_APPROACH_NEVER        = "Never"
)

var mapTrackerMoveDefaultParam = MapTrackerMoveParam{
	FineApproach:           FINE_APPROACH_FINAL_TARGET,
	MapNameMatchRule:       "^%s(_tier_\\w+)?$",
	ArrivalThreshold:       2.5,
	ArrivalTimeout:         60000,
	RotationLowerThreshold: 7.5,
	RotationUpperThreshold: 60.0,
	SprintThreshold:        20.0,
	StuckThreshold:         2000,
	StuckTimeout:           10000,
}

var mapTrackerInferParamForMove = MapTrackerInferParam{
	Precision: 0.7,
	Threshold: 0.3,
}

// PlayerMovement represents different movement state in the game
type PlayerMovement struct {
	Speed         float64 // Movement speed (px/s)
	RotationSpeed float64 // Rotation adjustment response speed (degrees/s)
}

var (
	MovementWalk   = PlayerMovement{2.0, 270.0}
	MovementRun    = PlayerMovement{8.0, 540.0}
	MovementSprint = PlayerMovement{12.0, 1080.0}
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

var previewMapCache = struct {
	mu  sync.RWMutex
	key string
	img *image.RGBA
}{}

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
	ca := control.NewControlAdaptor(ctx, ctrl, mt.WORK_W, mt.WORK_H)
	loopInterval := time.Duration(mt.INFER_INTERVAL_MS) * time.Millisecond

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
	ca.KeyDown(control.KEY_S, 50)
	ca.KeyType(control.KEY_SHIFT, 50)
	ca.KeyUp(control.KEY_S, 50)
	ca.KeyType(control.KEY_W, 50)
	movement := &MovementRun

	// Adaptive rotation sensitivity local state
	rotationSpeed := mt.ROTATION_DEFAULT_SPEED
	var rotAdjState, rotAdjStateCache *PlayerRotationAdjustmentState

	// For each target point
	for i, target := range param.Path {
		targetX, targetY := target[0], target[1]
		enableFineApproach := (param.FineApproach == FINE_APPROACH_ALL_TARGETS) ||
			(param.FineApproach == FINE_APPROACH_FINAL_TARGET && i == len(param.Path)-1)
		log.Info().Int("index", i).Float64("targetX", targetX).Float64("targetY", targetY).Msg("Navigating to next target point")

		// Show navigation UI
		var initRot int
		if initResult, err := doInfer(ctx, ctrl, param); err == nil && initResult != nil {
			initRot = calcTargetRotation(initResult.X, initResult.Y, targetX, targetY)
			if !param.NoPrint {
				maafocus.NodeActionStarting(
					ca.Ctx(),
					a.buildNavigationMovingHTML(param, i, initResult.X, initResult.Y, targetX, targetY),
				)
			}
		} else if err != nil {
			log.Debug().Err(err).Msg("Initial infer failed for moving UI")
		}

		var (
			lastLoopTime                = time.Time{}
			lastArrivalTime             = time.Now()
			prevLocationTime            = time.Time{}
			prevLocation                *[2]float64
			fineApproachOngoing         = false
			fineApproachExpectedEndTime = time.Time{}
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
				ca.KeyUp(control.KEY_W, 25)
				return false
			}

			// Check arrival timeout
			deltaArrivalMs := loopStartTime.Sub(lastArrivalTime).Milliseconds()
			if deltaArrivalMs > param.ArrivalTimeout {
				if fineApproachOngoing {
					log.Warn().Msg("Fine approach timeout, ending fine approach")
					break
				} else {
					log.Error().Msg("Arrival timeout, stopping task")
					doEmergencyStop(ca, param.NoPrint)
					return false
				}
			}

			// Run inference to get current location and rotation
			result, err := doInfer(ctx, ctrl, param)
			if err != nil {
				log.Error().Err(err).Msg("Inference failed during navigation")
				ca.KeyUp(control.KEY_W, 25)
				continue
			}
			curX, curY := result.X, result.Y
			rot := result.Rot

			// Calculate rotation difference
			targetRot := calcTargetRotation(curX, curY, targetX, targetY)
			rawDeltaRot := calcDeltaRotation(rot, targetRot)
			absRawDeltaRot := math.Abs(float64(rawDeltaRot))

			// Check arrival
			finishCurrentTarget := func(curX, curY float64, rot int) {
				if i < len(param.Path)-1 {
					nextX, nextY := param.Path[i+1][0], param.Path[i+1][1]
					nextTargetRot := calcTargetRotation(curX, curY, nextX, nextY)
					nextDeltaRot := calcDeltaRotation(rot, nextTargetRot)
					// Pause slightly if next target is in a very different direction
					if math.Abs(float64(nextDeltaRot)) > param.RotationUpperThreshold {
						ca.KeyUp(control.KEY_W, 25)
					}
				}
			}
			dist := math.Hypot(curX-targetX, curY-targetY)
			if fineApproachOngoing {
				if loopStartTime.After(fineApproachExpectedEndTime) || dist < mt.FINE_APPROACH_COMPLETE_THRESHOLD {
					log.Info().Int("index", i).Float64("dist", dist).Msg("Target point reached (fine approach)")
					finishCurrentTarget(curX, curY, rot)
					break
				}
			} else {
				if dist < param.ArrivalThreshold {
					if enableFineApproach {
						fineApproachOngoing = true
						fineApproachExpectedElapsed := time.Duration(float64(time.Second) * (dist / MovementWalk.Speed))
						fineApproachExpectedEndTime = loopStartTime.Add(fineApproachExpectedElapsed)
						if movement.Speed > MovementWalk.Speed {
							ca.KeyType(control.KEY_CTRL, 25)
							movement = &MovementWalk
						}
						ca.KeyDown(control.KEY_W, 25)
						log.Info().Int("index", i).Float64("dist", dist).Dur("expectedElapsed", fineApproachExpectedElapsed).Msg("Entering fine approach")
					} else {
						log.Info().Int("index", i).Float64("x", curX).Float64("y", curY).Msg("Target point reached (ordinary approach)")
						finishCurrentTarget(curX, curY, rot)
						break
					}
				} else if math.Abs(float64(calcDeltaRotation(targetRot, initRot))) > 90.0 {
					log.Info().Int("targetRot", targetRot).Int("initRot", initRot).Int("index", i).Msg("Target point reached (guessed by rotation)")
					finishCurrentTarget(curX, curY, rot)
					break
				}
			}

			log.Debug().Float64("curX", curX).Float64("curY", curY).Int("curRot", rot).Float64("dist", dist).Int("targetRot", targetRot).Msg("Navigating to target")

			// Check Stuck
			if prevLocation != nil && math.Hypot(prevLocation[0]-curX, prevLocation[1]-curY) < 1.0 {
				deltaLocationMs := loopStartTime.Sub(prevLocationTime).Milliseconds()
				if deltaLocationMs > param.StuckTimeout {
					log.Error().Msg("Stuck for too long, stopping task")
					doEmergencyStop(ca, param.NoPrint)
					return false
				}
				if deltaLocationMs > param.StuckThreshold {
					log.Info().Msg("Stuck detected, jumping...")
					ca.KeyType(control.KEY_SPACE, 100)
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
							if idealRotSpeed >= mt.ROTATION_MIN_SPEED && idealRotSpeed <= mt.ROTATION_MAX_SPEED {
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
						ca.KeyType(control.KEY_CTRL, 25)
						movement = &MovementWalk
					}
				} else if !fineApproachOngoing {
					// Rotation is good: at least set to 'run'
					if movement.Speed < MovementRun.Speed {
						ca.KeyType(control.KEY_CTRL, 25)
						movement = &MovementRun
					}
					ca.KeyDown(control.KEY_W, 5)

					if dist > param.SprintThreshold {
						// Target is far enough: enable 'sprint'
						if movement.Speed < MovementSprint.Speed {
							ca.KeyType(control.KEY_SHIFT, 100)
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
							ca.KeyType(control.KEY_CTRL, 25)
							movement = &MovementWalk
						}
						ca.RotateCamera(int(finalDeltaRot*rotationSpeed), 0, 75, 25)
						ca.KeyDown(control.KEY_W, 25)
					} else if !fineApproachOngoing {
						// Rotation is acceptable but can be improved: at least ensure 'run'
						if movement.Speed < MovementRun.Speed {
							ca.KeyType(control.KEY_CTRL, 25)
							movement = &MovementRun
						}
						ca.KeyDown(control.KEY_W, 25)
						ca.RotateCamera(int(finalDeltaRot*rotationSpeed), 0, 75, 25)
					}

					// Update adaptive rotation state
					rotAdjState = &PlayerRotationAdjustmentState{
						fromPos:         [2]float64{curX, curY},
						fromRot:         rot,
						deltaRot:        finalDeltaRot,
						startTime:       time.Now(),
						expectedElapsed: time.Duration(float64(time.Second) * math.Abs(finalDeltaRot) / movement.RotationSpeed),
					}
					ca.RotateCameraEliminateSideEffect(25)
				}
			}
		}
		// End of loop, one target reached
	}

	// End of all targets reached, stop movement and reset to running mode
	ca.KeyUp(control.KEY_W, 25)
	if movement.Speed < MovementRun.Speed {
		ca.KeyType(control.KEY_CTRL, 25)
	}

	// Show finished UI summary
	if !param.NoPrint {
		finishedX, finishedY := 0.0, 0.0
		if len(param.Path) > 0 {
			finishedX, finishedY = param.Path[len(param.Path)-1][0], param.Path[len(param.Path)-1][1]
		}
		if finalInfer, err := doInfer(ctx, ctrl, param); err == nil && finalInfer != nil {
			finishedX, finishedY = finalInfer.X, finalInfer.Y
		}
		maafocus.NodeActionStarting(
			ca.Ctx(),
			a.buildNavigationFinishedHTML(param, finishedX, finishedY),
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
		param.ArrivalThreshold = mapTrackerMoveDefaultParam.ArrivalThreshold
	}

	if param.ArrivalTimeout < 0 {
		return nil, fmt.Errorf("arrival_timeout must be non-negative")
	} else if param.ArrivalTimeout == 0 {
		param.ArrivalTimeout = mapTrackerMoveDefaultParam.ArrivalTimeout
	}

	if len(param.FineApproach) == 0 {
		param.FineApproach = mapTrackerMoveDefaultParam.FineApproach
	}
	switch param.FineApproach {
	case FINE_APPROACH_FINAL_TARGET, FINE_APPROACH_ALL_TARGETS, FINE_APPROACH_NEVER:
		// valid
	default:
		return nil, fmt.Errorf("fine_approach must be one of %q, %q, %q", FINE_APPROACH_FINAL_TARGET, FINE_APPROACH_ALL_TARGETS, FINE_APPROACH_NEVER)
	}

	if len(param.MapNameMatchRule) == 0 {
		param.MapNameMatchRule = mapTrackerMoveDefaultParam.MapNameMatchRule
	}
	mapNameRegex := buildMapNameRegex(param.MapNameMatchRule, param.MapName)
	if _, err := regexp.Compile(mapNameRegex); err != nil {
		return nil, fmt.Errorf("map_name_match_rule produced invalid regex %q: %w", mapNameRegex, err)
	}

	if param.RotationLowerThreshold < 0 {
		return nil, fmt.Errorf("rotation_lower_threshold must be non-negative")
	} else if param.RotationLowerThreshold > 180 {
		return nil, fmt.Errorf("rotation_lower_threshold must be between 0 and 180 degrees")
	} else if param.RotationLowerThreshold == 0 {
		param.RotationLowerThreshold = mapTrackerMoveDefaultParam.RotationLowerThreshold
	}

	if param.RotationUpperThreshold < 0 {
		return nil, fmt.Errorf("rotation_upper_threshold must be non-negative")
	} else if param.RotationUpperThreshold > 180 {
		return nil, fmt.Errorf("rotation_upper_threshold must be between 0 and 180 degrees")
	} else if param.RotationUpperThreshold == 0 {
		param.RotationUpperThreshold = mapTrackerMoveDefaultParam.RotationUpperThreshold
	}

	if param.SprintThreshold < 0 {
		return nil, fmt.Errorf("sprint_threshold must be non-negative")
	} else if param.SprintThreshold == 0 {
		param.SprintThreshold = mapTrackerMoveDefaultParam.SprintThreshold
	}

	if param.StuckThreshold < 0 {
		return nil, fmt.Errorf("stuck_threshold must be non-negative")
	} else if param.StuckThreshold == 0 {
		param.StuckThreshold = mapTrackerMoveDefaultParam.StuckThreshold
	}

	if param.StuckTimeout < 0 {
		return nil, fmt.Errorf("stuck_timeout must be non-negative")
	} else if param.StuckTimeout == 0 {
		param.StuckTimeout = mapTrackerMoveDefaultParam.StuckTimeout
	}

	return &param, nil
}

func doEmergencyStop(ca control.ControlAdaptor, noPrint bool) {
	log.Warn().Msg("Emergency stop triggered")
	if !noPrint {
		maafocus.NodeActionStarting(ca.Ctx(), emergencyStopHTML)
	}
	ca.KeyUp(control.KEY_W, 100)
	ca.Ctx().GetTasker().PostStop()
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
	mapNameRegex := buildMapNameRegex(param.MapNameMatchRule, param.MapName)
	inferConfig := map[string]any{
		"map_name_regex": mapNameRegex,
		"precision":      mapTrackerInferParamForMove.Precision,
		"threshold":      mapTrackerInferParamForMove.Threshold,
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

func buildMapNameRegex(rule string, mapName string) string {
	escapedName := regexp.QuoteMeta(mapName)
	if strings.Contains(rule, "%s") {
		return fmt.Sprintf(rule, escapedName)
	}
	return rule
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

func (a *MapTrackerMove) buildNavigationMovingHTML(
	param *MapTrackerMoveParam,
	targetIndex int,
	currentX float64,
	currentY float64,
	targetX float64,
	targetY float64,
) string {
	previewImageURL := buildNavigationPreviewDataURL(param.Path, targetIndex, param.MapName, currentX, currentY, targetX, targetY)

	return fmt.Sprintf(navigationMovingHTML,
		targetIndex+1,
		len(param.Path),
		currentX,
		currentY,
		targetX,
		targetY,
		previewImageURL,
	)
}

func (a *MapTrackerMove) buildNavigationFinishedHTML(param *MapTrackerMoveParam, currentX, currentY float64) string {
	targetX, targetY := currentX, currentY
	targetIndex := 0
	if len(param.Path) > 0 {
		targetIndex = len(param.Path) - 1
		targetX = param.Path[targetIndex][0]
		targetY = param.Path[targetIndex][1]
	}

	previewImageURL := buildNavigationPreviewDataURL(param.Path, targetIndex, param.MapName, currentX, currentY, targetX, targetY)

	return fmt.Sprintf(
		navigationFinishedHTML,
		len(param.Path),
		len(param.Path),
		currentX,
		currentY,
		previewImageURL,
	)
}

func buildNavigationPreviewDataURL(path [][2]float64, targetIndex int, mapName string, currentX, currentY, targetX, targetY float64) string {
	// Prepare map image
	mapRGBA, err := getCachedPreviewMapRGBA(mapName)
	if err != nil {
		log.Debug().Err(err).Str("map", mapName).Msg("Failed to load map image for moving preview")
		return ""
	}

	// Prepare points to focus on
	focusPoints := make([][2]float64, 0, 9)
	if len(path) > 0 {
		start := max(0, targetIndex-4)
		end := min(len(path)-1, targetIndex+4)
		focusPoints = append(focusPoints, path[start:end+1]...)
	}
	if len(focusPoints) == 0 {
		focusPoints = append(focusPoints, [2]float64{targetX, targetY})
	}

	drawPath := path
	if len(drawPath) == 0 {
		drawPath = focusPoints
	}

	// Calculate geometry and crop map image
	const canvasSize = 192

	scale, offsetX, offsetY,
		currentViewX, currentViewY := calcNavigationPreviewGeometry(focusPoints, currentX, currentY, canvasSize, 96, 192)
	if scale <= 0 {
		scale = 1.0
	}

	canvas := image.NewRGBA(image.Rect(0, 0, canvasSize, canvasSize))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{0xf7, 0xfb, 0xff, 0xff}}, image.Point{}, draw.Src)

	b := mapRGBA.Bounds()
	srcMinX := int(math.Floor((-offsetX) / scale))
	srcMinY := int(math.Floor((-offsetY) / scale))
	srcMaxX := int(math.Ceil((float64(canvasSize) - offsetX) / scale))
	srcMaxY := int(math.Ceil((float64(canvasSize) - offsetY) / scale))
	srcMinX = max(b.Min.X, srcMinX)
	srcMinY = max(b.Min.Y, srcMinY)
	srcMaxX = min(b.Max.X, srcMaxX)
	srcMaxY = min(b.Max.Y, srcMaxY)

	if srcMaxX <= srcMinX || srcMaxY <= srcMinY {
		srcMinX, srcMinY, srcMaxX, srcMaxY = b.Min.X, b.Min.Y, b.Max.X, b.Max.Y
	}

	srcRect := image.Rect(srcMinX, srcMinY, srcMaxX, srcMaxY)
	cropped := minicv.ImageCropRect(mapRGBA, srcRect)
	scaledCrop := minicv.ImageScale(cropped, scale)
	dstMinX := int(math.Round(offsetX + float64(srcRect.Min.X)*scale))
	dstMinY := int(math.Round(offsetY + float64(srcRect.Min.Y)*scale))
	dstRect := image.Rect(dstMinX, dstMinY, dstMinX+scaledCrop.Bounds().Dx(), dstMinY+scaledCrop.Bounds().Dy())
	draw.Draw(canvas, dstRect, scaledCrop, image.Point{}, draw.Over)

	// Draw path and points
	var (
		colorRed   = color.RGBA{0xdb, 0x39, 0x2b, 0xff} // 0xdb392b
		colorGreen = color.RGBA{0x27, 0xce, 0x60, 0xff} // 0x27ce60
		colorBlue  = color.RGBA{0x2b, 0x62, 0xc0, 0xff} // 0x2b62c0
	)

	for i := 0; i+1 < len(drawPath); i++ {
		x1 := int(math.Round(drawPath[i][0]*scale + offsetX))
		y1 := int(math.Round(drawPath[i][1]*scale + offsetY))
		x2 := int(math.Round(drawPath[i+1][0]*scale + offsetX))
		y2 := int(math.Round(drawPath[i+1][1]*scale + offsetY))
		minicv.ImageDrawLine(canvas, x1, y1, x2, y2, colorBlue, 3)
	}

	for _, p := range drawPath {
		x := int(math.Round(p[0]*scale + offsetX))
		y := int(math.Round(p[1]*scale + offsetY))
		minicv.ImageDrawFilledCircle(canvas, x, y, 4, colorBlue)
	}

	curX := int(math.Round(currentViewX))
	curY := int(math.Round(currentViewY))
	tgtX := int(math.Round(targetX*scale + offsetX))
	tgtY := int(math.Round(targetY*scale + offsetY))
	minicv.ImageDrawLine(canvas, curX, curY, tgtX, tgtY, colorRed, 3)
	minicv.ImageDrawFilledCircle(canvas, tgtX, tgtY, 5, colorRed)
	minicv.ImageDrawFilledCircle(canvas, curX, curY, 5, colorGreen)

	// Return as base64 data URL
	base64JPEG, err := minicv.ImageToBase64JPEG(canvas, 90)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to encode moving preview image")
		return ""
	}

	return "data:image/jpeg;base64," + base64JPEG
}

func getCachedPreviewMapRGBA(mapName string) (*image.RGBA, error) {
	mapPath := mt.FindResource(filepath.ToSlash(filepath.Join(mt.MAP_DIR, mapName+".png")))
	if mapPath == "" {
		return nil, fmt.Errorf("map image not found")
	}

	previewMapCache.mu.RLock()
	if previewMapCache.key == mapPath && previewMapCache.img != nil {
		cached := previewMapCache.img
		previewMapCache.mu.RUnlock()
		return cached, nil
	}
	previewMapCache.mu.RUnlock()

	f, err := os.Open(mapPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	decoded, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}

	previewMapCache.mu.Lock()
	previewMapCache.key = mapPath
	img := minicv.ImageConvertRGBA(decoded)
	previewMapCache.img = img
	previewMapCache.mu.Unlock()
	return img, nil
}

func calcNavigationPreviewGeometry(focusPoints [][2]float64, currentX, currentY float64, canvasSize int, minSize int, maxSize int) (
	scale, offsetX, offsetY,
	currentViewX, currentViewY float64,
) {
	if canvasSize < 1 {
		canvasSize = 1
	}
	if minSize < 1 {
		minSize = 1
	}
	if maxSize < minSize {
		maxSize = minSize
	}

	previewSize := float64(canvasSize)
	minSpan := float64(minSize)
	maxSpan := float64(maxSize)

	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	update := func(x, y float64) {
		if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			return
		}
		minX = math.Min(minX, x)
		minY = math.Min(minY, y)
		maxX = math.Max(maxX, x)
		maxY = math.Max(maxY, y)
	}
	for _, p := range focusPoints {
		update(p[0], p[1])
	}
	update(currentX, currentY)

	if math.IsNaN(minX) || math.IsInf(minX, 0) ||
		math.IsNaN(minY) || math.IsInf(minY, 0) ||
		math.IsNaN(maxX) || math.IsInf(maxX, 0) ||
		math.IsNaN(maxY) || math.IsInf(maxY, 0) {
		minX, minY = 0, 0
		maxX, maxY = previewSize, previewSize
	}

	spanX := min(max(maxX-minX, minSpan), maxSpan)
	spanY := min(max(maxY-minY, minSpan), maxSpan)
	scale = math.Min(previewSize/spanX, previewSize/spanY)

	centerX := (minX + maxX) * 0.5
	centerY := (minY + maxY) * 0.5
	offsetX = previewSize*0.5 - centerX*scale
	offsetY = previewSize*0.5 - centerY*scale

	currentViewX = currentX*scale + offsetX
	currentViewY = currentY*scale + offsetY

	return
}
