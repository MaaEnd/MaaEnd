package core

import (
	"image"
	"math"
)

// Stationary compares only the material list, excluding account/connection text.
func Stationary(a, b image.Image) bool {
	return stationaryIn(a, b, image.Rect(425, 250, 1238, 665))
}

// StationaryAboveToast compares the list without its bottom strip, where the cloud
// client shows transient toasts (for example an unstable-network notice).
func StationaryAboveToast(a, b image.Image) bool {
	return stationaryIn(a, b, image.Rect(425, 250, 1238, 620))
}

func stationaryIn(a, b image.Image, area image.Rectangle) bool {
	if a == nil || b == nil || a.Bounds() != b.Bounds() {
		return false
	}
	var diff float64
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			diff += math.Abs(float64(ar)-float64(br)) + math.Abs(float64(ag)-float64(bg)) + math.Abs(float64(ab)-float64(bb))
		}
	}
	return diff/(257*3*float64(area.Dx()*area.Dy())) < .8
}

// Scrollbar returns the longest bright thumb in the measured right edge.
func Scrollbar(img image.Image) (int, int, bool) {
	if img == nil || img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 720 {
		return 0, 0, false
	}
	bestStart, bestEnd, start := -1, -1, -1
	for y := 250; y <= 665; y++ {
		bright := false
		if y < 665 {
			var sum float64
			for x := 1232; x < 1238; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				sum += float64(r+g+b) / 257
			}
			bright = sum/18 > 140
		}
		if bright && start < 0 {
			start = y
		}
		if !bright && start >= 0 {
			if y-start >= 8 && y-start > bestEnd-bestStart {
				bestStart, bestEnd = start, y-1
			}
			start = -1
		}
	}
	return bestStart, bestEnd, bestStart >= 0
}

// TabSelected confirms the measured white selected-tab region.
func TabSelected(img image.Image, x, y int) bool {
	if img == nil || img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 720 {
		return false
	}
	count := 0
	for yy := y - 12; yy < y+12; yy++ {
		for xx := x - 65; xx < x+65; xx++ {
			r, g, b, _ := img.At(xx, yy).RGBA()
			if r/257 > 200 && g/257 > 200 && b/257 > 200 {
				count++
			}
		}
	}
	return float64(count)/(24*130) > .45
}

// AlignCellX snaps an estimated square icon cell to the bright card edges within
// ±reach pixels when both card sides are visible. A clipped side keeps the
// original estimate. The owned-count label is not centred under every card, so an
// estimate from it can be several pixels off, enough to fail icon matching.
func AlignCellX(img image.Image, cell image.Rectangle, reach int) image.Rectangle {
	b := img.Bounds()
	column := func(x int) float64 {
		if x < b.Min.X || x >= b.Max.X {
			return 0
		}
		var sum float64
		n := 0
		for y := cell.Min.Y + cell.Dy()/10; y < cell.Min.Y+cell.Dy()*6/10; y += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += float64(r+g+bl) / (3 * 257)
			n++
		}
		return sum / float64(n)
	}
	edge := func(x int) float64 {
		// Inside minus outside brightness across the left and right card borders.
		left := column(x) + column(x+1) - column(x-2) - column(x-3)
		right := column(x+cell.Dx()-1) + column(x+cell.Dx()-2) - column(x+cell.Dx()+1) - column(x+cell.Dx()+2)
		// A clipped card can expose one strong edge inside the icon. Require
		// both sides so it cannot pull the cell into neighbouring content.
		return 2 * math.Min(left, right)
	}
	best, bestScore := cell.Min.X, edge(cell.Min.X)
	for dx := -reach; dx <= reach; dx++ {
		if score := edge(cell.Min.X + dx); score > bestScore {
			best, bestScore = cell.Min.X+dx, score
		}
	}
	// Without a clear border contrast keep the estimate.
	if bestScore < 120 {
		return cell
	}
	return cell.Add(image.Pt(best-cell.Min.X, 0))
}
