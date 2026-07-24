package aerosalvage

import (
	"fmt"
	"image"
	"math"
	"slices"
)

// GridPointConfig controls local quadrilateral filtering and duplicate-center grouping.
type GridPointConfig struct {
	BaseCellWidth        float64
	BaseCellHeight       float64
	IShapeInset          float64
	MinPerspectiveScale  float64
	MaxPerspectiveScale  float64
	EqualSideTolerance   float64
	MinHorizontalExcess  float64
	MaxHorizontalExcess  float64
	MaxOppositeSideRatio float64
	CenterMergeDistance  float64
	CenterROI            image.Rectangle
}

func gridPointConfig(centerROI image.Rectangle) GridPointConfig {
	return GridPointConfig{
		BaseCellWidth:        43,
		BaseCellHeight:       44,
		IShapeInset:          6,
		MinPerspectiveScale:  0.7,
		MaxPerspectiveScale:  1.35,
		EqualSideTolerance:   3,
		MinHorizontalExcess:  8,
		MaxHorizontalExcess:  12,
		MaxOppositeSideRatio: 0.2,
		CenterMergeDistance:  10,
		CenterROI:            centerROI,
	}
}

// GridPoint is the center of a locally supported grid-cell candidate.
type GridPoint struct {
	Row              int
	Column           int
	Center           Point
	HorizontalLength float64
	VerticalLength   float64
	Score            float64
	Corners          [4]Point
}

// GridPointResult contains raw local candidates and merged center observations.
type GridPointResult struct {
	Candidates  []GridPoint
	LocalPoints []GridPoint
	Points      []GridPoint
}

// DetectGridPoints computes locally supported cell centers from cleansed line families.
func DetectGridPoints(cleaned *CleanseResult, roi image.Rectangle, cfg GridPointConfig) (*GridPointResult, error) {
	if cleaned == nil {
		return nil, fmt.Errorf("nil cleansing result")
	}
	if cfg.BaseCellWidth <= 0 || cfg.BaseCellHeight <= 0 || cfg.IShapeInset <= 0 || cfg.MinPerspectiveScale <= 0 || cfg.MaxPerspectiveScale <= cfg.MinPerspectiveScale || cfg.EqualSideTolerance < 0 || cfg.MinHorizontalExcess < 0 || cfg.MaxHorizontalExcess < cfg.MinHorizontalExcess || cfg.MaxOppositeSideRatio <= 0 || cfg.CenterMergeDistance <= 0 {
		return nil, fmt.Errorf("invalid grid-point parameters")
	}
	if cfg.CenterROI.Empty() || !cfg.CenterROI.In(roi) {
		return nil, fmt.Errorf("center ROI %v is outside grid ROI %v", cfg.CenterROI, roi)
	}

	horizontal := sortLinesByPosition(cleaned.Families[HorizontalFamily].Lines, HorizontalFamily, roi)
	vertical := sortLinesByPosition(cleaned.Families[VerticalFamily].Lines, VerticalFamily, roi)
	result := &GridPointResult{}
	for upperIndex := 0; upperIndex < len(horizontal); upperIndex++ {
		for lowerIndex := upperIndex + 1; lowerIndex < len(horizontal); lowerIndex++ {
			for leftIndex := 0; leftIndex < len(vertical); leftIndex++ {
				for rightIndex := leftIndex + 1; rightIndex < len(vertical); rightIndex++ {
					candidate, ok := buildGridPoint(
						horizontal[upperIndex],
						horizontal[lowerIndex],
						vertical[leftIndex],
						vertical[rightIndex],
						roi,
						cfg,
					)
					if ok {
						result.Candidates = append(result.Candidates, candidate)
					}
				}
			}
		}
	}
	result.LocalPoints = mergeGridPoints(result.Candidates, cfg.CenterMergeDistance)
	points, err := fitGridLattice(result.LocalPoints, cleaned, roi, cfg)
	if err != nil {
		return nil, err
	}
	result.Points = points
	return result, nil
}

type projectiveTransform struct {
	vanishingLine [3]float64
	directionU    floatPoint
	directionV    floatPoint
	determinant   float64
}

func fitGridLattice(points []GridPoint, cleaned *CleanseResult, roi image.Rectangle, cfg GridPointConfig) ([]GridPoint, error) {
	if len(points) < 4 {
		return nil, fmt.Errorf("fewer than four local grid-point candidates")
	}
	transform, err := newProjectiveTransform(cleaned, roi)
	if err != nil {
		return nil, err
	}
	uValues := make([]float64, 0, len(points))
	vValues := make([]float64, 0, len(points))
	for _, point := range points {
		rectified, ok := transform.rectify(point.Center)
		if !ok {
			continue
		}
		u, v := transform.coordinates(rectified)
		uValues = append(uValues, u)
		vValues = append(vValues, v)
	}
	bestScore := math.Inf(1)
	var bestGrid []GridPoint
	for _, center := range centerCandidates(points, cfg.CenterROI) {
		rectifiedCenter, ok := transform.rectify(center)
		if !ok {
			continue
		}
		centerU, centerV := transform.coordinates(rectifiedCenter)
		uCandidates, err := fitAnchoredFiveLevelCandidates(uValues, centerU)
		if err != nil {
			continue
		}
		vCandidates, err := fitAnchoredFiveLevelCandidates(vValues, centerV)
		if err != nil {
			continue
		}
		for _, uCandidate := range uCandidates {
			for _, vCandidate := range vCandidates {
				uLevels := slices.Clone(uCandidate.Levels)
				vLevels := slices.Clone(vCandidate.Levels)
				normalizeLevelDirections(uLevels, vLevels, transform)
				grid, ok := restoreGrid(uLevels, vLevels, transform, roi, cfg.CenterROI, center)
				if !ok {
					continue
				}
				lengthPenalty, ok := latticeLengthPenalty(grid, cfg)
				if !ok {
					continue
				}
				score := uCandidate.Score + vCandidate.Score + lengthPenalty
				if score < bestScore {
					bestScore = score
					bestGrid = grid
				}
			}
		}
	}
	if len(bestGrid) == 0 {
		return nil, fmt.Errorf("no five-by-five lattice is strictly inside ROI %v", roi)
	}
	return bestGrid, nil
}

func centerCandidates(points []GridPoint, centerROI image.Rectangle) []Point {
	candidates := []Point{{
		X: float64(centerROI.Min.X+centerROI.Max.X) / 2,
		Y: float64(centerROI.Min.Y+centerROI.Max.Y) / 2,
	}}
	for _, point := range points {
		if floatPointInRect(point.Center, centerROI) {
			candidates = append(candidates, point.Center)
		}
	}
	return candidates
}

func latticeLengthPenalty(grid []GridPoint, cfg GridPointConfig) (float64, bool) {
	point := func(row, column int) Point {
		return grid[row*5+column].Center
	}
	horizontalLength := (pointDistance(point(2, 1), point(2, 2)) + pointDistance(point(2, 2), point(2, 3))) / 2
	verticalLength := (pointDistance(point(1, 2), point(2, 2)) + pointDistance(point(2, 2), point(3, 2))) / 2
	if horizontalLength < cfg.BaseCellWidth*cfg.MinPerspectiveScale || horizontalLength > cfg.BaseCellWidth*cfg.MaxPerspectiveScale || verticalLength < cfg.BaseCellHeight*cfg.MinPerspectiveScale || verticalLength > cfg.BaseCellHeight*cfg.MaxPerspectiveScale {
		return 0, false
	}
	excess := horizontalLength - verticalLength
	scalePenalty := math.Abs(horizontalLength/cfg.BaseCellWidth-verticalLength/cfg.BaseCellHeight) * 10
	if math.Abs(excess) <= cfg.EqualSideTolerance {
		return math.Abs(excess) + scalePenalty, true
	}
	if excess >= cfg.MinHorizontalExcess && excess <= cfg.MaxHorizontalExcess {
		idealExcess := (cfg.MinHorizontalExcess + cfg.MaxHorizontalExcess) / 2
		return math.Abs(excess-idealExcess) + scalePenalty, true
	}
	return 0, false
}

func restoreGrid(uLevels, vLevels []float64, transform projectiveTransform, roi, centerROI image.Rectangle, centerAnchor Point) ([]GridPoint, bool) {
	grid := make([]GridPoint, 0, 25)
	for row, v := range vLevels {
		for column, u := range uLevels {
			rectified := floatPoint{
				X: u*transform.directionU.X + v*transform.directionV.X,
				Y: u*transform.directionU.Y + v*transform.directionV.Y,
			}
			center, ok := transform.restore(rectified)
			if !ok || !floatPointInRect(center, roi) {
				return nil, false
			}
			if row == 2 && column == 2 {
				center = centerAnchor
				if !floatPointInRect(center, centerROI) {
					return nil, false
				}
			}
			grid = append(grid, GridPoint{Row: row, Column: column, Center: center})
		}
	}
	return grid, true
}

func normalizeLevelDirections(uLevels, vLevels []float64, transform projectiveTransform) {
	middleU := uLevels[len(uLevels)/2]
	middleV := vLevels[len(vLevels)/2]
	left, leftOK := transform.restore(floatPoint{
		X: uLevels[0]*transform.directionU.X + middleV*transform.directionV.X,
		Y: uLevels[0]*transform.directionU.Y + middleV*transform.directionV.Y,
	})
	right, rightOK := transform.restore(floatPoint{
		X: uLevels[len(uLevels)-1]*transform.directionU.X + middleV*transform.directionV.X,
		Y: uLevels[len(uLevels)-1]*transform.directionU.Y + middleV*transform.directionV.Y,
	})
	if leftOK && rightOK && left.X > right.X {
		slices.Reverse(uLevels)
	}
	top, topOK := transform.restore(floatPoint{
		X: middleU*transform.directionU.X + vLevels[0]*transform.directionV.X,
		Y: middleU*transform.directionU.Y + vLevels[0]*transform.directionV.Y,
	})
	bottom, bottomOK := transform.restore(floatPoint{
		X: middleU*transform.directionU.X + vLevels[len(vLevels)-1]*transform.directionV.X,
		Y: middleU*transform.directionU.Y + vLevels[len(vLevels)-1]*transform.directionV.Y,
	})
	if topOK && bottomOK && top.Y > bottom.Y {
		slices.Reverse(vLevels)
	}
}

func newProjectiveTransform(cleaned *CleanseResult, roi image.Rectangle) (projectiveTransform, error) {
	horizontalPoint := homogeneousVanishingPoint(cleaned.Families[HorizontalFamily], HorizontalFamily)
	verticalPoint := homogeneousVanishingPoint(cleaned.Families[VerticalFamily], VerticalFamily)
	line := crossHomogeneous(horizontalPoint, verticalPoint)
	if math.Abs(line[2]) < 1e-9 {
		return projectiveTransform{}, fmt.Errorf("vanishing line cannot be normalized")
	}
	for i := range line {
		line[i] /= line[2]
	}
	transform := projectiveTransform{vanishingLine: line}
	horizontalDirection, err := rectifiedLineDirection(cleaned.Families[HorizontalFamily].Lines, roi, transform)
	if err != nil {
		return projectiveTransform{}, fmt.Errorf("horizontal rectified direction: %w", err)
	}
	verticalDirection, err := rectifiedLineDirection(cleaned.Families[VerticalFamily].Lines, roi, transform)
	if err != nil {
		return projectiveTransform{}, fmt.Errorf("vertical rectified direction: %w", err)
	}
	transform.directionU = horizontalDirection
	transform.directionV = verticalDirection
	transform.determinant = horizontalDirection.X*verticalDirection.Y - verticalDirection.X*horizontalDirection.Y
	if math.Abs(transform.determinant) < 1e-9 {
		return projectiveTransform{}, fmt.Errorf("rectified grid directions are parallel")
	}
	return transform, nil
}

func homogeneousVanishingPoint(result FamilyCleanseResult, _ LineFamily) [3]float64 {
	if result.Geometry == ConvergentGeometry {
		return [3]float64{result.PreciseVanishingPoint.X, result.PreciseVanishingPoint.Y, 1}
	}
	if len(result.Lines) == 0 {
		return [3]float64{}
	}
	theta := result.Lines[0].Theta
	return [3]float64{-math.Sin(theta), math.Cos(theta), 0}
}

func crossHomogeneous(left, right [3]float64) [3]float64 {
	return [3]float64{
		left[1]*right[2] - left[2]*right[1],
		left[2]*right[0] - left[0]*right[2],
		left[0]*right[1] - left[1]*right[0],
	}
}

func rectifiedLineDirection(lines []Line, roi image.Rectangle, transform projectiveTransform) (floatPoint, error) {
	for _, line := range lines {
		from, to, ok := clipLineToRect(line, roi)
		if !ok {
			continue
		}
		first, firstOK := transform.rectify(floatPoint{X: float64(from.X), Y: float64(from.Y)})
		second, secondOK := transform.rectify(floatPoint{X: float64(to.X), Y: float64(to.Y)})
		if !firstOK || !secondOK {
			continue
		}
		direction := floatPoint{X: second.X - first.X, Y: second.Y - first.Y}
		norm := math.Hypot(direction.X, direction.Y)
		if norm > 1e-9 {
			return floatPoint{X: direction.X / norm, Y: direction.Y / norm}, nil
		}
	}
	return floatPoint{}, fmt.Errorf("no usable line")
}

func (transform projectiveTransform) rectify(point floatPoint) (floatPoint, bool) {
	denominator := transform.vanishingLine[0]*point.X + transform.vanishingLine[1]*point.Y + transform.vanishingLine[2]
	if math.Abs(denominator) < 1e-9 {
		return floatPoint{}, false
	}
	return floatPoint{X: point.X / denominator, Y: point.Y / denominator}, true
}

func (transform projectiveTransform) restore(point floatPoint) (floatPoint, bool) {
	denominator := 1 - transform.vanishingLine[0]*point.X - transform.vanishingLine[1]*point.Y
	if math.Abs(denominator) < 1e-9 {
		return floatPoint{}, false
	}
	return floatPoint{X: point.X / denominator, Y: point.Y / denominator}, true
}

func (transform projectiveTransform) coordinates(point floatPoint) (float64, float64) {
	u := (point.X*transform.directionV.Y - transform.directionV.X*point.Y) / transform.determinant
	v := (transform.directionU.X*point.Y - point.X*transform.directionU.Y) / transform.determinant
	return u, v
}

type levelCandidate struct {
	Levels []float64
	Score  float64
}

func fitAnchoredFiveLevelCandidates(values []float64, center float64) ([]levelCandidate, error) {
	if len(values) < 3 {
		return nil, fmt.Errorf("fewer than three coordinate observations")
	}
	var candidates []levelCandidate
	for _, value := range values {
		for level := -2; level <= 2; level++ {
			if level == 0 {
				continue
			}
			step := (value - center) / float64(level)
			if step <= 1e-9 {
				continue
			}
			levels := make([]float64, 5)
			for index := range levels {
				levels[index] = center + float64(index-2)*step
			}
			score := levelFitScore(values, levels, step)
			candidates = append(candidates, levelCandidate{Levels: levels, Score: score})
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no level sequence")
	}
	slices.SortFunc(candidates, func(left, right levelCandidate) int {
		if left.Score < right.Score {
			return -1
		}
		if left.Score > right.Score {
			return 1
		}
		return 0
	})
	const maxCandidates = 500
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}
	return candidates, nil
}

func levelFitScore(values, levels []float64, step float64) float64 {
	const missingLevelPenalty = 2
	counts := make([]int, len(levels))
	score := 0.0
	for _, value := range values {
		bestIndex := 0
		bestDistance := math.Abs(value - levels[0])
		for index := 1; index < len(levels); index++ {
			distance := math.Abs(value - levels[index])
			if distance < bestDistance {
				bestDistance = distance
				bestIndex = index
			}
		}
		normalized := bestDistance / step
		if normalized <= 0.2 {
			counts[bestIndex]++
			score += normalized * normalized
		} else {
			score += 1
		}
	}
	for _, count := range counts {
		if count == 0 {
			score += missingLevelPenalty
		}
	}
	return score / float64(len(values))
}

func sortLinesByPosition(lines []Line, family LineFamily, roi image.Rectangle) []Line {
	reference := float64(roi.Min.Y+roi.Max.Y) / 2
	if family == HorizontalFamily {
		reference = float64(roi.Min.X+roi.Max.X) / 2
	}
	sorted := slices.Clone(lines)
	slices.SortFunc(sorted, func(left, right Line) int {
		leftPosition, _ := linePosition(left, family, reference)
		rightPosition, _ := linePosition(right, family, reference)
		if leftPosition < rightPosition {
			return -1
		}
		if leftPosition > rightPosition {
			return 1
		}
		return 0
	})
	return sorted
}

func buildGridPoint(upper, lower, left, right Line, roi image.Rectangle, cfg GridPointConfig) (GridPoint, bool) {
	upperLeft, ok := intersectLines(upper, left)
	if !ok {
		return GridPoint{}, false
	}
	upperRight, ok := intersectLines(upper, right)
	if !ok {
		return GridPoint{}, false
	}
	lowerLeft, ok := intersectLines(lower, left)
	if !ok {
		return GridPoint{}, false
	}
	lowerRight, ok := intersectLines(lower, right)
	if !ok {
		return GridPoint{}, false
	}
	corners := [4]floatPoint{upperLeft, upperRight, lowerLeft, lowerRight}
	for _, corner := range corners {
		if !floatPointInRect(corner, roi) {
			return GridPoint{}, false
		}
	}

	upperLength := pointDistance(upperLeft, upperRight)
	lowerLength := pointDistance(lowerLeft, lowerRight)
	leftLength := pointDistance(upperLeft, lowerLeft)
	rightLength := pointDistance(upperRight, lowerRight)
	horizontalLength := (upperLength + lowerLength) / 2
	verticalLength := (leftLength + rightLength) / 2
	horizontalScale := horizontalLength / cfg.BaseCellWidth
	verticalScale := verticalLength / cfg.BaseCellHeight
	if horizontalScale < cfg.MinPerspectiveScale || horizontalScale > cfg.MaxPerspectiveScale || verticalScale < cfg.MinPerspectiveScale || verticalScale > cfg.MaxPerspectiveScale {
		return GridPoint{}, false
	}
	if relativeDifference(upperLength, lowerLength) > cfg.MaxOppositeSideRatio || relativeDifference(leftLength, rightLength) > cfg.MaxOppositeSideRatio {
		return GridPoint{}, false
	}
	excess := horizontalLength - verticalLength
	sidePenalty := math.Abs(excess)
	if math.Abs(excess) > cfg.EqualSideTolerance {
		if excess < cfg.MinHorizontalExcess || excess > cfg.MaxHorizontalExcess {
			return GridPoint{}, false
		}
		sidePenalty = math.Abs(excess - (cfg.MinHorizontalExcess+cfg.MaxHorizontalExcess)/2)
	}

	center, ok := intersectSegments(upperLeft, lowerRight, upperRight, lowerLeft)
	if !ok || !floatPointInRect(center, roi) {
		return GridPoint{}, false
	}
	scalePenalty := math.Abs(horizontalScale-verticalScale) * 20
	insetPenalty := insetDistancePenalty(horizontalLength, cfg.BaseCellWidth, cfg.IShapeInset) + insetDistancePenalty(verticalLength, cfg.BaseCellHeight, cfg.IShapeInset)
	score := sidePenalty + scalePenalty + insetPenalty + relativeDifference(upperLength, lowerLength)*10 + relativeDifference(leftLength, rightLength)*10
	return GridPoint{
		Center:           center,
		HorizontalLength: horizontalLength,
		VerticalLength:   verticalLength,
		Score:            score,
		Corners:          corners,
	}, true
}

func insetDistancePenalty(length, baseline, inset float64) float64 {
	distance := min(math.Abs(length-(baseline-inset)), math.Abs(length-(baseline+inset)))
	if distance >= 2 {
		return 0
	}
	return 2 - distance
}

func intersectLines(first, second Line) (floatPoint, bool) {
	a1, b1 := math.Cos(first.Theta), math.Sin(first.Theta)
	a2, b2 := math.Cos(second.Theta), math.Sin(second.Theta)
	determinant := a1*b2 - a2*b1
	if math.Abs(determinant) < 1e-9 {
		return floatPoint{}, false
	}
	return floatPoint{
		X: (first.Rho*b2 - second.Rho*b1) / determinant,
		Y: (a1*second.Rho - a2*first.Rho) / determinant,
	}, true
}

func intersectSegments(firstStart, firstEnd, secondStart, secondEnd floatPoint) (floatPoint, bool) {
	first := lineFromPoints(firstStart, firstEnd)
	second := lineFromPoints(secondStart, secondEnd)
	return intersectLines(first, second)
}

func lineFromPoints(first, second floatPoint) Line {
	dx := second.X - first.X
	dy := second.Y - first.Y
	norm := math.Hypot(dx, dy)
	a, b := dy/norm, -dx/norm
	theta := normalizeTheta(math.Atan2(b, a))
	rho := first.X*a + first.Y*b
	if math.Cos(theta)*a+math.Sin(theta)*b < 0 {
		rho = -rho
	}
	return Line{Rho: rho, Theta: theta}
}

func mergeGridPoints(candidates []GridPoint, threshold float64) []GridPoint {
	remaining := slices.Clone(candidates)
	var merged []GridPoint
	for len(remaining) > 0 {
		seed := remaining[0]
		cluster := []GridPoint{seed}
		next := remaining[:0]
		for _, candidate := range remaining[1:] {
			if pointDistance(seed.Center, candidate.Center) <= threshold {
				cluster = append(cluster, candidate)
			} else {
				next = append(next, candidate)
			}
		}
		remaining = next
		slices.SortFunc(cluster, func(left, right GridPoint) int {
			if left.Score < right.Score {
				return -1
			}
			if left.Score > right.Score {
				return 1
			}
			return 0
		})
		merged = append(merged, cluster[0])
	}
	slices.SortFunc(merged, func(left, right GridPoint) int {
		if math.Abs(left.Center.Y-right.Center.Y) > threshold {
			if left.Center.Y < right.Center.Y {
				return -1
			}
			return 1
		}
		if left.Center.X < right.Center.X {
			return -1
		}
		return 1
	})
	return merged
}

func floatPointInRect(point floatPoint, rect image.Rectangle) bool {
	return point.X >= float64(rect.Min.X) && point.X < float64(rect.Max.X) && point.Y >= float64(rect.Min.Y) && point.Y < float64(rect.Max.Y)
}

func pointDistance(first, second floatPoint) float64 {
	return math.Hypot(first.X-second.X, first.Y-second.Y)
}

func relativeDifference(first, second float64) float64 {
	return math.Abs(first-second) / max(first, second)
}

func roundPoint(point floatPoint) image.Point {
	return image.Pt(int(math.Round(point.X)), int(math.Round(point.Y)))
}
