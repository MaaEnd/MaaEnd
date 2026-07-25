package aerosalvage

import (
	"encoding/json"
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &GridRecognition{}

type gridCoordinate struct {
	Row    int     `json:"row"`
	Column int     `json:"column"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

type recognitionDetail struct {
	GridPoints []gridCoordinate `json:"grid_points"`
}

type recognitionParam struct {
	GridROI   [4]int `json:"grid_roi"`
	CenterROI [4]int `json:"center_roi"`
}

// GridRecognition detects the Aerial Salvage lattice.
type GridRecognition struct{}

// Run returns the 25 row-major grid coordinates in Detail.
func (r *GridRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", "AeroSalvageGridRecognition").Msg("custom recognition arg or image is nil")
		return nil, false
	}
	params, err := parseRecognitionParam(arg.CustomRecognitionParam)
	if err != nil {
		return recognitionError("parse parameters", err)
	}
	points, err := detectGridPoints(arg.Img, params)
	if err != nil {
		return recognitionError("detect grid", err)
	}
	if len(points) != 25 {
		log.Warn().Str("component", "AeroSalvageGridRecognition").Int("grid_points", len(points)).Msg("unexpected grid point count")
		return nil, false
	}

	detail := recognitionDetail{
		GridPoints: make([]gridCoordinate, 0, len(points)),
	}
	for _, point := range points {
		detail.GridPoints = append(detail.GridPoints, gridCoordinate{
			Row: point.Row, Column: point.Column, X: point.Center.X, Y: point.Center.Y,
		})
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return recognitionError("marshal result", err)
	}

	log.Debug().
		Str("component", "AeroSalvageGridRecognition").
		Int("grid_points", len(detail.GridPoints)).
		Msg("aerial salvage recognized")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailJSON)}, true
}

func detectGridPoints(img image.Image, params recognitionParam) ([]GridPoint, error) {
	detected, err := DetectGridLines(img, gridLineConfig(rectangle(params.GridROI)))
	if err != nil {
		return nil, err
	}
	cleaned, err := CleanseGridLines(detected, cleanseConfig())
	if err != nil {
		return nil, err
	}
	grid, err := DetectGridPoints(cleaned, detected.ROI, gridPointConfig(rectangle(params.CenterROI)))
	if err != nil {
		return nil, err
	}
	return grid.Points, nil
}

func parseRecognitionParam(raw string) (recognitionParam, error) {
	if raw == "" {
		return recognitionParam{}, fmt.Errorf("custom recognition parameters are empty")
	}
	var params recognitionParam
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return recognitionParam{}, fmt.Errorf("unmarshal custom recognition parameters: %w", err)
	}
	return params, nil
}

func rectangle(values [4]int) image.Rectangle {
	return image.Rect(values[0], values[1], values[0]+values[2], values[1]+values[3])
}

func recognitionError(step string, err error) (*maa.CustomRecognitionResult, bool) {
	log.Warn().Err(err).Str("component", "AeroSalvageGridRecognition").Str("step", step).Msg("recognition failed")
	return nil, false
}
