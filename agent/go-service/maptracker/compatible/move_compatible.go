// Copyright (c) 2026 Harry Huang
package maptrackercompatible

import (
	"encoding/json"
	"fmt"
	"strings"

	maptrackerdefault "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/default"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &MapTrackerMoveCompatible{}

// MapTrackerMoveCompatible converts MapNavigateAction-style params and runs MapTrackerMove.
type MapTrackerMoveCompatible struct{}

type mapNavigateCompatibleParam struct {
	MapName                      string            `json:"map_name"`
	Path                         []json.RawMessage `json:"path"`
	NoPrint                      bool              `json:"no_print,omitempty"`
	PathTrim                     bool              `json:"path_trim,omitempty"`
	FineApproach                 string            `json:"fine_approach,omitempty"`
	NoEnsureInitialMovementState bool              `json:"no_ensure_initial_movement_state,omitempty"`
	ArrivalThreshold             float64           `json:"arrival_threshold,omitempty"`
	ArrivalTimeout               int64             `json:"arrival_timeout,omitempty"`
	RotationLowerThreshold       float64           `json:"rotation_lower_threshold,omitempty"`
	RotationUpperThreshold       float64           `json:"rotation_upper_threshold,omitempty"`
	SprintThreshold              float64           `json:"sprint_threshold,omitempty"`
	StuckThreshold               int64             `json:"stuck_threshold,omitempty"`
	StuckTimeout                 int64             `json:"stuck_timeout,omitempty"`
	StuckMitigators              []string          `json:"stuck_mitigators,omitempty"`
	MapNameMatchRule             string            `json:"map_name_match_rule,omitempty"`
	X                            *float64          `json:"x"`
	Y                            *float64          `json:"y"`
	Action                       any               `json:"action,omitempty"`
	Actions                      any               `json:"actions,omitempty"`
	Target                       []float64         `json:"target,omitempty"`
}

type compatibleWaypoint struct {
	X                  float64
	Y                  float64
	ZoneID             string
	HasPosition        bool
	UnsupportedActions []string
}

type compatibleWaypointObject struct {
	Action     any       `json:"action"`
	Actions    any       `json:"actions"`
	ZoneID     string    `json:"zone_id"`
	ZoneIDAlt  string    `json:"zoneId"`
	Zone       string    `json:"zone"`
	MapName    string    `json:"map_name"`
	MapNameAlt string    `json:"mapName"`
	X          *float64  `json:"x"`
	Y          *float64  `json:"y"`
	Target     []float64 `json:"target"`
}

func (a *MapTrackerMoveCompatible) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 1. Parse MapNavigateAction-compatible input.
	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerMoveCompatible")
		return false
	}
	if len(param.Path) == 0 {
		return true
	}

	// 2. Convert locator coordinates into MapTrackerMove parameters.
	moveParam, err := a.convertParam(param)
	if err != nil {
		log.Error().Err(err).Msg("Failed to convert parameters for MapTrackerMoveCompatible")
		return false
	}
	if len(moveParam.Path) == 0 {
		return true
	}

	// 3. Forward to the real MapTrackerMove runner.
	moveParamText, err := json.Marshal(moveParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to serialize MapTrackerMoveCompatible converted parameters")
		return false
	}

	log.Info().
		Str("map", moveParam.MapName).
		Int("targetsCount", len(moveParam.Path)).
		Msg("MapTrackerMoveCompatible converted parameters")

	forwardArg := *arg
	forwardArg.CustomActionName = "MapTrackerMove"
	forwardArg.CustomActionParam = string(moveParamText)
	return (&maptrackerdefault.MapTrackerMove{}).Run(ctx, &forwardArg)
}

func (a *MapTrackerMoveCompatible) parseParam(paramStr string) (*mapNavigateCompatibleParam, error) {
	if strings.TrimSpace(paramStr) == "" {
		return &mapNavigateCompatibleParam{}, nil
	}

	var param mapNavigateCompatibleParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}
	if len(param.Path) == 0 && (param.X != nil || param.Y != nil || param.Target != nil || param.Action != nil || param.Actions != nil) {
		raw := json.RawMessage(paramStr)
		param.Path = []json.RawMessage{raw}
	}
	return &param, nil
}

func (a *MapTrackerMoveCompatible) convertParam(param *mapNavigateCompatibleParam) (*maptrackerdefault.MapTrackerMoveParam, error) {
	// 1. Parse mixed array/object waypoints into a normalized list.
	waypoints, err := parseCompatibleWaypoints(param)
	if err != nil {
		return nil, err
	}
	if len(waypoints) == 0 {
		return nil, fmt.Errorf("MapTrackerMoveCompatible requires at least one coordinate waypoint")
	}

	// 2. Extract coordinate points from retained waypoints.
	points := make([]compatibleSourcePoint, 0, len(waypoints))
	for _, waypoint := range waypoints {
		if !waypoint.HasPosition {
			continue
		}

		for _, action := range waypoint.UnsupportedActions {
			log.Warn().Str("action", action).Msg("MapTrackerMoveCompatible ignores unsupported waypoint action")
		}

		sourceMapName := firstNonEmptyString(waypoint.ZoneID, param.MapName)
		if sourceMapName == "" {
			return nil, fmt.Errorf("waypoint is missing map_name or zone_id")
		}
		points = append(points, compatibleSourcePoint{SourceName: sourceMapName, X: waypoint.X, Y: waypoint.Y})
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("MapTrackerMoveCompatible requires at least one coordinate waypoint")
	}

	// 3. Convert source map coordinates into MapTracker coordinates.
	converted, err := convertCompatiblePoints(param.MapName, points)
	if err != nil {
		return nil, err
	}

	// 4. Preserve the MapTrackerMove options that have compatible semantics.
	return &maptrackerdefault.MapTrackerMoveParam{
		MapName:                      converted.MapName,
		Path:                         converted.Path,
		NoPrint:                      param.NoPrint,
		PathTrim:                     param.PathTrim,
		FineApproach:                 param.FineApproach,
		NoEnsureInitialMovementState: param.NoEnsureInitialMovementState,
		ArrivalThreshold:             param.ArrivalThreshold,
		ArrivalTimeout:               param.ArrivalTimeout,
		RotationLowerThreshold:       param.RotationLowerThreshold,
		RotationUpperThreshold:       param.RotationUpperThreshold,
		SprintThreshold:              param.SprintThreshold,
		StuckThreshold:               param.StuckThreshold,
		StuckTimeout:                 param.StuckTimeout,
		StuckMitigators:              param.StuckMitigators,
		MapNameMatchRule:             param.MapNameMatchRule,
	}, nil
}

func parseCompatibleWaypoints(param *mapNavigateCompatibleParam) ([]compatibleWaypoint, error) {
	waypoints := make([]compatibleWaypoint, 0, len(param.Path))
	zoneContext := param.MapName
	for index, raw := range param.Path {
		parsed, ok, err := parseCompatibleWaypoint(raw, zoneContext)
		if err != nil {
			return nil, fmt.Errorf("failed to parse waypoint %d: %w", index, err)
		}
		if !ok {
			continue
		}
		if parsed.ZoneID != "" {
			zoneContext = parsed.ZoneID
		}
		waypoints = append(waypoints, parsed)
	}
	return waypoints, nil
}

func parseCompatibleWaypoint(raw json.RawMessage, zoneContext string) (compatibleWaypoint, bool, error) {
	parseArray := func(items []any) (compatibleWaypoint, bool, error) {
		if len(items) < 2 {
			return compatibleWaypoint{}, false, nil
		}
		x, xOk := items[0].(float64)
		y, yOk := items[1].(float64)
		if !xOk || !yOk {
			return compatibleWaypoint{}, false, fmt.Errorf("array waypoint requires numeric x and y")
		}

		waypoint := compatibleWaypoint{X: x, Y: y, ZoneID: zoneContext, HasPosition: true}
		for _, item := range items[2:] {
			zone, ok := item.(string)
			if !ok {
				continue
			}
			if isCompatibleActionToken(zone) {
				if !isCompatiblePlainRunAction(zone) {
					waypoint.UnsupportedActions = append(waypoint.UnsupportedActions, strings.ToUpper(zone))
				}
				continue
			}
			waypoint.ZoneID = zone
		}
		return waypoint, true, nil
	}

	parseObject := func(object compatibleWaypointObject) (compatibleWaypoint, bool, error) {
		zoneID := firstNonEmptyString(object.ZoneID, object.ZoneIDAlt, object.Zone, object.MapName, object.MapNameAlt, zoneContext)
		actions := append(compatibleActionList(object.Action), compatibleActionList(object.Actions)...)
		primaryAction := ""
		if len(actions) > 0 {
			primaryAction = actions[0]
		}
		if primaryAction == "ZONE" {
			return compatibleWaypoint{ZoneID: zoneID}, zoneID != "", nil
		}
		if primaryAction == "NAVMESH" {
			log.Warn().Msg("MapTrackerMoveCompatible ignores unsupported NAVMESH waypoint")
			return compatibleWaypoint{}, false, nil
		}
		if primaryAction == "HEADING" {
			if len(object.Target) != 2 {
				log.Warn().Msg("MapTrackerMoveCompatible ignores unsupported heading-only waypoint")
				return compatibleWaypoint{}, false, nil
			}
			return compatibleWaypoint{X: object.Target[0], Y: object.Target[1], ZoneID: zoneID, HasPosition: true, UnsupportedActions: []string{"HEADING"}}, true, nil
		}
		if object.X != nil && object.Y != nil {
			return compatibleWaypoint{X: *object.X, Y: *object.Y, ZoneID: zoneID, HasPosition: true, UnsupportedActions: unsupportedCompatibleActions(actions)}, true, nil
		}
		return compatibleWaypoint{}, false, nil
	}

	var items []any
	if err := json.Unmarshal(raw, &items); err == nil {
		return parseArray(items)
	}

	var object compatibleWaypointObject
	if err := json.Unmarshal(raw, &object); err != nil {
		return compatibleWaypoint{}, false, err
	}
	return parseObject(object)
}

func unsupportedCompatibleActions(actions []string) []string {
	result := make([]string, 0, len(actions))
	for _, action := range actions {
		if !isCompatiblePlainRunAction(action) {
			result = append(result, strings.ToUpper(action))
		}
	}
	return result
}

func compatibleActionList(input any) []string {
	switch value := input.(type) {
	case string:
		return []string{strings.ToUpper(value)}
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				result = append(result, strings.ToUpper(text))
			}
		}
		return result
	default:
		return nil
	}
}

func isCompatiblePlainRunAction(text string) bool {
	return text == "" || strings.EqualFold(text, "RUN")
}

func isCompatibleActionToken(text string) bool {
	switch strings.ToUpper(text) {
	case "RUN", "SPRINT", "JUMP", "FIGHT", "INTERACT", "TRANSFER", "PORTAL", "HEADING", "NAVMESH", "ZONE", "COLLECT", "DIG":
		return true
	default:
		return false
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
