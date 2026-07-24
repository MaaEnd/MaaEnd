package aerosalvage

import (
	"fmt"
	"image"
	"math"
	"regexp"
	"sort"
	"strconv"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &InitialStateRecognition{}

const placementSiteTemplateNode = "AeroSalvagePlacementSiteTemplate"

var firstIntegerPattern = regexp.MustCompile(`\d+`)

type gridPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}

var placementSitePositions []gridPosition
var balloonConfigs map[int]balloonConfig

type balloonConfig struct {
	Value     int
	Count     int
	CountNode string
}

// InitialStateRecognition recognizes and caches the Aerial Salvage initial state.
type InitialStateRecognition struct{}

// Run clears the initial-state caches, then recognizes and caches the current placement sites and balloon configurations.
func (r *InitialStateRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	placementSitePositions = nil
	balloonConfigs = nil
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", "AeroSalvageInitialStateRecognition").Msg("custom recognition arg or image is nil")
		return nil, false
	}
	params, err := parseRecognitionParam(arg.CustomRecognitionParam)
	if err != nil {
		return initialStateRecognitionError("parse parameters", err)
	}
	points, err := detectGridPoints(arg.Img, params)
	if err != nil {
		return initialStateRecognitionError("detect grid", err)
	}
	if len(points) != 25 {
		log.Warn().Str("component", "AeroSalvageInitialStateRecognition").Int("grid_points", len(points)).Msg("unexpected grid point count")
		return nil, false
	}
	rois, err := recognizePlacementSites(ctx, arg.Img)
	if err != nil {
		return initialStateRecognitionError("recognize placement sites", err)
	}
	positions := nearestGridPositions(rois, points)
	if len(positions) == 0 {
		log.Warn().Str("component", "AeroSalvageInitialStateRecognition").Msg("no placement sites recognized")
		return nil, false
	}
	configs, err := recognizeBalloonConfigs(ctx, arg.Img)
	if err != nil {
		return initialStateRecognitionError("recognize balloon configurations", err)
	}
	if len(configs) == 0 {
		log.Warn().Str("component", "AeroSalvageInitialStateRecognition").Msg("no balloon configurations recognized")
		return nil, false
	}
	placementSitePositions = positions
	balloonConfigs = configs
	log.Debug().
		Str("component", "AeroSalvageInitialStateRecognition").
		Int("placement_sites", len(positions)).
		Int("balloon_configs", len(configs)).
		Interface("placement_site_positions", positions).
		Interface("balloon_configs", sortedBalloonConfigs(configs)).
		Msg("initial state recognized")
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

func recognizeBalloonConfigs(ctx *maa.Context, img image.Image) (map[int]balloonConfig, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil context")
	}

	configs := make(map[int]balloonConfig, 4)
	for slot := 1; slot <= 4; slot++ {
		rawValue, valueFound, err := recognizeBalloonNumber(ctx, img, fmt.Sprintf("AeroSalvageBalloonText%d", slot))
		if err != nil {
			return nil, err
		}
		countNode := fmt.Sprintf("AeroSalvageBalloonCount%d", slot)
		count, countFound, err := recognizeBalloonNumber(ctx, img, countNode)
		if err != nil {
			return nil, err
		}
		if !valueFound && !countFound {
			continue
		}
		if !valueFound || !countFound {
			return nil, fmt.Errorf("balloon slot %d has incomplete OCR results", slot)
		}
		value, ok := balloonValue(rawValue)
		if !ok {
			return nil, fmt.Errorf("balloon slot %d has unsupported raw value %d", slot, rawValue)
		}
		if _, exists := configs[value]; exists {
			return nil, fmt.Errorf("balloon value %d is duplicated", value)
		}
		configs[value] = balloonConfig{Value: value, Count: count, CountNode: countNode}
	}
	return configs, nil
}

func balloonValue(rawValue int) (int, bool) {
	switch rawValue {
	case 1, 2, 3:
		return rawValue, true
	case 4:
		return 6, true
	default:
		return 0, false
	}
}

func sortedBalloonConfigs(configs map[int]balloonConfig) []balloonConfig {
	ordered := make([]balloonConfig, 0, len(configs))
	for _, config := range configs {
		ordered = append(ordered, config)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].CountNode < ordered[j].CountNode
	})
	return ordered
}

func recognizeBalloonNumber(ctx *maa.Context, img image.Image, nodeName string) (int, bool, error) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil {
		return 0, false, fmt.Errorf("run %s: %w", nodeName, err)
	}
	text, found := firstOCRText(detail)
	if !found {
		return 0, false, nil
	}
	match := firstIntegerPattern.FindString(text)
	if match == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(match)
	if err != nil {
		return 0, false, fmt.Errorf("parse %s OCR number %q: %w", nodeName, match, err)
	}
	return value, true, nil
}

func firstOCRText(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || detail.Results == nil {
		return "", false
	}
	for _, results := range [][]*maa.RecognitionResult{{detail.Results.Best}, detail.Results.Filtered, detail.Results.All} {
		for _, result := range results {
			if result == nil {
				continue
			}
			ocr, ok := result.AsOCR()
			if ok && ocr != nil && ocr.Text != "" {
				return ocr.Text, true
			}
		}
	}
	return "", false
}

func initialStateRecognitionError(step string, err error) (*maa.CustomRecognitionResult, bool) {
	log.Warn().Err(err).Str("component", "AeroSalvageInitialStateRecognition").Str("step", step).Msg("recognition failed")
	return nil, false
}
