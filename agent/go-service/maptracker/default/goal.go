// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"time"

	maptrackerbigmap "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/bigmap"
	internal "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerGoal navigates to a target through MapTracker NavMesh.
type MapTrackerGoal struct{}

var _ maa.CustomActionRunner = &MapTrackerGoal{}

const (
	startPointCostFactor = 1.05
	startPointMCD        = 20.0
	endPointCostFactor   = 1.05
	endPointMCD          = 20.0

	ZIPLINE_POLICY_NEVER      = "Never"
	ZIPLINE_POLICY_LAZY       = "Lazy"
	ZIPLINE_POLICY_ACTIVE     = "Active"
	ZIPLINE_POLICY_AGGRESSIVE = "Aggressive"

	ZIPLINE_EXPECTED_DISTANCE = 9.0
	ZIPLINE_MAX_REPLAN        = 16
	ZIPLINE_FIRST_EDGE_ID     = -1000000
	ZIPLINE_EDGE_ID_OFFSET    = -1

	ziplineMaxDistance     = 85.0
	ziplineConnectDistance = 20.0

	mapTrackerGoalDebugDir = "debug/vision"
)

// MapTrackerGoalParam represents the custom_action_param for MapTrackerGoal.
type MapTrackerGoalParam struct {
	MapTrackerMoveParam
	Target        *[2]float64 `json:"target,omitempty"`
	EntityID      *int64      `json:"entity_id,omitempty"`
	ZiplinePolicy string      `json:"zipline_policy,omitempty"`
}

type goalContext struct {
	ctx    *maa.Context
	arg    *maa.CustomActionArg
	param  *MapTrackerGoalParam
	ctrl   *maa.Controller
	mesh   *internal.NavMesh
	target [2]float64
}

type ziplinePolicy struct {
	MinNeedZiplineDistance       float64
	ToZiplineEdgeCostFactor      float64
	FromZiplineEdgeCostFactor    float64
	BetweenZiplineEdgeCostFactor float64
}

type ziplineEdgeCostFactors struct {
	ToVertex      float64
	FromVertex    float64
	BetweenVertex float64
}

var mapTrackerGoalZiplinePolicies = map[string]ziplinePolicy{
	ZIPLINE_POLICY_NEVER: {
		MinNeedZiplineDistance:       -1,
		ToZiplineEdgeCostFactor:      64,
		FromZiplineEdgeCostFactor:    16,
		BetweenZiplineEdgeCostFactor: 2.0,
	},
	ZIPLINE_POLICY_LAZY: {
		MinNeedZiplineDistance:       180,
		ToZiplineEdgeCostFactor:      64,
		FromZiplineEdgeCostFactor:    16,
		BetweenZiplineEdgeCostFactor: 2.0,
	},
	ZIPLINE_POLICY_ACTIVE: {
		MinNeedZiplineDistance:       45,
		ToZiplineEdgeCostFactor:      8,
		FromZiplineEdgeCostFactor:    4,
		BetweenZiplineEdgeCostFactor: 0.5,
	},
	ZIPLINE_POLICY_AGGRESSIVE: {
		MinNeedZiplineDistance:       15,
		ToZiplineEdgeCostFactor:      1,
		FromZiplineEdgeCostFactor:    1,
		BetweenZiplineEdgeCostFactor: 0.25,
	},
}

// Run implements maa.CustomActionRunner.
func (a *MapTrackerGoal) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerGoal")
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	inferResult, mesh, target, err := a.prepare(ctx, ctrl, param)
	if err != nil {
		log.Error().Err(err).Msg("Failed to prepare MapTrackerGoal")
		return false
	}

	goalCtx := &goalContext{ctx: ctx, arg: arg, param: param, ctrl: ctrl, mesh: mesh, target: target}
	if mapTrackerGoalZiplinePolicies[param.ZiplinePolicy].MinNeedZiplineDistance >= 0 {
		if a.runZiplineGoal(goalCtx, inferResult) {
			return true
		}
		log.Warn().Msg("MapTrackerGoal zipline path failed, falling back to ordinary path")
		inferResult, mesh, target, err = a.prepare(ctx, ctrl, param)
		if err != nil {
			log.Error().Err(err).Msg("Failed to prepare ordinary fallback for MapTrackerGoal")
			return false
		}
		goalCtx = &goalContext{ctx: ctx, arg: arg, param: param, ctrl: ctrl, mesh: mesh, target: target}
	}

	return a.runOrdinaryGoal(goalCtx, inferResult)
}

func (a *MapTrackerGoal) parseParam(paramStr string) (*MapTrackerGoalParam, error) {
	var param MapTrackerGoalParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}
	if param.MapName == "" {
		return nil, fmt.Errorf("map_name is required in parameters, got empty")
	}
	if param.Target == nil && param.EntityID == nil {
		return nil, fmt.Errorf("target or entity_id is required in parameters")
	}
	if param.Target == nil && param.EntityID != nil && *param.EntityID <= 0 {
		return nil, fmt.Errorf("entity_id must be positive")
	}
	if param.Target != nil {
		if math.IsNaN(param.Target[0]) || math.IsInf(param.Target[0], 0) || math.IsNaN(param.Target[1]) || math.IsInf(param.Target[1], 0) {
			return nil, fmt.Errorf("target contains invalid coordinate")
		}
	}
	if param.ZiplinePolicy == "" {
		param.ZiplinePolicy = ZIPLINE_POLICY_NEVER
	}
	if _, ok := mapTrackerGoalZiplinePolicies[param.ZiplinePolicy]; !ok {
		return nil, fmt.Errorf("zipline_policy must be one of %q, %q, %q, %q", ZIPLINE_POLICY_NEVER, ZIPLINE_POLICY_LAZY, ZIPLINE_POLICY_ACTIVE, ZIPLINE_POLICY_AGGRESSIVE)
	}
	return &param, nil
}

func (a *MapTrackerGoal) prepare(ctx *maa.Context, ctrl *maa.Controller, param *MapTrackerGoalParam) (*MapTrackerInferResult, *internal.NavMesh, [2]float64, error) {
	inferMoveParam := &MapTrackerMoveParam{
		MapName:          param.MapName,
		MapNameMatchRule: param.MapNameMatchRule,
	}
	if inferMoveParam.MapNameMatchRule == "" {
		inferMoveParam.MapNameMatchRule = mapTrackerMoveDefaultParam.MapNameMatchRule
	}

	inferResult, err := doInfer(ctx, ctrl, inferMoveParam)
	if err != nil {
		return nil, nil, [2]float64{}, fmt.Errorf("failed to infer current location for MapTrackerGoal: %w", err)
	}
	if !isMapNameCoreMatch(inferResult.MapName, param.MapName) {
		return nil, nil, [2]float64{}, fmt.Errorf("current map %q does not match target map %q", inferResult.MapName, param.MapName)
	}

	mesh, err := internal.LoadNavMesh(param.MapName)
	if err != nil {
		return nil, nil, [2]float64{}, fmt.Errorf("failed to load NavMesh for MapTrackerGoal: %w", err)
	}

	target, err := a.resolveTarget(mesh, param)
	if err != nil {
		return nil, nil, [2]float64{}, fmt.Errorf("failed to resolve MapTrackerGoal target: %w", err)
	}
	return inferResult, mesh, target, nil
}

func (a *MapTrackerGoal) resolveTarget(mesh *internal.NavMesh, param *MapTrackerGoalParam) ([2]float64, error) {
	if param.Target != nil {
		return *param.Target, nil
	}
	vertex, ok := mesh.FindVertexByEntityID(*param.EntityID)
	if !ok {
		return [2]float64{}, fmt.Errorf("entity_id %d not found in NavMesh", *param.EntityID)
	}
	return [2]float64{vertex.X, vertex.Y}, nil
}

func (a *MapTrackerGoal) runOrdinaryGoal(goalCtx *goalContext, inferResult *MapTrackerInferResult) bool {
	mesh := goalCtx.mesh
	mesh.ClearTemporaryVertex()
	defer mesh.ClearTemporaryVertex()
	startID, _ := mesh.AddTemporaryVertex(inferResult.X, inferResult.Y, startPointCostFactor, startPointMCD)
	targetID, _ := mesh.AddTemporaryVertex(goalCtx.target[0], goalCtx.target[1], endPointCostFactor, endPointMCD)
	path, err := mesh.FindPath(startID, targetID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to find NavMesh path for MapTrackerGoal")
		return false
	}

	log.Info().Str("map", goalCtx.param.MapName).
		Float64("startX", inferResult.X).
		Float64("startY", inferResult.Y).
		Float64("targetX", goalCtx.target[0]).
		Float64("targetY", goalCtx.target[1]).
		Int("pathCount", len(path)).
		Msg("MapTrackerGoal path generated")

	return a.runFinalGoalMove(goalCtx, path)
}

func (a *MapTrackerGoal) runZiplineGoal(goalCtx *goalContext, inferResult *MapTrackerInferResult) bool {
	ordinaryPath, err := a.findOrdinaryPathFromLocation(goalCtx, inferResult.X, inferResult.Y)
	var mustSeePoints [][2]int
	if err != nil {
		log.Warn().Err(err).Msg("Failed to find ordinary path before zipline search, using fallback must-see points")
		mustSeePoints = fallbackMustSeePoints([2]float64{inferResult.X, inferResult.Y}, goalCtx.target)
	} else {
		ordinaryDistance := internal.PathTotalDistance(ordinaryPath)
		policy := mapTrackerGoalZiplinePolicies[goalCtx.param.ZiplinePolicy]
		if ordinaryDistance <= policy.MinNeedZiplineDistance {
			log.Debug().
				Float64("ordinaryDistance", ordinaryDistance).
				Float64("minNeedZiplineDistance", policy.MinNeedZiplineDistance).
				Msg("Ordinary path is short enough, skipping zipline search")
			return false
		}
		mustSeePoints = pathToMustSeePoints(ordinaryPath)
	}

	ziplineIDs, err := a.loadRuntimeZiplines(goalCtx, mustSeePoints)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load runtime ziplines")
		return false
	}
	if len(ziplineIDs) == 0 {
		log.Warn().Msg("No runtime ziplines found")
		return false
	}

	current := inferResult
	onZipline := false
	defer goalCtx.mesh.ClearTemporaryVertex()
	for replan := 0; replan < ZIPLINE_MAX_REPLAN; replan++ {
		if goalCtx.ctx.GetTasker().Stopping() {
			log.Warn().Msg("Task is stopping, exiting zipline goal")
			return false
		}

		pathIDs, path, err := a.findPathFromLocation(goalCtx, current.X, current.Y)
		if err != nil {
			log.Warn().Err(err).Int("replan", replan).Msg("Zipline-aware path search failed")
			return false
		}
		a.saveDebugImage(goalCtx, pathIDs, path, ziplineIDs, current, replan)

		if len(pathIDs) < 2 {
			log.Warn().Int("pathLen", len(pathIDs)).Msg("Zipline-aware path is too short")
			return false
		}
		ziplineIndex := findFirstZiplineEdgeIndex(goalCtx.mesh, pathIDs)
		if ziplineIndex < 0 {
			if runMove, movePath := a.handlePathWithoutZiplineEdge(goalCtx, &onZipline, &current, path, replan); runMove {
				return a.runFinalGoalMove(goalCtx, movePath)
			}
			continue
		}

		sourceID := pathIDs[ziplineIndex]
		destID := pathIDs[ziplineIndex+1]
		if !a.approachAndMountZipline(goalCtx, &onZipline, &current, pathIDs, ziplineIndex, sourceID, destID) {
			continue
		}

		destPoint, _ := goalCtx.mesh.VertexPoint(destID)
		if !a.runZipline(goalCtx, destPoint) {
			if edge, ok := goalCtx.mesh.IsZiplineEdge(sourceID, destID); ok {
				goalCtx.mesh.DisableEdge(edge.ID)
			}
			log.Warn().Int("source", sourceID).Int("destination", destID).Msg("Zipline fast travel failed, disabling edge")
			current = a.inferOrFallback(goalCtx, current)
			continue
		}

		if !a.validateZiplineArrival(goalCtx, &onZipline, &current, pathIDs, ziplineIndex, destID, destPoint) {
			continue
		}
	}

	log.Warn().Int("maxReplan", ZIPLINE_MAX_REPLAN).Msg("Zipline-aware path exceeded replan limit")
	return false
}

// handlePathWithoutZiplineEdge handles the case when a computed path contains no zipline edge.
// It gets the player off the zipline if currently on one, or returns the path for ordinary movement.
func (a *MapTrackerGoal) handlePathWithoutZiplineEdge(
	goalCtx *goalContext,
	onZipline *bool,
	current **MapTrackerInferResult,
	path [][2]float64,
	replan int,
) (shouldRunMove bool, movePath [][2]float64) {
	if *onZipline {
		*onZipline = false
		*current = a.inferAndGetOffZipline(goalCtx, *current)
		log.Info().Int("replan", replan).Int("pathCount", len(path)).Msg("Got off zipline before ordinary movement, replanning")
		return false, nil
	}
	log.Info().Int("replan", replan).Int("pathCount", len(path)).Msg("Zipline-aware path has no zipline edge, running ordinary move")
	return true, path
}

// approachAndMountZipline moves the player to the zipline source vertex and mounts on it.
// Returns true when the player is on the zipline and ready to travel.
func (a *MapTrackerGoal) approachAndMountZipline(
	goalCtx *goalContext,
	onZipline *bool,
	current **MapTrackerInferResult,
	pathIDs []int,
	ziplineIndex int,
	sourceID int,
	destID int,
) bool {
	sourcePoint, _ := goalCtx.mesh.VertexPoint(sourceID)
	alreadyAtSource := *onZipline && math.Hypot((*current).X-sourcePoint[0], (*current).Y-sourcePoint[1]) <= ZIPLINE_EXPECTED_DISTANCE
	if alreadyAtSource {
		_, ok := goalCtx.mesh.IsZiplineEdge(sourceID, destID)
		if !ok {
			log.Warn().Int("source", sourceID).Int("destination", destID).Msg("Expected zipline edge disappeared")
		}
		return ok
	}

	if *onZipline {
		*onZipline = false
		*current = a.inferAndGetOffZipline(goalCtx, *current)
		return false
	}
	if !a.runMoveToZiplineSource(goalCtx, pathIDs, ziplineIndex, sourceID) {
		goalCtx.mesh.DisableVertex(sourceID)
		log.Warn().Int("vertex", sourceID).Msg("Failed to move to zipline point, disabling it")
		*current = a.inferOrFallback(goalCtx, *current)
		return false
	}

	_, ok := goalCtx.mesh.IsZiplineEdge(sourceID, destID)
	if !ok {
		log.Warn().Int("source", sourceID).Int("destination", destID).Msg("Expected zipline edge disappeared")
		return false
	}

	if !*onZipline {
		detail, err := goalCtx.ctx.RunTask("MapTrackerOpenWorld_GetOnZipline")
		if err != nil || detail == nil || !detail.Status.Success() {
			goalCtx.mesh.DisableVertex(sourceID)
			event := log.Warn().Err(err).Int("vertex", sourceID)
			if detail != nil {
				event = event.Int64("subtaskID", detail.ID).Str("subtaskStatus", detail.Status.String())
			}
			event.Msg("Cannot get on zipline, disabling source point")
			*current = a.inferOrFallback(goalCtx, *current)
			return false
		}
		*onZipline = true
		time.Sleep(1000 * time.Millisecond)
	}
	return true
}

// validateZiplineArrival infers the current location after zipline travel and validates
// whether the player arrived at the expected destination. It then decides if the zipline
// chain should continue.
func (a *MapTrackerGoal) validateZiplineArrival(
	goalCtx *goalContext,
	onZipline *bool,
	current **MapTrackerInferResult,
	pathIDs []int,
	ziplineIndex int,
	destID int,
	destPoint [2]float64,
) bool {
	*current = a.inferOrFallback(goalCtx, *current)
	if math.Hypot((*current).X-destPoint[0], (*current).Y-destPoint[1]) > ZIPLINE_EXPECTED_DISTANCE {
		goalCtx.mesh.DisableVertex(destID)
		log.Warn().
			Int("vertex", destID).
			Float64("curX", (*current).X).Float64("curY", (*current).Y).
			Float64("targetX", destPoint[0]).Float64("targetY", destPoint[1]).
			Msg("Zipline arrived at unexpected point, disabling expected point")
		*onZipline = false
		*current = a.inferAndGetOffZipline(goalCtx, *current)
		return false
	}

	if ziplineIndex+2 >= len(pathIDs) {
		*onZipline = false
		*current = a.inferAndGetOffZipline(goalCtx, *current)
		return false
	}
	if _, ok := goalCtx.mesh.IsZiplineEdge(destID, pathIDs[ziplineIndex+2]); !ok {
		*onZipline = false
		*current = a.inferAndGetOffZipline(goalCtx, *current)
		return false
	}
	return true
}

func (a *MapTrackerGoal) runZipline(goalCtx *goalContext, target [2]float64) bool {
	param := MapTrackerZiplineParam{
		MapName:          goalCtx.param.MapName,
		Target:           &target,
		MapNameMatchRule: goalCtx.param.MapNameMatchRule,
	}
	paramBytes, err := json.Marshal(param)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal MapTrackerZipline parameters for MapTrackerGoal")
		return false
	}
	return (&MapTrackerZipline{}).Run(goalCtx.ctx, &maa.CustomActionArg{
		TaskID:            goalCtx.arg.TaskID,
		CurrentTaskName:   goalCtx.arg.CurrentTaskName,
		CustomActionName:  "MapTrackerZipline",
		CustomActionParam: string(paramBytes),
		RecognitionDetail: goalCtx.arg.RecognitionDetail,
		Box:               goalCtx.arg.Box,
	})
}

func (a *MapTrackerGoal) findPathFromLocation(goalCtx *goalContext, x, y float64) ([]int, [][2]float64, error) {
	goalCtx.mesh.ClearTemporaryVertex()
	startID, _ := goalCtx.mesh.AddTemporaryVertex(x, y, startPointCostFactor, startPointMCD)
	targetID, _ := goalCtx.mesh.AddTemporaryVertex(goalCtx.target[0], goalCtx.target[1], endPointCostFactor, endPointMCD)
	pathIDs, err := goalCtx.mesh.FindPathIDs(startID, targetID)
	if err != nil {
		return nil, nil, err
	}
	path, err := goalCtx.mesh.PathIDsToPoints(pathIDs)
	if err != nil {
		return nil, nil, err
	}
	return pathIDs, path, nil
}

func (a *MapTrackerGoal) findOrdinaryPathFromLocation(goalCtx *goalContext, x, y float64) ([][2]float64, error) {
	goalCtx.mesh.ClearTemporaryVertex()
	defer goalCtx.mesh.ClearTemporaryVertex()
	startID, _ := goalCtx.mesh.AddTemporaryVertex(x, y, startPointCostFactor, startPointMCD)
	targetID, _ := goalCtx.mesh.AddTemporaryVertex(goalCtx.target[0], goalCtx.target[1], endPointCostFactor, endPointMCD)
	return goalCtx.mesh.FindPath(startID, targetID)
}

func (a *MapTrackerGoal) runFinalGoalMove(goalCtx *goalContext, path [][2]float64) bool {
	moveParam := goalCtx.param.MapTrackerMoveParam
	moveParam.Path = path
	moveParam.MapName = goalCtx.param.MapName
	return a.runMoveWithParam(goalCtx, moveParam, true)
}

func (a *MapTrackerGoal) runMoveToZiplineSource(goalCtx *goalContext, pathIDs []int, ziplineIndex int, sourceID int) bool {
	movePath, err := goalCtx.mesh.PathIDsToPoints(pathIDs[:ziplineIndex+1])
	if err != nil {
		log.Warn().Err(err).Int("vertex", sourceID).Msg("Failed to convert path to zipline point")
		return false
	}
	if len(movePath) <= 1 {
		return true
	}

	moveParam := goalCtx.param.MapTrackerMoveParam
	moveParam.Path = movePath
	moveParam.MapName = goalCtx.param.MapName
	log.Info().Int("vertex", sourceID).Int("pathCount", len(movePath)).Msg("Moving only to next zipline point")
	return a.runMoveWithParam(goalCtx, moveParam, false)
}

func (a *MapTrackerGoal) runMoveWithParam(goalCtx *goalContext, moveParam MapTrackerMoveParam, isFinalGoal bool) bool {
	if !isFinalGoal {
		// Strip on_finish so it never fires on intermediate segments of a goal navigation.
		moveParam.OnFinish = nil
	}
	moveParamBytes, err := json.Marshal(moveParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal MapTrackerMove parameters for MapTrackerGoal")
		return false
	}
	return (&MapTrackerMove{}).Run(goalCtx.ctx, &maa.CustomActionArg{
		TaskID:            goalCtx.arg.TaskID,
		CurrentTaskName:   goalCtx.arg.CurrentTaskName,
		CustomActionName:  "MapTrackerMove",
		CustomActionParam: string(moveParamBytes),
		RecognitionDetail: goalCtx.arg.RecognitionDetail,
		Box:               goalCtx.arg.Box,
	})
}

func (a *MapTrackerGoal) inferAndGetOffZipline(goalCtx *goalContext, fallback *MapTrackerInferResult) *MapTrackerInferResult {
	result := a.inferOrFallback(goalCtx, fallback)
	if _, err := goalCtx.ctx.RunTask("MapTrackerOpenWorld_GetOffZipline"); err != nil {
		log.Warn().Err(err).Msg("Failed to get off zipline")
	}
	return result
}

func (a *MapTrackerGoal) inferOrFallback(goalCtx *goalContext, fallback *MapTrackerInferResult) *MapTrackerInferResult {
	param := &MapTrackerMoveParam{MapName: goalCtx.param.MapName, MapNameMatchRule: goalCtx.param.MapNameMatchRule}
	if param.MapNameMatchRule == "" {
		param.MapNameMatchRule = mapTrackerMoveDefaultParam.MapNameMatchRule
	}
	result, err := doInfer(goalCtx.ctx, goalCtx.ctrl, param)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to infer location, using previous location")
		return fallback
	}
	return result
}

func (a *MapTrackerGoal) loadRuntimeZiplines(goalCtx *goalContext, mustSeePoints [][2]int) ([]int, error) {
	ca, err := control.NewControlAdaptor(goalCtx.ctx, goalCtx.ctrl, WORK_W, WORK_H)
	if err != nil {
		return nil, fmt.Errorf("failed to create control adaptor: %w", err)
	}
	closeBigMap := func() {
		ca.KeyType(27, 1000)
	}

	if _, err := goalCtx.ctx.RunTask("MapTrackerBigMap_OpenBigMap"); err != nil {
		return nil, fmt.Errorf("failed to open big map for zipline inference: %w", err)
	}
	defer closeBigMap()

	if _, err := goalCtx.ctx.RunTask("MapTrackerBigMap_FilterOnlyZipline"); err != nil {
		return nil, fmt.Errorf("failed to filter ziplines on big map: %w", err)
	}

	matches, err := a.findBigMapZiplines(goalCtx, mustSeePoints)
	if err != nil {
		return nil, err
	}

	ids := make([]int, 0, len(matches))
	for _, match := range matches {
		id, _ := goalCtx.mesh.AddRuntimeVertex(match.MapX, match.MapY, 0, 0, internal.NavMeshVertexFlagZipline)
		ids = append(ids, id)
	}
	policy := mapTrackerGoalZiplinePolicies[goalCtx.param.ZiplinePolicy]
	factors := ziplineEdgeCostFactors{
		ToVertex:      policy.ToZiplineEdgeCostFactor,
		FromVertex:    policy.FromZiplineEdgeCostFactor,
		BetweenVertex: policy.BetweenZiplineEdgeCostFactor,
	}
	connectRuntimeZiplines(goalCtx.mesh, ids, ZIPLINE_FIRST_EDGE_ID, ZIPLINE_EDGE_ID_OFFSET, factors)
	log.Info().Int("ziplineCount", len(ids)).Int("runtimeEdges", len(goalCtx.mesh.RuntimeEdges)).Msg("Runtime ziplines loaded")
	return ids, nil
}

func (a *MapTrackerGoal) findBigMapZiplines(goalCtx *goalContext, mustSeePoints [][2]int) ([]maptrackerbigmap.MapTrackerBigMapFindImageMatch, error) {
	goalCtx.ctrl.PostScreencap().Wait()
	img, err := goalCtx.ctrl.CacheImage()
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}
	param := struct {
		Template      string   `json:"template"`
		Expected      bool     `json:"expected"`
		GreenMask     bool     `json:"green_mask,omitempty"`
		WithRotation  bool     `json:"with_rotation,omitempty"`
		ZoomValue     float64  `json:"zoom_value,omitempty"`
		MustSeePoints [][2]int `json:"must_see_points,omitempty"`
	}{
		Template:      "image/MapTracker/BigMapIcons/Zipline.png",
		Expected:      true,
		GreenMask:     true,
		WithRotation:  false,
		ZoomValue:     0.6,
		MustSeePoints: mustSeePoints,
	}
	paramBytes, err := json.Marshal(param)
	if err != nil {
		return nil, err
	}
	result, hit := (&maptrackerbigmap.MapTrackerBigMapFindImage{}).Run(goalCtx.ctx, &maa.CustomRecognitionArg{
		TaskID:                 goalCtx.arg.TaskID,
		CurrentTaskName:        goalCtx.arg.CurrentTaskName,
		CustomRecognitionName:  "MapTrackerBigMapFindImage",
		CustomRecognitionParam: string(paramBytes),
		Img:                    img,
		Roi:                    maa.Rect{0, 0, img.Bounds().Dx(), img.Bounds().Dy()},
	})
	if result == nil || result.Detail == "" {
		if !hit {
			return nil, nil
		}
		return nil, fmt.Errorf("MapTrackerBigMapFindImage returned empty detail")
	}
	var matches []maptrackerbigmap.MapTrackerBigMapFindImageMatch
	if err := json.Unmarshal([]byte(result.Detail), &matches); err != nil {
		return nil, err
	}
	return matches, nil
}

func (a *MapTrackerGoal) saveDebugImage(goalCtx *goalContext, pathIDs []int, path [][2]float64, ziplineIDs []int, current *MapTrackerInferResult, replan int) {
	mapRGBA, err := getCachedPreviewMapRGBA(goalCtx.param.MapName)
	if err != nil {
		log.Debug().Err(err).Str("map", goalCtx.param.MapName).Msg("Failed to load map image for MapTrackerGoal debug image")
		return
	}

	canvas := minicv.ImageCopy(mapRGBA)

	colorPath := color.RGBA{0x2b, 0x62, 0xc0, 0xff}
	colorPathZipline := color.RGBA{0xff, 0x8c, 0x00, 0xff}
	colorPoint := color.RGBA{0x2b, 0x62, 0xc0, 0xff}
	colorZipline := color.RGBA{0x9b, 0x59, 0xb6, 0xff}
	colorCurrent := color.RGBA{0x27, 0xce, 0x60, 0xff}
	colorTarget := color.RGBA{0xdb, 0x39, 0x2b, 0xff}

	toPixel := func(point [2]float64) (int, int) {
		return int(math.Round(point[0])), int(math.Round(point[1]))
	}

	for i := 0; i+1 < len(path); i++ {
		x1, y1 := toPixel(path[i])
		x2, y2 := toPixel(path[i+1])
		lineColor := colorPath
		if i+1 < len(pathIDs) {
			if _, ok := goalCtx.mesh.IsZiplineEdge(pathIDs[i], pathIDs[i+1]); ok {
				lineColor = colorPathZipline
			}
		}
		minicv.ImageDrawLine(canvas, x1, y1, x2, y2, lineColor, 3)
	}

	for _, id := range ziplineIDs {
		if goalCtx.mesh.DisabledVertices[id] {
			continue
		}
		point, ok := goalCtx.mesh.VertexPoint(id)
		if !ok {
			continue
		}
		x, y := toPixel(point)
		minicv.ImageDrawFilledCircle(canvas, x, y, 5, colorZipline)
	}

	for _, point := range path {
		x, y := toPixel(point)
		minicv.ImageDrawFilledCircle(canvas, x, y, 3, colorPoint)
	}

	if current != nil {
		x, y := toPixel([2]float64{current.X, current.Y})
		minicv.ImageDrawFilledCircle(canvas, x, y, 6, colorCurrent)
	}
	targetX, targetY := toPixel(goalCtx.target)
	minicv.ImageDrawFilledCircle(canvas, targetX, targetY, 6, colorTarget)

	if err := minicv.ImageSaveDebug(canvas, mapTrackerGoalDebugDir, "MapTrackerGoal", 4); err != nil {
		log.Debug().Err(err).Str("path", mapTrackerGoalDebugDir).Msg("Failed to save MapTrackerGoal debug image")
		return
	}
	log.Debug().Int("replan", replan).Str("path", mapTrackerGoalDebugDir).Msg("MapTrackerGoal debug image saved")
}

func fallbackMustSeePoints(start, target [2]float64) [][2]int {
	mid := [2]float64{(start[0] + target[0]) / 2, (start[1] + target[1]) / 2}
	return pathToMustSeePoints([][2]float64{start, mid, target})
}

func pathToMustSeePoints(path [][2]float64) [][2]int {
	points := make([][2]int, 0, len(path))
	for _, point := range path {
		converted := [2]int{int(math.Round(point[0])), int(math.Round(point[1]))}
		if len(points) > 0 && points[len(points)-1] == converted {
			continue
		}
		points = append(points, converted)
	}
	return points
}

func findFirstZiplineEdgeIndex(mesh *internal.NavMesh, pathIDs []int) int {
	for i := 0; i+1 < len(pathIDs); i++ {
		if _, ok := mesh.IsZiplineEdge(pathIDs[i], pathIDs[i+1]); ok {
			return i
		}
	}
	return -1
}

func connectRuntimeZiplines(mesh *internal.NavMesh, ziplineIDs []int, firstEdgeID int, edgeIDOffset int, factors ziplineEdgeCostFactors) {
	nextEdgeID := firstEdgeID
	nextID := func() int {
		id := nextEdgeID
		nextEdgeID += edgeIDOffset
		return id
	}
	for _, ziplineID := range ziplineIDs {
		ziplinePoint, _ := mesh.VertexPoint(ziplineID)
		for id, vertex := range mesh.Vertices {
			if vertex.Flags&internal.NavMeshVertexFlagHidden != 0 {
				continue
			}
			dist := math.Hypot(vertex.X-ziplinePoint[0], vertex.Y-ziplinePoint[1])
			if dist < ziplineConnectDistance {
				mesh.AddRuntimeEdge(nextID(), id, ziplineID, false, factors.ToVertex*dist, 0)
				mesh.AddRuntimeEdge(nextID(), ziplineID, id, false, factors.FromVertex*dist, 0)
			}
		}
	}
	for i := 0; i < len(ziplineIDs); i++ {
		left, _ := mesh.VertexPoint(ziplineIDs[i])
		for j := i + 1; j < len(ziplineIDs); j++ {
			right, _ := mesh.VertexPoint(ziplineIDs[j])
			dist := math.Hypot(left[0]-right[0], left[1]-right[1])
			if dist <= ziplineMaxDistance {
				mesh.AddRuntimeEdge(nextID(), ziplineIDs[i], ziplineIDs[j], true, factors.BetweenVertex*dist, internal.NavMeshEdgeFlagZipline)
			}
		}
	}
}
