package aerosalvage

import (
	"fmt"
	"image"
	"math"
	"regexp"
	"slices"
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

var (
	placementSitePositions []gridPosition
	balloonConfigs         map[int]balloonConfig
	balloonPlacements      []balloonPlacement
	// balloonPlanSlots 方案涉及的气球槽位（CountNode 升序），BalloonStateRecognition 按此序观测。
	balloonPlanSlots []string
	// balloonPathStates 方案进度与 balloon state 的一一映射：第 k 项为按方案顺序执行完
	// 前 k 个条目后的剩余数量元组，ConfigureSwipeAction 以此校验观测状态。
	balloonPathStates [][]int
	// balloonObservedState 最近一次观测的 balloon state，与 balloonPlanSlots 对齐。
	balloonObservedState []int
	balloonStateIndex    int
	// balloonStateSignature / balloonStateRepeatCount 熔断计数：同一 balloon state 累计出现次数。
	balloonStateSignature   string
	balloonStateRepeatCount int
)

type balloonConfig struct {
	Value     int
	Count     int
	CountNode string
}

type balloonPlacement struct {
	Config    balloonConfig `json:"config"`
	TargetPos gridPosition  `json:"target_pos"`
}

// InitialStateRecognition recognizes and caches the Aerial Salvage initial state.
type InitialStateRecognition struct{}

// Run clears the initial-state caches, then recognizes and caches the current placement sites and balloon configurations.
func (r *InitialStateRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	placementSitePositions = nil
	balloonConfigs = nil
	balloonPlacements = nil
	balloonPlanSlots = nil
	balloonPathStates = nil
	balloonObservedState = nil
	gridPointsCache = nil
	balloonStateIndex = 0
	balloonStateSignature = ""
	balloonStateRepeatCount = 0
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
	placements, err := solveBalloonPlacements(positions, configs)
	if err != nil {
		return initialStateRecognitionError("solve balloon placements", err)
	}
	slots, pathStates, err := buildPathStates(placements, configs)
	if err != nil {
		return initialStateRecognitionError("build path states", err)
	}
	placementSitePositions = positions
	balloonConfigs = configs
	balloonPlacements = placements
	balloonPlanSlots = slots
	balloonPathStates = pathStates
	log.Debug().
		Str("component", "AeroSalvageInitialStateRecognition").
		Int("placement_sites", len(positions)).
		Int("balloon_configs", len(configs)).
		Int("balloon_placements", len(placements)).
		Strs("balloon_plan_slots", slots).
		Interface("placement_site_positions", positions).
		Interface("balloon_configs", sortedBalloonConfigs(configs)).
		Interface("balloon_placements", placements).
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

type placementSolveState struct {
	SiteIndex int
	Remaining [4]int
	SumX      int
	SumY      int
}

// solveBalloonPlacements finds one balanced assignment of every balloon to a distinct placement site.
func solveBalloonPlacements(positions []gridPosition, configs map[int]balloonConfig) ([]balloonPlacement, error) {
	sites := append([]gridPosition(nil), positions...)
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].Y != sites[j].Y {
			return sites[i].Y < sites[j].Y
		}
		return sites[i].X < sites[j].X
	})

	orderedConfigs := sortedBalloonConfigs(configs)
	if len(orderedConfigs) > 4 {
		return nil, fmt.Errorf("balloon configuration count %d exceeds solver capacity", len(orderedConfigs))
	}
	var remaining [4]int
	balloonCount := 0
	for index, config := range orderedConfigs {
		if config.Count <= 0 {
			return nil, fmt.Errorf("balloon %s has invalid count %d", config.CountNode, config.Count)
		}
		remaining[index] = config.Count
		balloonCount += config.Count
	}
	if balloonCount > len(sites) {
		return nil, fmt.Errorf("balloon count %d exceeds placement site count %d", balloonCount, len(sites))
	}

	failed := make(map[placementSolveState]struct{})
	placements := make([]balloonPlacement, 0, balloonCount)
	var search func(siteIndex int, remaining [4]int, sumX, sumY int) bool
	search = func(siteIndex int, remaining [4]int, sumX, sumY int) bool {
		left := 0
		remainingWeight := 0
		for index, config := range orderedConfigs {
			count := remaining[index]
			left += count
			remainingWeight += count * config.Value
		}
		if left == 0 {
			return sumX == 0 && sumY == 0
		}
		if len(sites)-siteIndex < left || abs(sumX) > 2*remainingWeight || abs(sumY) > 2*remainingWeight {
			return false
		}

		state := placementSolveState{SiteIndex: siteIndex, Remaining: remaining, SumX: sumX, SumY: sumY}
		if _, known := failed[state]; known {
			return false
		}
		site := sites[siteIndex]
		for configIndex, config := range orderedConfigs {
			if remaining[configIndex] == 0 {
				continue
			}
			remaining[configIndex]--
			placements = append(placements, balloonPlacement{Config: config, TargetPos: site})
			if search(siteIndex+1, remaining, sumX+site.X*config.Value, sumY+site.Y*config.Value) {
				return true
			}
			placements = placements[:len(placements)-1]
			remaining[configIndex]++
		}
		if len(sites)-siteIndex > left && search(siteIndex+1, remaining, sumX, sumY) {
			return true
		}
		failed[state] = struct{}{}
		return false
	}

	if !search(0, remaining, 0, 0) {
		return nil, fmt.Errorf("no balanced placement plan for %d balloons across %d placement sites", balloonCount, len(sites))
	}
	sort.Slice(placements, func(i, j int) bool {
		if placements[i].TargetPos.Y != placements[j].TargetPos.Y {
			return placements[i].TargetPos.Y < placements[j].TargetPos.Y
		}
		return placements[i].TargetPos.X < placements[j].TargetPos.X
	})
	return placements, nil
}

// buildPathStates 推导方案进度与 balloon state 的一一映射：slots 为方案槽位序
// （CountNode 升序），states 第 k 项为按方案顺序执行完前 k 个条目后的剩余数量元组。
func buildPathStates(placements []balloonPlacement, configs map[int]balloonConfig) ([]string, [][]int, error) {
	ordered := sortedBalloonConfigs(configs)
	slots := make([]string, len(ordered))
	slotIndexes := make(map[string]int, len(ordered))
	state := make([]int, len(ordered))
	for i, config := range ordered {
		slots[i] = config.CountNode
		slotIndexes[config.CountNode] = i
		state[i] = config.Count
	}
	states := make([][]int, 0, len(placements)+1)
	states = append(states, slices.Clone(state))
	for _, placement := range placements {
		index, ok := slotIndexes[placement.Config.CountNode]
		if !ok {
			return nil, nil, fmt.Errorf("placement targets unknown balloon count node %s", placement.Config.CountNode)
		}
		if state[index] <= 0 {
			return nil, nil, fmt.Errorf("balloon %s has no remaining count for placement", placement.Config.CountNode)
		}
		state[index]--
		states = append(states, slices.Clone(state))
	}
	return slots, states, nil
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
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
