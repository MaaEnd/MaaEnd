// Copyright (c) 2026 Harry Huang
package maptrackerbigmap

import (
	"encoding/json"
	"fmt"
	"image"
	"math"

	internal "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerBigMapZoom adjusts the current big-map zoom slider to a target position.
type MapTrackerBigMapZoom struct{}

// MapTrackerBigMapZoomParam represents the custom_action_param for MapTrackerBigMapZoom.
type MapTrackerBigMapZoomParam struct {
	// ZoomValue is the target zoom slider position.
	// Set to 0 or omitted to disable the zoom action. Other values should be in range (0, 1],
	// where 1.0 is zoom-out end and values near 0 are zoom-in end.
	ZoomValue float64 `json:"zoom_value,omitempty"`
}

const (
	SLIDER_UI_TRIGGER_MS = 100
	SLIDER_UI_DELAY_MS   = 250
	ZOOMING_RESPONSE_MS  = 250
)

var _ maa.CustomActionRunner = &MapTrackerBigMapZoom{}

// Run implements maa.CustomActionRunner.
func (a *MapTrackerBigMapZoom) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerBigMapZoom")
		return false
	}

	if param.ZoomValue == 0 {
		log.Info().Msg("Zoom value is set to 0, skipping MapTrackerBigMapZoom")
		return true
	}

	ctrl := ctx.GetTasker().GetController()
	ca, err := control.NewControlAdaptor(ctx, ctrl, WORK_W, WORK_H)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create control adaptor")
		return false
	}

	if err := doBigMapZoom(ctrl, ca, param.ZoomValue); err != nil {
		log.Error().Err(err).Float64("zoomValue", param.ZoomValue).Msg("Failed to adjust big-map zoom")
		return false
	}

	return true
}

func (a *MapTrackerBigMapZoom) parseParam(paramStr string) (*MapTrackerBigMapZoomParam, error) {
	if paramStr == "" {
		return nil, fmt.Errorf("zoom_value is required")
	}

	var param MapTrackerBigMapZoomParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
	}
	if !(0 <= param.ZoomValue && param.ZoomValue <= 1) {
		return nil, fmt.Errorf("zoom_value must be in range [0, 1]")
	}

	return &param, nil
}

func doBigMapZoom(ctrl *maa.Controller, ca control.ControlAdaptor, position float64) error {
	// Zoom-in (+) button template
	zoomInTemplate, err := internal.Resource.ZoomInTemplate.Get()
	if err != nil {
		return fmt.Errorf("failed to load zoom-in template: %w", err)
	}

	// Zoom-out (-) button template
	zoomOutTemplate, err := internal.Resource.ZoomOutTemplate.Get()
	if err != nil {
		return fmt.Errorf("failed to load zoom-out template: %w", err)
	}

	triedAgain := false
	for {
		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil {
			return fmt.Errorf("failed to get cached image for auto zoom: %w", err)
		}
		if img == nil {
			return fmt.Errorf("cached image is nil for auto zoom")
		}

		screen := minicv.ImageConvertRGBA(img)
		searchArea := [4]int{
			int(math.Round(ZOOM_BUTTON_AREA_X)),
			int(math.Round(ZOOM_BUTTON_AREA_Y)),
			int(math.Round(ZOOM_BUTTON_AREA_W)),
			int(math.Round(ZOOM_BUTTON_AREA_H)),
		}
		screenIntegral := minicv.GetIntegralArray(screen)

		zoomOutX, zoomOutY, outVal := minicv.MatchTemplateInArea(
			screen,
			screenIntegral,
			zoomOutTemplate.Image,
			zoomOutTemplate.Stats,
			searchArea,
		)
		zoomInX, zoomInY, inVal := minicv.MatchTemplateInArea(
			screen,
			screenIntegral,
			zoomInTemplate.Image,
			zoomInTemplate.Stats,
			searchArea,
		)

		outMatched := outVal >= ZOOM_BUTTON_THRESHOLD
		inMatched := inVal >= ZOOM_BUTTON_THRESHOLD

		log.Debug().
			Float64("zoomOutBtn", outVal).
			Float64("zoomInBtn", inVal).
			Msg("Big-map zoom buttons template match completed")

		if outMatched && inMatched {
			// Good case: both zoom-out and zoom-in buttons are detected, likely showing the zoom slider,
			// we can click the slider area to adjust zoom level precisely.
			cx := int(math.Round((zoomOutX + zoomInX) / 2.0))
			zoomInYFixed := zoomInY + float64(zoomInTemplate.Image.Rect.Dy()) // Use the bottom edge of zoom-in button as the slider baseline
			cy := int(math.Round(zoomInYFixed + (zoomOutY-zoomInYFixed)*position))
			ca.TouchClick(0, cx, cy, SLIDER_UI_TRIGGER_MS, SLIDER_UI_DELAY_MS)
			ca.TouchMove(0, 1, 1, ZOOMING_RESPONSE_MS)

			log.Info().Float64("position", position).Msg("Big-map zoom adjusted by clicking slider area")
			return nil // Finished auto zoom
		} else if !outMatched && !inMatched {
			// Worst case: neither zoom-out nor zoom-in button is detected,
			// just skip auto zoom to avoid wrong operation.
			log.Warn().Msg("No zoom button matched for big-map zoom")
			return nil // Skipped auto zoom
		} else {
			// Not good case: only one of the two buttons is detected, due to the current zoom level hit the limit,
			// we can press the detected button to adjust zoom blindly, then start another round of detection.
			pressZoomButton := func(matchX, matchY float64, tpl *image.RGBA) {
				cx := int(math.Round(matchX + float64(tpl.Rect.Dx())/2.0))
				cy := int(math.Round(matchY + float64(tpl.Rect.Dy())/2.0))
				ca.TouchClick(0, cx, cy, SLIDER_UI_TRIGGER_MS, SLIDER_UI_DELAY_MS)
				ca.TouchMove(0, 1, 1, ZOOMING_RESPONSE_MS)
			}

			if outMatched {
				pressZoomButton(zoomOutX, zoomOutY, zoomOutTemplate.Image)
				log.Info().Msg("Big-map zoom adjusted by pressing zoom-out button")
			} else {
				pressZoomButton(zoomInX, zoomInY, zoomInTemplate.Image)
				log.Info().Msg("Big-map zoom adjusted by pressing zoom-in button")
			}

			if triedAgain {
				log.Warn().Msg("Still only one button appeared (expect two), giving up big-map zoom")
				return nil // Avoid infinite loop, give up after trying again
			}
			triedAgain = true
		}
	}
}
