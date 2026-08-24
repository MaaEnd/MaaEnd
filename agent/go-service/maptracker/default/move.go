// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	internal "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type MapTrackerMove struct{}

// MapTrackerMoveParam represents the custom_action_param for MapTrackerMove
type MapTrackerMoveParam struct {
	// MapName is the name of the map to navigate (required).
	MapName string `json:"map_name"`
	// Path is a sequence of [x, y] coordinate points to follow (required).
	Path []internal.Point `json:"path"`
	// NoPrint controls whether to suppress printing navigation status to the GUI.
	NoPrint bool `json:"no_print,omitempty"`
	// PathTrim trims the path to start from the nearest point to the current location when enabled.
	PathTrim bool `json:"path_trim,omitempty"`
	// FineApproach controls when to enable fine approaching behavior. Valid values: "FinalTarget", "AllTargets", "Never".
	FineApproach string `json:"fine_approach,omitempty"`
	// OnFinish is an inline pipeline node object executed once after the navigation succeeds.
	OnFinish map[string]any `json:"on_finish,omitempty"`
	// NoEnsureInitialMovementState controls whether to skip ensuring the movement state when starting the initial movement.
	NoEnsureInitialMovementState bool `json:"no_ensure_initial_movement_state,omitempty"`
	// ArrivalThreshold is the minimum distance to consider a target reached.
	ArrivalThreshold float64 `json:"arrival_threshold,omitempty"`
	// ArrivalTimeout is the maximum allowed time in milliseconds to reach each target point.
	ArrivalTimeout int64 `json:"arrival_timeout,omitempty"`
	// RotationLowerThreshold is the minimum angular difference in degrees to trigger rotation adjustment.
	RotationLowerThreshold float64 `json:"rotation_lower_threshold,omitempty"`
	// RotationUpperThreshold is the angular difference in degrees above which a more aggressive correction is applied.
	RotationUpperThreshold float64 `json:"rotation_upper_threshold,omitempty"`
	// RotationSlowerThreshold is the angular difference below which rotation uses the slower speed.
	RotationSlowerThreshold float64 `json:"rotation_slower_threshold,omitempty"`
	// RotationFasterThreshold is the angular difference above which rotation uses the adaptive full speed.
	RotationFasterThreshold float64 `json:"rotation_faster_threshold,omitempty"`
	// SprintThreshold is the minimum distance beyond which sprinting is used.
	SprintThreshold float64 `json:"sprint_threshold,omitempty"`
	// StuckThreshold is the duration in milliseconds after which lack of movement is considered a stuck condition.
	StuckThreshold int64 `json:"stuck_threshold,omitempty"`
	// StuckTimeout is the maximum time in milliseconds to tolerate being stuck.
	StuckTimeout int64 `json:"stuck_timeout,omitempty"`
	// StuckMitigators controls the sequential actions to take when a stuck condition is detected.
	// Actions are cycled in order on each stuck event. Valid actions: "Jump", "MoveOrDeleteDevice".
	StuckMitigators []string `json:"stuck_mitigators,omitempty"`
	// MapNameMatchRule is the regex template used to match recognized map names. Use %s as map_name placeholder.
	MapNameMatchRule string `json:"map_name_match_rule,omitempty"`
}

const (
	FINE_APPROACH_FINAL_TARGET = "FinalTarget"
	FINE_APPROACH_ALL_TARGETS  = "AllTargets"
	FINE_APPROACH_NEVER        = "Never"
)

var mapTrackerMoveDefaultParam = MapTrackerMoveParam{
	FineApproach:            FINE_APPROACH_FINAL_TARGET,
	MapNameMatchRule:        "^%s(_tier_\\w+)?$",
	ArrivalThreshold:        2.5,
	ArrivalTimeout:          60000,
	RotationLowerThreshold:  3.0,
	RotationUpperThreshold:  60.0,
	RotationSlowerThreshold: 15.0,
	RotationFasterThreshold: 90.0,
	SprintThreshold:         10.0,
	StuckThreshold:          2000,
	StuckTimeout:            10000,
	StuckMitigators:         []string{"MoveOrDeleteDevice", "Jump"},
}

// fineApproachOutcome describes how a fine approach ended.
type fineApproachOutcome int

const (
	fineApproachReached fineApproachOutcome = iota
	fineApproachExhausted
	fineApproachDiverged
	fineApproachAborted
)

// PlayerRotationAdjustmentState keeps track of one rotation adjustment
type PlayerRotationAdjustmentState struct {
	fromPos         internal.Point // Last position where rotation adjustment started to apply
	fromRot         int            // Last rotation when rotation adjustment started to apply
	deltaRot        float64        // Last rotation difference to apply
	startTime       time.Time      // Last time when rotation adjustment started to apply
	expectedElapsed time.Duration  // Expected time for this rotation adjustment to take effect
}

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
	ctrlType, _ := control.GetControlType(ctrl)
	ca, err := control.NewControlAdaptor(ctx, ctrl, WORK_W, WORK_H)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create control adaptor")
		return false
	}

	loopInterval := time.Duration(INFER_INTERVAL_MS) * time.Millisecond

	if param.PathTrim && len(param.Path) > 1 {
		if initRes, err := doInfer(ctx, ctrl, param); err == nil && initRes != nil {
			closestIdx := 0
			minDist := math.MaxFloat64
			for i, p := range param.Path {
				dist := initRes.Loc.DistanceTo(p)
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

	// Start of all targets, reset cursor and initial movement state
	ca.ResetCursor(control.CursorResetActive)
	if !param.NoEnsureInitialMovementState {
		// Reset player movement state
		ca.AggressivelyResetPlayerMovement()
	}

	// Adaptive rotation sensitivity local state
	rotationSpeed := ROTATION_DEFAULT_SPEED
	if ctrlType == control.CONTROL_TYPE_LINUX {
		rotationSpeed = ROTATION_DEFAULT_SPEED_LINUX
	}
	var rotAdjState, rotAdjStateCache *PlayerRotationAdjustmentState

	// For each target point
	for i, targetLoc := range param.Path {
		enableFineApproach := (param.FineApproach == FINE_APPROACH_ALL_TARGETS) ||
			(param.FineApproach == FINE_APPROACH_FINAL_TARGET && i == len(param.Path)-1)
		log.Info().Int("index", i).Interface("target", targetLoc).Msg("Navigating to next target point")

		// Show navigation UI
		var initRot int
		if initResult, err := doInfer(ctx, ctrl, param); err == nil && initResult != nil {
			initRot = int(initResult.Loc.AngleTo(targetLoc))
			if !param.NoPrint {
				maafocus.PrintLargeContentTrimNewline(
					a.buildNavigationMovingHTML(param, i, initResult.Loc, targetLoc),
				)
			}
		} else if err != nil {
			log.Debug().Err(err).Msg("Initial infer failed for moving UI")
		}

		var (
			lastLoopTime      = time.Time{}
			lastArrivalTime   = time.Now()
			prevLocationTime  = time.Time{}
			prevLocation      *internal.Point
			stuckMitigatorIdx = 0
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
				doPlayerStop(ca)
				return false
			}

			// Check arrival timeout
			deltaArrivalMs := loopStartTime.Sub(lastArrivalTime).Milliseconds()
			if deltaArrivalMs > param.ArrivalTimeout {
				log.Error().Msg("Arrival timeout, stopping task")
				doEmergencyStop(ca, param.NoPrint)
				return false
			}

			// Run inference to get current location and rotation
			result, err := doInfer(ctx, ctrl, param)
			if err != nil {
				log.Error().Err(err).Msg("Inference failed during navigation")
				ca.SetPlayerMovement(control.MovementStop, control.PolicyDefault)
				continue
			}
			curLoc := result.Loc
			curRot := result.Rot

			// Calculate rotation difference
			targetRot := int(math.Round(curLoc.AngleTo(targetLoc)))
			deltaRot := internal.DeltaRotation(curRot, targetRot)

			// Check arrival
			dist := curLoc.DistanceTo(targetLoc)
			reachedByRotation := math.Abs(internal.DeltaRotation(targetRot, initRot)) > 90.0
			if dist < param.ArrivalThreshold || reachedByRotation {
				if reachedByRotation {
					log.Info().Int("index", i).Float64("dist", dist).Int("targetRot", targetRot).Int("initRot", initRot).Msg("Target point reached (ordinary approach, guessed by rotation)")
				} else {
					log.Info().Int("index", i).Float64("dist", dist).Object("pos", curLoc).Msg("Target point reached (ordinary approach)")
				}

				if enableFineApproach {
					runFineApproach(ctx, ctrl, ca, param, targetLoc, curRot)
				}

				break
			}

			log.Debug().Object("cur", curLoc).Int("curRot", curRot).Float64("dist", dist).Int("targetRot", targetRot).Msg("Navigating to target")

			// Check stuck
			if prevLocation != nil && prevLocation.DistanceTo(curLoc) < 2.0 {
				deltaLocationMs := loopStartTime.Sub(prevLocationTime).Milliseconds()
				if deltaLocationMs > param.StuckTimeout {
					log.Error().Msg("Stuck for too long, stopping task")
					doEmergencyStop(ca, param.NoPrint)
					return false
				}
				if deltaLocationMs > param.StuckThreshold {
					if len(param.StuckMitigators) > 0 {
						action := param.StuckMitigators[stuckMitigatorIdx%len(param.StuckMitigators)]
						stuckMitigatorIdx++
						executeStuckMitigator(ctx, ca, action)
					} else {
						log.Debug().Msg("Stuck but no mitigators configured, skipping mitigation")
					}
				}
			} else {
				prevLocation = &curLoc
				prevLocationTime = loopStartTime
			}

			// Update adaptive rotation speed
			if rotAdjState != nil &&
				// ...only if the old rotation adjustment state cache is expired
				(rotAdjStateCache == nil || rotAdjState.startTime.After(rotAdjStateCache.startTime)) &&
				// ...and the current rotation adjustment is completed
				loopStartTime.Sub(rotAdjState.startTime) > rotAdjState.expectedElapsed {

				// Check if player is moving and rotating sufficiently to trust rotation measurement
				distTravel := curLoc.DistanceTo(rotAdjState.fromPos)
				if distTravel > control.MovementWalk.DistanceDuring(rotAdjState.expectedElapsed) {
					// Check if rotation difference is sufficient to consider adjusting rotation speed
					actualDeltaRot := internal.DeltaRotation(rotAdjState.fromRot, curRot)
					if math.Abs(actualDeltaRot) > param.RotationLowerThreshold && math.Abs(rotAdjState.deltaRot) > param.RotationLowerThreshold {
						idealRotSpeed := rotationSpeed * rotAdjState.deltaRot / (actualDeltaRot + 1e-6)
						if idealRotSpeed >= ROTATION_MIN_SPEED && idealRotSpeed <= ROTATION_MAX_SPEED {
							learningRate := internal.Lerp(
								0.135,
								0.382,
								internal.UnitRamp(math.Abs(actualDeltaRot), param.RotationSlowerThreshold, param.RotationFasterThreshold),
							)
							rotationSpeed = internal.Lerp(rotationSpeed, idealRotSpeed, learningRate)
							rotAdjStateCache = rotAdjState
							log.Debug().
								Float64("idealRotSpeed", idealRotSpeed).
								Float64("newRotSpeed", rotationSpeed).
								Float64("actualDeltaRot", actualDeltaRot).
								Float64("lastDeltaRot", rotAdjState.deltaRot).
								Msg("Adaptive rotation speed updated")
						}
					}
				}
			}

			// Check if no active rotation adjustment
			if rotAdjState == nil || loopStartTime.Sub(rotAdjState.startTime) > rotAdjState.expectedElapsed {
				// Check if rotation is not good enough to sprint now
				absDeltaRot := math.Abs(deltaRot)
				if ca.GetPlayerMovement().Equals(control.MovementSprint) {
					if absDeltaRot > param.RotationLowerThreshold {
						// Ensure no sprinting: forcibly set to 'walk'
						ca.SetPlayerMovement(control.MovementWalk, control.PolicyDefault)
					}
				}

				// Reselect movement speed
				if absDeltaRot > param.RotationUpperThreshold {
					// Rotation is bad: set to 'walk'
					ca.SetPlayerMovement(control.MovementWalk, control.PolicyDefault)
				} else if absDeltaRot > param.RotationLowerThreshold {
					// Rotation is good: at least set to 'run'
					ca.SetPlayerMovement(control.MovementRun, control.PolicyDefault)
				} else {
					// Rotation is very good: can try 'sprint' if target is far enough
					if dist > param.SprintThreshold {
						ca.SetPlayerMovement(control.MovementSprint, control.PolicyDefault)
					} else {
						ca.SetPlayerMovement(control.MovementRun, control.PolicyDefault)
					}
				}

				// Calculate rotation adjustment parameters
				finalRotSpeed := internal.Lerp(
					math.Sqrt(rotationSpeed),
					rotationSpeed,
					internal.UnitRamp(absDeltaRot, param.RotationSlowerThreshold, param.RotationFasterThreshold),
				)
				cameraShift := int(deltaRot * finalRotSpeed)

				// Start a new rotation adjustment
				if cameraShift != 0 {
					ca.RotateCamera(cameraShift, 0)

					// Update adaptive rotation state
					rotAdjState = &PlayerRotationAdjustmentState{
						fromPos:         curLoc,
						fromRot:         curRot,
						deltaRot:        deltaRot,
						startTime:       time.Now(),
						expectedElapsed: ca.GetPlayerMovement().EtaOfRotation(absDeltaRot),
					}
					ca.ResetCursor(control.CursorResetLazy)
				}
			}
		}
		// End of loop, one target reached
	}

	// End of all targets reached, reset and stop movement
	doPlayerStop(ca)

	// Show finished UI summary
	if !param.NoPrint {
		finished := internal.Point{X: 0.0, Y: 0.0}
		if len(param.Path) > 0 {
			finished = param.Path[len(param.Path)-1]
		}
		if finalInfer, err := doInfer(ctx, ctrl, param); err == nil && finalInfer != nil {
			finished = finalInfer.Loc
		}
		maafocus.PrintLargeContentTrimNewline(
			a.buildNavigationFinishedHTML(param, finished),
		)
	}

	// Run the on_finish pipeline node once if provided
	if len(param.OnFinish) > 0 {
		log.Info().Msg("Running on_finish node for MapTrackerMove")
		if err := runOnFinishNode(ctx, param.OnFinish); err != nil {
			log.Error().Err(err).Msg("Failed to run on_finish node for MapTrackerMove")
			return false
		}
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
	for i, p := range param.Path {
		if !p.IsValid() {
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

	if param.RotationSlowerThreshold < 0 || param.RotationSlowerThreshold > 180 {
		return nil, fmt.Errorf("rotation_slower_threshold must be between 0 and 180 degrees")
	} else if param.RotationSlowerThreshold == 0 {
		param.RotationSlowerThreshold = mapTrackerMoveDefaultParam.RotationSlowerThreshold
	}

	if param.RotationFasterThreshold < 0 || param.RotationFasterThreshold > 180 {
		return nil, fmt.Errorf("rotation_faster_threshold must be between 0 and 180 degrees")
	} else if param.RotationFasterThreshold == 0 {
		param.RotationFasterThreshold = mapTrackerMoveDefaultParam.RotationFasterThreshold
	}
	if param.RotationFasterThreshold <= param.RotationSlowerThreshold {
		return nil, fmt.Errorf("rotation_faster_threshold must be greater than rotation_slower_threshold")
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

	if len(param.StuckMitigators) == 0 {
		param.StuckMitigators = mapTrackerMoveDefaultParam.StuckMitigators
	}

	if len(param.MapNameMatchRule) == 0 {
		param.MapNameMatchRule = mapTrackerMoveDefaultParam.MapNameMatchRule
	}
	mapNameRegex := buildMapNameRegex(param.MapNameMatchRule, param.MapName)
	if _, err := regexp.Compile(mapNameRegex); err != nil {
		return nil, fmt.Errorf("map_name_match_rule produced invalid regex %q: %w", mapNameRegex, err)
	}

	return &param, nil
}

func doPlayerStop(ca control.ControlAdaptor) {
	// Actively reset cursor to prevent other tasks' potential issue
	ca.ResetCursor(control.CursorResetActive)
	// Softly stop movement first
	ca.SetPlayerMovement(control.MovementStop, control.PolicyLazy)
	// Then reset player to running state to ensure consistent movement state for next navigation
	ca.SetPlayerMovement(control.MovementRun, control.PolicyLazy)
	// Finally set to stop to ensure immediate response to stopping signal
	ca.SetPlayerMovement(control.MovementStop, control.PolicyDefault)
}

func doEmergencyStop(ca control.ControlAdaptor, noPrint bool) {
	log.Warn().Msg("Emergency stop triggered")
	if !noPrint {
		maafocus.PrintLargeContentTrimNewline(i18n.RenderHTML("maptracker.emergency_stop", nil))
	}
	doPlayerStop(ca)
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
		"precision":      mapTrackerInferDefaultParam.Precision,
		"threshold":      mapTrackerInferDefaultParam.Threshold,
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

	resultWrapper, hit := MapTrackerInferRunner.Run(ctx, &maa.CustomRecognitionArg{
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
	if resultWrapper == nil || resultWrapper.Detail == "" {
		log.Error().Msg("Location inference result is empty")
		return nil, fmt.Errorf("location inference result is empty")
	}

	// Extract result
	var result MapTrackerInferResult
	if err := json.Unmarshal([]byte(resultWrapper.Detail), &result); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal MapTrackerInferResult")
		return nil, err
	}

	return &result, nil
}

// runFineApproach precisely converges the player onto the target
// by performing short movement impulses along the camera axes.
//
// The camera is intentionally never rotated here, so the camera yaw stays constant
// and the only unknown is its value, which is seeded from seedRot and then re-calibrated
// from the measured player rotation.
//
// It returns the outcome together with the last successful inference,
// which the caller can reuse as an up-to-date measurement.
func runFineApproach(
	ctx *maa.Context,
	ctrl *maa.Controller,
	ca control.ControlAdaptor,
	param *MapTrackerMoveParam,
	target internal.Point,
	seedRot int,
) (fineApproachOutcome, *MapTrackerInferResult) {
	settleDelay := time.Duration(FINE_APPROACH_SETTLE_MS) * time.Millisecond

	// Stop the player, so that every measurement is taken while standing still.
	ca.SetPlayerMovement(control.MovementStop, control.PolicyActive)
	ca.ResetCursor(control.CursorResetActive)
	time.Sleep(settleDelay)

	// The camera yaw equals the player rotation as long as the player moves straight forward,
	// which is how the preceding ordinary approach ends.
	yaw := float64(seedRot)
	lastImpulse := fineApproachImpulseState{finalAngle: math.NaN()}
	lastLoc := internal.Point{}
	var lastResult *MapTrackerInferResult

	for attempt := 0; ; attempt++ {
		if ctx.GetTasker().Stopping() {
			log.Warn().Msg("Task is stopping, exiting fine approach")
			return fineApproachAborted, lastResult
		}

		result, err := doInfer(ctx, ctrl, param)
		if err != nil {
			log.Warn().Err(err).Int("attempt", attempt).Msg("Inference failed during fine approach")
			return fineApproachAborted, lastResult
		}
		lastResult = result

		// Report the achieved speed of the previous impulse, to allow tuning the speed constants.
		if lastImpulse.totalOn > 0 {
			travelled := lastLoc.DistanceTo(result.Loc)
			log.Debug().
				Float64("travelled", travelled).
				Dur("onTime", lastImpulse.totalOn).
				Float64("achievedSpeed", travelled/lastImpulse.totalOn.Seconds()).
				Msg("Fine approach impulse response measured")
		}

		delta := internal.Point{X: target.X - result.Loc.X, Y: target.Y - result.Loc.Y}
		dist := result.Loc.DistanceTo(target)

		if dist <= FINE_APPROACH_COMPLETE_THRESHOLD {
			log.Info().Int("attempt", attempt).Float64("dist", dist).Object("pos", result.Loc).Msg("Target point reached (fine approach)")
			return fineApproachReached, result
		}
		// Guard against residuals that are too large for fine approach.
		if dist >= param.ArrivalThreshold {
			log.Warn().
				Int("attempt", attempt).
				Float64("dist", dist).
				Float64("threshold", param.ArrivalThreshold).
				Object("pos", result.Loc).
				Object("target", target).
				Msg("Fine approach residual exceeds arrival threshold, giving up the fine approach")
			return fineApproachDiverged, result
		}
		if attempt >= FINE_APPROACH_MAX_CALIBRATIONS {
			log.Warn().Int("attempt", attempt).Float64("dist", dist).Msg("Fine approach budget exhausted, accepting current position")
			return fineApproachExhausted, result
		}

		// Recalibrate the camera yaw: the player turns toward the direction it moves,
		// so the measured rotation reveals the world direction of the previous impulse's final segment.
		// A short segment is ignored, since the player may not have finished turning by then.
		if !math.IsNaN(lastImpulse.finalAngle) && lastImpulse.finalOn >= FINE_APPROACH_MIN_YAW_UPDATE_MS*time.Millisecond {
			correction := internal.DeltaRotation(int(math.Round(yaw+lastImpulse.finalAngle)), result.Rot)
			if math.Abs(correction) <= FINE_APPROACH_MAX_YAW_UPDATE_DEG {
				yaw = math.Mod(yaw+correction+360, 360)
				log.Debug().Float64("correction", correction).Float64("yaw", yaw).Msg("Fine approach camera yaw recalibrated")
			} else {
				log.Debug().Float64("correction", correction).Msg("Fine approach camera yaw correction rejected as implausible")
			}
		}

		// Shrink the requested offset so short impulses do not overshoot.
		// If the residual is already at or below the offset, no useful pulse can be fired.
		if dist <= FINE_APPROACH_IMPULSE_OFFSET {
			log.Debug().
				Int("attempt", attempt).
				Float64("dist", dist).
				Float64("offset", FINE_APPROACH_IMPULSE_OFFSET).
				Msg("Fine approach residual at physical impulse floor, accepting current position")
			return fineApproachReached, result
		}
		scale := (dist - FINE_APPROACH_IMPULSE_OFFSET) / dist
		forward, right := internal.RotateToLocalFrame(delta, yaw)
		forward *= scale
		right *= scale
		impulse := fineApproachImpulse(forward, right)

		log.Debug().
			Int("attempt", attempt).
			Float64("dist", dist).
			Float64("yaw", yaw).
			Int("rot", result.Rot).
			Float64("scale", scale).
			Float64("forward", forward).
			Float64("right", right).
			Dur("forwardOn", impulse.forwardOn).
			Dur("rightOn", impulse.rightOn).
			Msg("Fine approach impulse")

		ca.PlayerPulseMove(impulse.forwardOn, impulse.rightOn, control.MovementWalk)
		time.Sleep(settleDelay)

		lastLoc = result.Loc
		lastImpulse = impulse
	}
}

// fineApproachImpulse describes one movement impulse of the fine approach.
type fineApproachImpulseState struct {
	forwardOn  time.Duration
	rightOn    time.Duration
	totalOn    time.Duration
	finalOn    time.Duration
	finalAngle float64
}

// fineApproachImpulse converts a local-frame offset into a movement impulse.
//
// Both axes are actuated together for the shorter on-time, during which the player moves
// diagonally at the same speed as along a single axis, so each axis only advances by a factor of
// 1/sqrt2. The remaining offset is then covered by the dominant axis alone.
func fineApproachImpulse(forward, right float64) fineApproachImpulseState {
	absForward, absRight := math.Abs(forward), math.Abs(right)
	shared := control.MovementWalk.EtaOfDistance(math.Sqrt2 * min(absForward, absRight))
	exclusive := control.MovementWalk.EtaOfDistance(math.Abs(absForward - absRight))

	impulse := fineApproachImpulseState{forwardOn: shared, rightOn: shared}
	if absForward >= absRight {
		impulse.forwardOn += exclusive
	} else {
		impulse.rightOn += exclusive
	}
	impulse.totalOn = shared + exclusive

	// The impulse ends on the dominant axis alone, unless both axes are actuated for the same
	// time, in which case the whole impulse is diagonal.
	finalForward, finalRight := forward, right
	impulse.finalOn = exclusive
	if exclusive <= 0 {
		impulse.finalOn = shared
	} else if absForward >= absRight {
		finalRight = 0
	} else {
		finalForward = 0
	}
	impulse.finalAngle = math.Mod(math.Atan2(finalRight, finalForward)*180/math.Pi+360, 360)

	if forward < 0 {
		impulse.forwardOn = -impulse.forwardOn
	}
	if right < 0 {
		impulse.rightOn = -impulse.rightOn
	}
	return impulse
}

// runOnFinishNode registers the given inline node object under a temporary name and runs it once.
// It defaults pre_delay and post_delay to 0 ms when they are not specified by the node.
func runOnFinishNode(ctx *maa.Context, node map[string]any) error {
	const onFinishNodeName = "__MapTrackerMoveOnFinish"
	nodeWithDefaults := maps.Clone(node)
	if _, ok := nodeWithDefaults["pre_delay"]; !ok {
		nodeWithDefaults["pre_delay"] = 0
	}
	if _, ok := nodeWithDefaults["post_delay"]; !ok {
		nodeWithDefaults["post_delay"] = 0
	}
	override := map[string]any{onFinishNodeName: nodeWithDefaults}
	if _, err := ctx.RunTask(onFinishNodeName, override); err != nil {
		return fmt.Errorf("failed to run on_finish temporary node: %w", err)
	}
	return nil
}

func executeStuckMitigator(ctx *maa.Context, ca control.ControlAdaptor, action string) {
	log.Info().Str("mitigator", action).Msg("Executing stuck mitigator action")
	switch action {
	case "Jump":
		ca.SetPlayerMovement(ca.GetPlayerMovement(), control.PolicyActive)
		ca.PlayerJump()
	case "MoveOrDeleteDevice":
		if _, err := ctx.RunTask("MapNavigatorObstacleDevice"); err != nil {
			log.Warn().Err(err).Msg("Stuck mitigator MoveOrDeleteDevice failed")
		}
	default:
		log.Warn().Str("action", action).Msg("Unknown stuck mitigator action")
	}
}

func buildMapNameRegex(rule string, mapName string) string {
	escapedName := regexp.QuoteMeta(mapName)
	if strings.Contains(rule, "%s") {
		return fmt.Sprintf(rule, escapedName)
	}
	return rule
}

func (a *MapTrackerMove) buildNavigationMovingHTML(
	param *MapTrackerMoveParam, targetIndex int, current internal.Point, target internal.Point,
) string {
	previewImageURL := buildNavigationPreviewDataURL(param.Path, targetIndex, param.MapName, current, target)

	return i18n.RenderHTML("maptracker.navigation_moving", map[string]any{
		"CurrentIdx": targetIndex + 1,
		"Total":      len(param.Path),
		"CurX":       current.X,
		"CurY":       current.Y,
		"TgtX":       target.X,
		"TgtY":       target.Y,
		"PreviewURL": previewImageURL,
	})
}

func (a *MapTrackerMove) buildNavigationFinishedHTML(param *MapTrackerMoveParam, current internal.Point) string {
	target := internal.Point{X: current.X, Y: current.Y}
	targetIndex := 0
	if len(param.Path) > 0 {
		targetIndex = len(param.Path) - 1
		target.X = param.Path[targetIndex].X
		target.Y = param.Path[targetIndex].Y
	}

	previewImageURL := buildNavigationPreviewDataURL(param.Path, targetIndex, param.MapName, current, target)

	return i18n.RenderHTML("maptracker.navigation_finished", map[string]any{
		"CurrentIdx": len(param.Path),
		"Total":      len(param.Path),
		"CurX":       current.X,
		"CurY":       current.Y,
		"PreviewURL": previewImageURL,
	})
}

func buildNavigationPreviewDataURL(path []internal.Point, targetIndex int, mapName string, current, target internal.Point) string {
	// Prepare map image
	mapRGBA, err := getCachedPreviewMapRGBA(mapName)
	if err != nil {
		log.Debug().Err(err).Str("map", mapName).Msg("Failed to load map image for moving preview")
		return ""
	}

	// Prepare points to focus on
	focusPoints := make([]internal.Point, 0, 9)
	if len(path) > 0 {
		start := max(0, targetIndex-4)
		end := min(len(path)-1, targetIndex+4)
		focusPoints = append(focusPoints, path[start:end+1]...)
	}
	if len(focusPoints) == 0 {
		focusPoints = append(focusPoints, internal.Point{X: target.X, Y: target.Y})
	}

	drawPath := path
	if len(drawPath) == 0 {
		drawPath = focusPoints
	}

	// Calculate geometry and crop map image
	const canvasSize = 192

	viewTransform, currentView := calcNavigationPreviewGeometry(focusPoints, current, canvasSize, 96, 192)
	if viewTransform.ScaleX <= 0 || viewTransform.ScaleY <= 0 || viewTransform.ScaleX != viewTransform.ScaleY {
		viewTransform = internal.LinearTransform{ScaleX: 1.0, ScaleY: 1.0}
		currentView = viewTransform.Apply(current)
	}
	scale := viewTransform.ScaleX

	canvas := image.NewRGBA(image.Rect(0, 0, canvasSize, canvasSize))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{0xf7, 0xfb, 0xff, 0xff}}, image.Point{}, draw.Src)

	b := mapRGBA.Bounds()
	topLeft := viewTransform.Inverse(internal.Point{X: 0, Y: 0})
	bottomRight := viewTransform.Inverse(internal.Point{X: canvasSize, Y: canvasSize})
	srcMinX := int(math.Floor(topLeft.X))
	srcMinY := int(math.Floor(topLeft.Y))
	srcMaxX := int(math.Ceil(bottomRight.X))
	srcMaxY := int(math.Ceil(bottomRight.Y))
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
	dstMin := viewTransform.Apply(internal.Point{X: float64(srcRect.Min.X), Y: float64(srcRect.Min.Y)})
	dstMinX := dstMin.IntX()
	dstMinY := dstMin.IntY()
	dstRect := image.Rect(dstMinX, dstMinY, dstMinX+scaledCrop.Bounds().Dx(), dstMinY+scaledCrop.Bounds().Dy())
	draw.Draw(canvas, dstRect, scaledCrop, image.Point{}, draw.Over)

	// Draw path and points
	var (
		colorRed   = color.RGBA{0xdb, 0x39, 0x2b, 0xff} // 0xdb392b
		colorGreen = color.RGBA{0x27, 0xce, 0x60, 0xff} // 0x27ce60
		colorBlue  = color.RGBA{0x2b, 0x62, 0xc0, 0xff} // 0x2b62c0
	)

	for i := 0; i+1 < len(drawPath); i++ {
		p1 := viewTransform.Apply(drawPath[i])
		p2 := viewTransform.Apply(drawPath[i+1])
		minicv.ImageDrawLine(canvas, p1.IntX(), p1.IntY(), p2.IntX(), p2.IntY(), colorBlue, 3)
	}

	for _, p := range drawPath {
		p_ := viewTransform.Apply(p)
		minicv.ImageDrawFilledCircle(canvas, p_.IntX(), p_.IntY(), 4, colorBlue)
	}

	target_ := viewTransform.Apply(target)
	minicv.ImageDrawLine(canvas, currentView.IntX(), currentView.IntY(), target_.IntX(), target_.IntY(), colorRed, 3)
	minicv.ImageDrawFilledCircle(canvas, target_.IntX(), target_.IntY(), 5, colorRed)
	minicv.ImageDrawFilledCircle(canvas, currentView.IntX(), currentView.IntY(), 5, colorGreen)

	// Return as base64 data URL
	base64JPEG, err := minicv.ImageToBase64JPEG(canvas, 90)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to encode moving preview image")
		return ""
	}

	return "data:image/jpeg;base64," + base64JPEG
}

func getCachedPreviewMapRGBA(mapName string) (*image.RGBA, error) {
	mapPath := resource.FindResource(filepath.ToSlash(filepath.Join(internal.MAP_DIR, mapName+".png")))
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

func calcNavigationPreviewGeometry(focusPoints []internal.Point, current internal.Point, canvasSize int, minSize int, maxSize int) (
	viewTransform internal.LinearTransform, currentView internal.Point,
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

	minX, minY, maxX, maxY := internal.PathBounds(focusPoints)
	if current.IsValid() {
		minX = math.Min(minX, current.X)
		minY = math.Min(minY, current.Y)
		maxX = math.Max(maxX, current.X)
		maxY = math.Max(maxY, current.Y)
	}

	if math.IsNaN(minX) || math.IsInf(minX, 0) ||
		math.IsNaN(minY) || math.IsInf(minY, 0) ||
		math.IsNaN(maxX) || math.IsInf(maxX, 0) ||
		math.IsNaN(maxY) || math.IsInf(maxY, 0) {
		minX, minY = 0, 0
		maxX, maxY = previewSize, previewSize
	}

	spanX := min(max(maxX-minX, minSpan), maxSpan)
	spanY := min(max(maxY-minY, minSpan), maxSpan)
	scale := math.Min(previewSize/spanX, previewSize/spanY)

	centerX := (minX + maxX) * 0.5
	centerY := (minY + maxY) * 0.5
	offsetX := previewSize*0.5 - centerX*scale
	offsetY := previewSize*0.5 - centerY*scale

	viewTransform = internal.LinearTransform{ScaleX: scale, ScaleY: scale, OffsetX: offsetX, OffsetY: offsetY}
	currentView = viewTransform.Apply(current)
	return
}
