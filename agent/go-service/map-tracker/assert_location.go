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
	MapName string     `json:"map_name"`
	Target  [4]float64 `json:"target"` // [x, y, w, h]
}

// MapTrackerAssertLocationParam represents the parameters for AssertLocation
type MapTrackerAssertLocationParam struct {
	// Expected is a list of conditions to check, using OR logic.
	Expected []LocationCondition `json:"expected"`
	// Precision controls the inference precision/speed tradeoff.
	Precision float64 `json:"precision,omitempty"`
	// Threshold controls the minimum confidence required to consider the inference successful.
	Threshold float64 `json:"threshold,omitempty"`
	// Whether to enable fast mode for matching.
	FastMode bool `json:"fast_mode,omitempty"`
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

	mapNameRegex := ".*"
	if param.FastMode {
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

		mapNameRegex = "^(" + strings.Join(mapNames, "|") + ")$"
	}

	// Prepare and run MapTrackerInfer
	inferConfig := map[string]any{
		"map_name_regex": mapNameRegex,
		"precision":      param.Precision,
		"threshold":      param.Threshold,
	}

	inferConfigBytes, err := json.Marshal(inferConfig)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal inference config")
		return nil, false
	}

	taskDetail, err := ctx.GetTaskJob().GetDetail()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get task detail")
		return nil, false
	}

	resultWrapper, hit := mapTrackerInferRunner.Run(ctx, &maa.CustomRecognitionArg{
		TaskID:                 taskDetail.ID,
		CurrentTaskName:        taskDetail.Entry,
		CustomRecognitionName:  "MapTrackerInfer",
		CustomRecognitionParam: string(inferConfigBytes),
		Img:                    arg.Img,
		Roi:                    arg.Roi,
	})

	if !hit {
		log.Info().Msg("Location assertion not satisfied, inference not hit")
		return nil, false
	}
	if resultWrapper == nil || resultWrapper.Detail == "" {
		log.Info().Msg("Location assertion not satisfied, inference returned no result")
		return nil, false
	}

	// Extract result
	var result MapTrackerInferResult
	if err := json.Unmarshal([]byte(resultWrapper.Detail), &result); err != nil {
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
					Detail: resultWrapper.Detail,
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
			return nil, fmt.Errorf("target must have 4 numbers [x, y, w, h] for expected condition at index %d", i)
		}
		if condition.Target[2] <= 0 || condition.Target[3] <= 0 {
			return nil, fmt.Errorf("width and height in target must be positive for expected condition at index %d", i)
		}
	}
	// Precision and Threshold will be validated in MapTrackerInfer, omitted here

	return &param, nil
}
