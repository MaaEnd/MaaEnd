package core

import (
	"errors"
	"image"
	"math"
)

// ListArea is the scrolling material list, without the scrollbar, in 1280x720.
var ListArea = image.Rect(430, 250, 1225, 665)

// ReadBand is the vertical range of 「拥有」 labels kept on a scrolled page
// (see OwnedRows); a scroll longer than this can step over a whole row.
const ReadBand = 635 - 355

// ScrollShift measures how many pixels the list content moved up from prev to
// cur (0 when it did not move), searching 0..maxShift. The best alignment must
// clearly beat every alignment more than a few pixels away; otherwise the
// shift is reported as unknown.
func ScrollShift(prev, cur image.Image, area image.Rectangle, maxShift int) (int, error) {
	if prev == nil || cur == nil || prev.Bounds() != cur.Bounds() || !area.In(prev.Bounds()) {
		return 0, errors.New("invalid frames for scroll measurement")
	}
	if maxShift > area.Dy()-60 {
		maxShift = area.Dy() - 60
	}
	a, b := grayGrid(prev, area), grayGrid(cur, area)
	rows, cols := len(a), len(a[0])
	scores := make([]float64, maxShift/2+1)
	for i := range scores {
		s := i // in grid rows (2 px)
		var sum float64
		for y := 0; y+s < rows; y++ {
			for x := 0; x < cols; x++ {
				sum += math.Abs(a[y+s][x] - b[y][x])
			}
		}
		scores[i] = sum / float64((rows-s)*cols)
	}
	best := 0
	for i := range scores {
		if scores[i] < scores[best] {
			best = i
		}
	}
	runner := math.Inf(1)
	for i := range scores {
		if (i < best-3 || i > best+3) && scores[i] < runner {
			runner = scores[i]
		}
	}
	// Measured on CloudADB: settled pages align at 0-13 gray levels, the next
	// best shift scores 20+, a changed screen scores ~30 everywhere.
	if scores[best] > 20 || !(scores[best] < runner*0.6) {
		return 0, errors.New("list scroll could not be measured")
	}
	return best * 2, nil
}

// grayGrid samples area every 2 px as gray levels 0-255.
func grayGrid(img image.Image, area image.Rectangle) [][]float64 {
	var out [][]float64
	for y := area.Min.Y; y < area.Max.Y; y += 2 {
		var row []float64
		for x := area.Min.X; x < area.Max.X; x += 2 {
			r, g, b, _ := img.At(x, y).RGBA()
			row = append(row, float64(r+g+b)/(3*257))
		}
		out = append(out, row)
	}
	return out
}

// PlacedRows accumulates material cells by their position in the whole list,
// so a page that overlaps the previous one by any amount adds only new cells.
type PlacedRows struct {
	Rows      []Material
	positions []image.Point
}

// Add places page cells (label centre x, label y + offset) and skips cells
// already seen at the same place; a different item at a seen place is an error.
func (p *PlacedRows) Add(rows []Material, points []image.Point, offset int) error {
	if len(rows) != len(points) {
		return errors.New("material rows and positions differ")
	}
	for i, row := range rows {
		at := image.Pt(points[i].X, points[i].Y+offset)
		seen := -1
		for j, q := range p.positions {
			if abs(q.X-at.X) <= 20 && abs(q.Y-at.Y) <= 14 {
				seen = j
				break
			}
		}
		if seen < 0 {
			p.Rows = append(p.Rows, row)
			p.positions = append(p.positions, at)
			continue
		}
		if p.Rows[seen] != row {
			return errors.New("different material read at the same list position")
		}
	}
	return nil
}
