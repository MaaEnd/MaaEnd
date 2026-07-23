package aerosalvage

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

type coordinate struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type gridCoordinate struct {
	Row    int     `json:"row"`
	Column int     `json:"column"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

type recognitionDetail struct {
	GridPoints []gridCoordinate `json:"grid_points"`
	Targets    []coordinate     `json:"targets"`
}

// Recognition detects the Aerial Salvage lattice and yellow targets.
type Recognition struct{}

// Run returns the 25 row-major grid coordinates and all target coordinates in Detail.
func (r *Recognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", "AeroSalvageRecognition").Msg("custom recognition arg or image is nil")
		return nil, false
	}

	detected, err := DetectGridLines(arg.Img, DefaultConfig())
	if err != nil {
		return recognitionError("detect grid lines", err)
	}
	cleaned, err := CleanseGridLines(detected, DefaultCleanseConfig())
	if err != nil {
		return recognitionError("cleanse grid lines", err)
	}
	grid, err := DetectGridPoints(cleaned, detected.ROI, DefaultGridPointConfig())
	if err != nil {
		return recognitionError("detect grid points", err)
	}
	if len(grid.Points) != 25 {
		log.Warn().Str("component", "AeroSalvageRecognition").Int("grid_points", len(grid.Points)).Msg("unexpected grid point count")
		return nil, false
	}

	detail := recognitionDetail{
		GridPoints: make([]gridCoordinate, 0, len(grid.Points)),
		Targets:    make([]coordinate, 0),
	}
	for _, point := range grid.Points {
		detail.GridPoints = append(detail.GridPoints, gridCoordinate{
			Row: point.Row, Column: point.Column, X: point.Center.X, Y: point.Center.Y,
		})
	}
	for _, target := range DetectTargets(detected.InterferenceMask, detected.ROI) {
		detail.Targets = append(detail.Targets, coordinate{X: target.X, Y: target.Y})
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return recognitionError("marshal result", err)
	}

	log.Debug().
		Str("component", "AeroSalvageRecognition").
		Int("grid_points", len(detail.GridPoints)).
		Int("targets", len(detail.Targets)).
		Msg("aerial salvage recognized")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailJSON)}, true
}

func recognitionError(step string, err error) (*maa.CustomRecognitionResult, bool) {
	log.Warn().Err(err).Str("component", "AeroSalvageRecognition").Str("step", step).Msg("recognition failed")
	return nil, false
}
