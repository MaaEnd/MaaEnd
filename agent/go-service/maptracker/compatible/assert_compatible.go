// Copyright (c) 2026 Harry Huang
package maptrackercompatible

import (
	"encoding/json"
	"fmt"

	maptrackerdefault "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/default"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &MapTrackerAssertLocationCompatible{}

// MapTrackerAssertLocationCompatible converts MapLocateAssertLocation-style params and runs MapTrackerAssertLocation.
type MapTrackerAssertLocationCompatible struct{}

type mapLocateAssertCompatibleParam struct {
	ZoneID            string     `json:"zone_id"`
	ZoneIDAlt         string     `json:"zoneId"`
	Zone              string     `json:"zone"`
	Target            [4]float64 `json:"target"`
	LocThreshold      float64    `json:"loc_threshold,omitempty"`
	YoloThreshold     float64    `json:"yolo_threshold,omitempty"`
	ForceGlobalSearch bool       `json:"force_global_search,omitempty"`
}

func (r *MapTrackerAssertLocationCompatible) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	param, err := r.parseParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerAssertLocationCompatible")
		return nil, false
	}

	trackerParam, err := r.convertParam(param)
	if err != nil {
		log.Error().Err(err).Msg("Failed to convert parameters for MapTrackerAssertLocationCompatible")
		return nil, false
	}

	trackerParamText, err := json.Marshal(trackerParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to serialize MapTrackerAssertLocationCompatible converted parameters")
		return nil, false
	}

	forwardArg := *arg
	forwardArg.CustomRecognitionName = "MapTrackerAssertLocation"
	forwardArg.CustomRecognitionParam = string(trackerParamText)
	return (&maptrackerdefault.MapTrackerAssertLocation{}).Run(ctx, &forwardArg)
}

func (r *MapTrackerAssertLocationCompatible) parseParam(paramStr string) (*mapLocateAssertCompatibleParam, error) {
	if paramStr == "" {
		return nil, fmt.Errorf("custom_recognition_param is required")
	}

	var param mapLocateAssertCompatibleParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}
	if firstNonEmptyString(param.ZoneID, param.ZoneIDAlt, param.Zone) == "" {
		return nil, fmt.Errorf("zone_id is required")
	}
	if param.Target[2] <= 0 || param.Target[3] <= 0 {
		return nil, fmt.Errorf("target width and height must be positive")
	}
	return &param, nil
}

func (r *MapTrackerAssertLocationCompatible) convertParam(param *mapLocateAssertCompatibleParam) (*maptrackerdefault.MapTrackerAssertLocationParam, error) {
	zoneID := firstNonEmptyString(param.ZoneID, param.ZoneIDAlt, param.Zone)
	converted, err := convertCompatibleRect("", compatibleSourceRect{
		SourceName: zoneID,
		X:          param.Target[0],
		Y:          param.Target[1],
		W:          param.Target[2],
		H:          param.Target[3],
	})
	if err != nil {
		return nil, err
	}

	return &maptrackerdefault.MapTrackerAssertLocationParam{
		Expected: []maptrackerdefault.LocationCondition{
			{
				MapName: converted.MapName,
				Target:  converted.Target,
			},
		},
		Threshold: param.LocThreshold,
		FastMode:  true,
	}, nil
}
