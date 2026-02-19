// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type MapTrackerAssertLocation struct{}

// LocationCondition represents a single condition to check
type LocationCondition struct {
	MapName string `json:"map_name"`
	Target  [4]int `json:"target"` // [x, y, w, h]
}

// MapTrackerAssertLocationParam represents the parameters for AssertLocation
type MapTrackerAssertLocationParam struct {
	Expected  []LocationCondition `json:"expected"`
	Precision float64             `json:"precision,omitempty"`
	Threshold float64             `json:"threshold,omitempty"`
}

var _ maa.CustomRecognitionRunner = &MapTrackerAssertLocation{}

// Run implements maa.CustomRecognitionRunner
func (r *MapTrackerAssertLocation) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// Parse parameters
	param, err := r.parseParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerAssertLocation")
		return nil, false
	}

	// Build map_name_regex based on expected conditions to focus the search
	mapNamesMap := make(map[string]struct{})
	var mapNames []string
	for _, condition := range param.Expected {
		if _, exists := mapNamesMap[condition.MapName]; !exists {
			mapNamesMap[condition.MapName] = struct{}{}
			mapNames = append(mapNames, regexp.QuoteMeta(condition.MapName))
		}
	}
	if len(mapNames) == 0 {
		log.Error().Msg("Failed to extract map names from expected conditions")
		return nil, false
	}

	mapNameRegex := "^(" + strings.Join(mapNames, "|") + ")$"

	// Prepare and run MapTrackerInfer
	nodeName := "MapTrackerAssertLocation_Infer"
	config := map[string]any{
		nodeName: map[string]any{
			"recognition":        "Custom",
			"custom_recognition": "MapTrackerInfer",
			"custom_recognition_param": map[string]any{
				"map_name_regex": mapNameRegex,
				"precision":      param.Precision,
				"threshold":      param.Threshold,
			},
		},
	}

	// We pass the same image provided to this recognition
	res, err := ctx.RunRecognition(nodeName, arg.Img, config)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run MapTrackerInfer during location assertion")
		return nil, false
	}
	if res == nil || res.DetailJson == "" {
		log.Info().Msg("Location assertion not satisfied, inference returned no result")
		return nil, false
	}

	// Extract inference result
	var result MapTrackerInferResult
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(res.DetailJson), &wrapped); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal wrapped inference result")
		return nil, false
	}
	if err := json.Unmarshal(wrapped.Best.Detail, &result); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal MapTrackerInferResult")
		return nil, false
	}

	// Check if current location satisfies any of the expected conditions
	for _, condition := range param.Expected {
		if result.MapName == condition.MapName {
			x, y, w, h := condition.Target[0], condition.Target[1], condition.Target[2], condition.Target[3]
			if result.X >= x && result.X < x+w && result.Y >= y && result.Y < y+h {
				log.Info().
					Interface("expected", condition).
					Msg("Location assertion satisfied")

				return &maa.CustomRecognitionResult{
					Box:    arg.Roi,
					Detail: res.DetailJson,
				}, true
			}
		}
	}

	log.Info().Msg("Location assertion not satisfied, no conditions met")
	return nil, false
}

func (r *MapTrackerAssertLocation) parseParam(paramStr string) (*MapTrackerAssertLocationParam, error) {
	var param MapTrackerAssertLocationParam
	if paramStr != "" {
		if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
			return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
		}
	}

	if len(param.Expected) == 0 {
		return nil, fmt.Errorf("expected conditions must be provided")
	}
	for i, condition := range param.Expected {
		if condition.MapName == "" {
			return nil, fmt.Errorf("map_name must be provided for expected condition at index %d", i)
		}
		if len(condition.Target) != 4 {
			return nil, fmt.Errorf("target must have 4 integers [x, y, w, h] for expected condition at index %d", i)
		}
		if condition.Target[2] <= 0 || condition.Target[3] <= 0 {
			return nil, fmt.Errorf("width and height in target must be positive for expected condition at index %d", i)
		}
	}
	// Precision and Threshold will be validated in MapTrackerInfer, omitted here

	return &param, nil
}
