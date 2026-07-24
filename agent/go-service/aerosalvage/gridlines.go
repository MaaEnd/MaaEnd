// Package aerosalvage detects the perspective grid used by Aerial Salvage.
package aerosalvage

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
)

// LineFamily identifies the source-grid direction represented by a detected line.
type LineFamily uint8

const (
	// HorizontalFamily contains projections of horizontal source-grid lines.
	HorizontalFamily LineFamily = iota
	// VerticalFamily contains projections of vertical source-grid lines.
	VerticalFamily
)

// Config controls the preprocessing and raw Hough candidate thresholds.
type Config struct {
	ROI                        image.Rectangle
	HorizontalGradientQuantile float64
	VerticalGradientQuantile   float64
	ThetaStepDegrees           float64
	GradientAngleTolerance     float64
	HorizontalPeakQuantile     float64
	VerticalPeakQuantile       float64
	MinPeakVoteRatio           float64
}

func gridLineConfig(roi image.Rectangle) Config {
	return Config{
		ROI:                        roi,
		HorizontalGradientQuantile: 0.50,
		VerticalGradientQuantile:   0.83,
		ThetaStepDegrees:           0.5,
		GradientAngleTolerance:     10,
		HorizontalPeakQuantile:     0.995,
		VerticalPeakQuantile:       0.995,
		MinPeakVoteRatio:           1.0,
	}
}

// Line is an infinite Hough line in full-image coordinates.
// Points on the line satisfy x*cos(Theta)+y*sin(Theta)=Rho.
type Line struct {
	Rho    float64
	Theta  float64
	Votes  int
	Family LineFamily
}

// Result contains uncleaned Hough line candidates.
type Result struct {
	ROI   image.Rectangle
	Lines []Line
}

// DetectGridLines preprocesses the configured ROI and returns raw Hough candidates.
func DetectGridLines(src image.Image, cfg Config) (*Result, error) {
	if src == nil {
		return nil, fmt.Errorf("nil source image")
	}
	if cfg.ROI.Empty() || !cfg.ROI.In(src.Bounds()) {
		return nil, fmt.Errorf("grid ROI %v is outside image bounds %v", cfg.ROI, src.Bounds())
	}
	if cfg.HorizontalGradientQuantile <= 0 || cfg.HorizontalGradientQuantile >= 1 || cfg.VerticalGradientQuantile <= 0 || cfg.VerticalGradientQuantile >= 1 {
		return nil, fmt.Errorf("gradient quantiles must be between 0 and 1")
	}
	if cfg.ThetaStepDegrees <= 0 || cfg.ThetaStepDegrees > 45 {
		return nil, fmt.Errorf("theta step must be in (0, 45] degrees")
	}
	if cfg.GradientAngleTolerance <= 0 || cfg.GradientAngleTolerance > 90 {
		return nil, fmt.Errorf("gradient angle tolerance must be in (0, 90] degrees")
	}
	if cfg.HorizontalPeakQuantile <= 0 || cfg.HorizontalPeakQuantile > 1 || cfg.VerticalPeakQuantile <= 0 || cfg.VerticalPeakQuantile > 1 {
		return nil, fmt.Errorf("peak reference quantiles must be in (0, 1]")
	}
	if cfg.MinPeakVoteRatio <= 0 || cfg.MinPeakVoteRatio > 1 {
		return nil, fmt.Errorf("minimum peak vote ratio must be in (0, 1]")
	}
	gray := grayscale(src, cfg.ROI)
	blurred := blur3x3(gray)
	gradient, _, directions, horizontalResponses, verticalResponses := sobelMagnitude(blurred)
	_, horizontalCandidates, verticalCandidates, _ := thresholdByFamily(
		gradient,
		horizontalResponses,
		verticalResponses,
		[2]float64{cfg.HorizontalGradientQuantile, cfg.VerticalGradientQuantile},
	)
	lines, _, _, _ := houghLines(horizontalCandidates, verticalCandidates, directions, cfg)
	translateLines(lines, cfg.ROI.Min)

	return &Result{
		ROI:   cfg.ROI,
		Lines: lines,
	}, nil
}

func grayscale(src image.Image, roi image.Rectangle) *image.Gray {
	dst := image.NewGray(image.Rect(0, 0, roi.Dx(), roi.Dy()))
	for y := range roi.Dy() {
		for x := range roi.Dx() {
			r, g, b, _ := src.At(roi.Min.X+x, roi.Min.Y+y).RGBA()
			value := (299*uint64(r) + 587*uint64(g) + 114*uint64(b) + 500) / 1000
			dst.SetGray(x, y, color.Gray{Y: uint8(value >> 8)})
		}
	}
	return dst
}

func blur3x3(src *image.Gray) *image.Gray {
	dst := image.NewGray(src.Bounds())
	copy(dst.Pix, src.Pix)
	weights := [3][3]int{{1, 2, 1}, {2, 4, 2}, {1, 2, 1}}
	for y := 1; y < src.Rect.Dy()-1; y++ {
		for x := 1; x < src.Rect.Dx()-1; x++ {
			sum := 0
			for ky := -1; ky <= 1; ky++ {
				for kx := -1; kx <= 1; kx++ {
					sum += int(src.GrayAt(x+kx, y+ky).Y) * weights[ky+1][kx+1]
				}
			}
			dst.SetGray(x, y, color.Gray{Y: uint8(sum / 16)})
		}
	}
	return dst
}

func sobelMagnitude(src *image.Gray) (*image.Gray, []int, []float64, []int, []int) {
	dst := image.NewGray(src.Bounds())
	magnitudes := make([]int, src.Rect.Dx()*src.Rect.Dy())
	directions := make([]float64, len(magnitudes))
	horizontalResponses := make([]int, len(magnitudes))
	verticalResponses := make([]int, len(magnitudes))
	for y := 1; y < src.Rect.Dy()-1; y++ {
		for x := 1; x < src.Rect.Dx()-1; x++ {
			a := func(dx, dy int) int { return int(src.GrayAt(x+dx, y+dy).Y) }
			gx := -a(-1, -1) + a(1, -1) - 2*a(-1, 0) + 2*a(1, 0) - a(-1, 1) + a(1, 1)
			gy := -a(-1, -1) - 2*a(0, -1) - a(1, -1) + a(-1, 1) + 2*a(0, 1) + a(1, 1)
			magnitude := int(math.Round(math.Hypot(float64(gx), float64(gy))))
			index := y*src.Rect.Dx() + x
			magnitudes[index] = magnitude
			directions[index] = normalizeTheta(math.Atan2(float64(gy), float64(gx)))
			horizontalResponses[index] = intAbs(gy)
			verticalResponses[index] = intAbs(gx)
			if magnitude > 255 {
				magnitude = 255
			}
			dst.SetGray(x, y, color.Gray{Y: uint8(magnitude)})
		}
	}
	return dst, magnitudes, directions, horizontalResponses, verticalResponses
}

func intAbs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func thresholdByFamily(gradient *image.Gray, horizontalResponses, verticalResponses []int, quantiles [2]float64) (*image.Gray, *image.Gray, *image.Gray, [2]int) {
	responses := [2][]int{horizontalResponses, verticalResponses}
	distributions := [2][]int{}
	for family := range distributions {
		for _, response := range responses[family] {
			if response > 0 {
				distributions[family] = append(distributions[family], response)
			}
		}
	}
	thresholds := [2]int{}
	for family := range thresholds {
		slices.Sort(distributions[family])
		thresholds[family] = intQuantile(distributions[family], quantiles[family])
	}
	combined := image.NewGray(gradient.Bounds())
	familyMasks := [2]*image.Gray{image.NewGray(gradient.Bounds()), image.NewGray(gradient.Bounds())}
	for family := range familyMasks {
		for y := range gradient.Rect.Dy() {
			for x := range gradient.Rect.Dx() {
				index := y*gradient.Rect.Dx() + x
				if responses[family][index] >= thresholds[family] && isDirectionalPeak(responses[family], gradient.Rect.Dx(), gradient.Rect.Dy(), x, y, LineFamily(family)) {
					combined.SetGray(x, y, color.Gray{Y: 255})
					familyMasks[family].SetGray(x, y, color.Gray{Y: 255})
				}
			}
		}
	}
	return combined, familyMasks[HorizontalFamily], familyMasks[VerticalFamily], thresholds
}

func isDirectionalPeak(responses []int, width, height, x, y int, family LineFamily) bool {
	value := responses[y*width+x]
	for offset := -1; offset <= 1; offset++ {
		if offset == 0 {
			continue
		}
		neighborX, neighborY := x, y+offset
		if family == VerticalFamily {
			neighborX, neighborY = x+offset, y
		}
		if neighborX < 0 || neighborX >= width || neighborY < 0 || neighborY >= height {
			continue
		}
		if responses[neighborY*width+neighborX] > value {
			return false
		}
	}
	return true
}

func houghLines(horizontalCandidates, verticalCandidates *image.Gray, directions []float64, cfg Config) ([]Line, [2]int, [2]int, [2]int) {
	step := cfg.ThetaStepDegrees * math.Pi / 180
	thetaCount := int(math.Ceil(math.Pi / step))
	rhoMax := int(math.Ceil(math.Hypot(float64(horizontalCandidates.Rect.Dx()), float64(horizontalCandidates.Rect.Dy()))))
	rhoCount := rhoMax*2 + 1
	accumulator := make([]int, thetaCount*rhoCount)
	cosines := make([]float64, thetaCount)
	sines := make([]float64, thetaCount)
	for thetaIndex := range thetaCount {
		theta := float64(thetaIndex) * step
		cosines[thetaIndex] = math.Cos(theta)
		sines[thetaIndex] = math.Sin(theta)
	}

	familyMasks := [2]*image.Gray{horizontalCandidates, verticalCandidates}
	for family, candidates := range familyMasks {
		for y := range candidates.Rect.Dy() {
			for x := range candidates.Rect.Dx() {
				if candidates.GrayAt(x, y).Y == 0 {
					continue
				}
				gradientTheta := directions[y*candidates.Rect.Dx()+x]
				for thetaIndex := range thetaCount {
					theta := float64(thetaIndex) * step
					if familyForTheta(theta) != LineFamily(family) || angleDistance(theta, gradientTheta) > cfg.GradientAngleTolerance*math.Pi/180 {
						continue
					}
					rho := int(math.Round(float64(x)*cosines[thetaIndex]+float64(y)*sines[thetaIndex])) + rhoMax
					accumulator[thetaIndex*rhoCount+rho]++
				}
			}
		}
	}

	maxVotes := [2]int{}
	voteDistributions := [2][]int{}
	for thetaIndex := range thetaCount {
		family := familyForTheta(float64(thetaIndex) * step)
		for rhoIndex := range rhoCount {
			votes := accumulator[thetaIndex*rhoCount+rhoIndex]
			maxVotes[family] = max(maxVotes[family], votes)
			if votes > 0 {
				voteDistributions[family] = append(voteDistributions[family], votes)
			}
		}
	}
	referenceVotes := [2]int{}
	minVotes := [2]int{}
	for family := range referenceVotes {
		slices.Sort(voteDistributions[family])
		quantile := cfg.HorizontalPeakQuantile
		if LineFamily(family) == VerticalFamily {
			quantile = cfg.VerticalPeakQuantile
		}
		referenceVotes[family] = intQuantile(voteDistributions[family], quantile)
		minVotes[family] = int(math.Ceil(float64(referenceVotes[family]) * cfg.MinPeakVoteRatio))
	}

	var lines []Line
	for thetaIndex := range thetaCount {
		theta := float64(thetaIndex) * step
		family := familyForTheta(theta)
		for rhoIndex := range rhoCount {
			votes := accumulator[thetaIndex*rhoCount+rhoIndex]
			if votes >= minVotes[family] && isLocalMaximum(accumulator, thetaCount, rhoCount, thetaIndex, rhoIndex) {
				line := Line{
					Rho:    float64(rhoIndex - rhoMax),
					Theta:  theta,
					Votes:  votes,
					Family: family,
				}
				if lineCrossesOppositeROISides(line, horizontalCandidates.Bounds()) {
					lines = append(lines, line)
				}
			}
		}
	}
	return lines, maxVotes, referenceVotes, minVotes
}

func lineCrossesOppositeROISides(line Line, roi image.Rectangle) bool {
	cosTheta := math.Cos(line.Theta)
	sinTheta := math.Sin(line.Theta)
	if line.Family == HorizontalFamily {
		if math.Abs(sinTheta) < 1e-9 {
			return false
		}
		leftY := (line.Rho - float64(roi.Min.X)*cosTheta) / sinTheta
		rightY := (line.Rho - float64(roi.Max.X-1)*cosTheta) / sinTheta
		return leftY >= float64(roi.Min.Y) && leftY < float64(roi.Max.Y) && rightY >= float64(roi.Min.Y) && rightY < float64(roi.Max.Y)
	}
	if math.Abs(cosTheta) < 1e-9 {
		return false
	}
	topX := (line.Rho - float64(roi.Min.Y)*sinTheta) / cosTheta
	bottomX := (line.Rho - float64(roi.Max.Y-1)*sinTheta) / cosTheta
	return topX >= float64(roi.Min.X) && topX < float64(roi.Max.X) && bottomX >= float64(roi.Min.X) && bottomX < float64(roi.Max.X)
}

func isLocalMaximum(accumulator []int, thetaCount, rhoCount, thetaIndex, rhoIndex int) bool {
	const thetaRadius = 2
	const rhoRadius = 3
	votes := accumulator[thetaIndex*rhoCount+rhoIndex]
	for thetaOffset := -thetaRadius; thetaOffset <= thetaRadius; thetaOffset++ {
		neighborTheta := thetaIndex + thetaOffset
		if neighborTheta < 0 || neighborTheta >= thetaCount {
			continue
		}
		for rhoOffset := -rhoRadius; rhoOffset <= rhoRadius; rhoOffset++ {
			neighborRho := rhoIndex + rhoOffset
			if neighborRho < 0 || neighborRho >= rhoCount || thetaOffset == 0 && rhoOffset == 0 {
				continue
			}
			neighborVotes := accumulator[neighborTheta*rhoCount+neighborRho]
			if neighborVotes > votes || neighborVotes == votes && (thetaOffset < 0 || thetaOffset == 0 && rhoOffset < 0) {
				return false
			}
		}
	}
	return true
}

func intQuantile(ordered []int, quantile float64) int {
	if len(ordered) == 0 {
		return 0
	}
	index := int(math.Round(float64(len(ordered)-1) * quantile))
	return ordered[index]
}

func normalizeTheta(theta float64) float64 {
	theta = math.Mod(theta, math.Pi)
	if theta < 0 {
		theta += math.Pi
	}
	return theta
}

func angleDistance(left, right float64) float64 {
	difference := math.Abs(left - right)
	return min(difference, math.Pi-difference)
}

func familyForTheta(theta float64) LineFamily {
	lineDirection := math.Mod(theta+math.Pi/2, math.Pi)
	if lineDirection <= math.Pi/4 || lineDirection >= 3*math.Pi/4 {
		return HorizontalFamily
	}
	return VerticalFamily
}

func translateLines(lines []Line, offset image.Point) {
	for i := range lines {
		lines[i].Rho += float64(offset.X)*math.Cos(lines[i].Theta) + float64(offset.Y)*math.Sin(lines[i].Theta)
	}
}

func clipLineToRect(line Line, rect image.Rectangle) (image.Point, image.Point, bool) {
	cosTheta := math.Cos(line.Theta)
	sinTheta := math.Sin(line.Theta)
	var points []image.Point
	add := func(x, y float64) {
		if x < float64(rect.Min.X)-0.5 || x > float64(rect.Max.X-1)+0.5 || y < float64(rect.Min.Y)-0.5 || y > float64(rect.Max.Y-1)+0.5 {
			return
		}
		point := image.Pt(int(math.Round(x)), int(math.Round(y)))
		if !slices.Contains(points, point) {
			points = append(points, point)
		}
	}
	if math.Abs(sinTheta) > 1e-9 {
		add(float64(rect.Min.X), (line.Rho-float64(rect.Min.X)*cosTheta)/sinTheta)
		add(float64(rect.Max.X-1), (line.Rho-float64(rect.Max.X-1)*cosTheta)/sinTheta)
	}
	if math.Abs(cosTheta) > 1e-9 {
		add((line.Rho-float64(rect.Min.Y)*sinTheta)/cosTheta, float64(rect.Min.Y))
		add((line.Rho-float64(rect.Max.Y-1)*sinTheta)/cosTheta, float64(rect.Max.Y-1))
	}
	if len(points) < 2 {
		return image.Point{}, image.Point{}, false
	}
	return points[0], points[1], true
}
