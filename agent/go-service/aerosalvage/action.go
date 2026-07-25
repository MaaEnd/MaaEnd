package aerosalvage

import (
	"encoding/json"
	"fmt"
	"math"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	aeroSalvageConfigureSwipeActionName = "AeroSalvageConfigureSwipeAction"
	aeroSalvageSwipeBalloonNode         = "AeroSalvageSwipeBalloon"
)

var _ maa.CustomActionRunner = &ConfigureSwipeAction{}

// ConfigureSwipeAction configures the next balloon swipe from the current grid recognition.
type ConfigureSwipeAction struct{}

// Run applies the current placement-plan entry to the next Swipe node, then consumes the entry.
func (a *ConfigureSwipeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", aeroSalvageConfigureSwipeActionName).Msg("context or custom action arg is nil")
		return false
	}

	override, nextIndex, placement, end, err := buildSwipeOverride(arg.RecognitionDetail, balloonPlacements, balloonPlacementIndex)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", aeroSalvageConfigureSwipeActionName).
			Int("placement_index", balloonPlacementIndex).
			Int("placement_count", len(balloonPlacements)).
			Msg("failed to configure balloon swipe")
		return false
	}
	if err := ctx.OverridePipeline(override); err != nil {
		log.Error().
			Err(err).
			Str("component", aeroSalvageConfigureSwipeActionName).
			Int("placement_index", balloonPlacementIndex).
			Msg("failed to override balloon swipe")
		return false
	}

	balloonPlacementIndex = nextIndex
	log.Debug().
		Str("component", aeroSalvageConfigureSwipeActionName).
		Str("balloon_count_node", placement.NodeName).
		Interface("target_position", placement.TargetPos).
		Ints("swipe_end", end).
		Int("next_placement_index", balloonPlacementIndex).
		Msg("balloon swipe configured")
	return true
}

func buildSwipeOverride(detail *maa.RecognitionDetail, placements []balloonPlacement, index int) (map[string]any, int, balloonPlacement, []int, error) {
	if index < 0 || index >= len(placements) {
		return nil, index, balloonPlacement{}, nil, fmt.Errorf("placement plan exhausted at index %d", index)
	}
	placement := placements[index]
	if placement.NodeName == "" {
		return nil, index, balloonPlacement{}, nil, fmt.Errorf("placement %d has an empty balloon count node", index)
	}

	points, err := parseRecognizedGridPoints(detail)
	if err != nil {
		return nil, index, balloonPlacement{}, nil, err
	}
	point, ok := points[placement.TargetPos]
	if !ok {
		return nil, index, balloonPlacement{}, nil, fmt.Errorf("target grid position %+v is absent from recognition detail", placement.TargetPos)
	}
	if math.IsNaN(point.X) || math.IsInf(point.X, 0) || math.IsNaN(point.Y) || math.IsInf(point.Y, 0) {
		return nil, index, balloonPlacement{}, nil, fmt.Errorf("target grid position %+v has invalid screen coordinates", placement.TargetPos)
	}

	end := []int{int(math.Round(point.X)), int(math.Round(point.Y)), 1, 1}
	return map[string]any{
		aeroSalvageSwipeBalloonNode: map[string]any{
			"begin": placement.NodeName,
			"end":   end,
		},
	}, index + 1, placement, end, nil
}

func parseRecognizedGridPoints(detail *maa.RecognitionDetail) (map[gridPosition]gridCoordinate, error) {
	if detail == nil || detail.DetailJson == "" {
		return nil, fmt.Errorf("grid recognition detail is empty")
	}

	rawDetail := unwrapGridRecognitionDetail(detail.DetailJson)
	var recognized recognitionDetail
	if err := json.Unmarshal([]byte(rawDetail), &recognized); err != nil {
		return nil, fmt.Errorf("unmarshal grid recognition detail: %w", err)
	}
	if len(recognized.GridPoints) == 0 {
		return nil, fmt.Errorf("grid recognition detail contains no grid points")
	}

	points := make(map[gridPosition]gridCoordinate, len(recognized.GridPoints))
	for _, point := range recognized.GridPoints {
		position := gridPosition{X: point.Column - 2, Y: point.Row - 2}
		if _, exists := points[position]; exists {
			return nil, fmt.Errorf("grid recognition detail contains duplicate grid position %+v", position)
		}
		points[position] = point
	}
	return points, nil
}

func unwrapGridRecognitionDetail(raw string) string {
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil && len(wrapped.Best.Detail) > 0 {
		if wrapped.Best.Detail[0] == '"' {
			var detail string
			if err := json.Unmarshal(wrapped.Best.Detail, &detail); err == nil {
				return detail
			}
		}
		return string(wrapped.Best.Detail)
	}
	return raw
}
