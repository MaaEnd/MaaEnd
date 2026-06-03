// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"encoding/json"
	"fmt"
	"math"

	internal "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/internal"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerGoal navigates to a target through MapTracker NavMesh.
type MapTrackerGoal struct{}

const (
	startPointCostFactor = 1.05
	startPointMCD        = 20.0
	endPointCostFactor   = 1.05
	endPointMCD          = 20.0
)

// MapTrackerGoalParam represents the custom_action_param for MapTrackerGoal.
type MapTrackerGoalParam struct {
	MapTrackerMoveParam
	Target   *[2]float64 `json:"target,omitempty"`
	EntityID *int64      `json:"entity_id,omitempty"`
}

var _ maa.CustomActionRunner = &MapTrackerGoal{}

// Run implements maa.CustomActionRunner.
func (a *MapTrackerGoal) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerGoal")
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	inferMoveParam := &MapTrackerMoveParam{
		MapName:          param.MapName,
		MapNameMatchRule: param.MapNameMatchRule,
	}
	if inferMoveParam.MapNameMatchRule == "" {
		inferMoveParam.MapNameMatchRule = mapTrackerMoveDefaultParam.MapNameMatchRule
	}

	inferResult, err := doInfer(ctx, ctrl, inferMoveParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to infer current location for MapTrackerGoal")
		return false
	}
	if !isMapNameCoreMatch(inferResult.MapName, param.MapName) {
		log.Error().Str("inferredMap", inferResult.MapName).Str("targetMap", param.MapName).Msg("Current map does not match MapTrackerGoal map")
		return false
	}

	mesh, err := internal.LoadNavMesh(param.MapName)
	if err != nil {
		log.Error().Err(err).Str("map", param.MapName).Msg("Failed to load NavMesh for MapTrackerGoal")
		return false
	}

	target, err := a.resolveTarget(mesh, param)
	if err != nil {
		log.Error().Err(err).Msg("Failed to resolve MapTrackerGoal target")
		return false
	}

	mesh.ClearTemporaryVertex()
	defer mesh.ClearTemporaryVertex()
	startID, _ := mesh.AddTemporaryVertex(inferResult.X, inferResult.Y, startPointCostFactor, startPointMCD)
	targetID, _ := mesh.AddTemporaryVertex(target[0], target[1], endPointCostFactor, endPointMCD)
	path, err := mesh.FindPath(startID, targetID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to find NavMesh path for MapTrackerGoal")
		return false
	}

	moveParam := param.MapTrackerMoveParam
	moveParam.Path = path
	moveParam.MapName = param.MapName
	moveParamBytes, err := json.Marshal(moveParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal MapTrackerMove parameters for MapTrackerGoal")
		return false
	}

	log.Info().Str("map", param.MapName).
		Float64("startX", inferResult.X).
		Float64("startY", inferResult.Y).
		Float64("targetX", target[0]).
		Float64("targetY", target[1]).
		Int("pathCount", len(path)).
		Msg("MapTrackerGoal path generated")

	return (&MapTrackerMove{}).Run(ctx, &maa.CustomActionArg{
		TaskID:            arg.TaskID,
		CurrentTaskName:   arg.CurrentTaskName,
		CustomActionName:  "MapTrackerMove",
		CustomActionParam: string(moveParamBytes),
		RecognitionDetail: arg.RecognitionDetail,
		Box:               arg.Box,
	})
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
	return &param, nil
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
