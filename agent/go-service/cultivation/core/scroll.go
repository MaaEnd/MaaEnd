package core

import (
	"errors"
	"image"
	"math"
)

// ListArea is the scrolling material list, without the scrollbar, in 1280x720.
var ListArea = image.Rect(430, 250, 1225, 665)

// ScrollArea covers material columns, excluding repeated obtain buttons and
// section controls that can produce a competing alignment peak.
var ScrollArea = image.Rect(640, 250, 1010, 665)

// ReadBand is the overlap required to keep every owned-label row visible.
const ReadBand = 635 - 355

// ScrollShift measures an upward translation using normalized vertical image
// edges. Normalization tolerates cloud-video contrast changes without matching
// inventory numbers. Weak texture or competing alignments return an error.
func ScrollShift(prev, cur image.Image, area image.Rectangle, maxShift int) (int, error) {
	if prev == nil || cur == nil || prev.Bounds() != cur.Bounds() || !area.In(prev.Bounds()) || area.Dx() < 20 || area.Dy() < 80 || maxShift < 0 {
		return 0, errors.New("invalid frames for scroll measurement")
	}
	maxShift = min(maxShift, area.Dy()-64)
	a, b := edgeGrid(prev, area), edgeGrid(cur, area)
	rows, cols := len(a), len(a[0])
	scores := make([]float64, maxShift/2+1)
	best := 0
	for shift := range scores {
		var dot, aa, bb float64
		for y := 0; y+shift < rows; y++ {
			for x := range cols {
				av, bv := a[y+shift][x], b[y][x]
				dot += av * bv
				aa += av * av
				bb += bv * bv
			}
		}
		// A featureless panel cannot prove a displacement, even if identical.
		minimumEnergy := float64((rows-shift)*cols) * 4
		scores[shift] = math.Inf(-1)
		if aa >= minimumEnergy && bb >= minimumEnergy {
			scores[shift] = dot / math.Sqrt(aa*bb)
		}
		if scores[shift] > scores[best] {
			best = shift
		}
	}
	runner := math.Inf(-1)
	for i, score := range scores {
		if (i < best-3 || i > best+3) && score > runner {
			runner = score
		}
	}
	// Four preserved failure pairs correlate 0.63..0.73 with gaps 0.24..0.50.
	// Retain both absolute agreement and a unique peak; ambiguous rows fail.
	if scores[best] < .60 || math.IsInf(runner, -1) || scores[best]-runner < .15 {
		return 0, errors.New("list image has no unique scroll alignment")
	}
	return best * 2, nil
}

// edgeGrid samples luminance every two pixels, then removes flat backgrounds
// with a vertical difference. The comparison only uses overlapping content.
func edgeGrid(img image.Image, area image.Rectangle) [][]float64 {
	var gray [][]float64
	for y := area.Min.Y; y < area.Max.Y; y += 2 {
		var row []float64
		for x := area.Min.X; x < area.Max.X; x += 2 {
			r, g, b, _ := img.At(x, y).RGBA()
			row = append(row, (.299*float64(r)+.587*float64(g)+.114*float64(b))/257)
		}
		gray = append(gray, row)
	}
	var out [][]float64
	for y := 1; y+1 < len(gray); y++ {
		row := make([]float64, len(gray[y]))
		for x := range row {
			row[x] = gray[y+1][x] - gray[y-1][x]
		}
		out = append(out, row)
	}
	return out
}
