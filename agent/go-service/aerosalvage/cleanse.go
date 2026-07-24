package aerosalvage

import (
	"fmt"
	"image"
	"math"
	"slices"
)

// FamilyGeometry describes whether a line family is parallel or convergent.
type FamilyGeometry uint8

const (
	// ParallelGeometry means the family has a vanishing point at infinity.
	ParallelGeometry FamilyGeometry = iota
	// ConvergentGeometry means the family shares a finite vanishing point.
	ConvergentGeometry
)

// CleanseConfig controls geometry-only candidate cleansing.
type CleanseConfig struct {
	GroupDistancePixels  float64
	AxisDominanceRatio   float64
	IntersectionQuantile float64
	SearchGridSize       int
	SearchRefinements    int
	OutlierMADScale      float64
	MinAngleResidual     float64
	MaxAngleResidual     float64
	MaxCleanseIterations int
	ParallelDistance     float64
}

func cleanseConfig() CleanseConfig {
	return CleanseConfig{
		GroupDistancePixels:  3,
		AxisDominanceRatio:   2,
		IntersectionQuantile: 0.1,
		SearchGridSize:       51,
		SearchRefinements:    4,
		OutlierMADScale:      2.5,
		MinAngleResidual:     0.75,
		MaxAngleResidual:     2,
		MaxCleanseIterations: 5,
		ParallelDistance:     5000,
	}
}

// FamilyCleanseResult contains all observable stages for one candidate family.
type FamilyCleanseResult struct {
	Family                LineFamily
	Geometry              FamilyGeometry
	VanishingPoint        image.Point
	PreciseVanishingPoint Point
	Grouped               []Line
	Inliers               []Line
	Outliers              []Line
	Lines                 []Line
	AngleLimit            float64
	AxisDominance         float64
	Iterations            int
}

// CleanseResult contains geometry-only cleansing results for both line families.
type CleanseResult struct {
	Families [2]FamilyCleanseResult
}

// CleanseGridLines removes geometrically inconsistent candidates and merges near duplicates.
func CleanseGridLines(result *Result, cfg CleanseConfig) (*CleanseResult, error) {
	if result == nil {
		return nil, fmt.Errorf("nil grid-line result")
	}
	if cfg.GroupDistancePixels <= 0 || cfg.AxisDominanceRatio <= 1 || cfg.IntersectionQuantile <= 0 || cfg.IntersectionQuantile >= 0.5 {
		return nil, fmt.Errorf("invalid cleansing thresholds")
	}
	if cfg.SearchGridSize < 3 || cfg.SearchRefinements < 1 || cfg.OutlierMADScale <= 0 || cfg.MinAngleResidual <= 0 || cfg.MaxAngleResidual < cfg.MinAngleResidual || cfg.MaxCleanseIterations < 1 || cfg.ParallelDistance <= 0 {
		return nil, fmt.Errorf("invalid cleansing search parameters")
	}

	cleaned := &CleanseResult{}
	for family := HorizontalFamily; family <= VerticalFamily; family++ {
		familyLines := filterFamily(result.Lines, family)
		familyResult, err := cleanseFamily(familyLines, family, result.ROI, cfg)
		if err != nil {
			return nil, fmt.Errorf("cleanse family %d: %w", family, err)
		}
		cleaned.Families[family] = familyResult
	}
	return cleaned, nil
}

func cleanseFamily(lines []Line, family LineFamily, roi image.Rectangle, cfg CleanseConfig) (FamilyCleanseResult, error) {
	result := FamilyCleanseResult{Family: family}
	if len(lines) == 0 {
		return result, fmt.Errorf("no candidate lines")
	}
	result.AxisDominance = axisDominance(lines, family)
	if result.AxisDominance >= cfg.AxisDominanceRatio {
		return cleanseAxisDominantFamily(result, lines, family, roi, cfg), nil
	}
	result.Geometry = ConvergentGeometry
	result.Grouped = groupNearbyLines(lines, family, roi, cfg.GroupDistancePixels)
	if len(result.Grouped) < 2 {
		if result.AxisDominance >= cfg.AxisDominanceRatio {
			return cleanseAxisDominantFamily(result, lines, family, roi, cfg), nil
		}
		return result, fmt.Errorf("fewer than two grouped candidates")
	}
	if inliers, ok := parallelLineInliers(result.Grouped, cfg); ok {
		return cleanseParallelFamily(result, inliers, family, roi, cfg), nil
	}
	point, inliers, iterations, angleLimit, err := refineVanishingPoint(result.Grouped, roi, cfg)
	if err != nil {
		if result.AxisDominance >= cfg.AxisDominanceRatio {
			return cleanseAxisDominantFamily(result, lines, family, roi, cfg), nil
		}
		return result, err
	}
	center := floatPoint{X: float64(roi.Min.X+roi.Max.X) / 2, Y: float64(roi.Min.Y+roi.Max.Y) / 2}
	if math.Hypot(point.X-center.X, point.Y-center.Y) >= cfg.ParallelDistance {
		return cleanseParallelFamily(result, inliers, family, roi, cfg), nil
	}
	result.VanishingPoint = image.Pt(int(math.Round(point.X)), int(math.Round(point.Y)))
	result.PreciseVanishingPoint = point
	result.Iterations = iterations
	result.AngleLimit = angleLimit
	result.Inliers, result.Outliers = partitionLines(result.Grouped, inliers)
	merged := groupNearbyLines(result.Inliers, family, roi, cfg.GroupDistancePixels)
	result.Lines = snapLinesToVanishingPoint(merged, family, roi, point)
	return result, nil
}

func cleanseParallelFamily(result FamilyCleanseResult, lines []Line, family LineFamily, roi image.Rectangle, cfg CleanseConfig) FamilyCleanseResult {
	result.Geometry = ParallelGeometry
	result.VanishingPoint = image.Point{}
	result.PreciseVanishingPoint = Point{}
	result.AngleLimit = 0
	result.Iterations = 0
	result.Inliers = slices.Clone(lines)
	result.Grouped = groupNearbyLines(result.Inliers, family, roi, cfg.GroupDistancePixels)
	result.Lines = snapLinesToSharedDirection(result.Grouped, family, roi)
	return result
}

func cleanseAxisDominantFamily(result FamilyCleanseResult, lines []Line, family LineFamily, roi image.Rectangle, cfg CleanseConfig) FamilyCleanseResult {
	inliers, outliers := splitAxisLines(lines, family)
	result = cleanseParallelFamily(result, inliers, family, roi, cfg)
	result.Outliers = outliers
	return result
}

// Point is a subpixel coordinate in the 720p full-image coordinate system.
type Point struct {
	X float64
	Y float64
}

type floatPoint = Point

type positionedLine struct {
	line     Line
	position float64
}

func filterFamily(lines []Line, family LineFamily) []Line {
	filtered := make([]Line, 0, len(lines))
	for _, line := range lines {
		if line.Family == family {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func axisDominance(lines []Line, family LineFamily) float64 {
	axisVotes := 0
	votesByTheta := make(map[float64]int)
	for _, line := range lines {
		distance := axisAngleDistance(line.Theta, family)
		if distance < 1e-9 {
			axisVotes += line.Votes
		} else {
			votesByTheta[line.Theta] += line.Votes
		}
	}
	maxOffAxisVotes := 0
	for _, votes := range votesByTheta {
		maxOffAxisVotes = max(maxOffAxisVotes, votes)
	}
	if axisVotes == 0 {
		return 0
	}
	if maxOffAxisVotes == 0 {
		return float64(axisVotes)
	}
	return float64(axisVotes) / float64(maxOffAxisVotes)
}

func splitAxisLines(lines []Line, family LineFamily) (inliers, outliers []Line) {
	// Exact axis bins are the most reliable observations when the raw family is axis-dominant.
	for _, line := range lines {
		if axisAngleDistance(line.Theta, family) < 1e-9 {
			inliers = append(inliers, line)
		} else {
			outliers = append(outliers, line)
		}
	}
	return inliers, outliers
}

func parallelLineInliers(lines []Line, cfg CleanseConfig) ([]Line, bool) {
	if len(lines) < 3 {
		return nil, false
	}
	var cosineSum, sineSum float64
	for _, line := range lines {
		weight := float64(max(line.Votes, 1))
		cosineSum += math.Cos(2*line.Theta) * weight
		sineSum += math.Sin(2*line.Theta) * weight
	}
	meanTheta := normalizeTheta(math.Atan2(sineSum, cosineSum) / 2)
	limit := cfg.MaxAngleResidual * math.Pi / 180
	inliers := make([]Line, 0, len(lines))
	for _, line := range lines {
		if angleDistance(line.Theta, meanTheta) <= limit {
			inliers = append(inliers, line)
		}
	}
	return inliers, len(inliers)*4 >= len(lines)*3
}

func snapLinesToSharedDirection(lines []Line, family LineFamily, roi image.Rectangle) []Line {
	if len(lines) == 0 {
		return nil
	}
	var cosineSum, sineSum, weightSum float64
	for _, line := range lines {
		weight := float64(max(line.Votes, 1))
		cosineSum += math.Cos(2*line.Theta) * weight
		sineSum += math.Sin(2*line.Theta) * weight
		weightSum += weight
	}
	if math.Hypot(cosineSum, sineSum) < 1e-9 || weightSum == 0 {
		return nil
	}
	theta := normalizeTheta(math.Atan2(sineSum, cosineSum) / 2)
	normalX, normalY := math.Cos(theta), math.Sin(theta)
	reference := float64(roi.Min.Y+roi.Max.Y) / 2
	if family == HorizontalFamily {
		reference = float64(roi.Min.X+roi.Max.X) / 2
	}
	snapped := make([]Line, 0, len(lines))
	for _, line := range lines {
		position, ok := linePosition(line, family, reference)
		if !ok {
			continue
		}
		anchor := floatPoint{X: position, Y: reference}
		if family == HorizontalFamily {
			anchor = floatPoint{X: reference, Y: position}
		}
		snapped = append(snapped, Line{
			Rho:    anchor.X*normalX + anchor.Y*normalY,
			Theta:  theta,
			Votes:  line.Votes,
			Family: family,
		})
	}
	return snapped
}

func axisAngleDistance(theta float64, family LineFamily) float64 {
	target := 0.0
	if family == HorizontalFamily {
		target = math.Pi / 2
	}
	return angleDistance(theta, target)
}

func groupNearbyLines(lines []Line, family LineFamily, roi image.Rectangle, threshold float64) []Line {
	if len(lines) == 0 {
		return nil
	}
	reference := float64(roi.Min.Y+roi.Max.Y) / 2
	if family == HorizontalFamily {
		reference = float64(roi.Min.X+roi.Max.X) / 2
	}
	positioned := make([]positionedLine, 0, len(lines))
	for _, line := range lines {
		position, ok := linePosition(line, family, reference)
		if ok {
			positioned = append(positioned, positionedLine{line: line, position: position})
		}
	}
	slices.SortFunc(positioned, func(left, right positionedLine) int {
		if left.position < right.position {
			return -1
		}
		if left.position > right.position {
			return 1
		}
		return 0
	})

	var grouped []Line
	for start := 0; start < len(positioned); {
		end := start + 1
		for end < len(positioned) && positioned[end].position-positioned[end-1].position <= threshold {
			end++
		}
		grouped = append(grouped, averageLines(positioned[start:end], family))
		start = end
	}
	return grouped
}

func linePosition(line Line, family LineFamily, reference float64) (float64, bool) {
	cosTheta := math.Cos(line.Theta)
	sinTheta := math.Sin(line.Theta)
	if family == HorizontalFamily {
		if math.Abs(sinTheta) < 1e-9 {
			return 0, false
		}
		return (line.Rho - reference*cosTheta) / sinTheta, true
	}
	if math.Abs(cosTheta) < 1e-9 {
		return 0, false
	}
	return (line.Rho - reference*sinTheta) / cosTheta, true
}

func averageLines(lines []positionedLine, family LineFamily) Line {
	var sumA, sumB, sumC, sumWeight float64
	var votes int
	for _, item := range lines {
		a := math.Cos(item.line.Theta)
		b := math.Sin(item.line.Theta)
		c := -item.line.Rho
		if sumWeight > 0 && a*sumA+b*sumB < 0 {
			a, b, c = -a, -b, -c
		}
		weight := float64(max(item.line.Votes, 1))
		sumA += a * weight
		sumB += b * weight
		sumC += c * weight
		sumWeight += weight
		votes += item.line.Votes
	}
	norm := math.Hypot(sumA, sumB)
	a, b, c := sumA/norm, sumB/norm, sumC/norm
	theta := normalizeTheta(math.Atan2(b, a))
	rho := -c
	if math.Cos(theta)*a+math.Sin(theta)*b < 0 {
		rho = -rho
	}
	return Line{Rho: rho, Theta: theta, Votes: votes, Family: family}
}

func fitVanishingPoint(lines []Line, cfg CleanseConfig) (floatPoint, error) {
	intersections := pairIntersections(lines)
	if len(intersections) == 0 {
		return floatPoint{}, fmt.Errorf("no finite candidate intersections")
	}
	xs := make([]float64, 0, len(intersections))
	ys := make([]float64, 0, len(intersections))
	for _, point := range intersections {
		xs = append(xs, point.X)
		ys = append(ys, point.Y)
	}
	slices.Sort(xs)
	slices.Sort(ys)
	best := intersections[0]
	bestValue := vanishPointValue(lines, best)
	for _, point := range intersections[1:] {
		value := vanishPointValue(lines, point)
		if value < bestValue {
			best = point
			bestValue = value
		}
	}
	spreadX := floatQuantile(xs, 1-cfg.IntersectionQuantile) - floatQuantile(xs, cfg.IntersectionQuantile)
	spreadY := floatQuantile(ys, 1-cfg.IntersectionQuantile) - floatQuantile(ys, cfg.IntersectionQuantile)
	radiusX := max(25, spreadX/10)
	radiusY := max(25, spreadY/10)
	minPoint := floatPoint{X: best.X - radiusX, Y: best.Y - radiusY}
	maxPoint := floatPoint{X: best.X + radiusX, Y: best.Y + radiusY}
	for range cfg.SearchRefinements {
		best = bruteVanishingPoint(lines, minPoint, maxPoint, cfg.SearchGridSize)
		stepX := (maxPoint.X - minPoint.X) / float64(cfg.SearchGridSize-1)
		stepY := (maxPoint.Y - minPoint.Y) / float64(cfg.SearchGridSize-1)
		minPoint = floatPoint{X: best.X - stepX, Y: best.Y - stepY}
		maxPoint = floatPoint{X: best.X + stepX, Y: best.Y + stepY}
	}
	return best, nil
}

func refineVanishingPoint(lines []Line, roi image.Rectangle, cfg CleanseConfig) (floatPoint, []Line, int, float64, error) {
	working := slices.Clone(lines)
	var point floatPoint
	var limit float64
	for iteration := 1; iteration <= cfg.MaxCleanseIterations; iteration++ {
		fitted, err := fitVanishingPoint(working, cfg)
		if err != nil {
			return floatPoint{}, nil, iteration, 0, err
		}
		point = fitted
		inliers, angleLimit := angularInliers(lines, point, roi, cfg)
		limit = angleLimit
		if len(inliers) < 2 {
			return floatPoint{}, nil, iteration, limit, fmt.Errorf("fewer than two angular inliers")
		}
		if sameLines(working, inliers) {
			return point, inliers, iteration, limit, nil
		}
		working = inliers
	}
	point, err := fitVanishingPoint(working, cfg)
	if err != nil {
		return floatPoint{}, nil, cfg.MaxCleanseIterations, limit, err
	}
	inliers, limit := angularInliers(lines, point, roi, cfg)
	return point, inliers, cfg.MaxCleanseIterations, limit, nil
}

func pairIntersections(lines []Line) []floatPoint {
	var intersections []floatPoint
	for i := 0; i < len(lines); i++ {
		for j := i + 1; j < len(lines); j++ {
			a1, b1 := math.Cos(lines[i].Theta), math.Sin(lines[i].Theta)
			a2, b2 := math.Cos(lines[j].Theta), math.Sin(lines[j].Theta)
			determinant := a1*b2 - a2*b1
			if math.Abs(determinant) < 1e-3 {
				continue
			}
			x := (lines[i].Rho*b2 - lines[j].Rho*b1) / determinant
			y := (a1*lines[j].Rho - a2*lines[i].Rho) / determinant
			if math.IsInf(x, 0) || math.IsInf(y, 0) || math.IsNaN(x) || math.IsNaN(y) {
				continue
			}
			intersections = append(intersections, floatPoint{X: x, Y: y})
		}
	}
	return intersections
}

func bruteVanishingPoint(lines []Line, minPoint, maxPoint floatPoint, gridSize int) floatPoint {
	best := minPoint
	bestValue := math.Inf(1)
	for yi := range gridSize {
		y := minPoint.Y + (maxPoint.Y-minPoint.Y)*float64(yi)/float64(gridSize-1)
		for xi := range gridSize {
			x := minPoint.X + (maxPoint.X-minPoint.X)*float64(xi)/float64(gridSize-1)
			value := vanishPointValue(lines, floatPoint{X: x, Y: y})
			if value < bestValue {
				bestValue = value
				best = floatPoint{X: x, Y: y}
			}
		}
	}
	return best
}

func vanishPointValue(lines []Line, point floatPoint) float64 {
	value := 0.0
	for _, line := range lines {
		distance := math.Abs(line.Rho - point.X*math.Cos(line.Theta) - point.Y*math.Sin(line.Theta))
		value += math.Log10(distance + 0.001)
	}
	return value
}

func angularInliers(lines []Line, point floatPoint, roi image.Rectangle, cfg CleanseConfig) (inliers []Line, limit float64) {
	center := floatPoint{X: float64(roi.Min.X+roi.Max.X) / 2, Y: float64(roi.Min.Y+roi.Max.Y) / 2}
	distanceToCenter := math.Hypot(point.X-center.X, point.Y-center.Y)
	residuals := make([]float64, len(lines))
	for i, line := range lines {
		distance := math.Abs(line.Rho - point.X*math.Cos(line.Theta) - point.Y*math.Sin(line.Theta))
		residuals[i] = math.Asin(min(1, distance/distanceToCenter)) * 180 / math.Pi
	}
	ordered := slices.Clone(residuals)
	slices.Sort(ordered)
	median := floatQuantile(ordered, 0.5)
	deviations := make([]float64, len(ordered))
	for i, residual := range ordered {
		deviations[i] = math.Abs(residual - median)
	}
	slices.Sort(deviations)
	mad := floatQuantile(deviations, 0.5)
	limit = max(cfg.MinAngleResidual, median+cfg.OutlierMADScale*1.4826*mad)
	limit = min(limit, cfg.MaxAngleResidual)
	for i, line := range lines {
		if residuals[i] <= limit {
			inliers = append(inliers, line)
		}
	}
	return inliers, limit
}

func partitionLines(lines, inliers []Line) (kept, rejected []Line) {
	for _, line := range lines {
		if slices.Contains(inliers, line) {
			kept = append(kept, line)
		} else {
			rejected = append(rejected, line)
		}
	}
	return kept, rejected
}

func sameLines(left, right []Line) bool {
	return slices.Equal(left, right)
}

func snapLinesToVanishingPoint(lines []Line, family LineFamily, roi image.Rectangle, point floatPoint) []Line {
	reference := float64(roi.Min.Y+roi.Max.Y) / 2
	if family == HorizontalFamily {
		reference = float64(roi.Min.X+roi.Max.X) / 2
	}
	snapped := make([]Line, 0, len(lines))
	for _, line := range lines {
		position, ok := linePosition(line, family, reference)
		if !ok {
			continue
		}
		anchor := floatPoint{X: position, Y: reference}
		if family == HorizontalFamily {
			anchor = floatPoint{X: reference, Y: position}
		}
		snapped = append(snapped, lineThroughPoints(point, anchor, line))
	}
	return snapped
}

func lineThroughPoints(first, second floatPoint, source Line) Line {
	dx := second.X - first.X
	dy := second.Y - first.Y
	norm := math.Hypot(dx, dy)
	a, b := dy/norm, -dx/norm
	rho := first.X*a + first.Y*b
	theta := normalizeTheta(math.Atan2(b, a))
	if math.Cos(theta)*a+math.Sin(theta)*b < 0 {
		rho = -rho
	}
	return Line{Rho: rho, Theta: theta, Votes: source.Votes, Family: source.Family}
}

func floatQuantile(ordered []float64, quantile float64) float64 {
	if len(ordered) == 0 {
		return 0
	}
	index := int(math.Round(float64(len(ordered)-1) * quantile))
	return ordered[index]
}
