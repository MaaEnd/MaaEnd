package aerosalvage

import (
	"encoding/json"
	"fmt"
	"image"
	"math"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

type gridCoordinate struct {
	Row    int     `json:"row"`
	Column int     `json:"column"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

type recognitionDetail struct {
	GridPoints     []gridCoordinate `json:"grid_points"`
	PlacementSites []gridPosition   `json:"placement_sites"`
}

type gridPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type recognitionParam struct {
	GridROI   [4]int `json:"grid_roi"`
	CenterROI [4]int `json:"center_roi"`
}

const placementSiteTemplateNode = "AeroSalvagePlacementSiteTemplate"

// Recognition detects the Aerial Salvage lattice and placement sites.
type Recognition struct{}

// Run returns the 25 row-major grid coordinates and placement-site positions in Detail.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", "AeroSalvageRecognition").Msg("custom recognition arg or image is nil")
		return nil, false
	}
	params, err := parseRecognitionParam(arg.CustomRecognitionParam)
	if err != nil {
		return recognitionError("parse parameters", err)
	}
	lineConfig := gridLineConfig(rectangle(params.GridROI))
	gridConfig := gridPointConfig(rectangle(params.CenterROI))

	detected, err := DetectGridLines(arg.Img, lineConfig)
	if err != nil {
		return recognitionError("detect grid lines", err)
	}
	cleaned, err := CleanseGridLines(detected, cleanseConfig())
	if err != nil {
		return recognitionError("cleanse grid lines", err)
	}
	grid, err := DetectGridPoints(cleaned, detected.ROI, gridConfig)
	if err != nil {
		return recognitionError("detect grid points", err)
	}
	if len(grid.Points) != 25 {
		log.Warn().Str("component", "AeroSalvageRecognition").Int("grid_points", len(grid.Points)).Msg("unexpected grid point count")
		return nil, false
	}

	detail := recognitionDetail{
		GridPoints:     make([]gridCoordinate, 0, len(grid.Points)),
		PlacementSites: []gridPosition{},
	}
	for _, point := range grid.Points {
		detail.GridPoints = append(detail.GridPoints, gridCoordinate{
			Row: point.Row, Column: point.Column, X: point.Center.X, Y: point.Center.Y,
		})
	}
	placementROIs, err := recognizePlacementSites(ctx, arg.Img)
	if err != nil {
		log.Warn().Err(err).Str("component", "AeroSalvageRecognition").Msg("failed to recognize placement sites")
	} else {
		detail.PlacementSites = nearestGridPositions(placementROIs, grid.Points)
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return recognitionError("marshal result", err)
	}

	log.Debug().
		Str("component", "AeroSalvageRecognition").
		Int("grid_points", len(detail.GridPoints)).
		Int("placement_sites", len(detail.PlacementSites)).
		Msg("aerial salvage recognized")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailJSON)}, true
}

func recognizePlacementSites(ctx *maa.Context, img image.Image) ([]image.Rectangle, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil context")
	}
	detail, err := ctx.RunRecognition(placementSiteTemplateNode, img, nil)
	if err != nil {
		return nil, err
	}
	if detail == nil || detail.Results == nil {
		return nil, nil
	}

	seen := make(map[image.Rectangle]struct{}, len(detail.Results.Filtered))
	rois := make([]image.Rectangle, 0, len(detail.Results.Filtered))
	for _, result := range detail.Results.Filtered {
		if result == nil {
			continue
		}
		matched, ok := result.AsTemplateMatch()
		if !ok || matched == nil {
			continue
		}
		roi := image.Rect(
			matched.Box.X(),
			matched.Box.Y(),
			matched.Box.X()+matched.Box.Width(),
			matched.Box.Y()+matched.Box.Height(),
		)
		if roi.Empty() {
			continue
		}
		if _, ok := seen[roi]; ok {
			continue
		}
		seen[roi] = struct{}{}
		rois = append(rois, roi)
	}
	return rois, nil
}

func nearestGridPositions(rois []image.Rectangle, points []GridPoint) []gridPosition {
	positions := make([]gridPosition, 0, len(rois))
	seen := make(map[gridPosition]struct{}, len(rois))
	for _, roi := range rois {
		centerX := float64(roi.Min.X+roi.Max.X) / 2
		centerY := float64(roi.Min.Y+roi.Max.Y) / 2
		nearest := -1
		nearestDistance := math.MaxFloat64
		for index, point := range points {
			deltaX := point.Center.X - centerX
			deltaY := point.Center.Y - centerY
			distance := deltaX*deltaX + deltaY*deltaY
			if distance < nearestDistance {
				nearest = index
				nearestDistance = distance
			}
		}
		if nearest < 0 {
			continue
		}
		position := gridPosition{X: points[nearest].Column - 2, Y: points[nearest].Row - 2}
		if _, ok := seen[position]; ok {
			continue
		}
		seen[position] = struct{}{}
		positions = append(positions, position)
	}
	return positions
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
	log.Warn().Err(err).Str("component", "AeroSalvageRecognition").Str("step", step).Msg("recognition failed")
	return nil, false
}
