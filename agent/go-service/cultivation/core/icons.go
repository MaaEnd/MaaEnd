package core

import (
	"image"
	"math"
)

// Stationary compares only the material list, excluding account/connection text.
func Stationary(a, b image.Image) bool {
	if a == nil || b == nil || a.Bounds() != b.Bounds() {
		return false
	}
	var diff float64
	for y := 250; y < 665; y++ {
		for x := 425; x < 1238; x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			diff += math.Abs(float64(ar)-float64(br)) + math.Abs(float64(ag)-float64(bg)) + math.Abs(float64(ab)-float64(bb))
		}
	}
	return diff/(257*3*415*813) < .8
}

// Scrollbar returns the longest bright thumb in the measured right edge.
func Scrollbar(img image.Image) (int, int, bool) {
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

// IconHash is a difference hash of the icon artwork inside cell, skipping the
// bottom quantity band so the same item hashes alike on every row and pass.
func IconHash(img image.Image, cell image.Rectangle) uint64 {
	art := image.Rect(cell.Min.X, cell.Min.Y, cell.Max.X, cell.Min.Y+cell.Dy()*7/10)
	var gray [8][9]float64
	for gy := range 8 {
		for gx := range 9 {
			x0 := art.Min.X + gx*art.Dx()/9
			x1 := art.Min.X + (gx+1)*art.Dx()/9
			y0 := art.Min.Y + gy*art.Dy()/8
			y1 := art.Min.Y + (gy+1)*art.Dy()/8
			var sum, n float64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					sum += 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
					n++
				}
			}
			if n > 0 {
				gray[gy][gx] = sum / n
			}
		}
	}
	var hash uint64
	for gy := range 8 {
		for gx := range 8 {
			hash <<= 1
			if gray[gy][gx] > gray[gy][gx+1] {
				hash |= 1
			}
		}
	}
	return hash
}

// AlignCellX snaps an estimated square icon cell to the bright card edges within
// ±reach pixels. The owned-count label is not centred under every card, so an
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
		return left + right
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
