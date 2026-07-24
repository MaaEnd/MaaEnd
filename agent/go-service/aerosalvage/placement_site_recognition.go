package aerosalvage

import (
	"fmt"
	"image"
	"math"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &PlacementSiteRecognition{}

const placementSiteTemplateNode = "AeroSalvagePlacementSiteTemplate"

type gridPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}

var placementSitePositions []gridPosition

// PlacementSiteRecognition caches placement-site grid positions after Pipeline confirms the balanced state.
type PlacementSiteRecognition struct{}

// Run clears the placement-site cache, then recognizes and caches the current placement-site grid positions.
func (r *PlacementSiteRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	placementSitePositions = nil
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", "AeroSalvagePlacementSiteRecognition").Msg("custom recognition arg or image is nil")
		return nil, false
	}
	params, err := parseRecognitionParam(arg.CustomRecognitionParam)
	if err != nil {
		return placementSiteRecognitionError("parse parameters", err)
	}
	points, err := detectGridPoints(arg.Img, params)
	if err != nil {
		return placementSiteRecognitionError("detect grid", err)
	}
	if len(points) != 25 {
		log.Warn().Str("component", "AeroSalvagePlacementSiteRecognition").Int("grid_points", len(points)).Msg("unexpected grid point count")
		return nil, false
	}
	rois, err := recognizePlacementSites(ctx, arg.Img)
	if err != nil {
		return placementSiteRecognitionError("recognize placement sites", err)
	}
	positions := nearestGridPositions(rois, points)
	if len(positions) == 0 {
		log.Warn().Str("component", "AeroSalvagePlacementSiteRecognition").Msg("no placement sites recognized")
		return nil, false
	}
	placementSitePositions = positions
	log.Debug().Str("component", "AeroSalvagePlacementSiteRecognition").Int("placement_sites", len(positions)).Msg("placement sites recognized")
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
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
		roi := image.Rect(matched.Box.X(), matched.Box.Y(), matched.Box.X()+matched.Box.Width(), matched.Box.Y()+matched.Box.Height())
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

func placementSiteRecognitionError(step string, err error) (*maa.CustomRecognitionResult, bool) {
	log.Warn().Err(err).Str("component", "AeroSalvagePlacementSiteRecognition").Str("step", step).Msg("recognition failed")
	return nil, false
}
