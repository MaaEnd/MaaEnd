// Copyright (c) 2026 Harry Huang
package puzzle

import (
	"encoding/json"
	"image"
	"image/color"
	"math"
	"sort"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

/* ******** Recognitions ******** */

type TemplateMatchDTO struct {
	X       int
	Y       int
	CenterX int
	CenterY int
	Score   float64
}

// matchTemplateAll performs template matching and returns all matches up to maxMatch.
func matchTemplateAll(ctx *maa.Context, img image.Image, template string, roi []int, maxMatch int) []TemplateMatchDTO {
	nodeName := "PuzzleSolverTemplateMatch_" + template
	config := map[string]any{
		nodeName: map[string]any{
			"recognition": "TemplateMatch",
			"template":    template,
			"threshold":   0.7,
			"roi":         roi,
			"order_by":    "score",
			"method":      5, // TM_CCOEFF_NORMED
		},
	}

	res := ctx.RunRecognition(nodeName, img, config)
	if res == nil || !res.Hit {
		return make([]TemplateMatchDTO, 0)
	}

	var detail struct {
		All []struct {
			Box   []int   `json:"box"`
			Score float64 `json:"score"`
		} `json:"all"`
	}

	if err := json.Unmarshal([]byte(res.DetailJson), &detail); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal match detail")
		return nil
	}

	matches := make([]TemplateMatchDTO, 0, min(len(detail.All), maxMatch))
	for i, m := range detail.All {
		if len(m.Box) >= 4 {
			matches = append(matches, TemplateMatchDTO{
				m.Box[0],
				m.Box[1],
				m.Box[0] + m.Box[2]/2,
				m.Box[1] + m.Box[3]/2,
				m.Score,
			})
		}
		if i+1 >= maxMatch {
			break
		}
	}
	return matches
}

/* ******** Colors ******** */

// getAreaVariance calculates the average standard deviation across RGB channels
func getAreaVariance(img image.Image, rect image.Rectangle) float64 {
	var sumR, sumG, sumB float64
	var count float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8), float64(g>>8), float64(b>>8)
			sumR += fr
			sumG += fg
			sumB += fb
			count++
		}
	}
	if count == 0 {
		return 0
	}
	avgR := sumR / count
	avgG := sumG / count
	avgB := sumB / count

	var varR, varG, varB float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8), float64(g>>8), float64(b>>8)
			varR += (fr - avgR) * (fr - avgR)
			varG += (fg - avgG) * (fg - avgG)
			varB += (fb - avgB) * (fb - avgB)
		}
	}
	return (math.Sqrt(varR/count) + math.Sqrt(varG/count) + math.Sqrt(varB/count)) / 3.0
}

// rgbToHSV converts normalized RGB [0, 1] to HSV: Hue[0, 360), Saturation[0, 1], Value[0, 1]
func rgbToHSV(fr, fg, fb float64) (float64, float64, float64) {
	maxC := math.Max(fr, math.Max(fg, fb))
	minC := math.Min(fr, math.Min(fg, fb))
	delta := maxC - minC

	// Value
	v := maxC

	// Saturation
	s := 0.0
	if maxC != 0 {
		s = delta / maxC
	}

	// Hue
	h := 0.0
	if delta != 0 {
		switch maxC {
		case fr:
			h = (fg - fb) / delta
			if fg < fb {
				h += 6
			}
		case fg:
			h = (fb-fr)/delta + 2
		default:
			h = (fr-fg)/delta + 4
		}
		h *= 60
	}

	return h, s, v
}

// getAreaMeanHSV calculates average Hue[0, 360), Saturation[0, 1], Value[0, 1] of an area
func getAreaMeanHSV(img image.Image, rect image.Rectangle) (float64, float64, float64) {
	var sumHue, sumSat, sumVal float64
	var count float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0

			h, s, v := rgbToHSV(fr, fg, fb)
			sumHue += h
			sumSat += s
			sumVal += v

			count++
		}
	}
	if count == 0 {
		return 0, 0, 0
	}
	return sumHue / count, sumSat / count, sumVal / count
}

// getAreaMidHSV calculates the median Hue[0, 360), Saturation[0, 1], Value[0, 1] of an area
func getAreaMidHSV(img image.Image, rect image.Rectangle) (float64, float64, float64) {
	var hues, sats, vals []float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0

			h, s, v := rgbToHSV(fr, fg, fb)
			hues = append(hues, h)
			sats = append(sats, s)
			vals = append(vals, v)
		}
	}

	if len(hues) == 0 {
		return 0, 0, 0
	}

	sort.Float64s(hues)
	sort.Float64s(sats)
	sort.Float64s(vals)

	mid := len(hues) / 2
	if len(hues)%2 == 0 {
		return (hues[mid-1] + hues[mid]) / 2, (sats[mid-1] + sats[mid]) / 2, (vals[mid-1] + vals[mid]) / 2
	}
	return hues[mid], sats[mid], vals[mid]
}

// getPixelHSV returns the Hue[0, 360), Saturation[0, 1], Value[0, 1] of a pixel
func getPixelHSV(img image.Image, x, y int, targetHue int, targetHueAllowance int) (float64, float64, float64) {
	r, g, b, _ := img.At(x, y).RGBA()
	fr, fg, fb := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0

	h, s, v := rgbToHSV(fr, fg, fb)

	if targetHue >= 0 {
		if diffHue(int(h), targetHue) > targetHueAllowance {
			return 0, 0, 0
		}
	}
	return h, s, v
}

// getSVGBImage returns a new image where the R, G, B channels are replaced by 0, Saturation, Value.
func getSVGBImage(img image.Image) image.Image {
	bounds := img.Bounds()
	newImg := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0

			_, s, v := rgbToHSV(fr, fg, fb)

			newImg.SetRGBA(x, y, color.RGBA{0, uint8(s * 255), uint8(v * 255), 255})
		}
	}
	return newImg
}

// diffHue returns the smallest difference between two hues [0, 360)
func diffHue(h1, h2 int) int {
	diff := int(math.Abs(float64(h1 - h2)))
	if diff > 180 {
		diff = 360 - diff
	}
	return diff
}

// meanHue calculates the circular mean of a slice of hues [0, 360)
func meanHue(hues []int) int {
	if len(hues) == 0 {
		return 0
	}
	var sumSin, sumCos float64
	for _, h := range hues {
		rad := float64(h) * math.Pi / 180.0
		sumSin += math.Sin(rad)
		sumCos += math.Cos(rad)
	}
	avgRad := math.Atan2(sumSin, sumCos)
	avgDeg := avgRad * 180.0 / math.Pi
	if avgDeg < 0 {
		avgDeg += 360
	}
	return int(math.Round(avgDeg))
}

// clusterHues groups hues that are close to each other within maxDiff
func clusterHues(hues []int, maxDiff int) map[int][]int {
	clusters := make(map[int][]int)
	processed := make(map[int]bool)

	for _, h1 := range hues {
		if processed[h1] {
			continue
		}

		clusterID := h1
		// Check if h1 belongs to an existing cluster
		foundCluster := false
		for center := range clusters {
			if diffHue(h1, center) <= maxDiff {
				clusterID = center
				foundCluster = true
				break
			}
		}

		if !foundCluster {
			clusters[clusterID] = []int{}
		}
		clusters[clusterID] = append(clusters[clusterID], h1)
		processed[h1] = true
	}
	return clusters
}

/* ******** Coordinate Conversions ******** */

// convertLTCoordToBoardCoord converts pixel LT coordinate to grid index.
// totalW/totalH are the dimensions of the board (used to determine odd/even grid alignment).
func convertLTCoordToBoardCoord(ltX, ltY int, totalW, totalH int) (int, int) {
	// Formula: idx = (LT - CenterLT) / BlockW + (TotalW - 1) / 2.0
	// This handles both Odd (center at integer index) and Even (center at half-integer index) correctly.

	gridX := int(math.Round((float64(ltX)-BOARD_CENTER_BLOCK_LT_X)/BOARD_BLOCK_W + float64(totalW-1)/2.0))
	gridY := int(math.Round((float64(ltY)-BOARD_CENTER_BLOCK_LT_Y)/BOARD_BLOCK_H + float64(totalH-1)/2.0))
	return gridX, gridY
}

// convertBoardCoordToLTCoord converts grid index to pixel LT coordinate.
// totalW/totalH are the dimensions of the board (used to determine odd/even grid alignment).
func convertBoardCoordToLTCoord(bx, by int, totalW, totalH int) (int, int) {
	// Formula: LT = CenterLT + (idx - (TotalW - 1) / 2.0) * BlockW

	ltX := BOARD_CENTER_BLOCK_LT_X + (float64(bx)-float64(totalW-1)/2.0)*BOARD_BLOCK_W
	ltY := BOARD_CENTER_BLOCK_LT_Y + (float64(by)-float64(totalH-1)/2.0)*BOARD_BLOCK_H
	return int(ltX), int(ltY)
}

/* ******** Actions ******** */

// ActionWrapper provides synchronized touch/key operations with built-in delays
type ActionWrapper struct {
	ctrl *maa.Controller
}

// NewActionWrapper creates a new ActionWrapper from a context
func NewActionWrapper(ctrl *maa.Controller) *ActionWrapper {
	return &ActionWrapper{ctrl}
}

// TouchUpSync releases touch contact and waits
func (aw *ActionWrapper) TouchUpSync(delayMillis int) {
	aw.ctrl.PostTouchUp(0).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// TouchDownSync moves to position then touches down
func (aw *ActionWrapper) TouchDownSync(contact, x, y int, delayMillis int) {
	halfDelay := delayMillis / 2
	aw.ctrl.PostTouchMove(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(halfDelay) * time.Millisecond)
	aw.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(delayMillis-halfDelay) * time.Millisecond)
}

// TouchMoveSync moves touch contact to position and waits
func (aw *ActionWrapper) TouchMoveSync(contact, x, y int, delayMillis int) {
	aw.ctrl.PostTouchMove(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// TypeKeySync sends a key press and waits
func (aw *ActionWrapper) TypeKeySync(keyCode int, delayMillis int) {
	aw.ctrl.PostClickKey(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}
