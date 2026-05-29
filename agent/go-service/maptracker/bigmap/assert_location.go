// Copyright (c) 2026 Harry Huang
package maptrackerbigmap

import (
	"encoding/json"
	"fmt"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// LocationCondition represents a single condition to check
type LocationCondition struct {
	MapName string     `json:"map_name"`
	Target  [4]float64 `json:"target"` // [x, y, w, h]
}

// MapTrackerBigMapAssertLocationParam represents the parameters for AssertLocation
type MapTrackerBigMapAssertLocationParam struct {
	// Expected is a list of conditions to check, using OR logic.
	Expected []LocationCondition `json:"expected"`
	// Threshold controls the minimum confidence required to consider the inference successful.
	Threshold float64 `json:"threshold,omitempty"`
	// ColorFilter is the configuration for filtering out a specific color overlay.
	// If not set or empty, no color correction will be applied.
	ColorFilter ColorFilterConfig `json:"color_filter,omitempty"`
}

var _ maa.CustomRecognitionRunner = &MapTrackerBigMapAssertLocation{}

type MapTrackerBigMapAssertLocation struct{}

// Run implements maa.CustomRecognitionRunner
func (r *MapTrackerBigMapAssertLocation) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// Parse parameters
	param, err := r.parseParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerBigMapAssertLocation")
		return nil, false
	}

	// Run big map inference to get current viewport
	inferConfig := map[string]any{
		"threshold": param.Threshold,
	}
	// Apply color filter if configured, otherwise use default golden overlay removal
	if param.ColorFilter.R != 0 || param.ColorFilter.G != 0 || param.ColorFilter.B != 0 {
		inferConfig["color_filter"] = param.ColorFilter
	} else {
		// Default UI overlay color (RGB: [72, 65, 7]) at screen center interferes with map recognition
		inferConfig["color_filter"] = ColorFilterConfig{
			R:         72,
			G:         65,
			B:         7,
			Threshold: 30,
		}
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

	resultWrapper, hit := MapTrackerBigMapInferRunner.Run(ctx, &maa.CustomRecognitionArg{
		TaskID:                 taskDetail.ID,
		CurrentTaskName:        taskDetail.Entry,
		CustomRecognitionName:  "MapTrackerBigMapInfer",
		CustomRecognitionParam: string(inferConfigBytes),
		Img:                    arg.Img,
		Roi:                    arg.Roi,
	})

	if !hit {
		log.Info().Msg("Big-map location assertion not satisfied, inference not hit")
		return nil, false
	}
	if resultWrapper == nil || resultWrapper.Detail == "" {
		log.Info().Msg("Big-map location assertion not satisfied, inference returned no result")
		return nil, false
	}

	// Extract result
	var result MapTrackerBigMapInferResult
	if err := json.Unmarshal([]byte(resultWrapper.Detail), &result); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal MapTrackerBigMapInferResult")
		return nil, false
	}

	// Calculate screen center point (based on 1280x720 resolution)
	screenCenterX := WORK_W / 2.0
	screenCenterY := WORK_H / 2.0

	// Convert screen center to map coordinates
	centerMapX, centerMapY := result.ViewPort.GetMapCoordOf(screenCenterX, screenCenterY)

	// Check if current location satisfies any of the expected conditions
	for _, condition := range param.Expected {
		if result.MapName == condition.MapName {
			x, y, w, h := condition.Target[0], condition.Target[1], condition.Target[2], condition.Target[3]
			if centerMapX >= x && centerMapX < x+w && centerMapY >= y && centerMapY < y+h {
				log.Info().
					Interface("expected", condition).
					Float64("centerMapX", centerMapX).
					Float64("centerMapY", centerMapY).
					Msg("Big-map location assertion satisfied")

				return &maa.CustomRecognitionResult{
					Box:    arg.Roi,
					Detail: resultWrapper.Detail,
				}, true
			}
		}
	}

	log.Info().
		Str("mapName", result.MapName).
		Float64("centerMapX", centerMapX).
		Float64("centerMapY", centerMapY).
		Msg("Big-map location assertion not satisfied, no conditions met")
	return nil, false
}

func (r *MapTrackerBigMapAssertLocation) parseParam(paramStr string) (*MapTrackerBigMapAssertLocationParam, error) {
	var param MapTrackerBigMapAssertLocationParam
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

	if param.Threshold == 0.0 {
		param.Threshold = 0.5
	} else if param.Threshold < 0.0 || param.Threshold > 1.0 {
		return nil, fmt.Errorf("invalid threshold value: %f", param.Threshold)
	}

	return &param, nil
}
