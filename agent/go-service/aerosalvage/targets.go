package aerosalvage

import (
	"image"
	"math"
)

const (
	targetMinSide   = 7
	targetMaxSide   = 11
	targetMinFill   = 0.7
	targetIdealSide = 9
)

// DetectTargets returns centers of dilated bright-yellow components shaped like a filled 9x9 square.
func DetectTargets(mask *image.Gray, roi image.Rectangle) []Point {
	if mask == nil || mask.Bounds().Dx() != roi.Dx() || mask.Bounds().Dy() != roi.Dy() {
		return nil
	}

	visited := make([]bool, mask.Bounds().Dx()*mask.Bounds().Dy())
	var targets []Point
	for y := range mask.Bounds().Dy() {
		for x := range mask.Bounds().Dx() {
			index := y*mask.Bounds().Dx() + x
			if visited[index] || mask.GrayAt(x, y).Y == 0 {
				continue
			}
			component := collectMaskComponent(mask, image.Pt(x, y), visited)
			width, height := component.bounds.Dx(), component.bounds.Dy()
			fill := float64(component.area) / float64(width*height)
			if width < targetMinSide || width > targetMaxSide || height < targetMinSide || height > targetMaxSide || fill < targetMinFill {
				continue
			}
			if math.Abs(float64(width-targetIdealSide))+math.Abs(float64(height-targetIdealSide)) > 4 {
				continue
			}
			targets = append(targets, Point{
				X: float64(roi.Min.X) + float64(component.bounds.Min.X+component.bounds.Max.X-1)/2,
				Y: float64(roi.Min.Y) + float64(component.bounds.Min.Y+component.bounds.Max.Y-1)/2,
			})
		}
	}
	return targets
}

type maskComponent struct {
	bounds image.Rectangle
	area   int
}

func collectMaskComponent(mask *image.Gray, start image.Point, visited []bool) maskComponent {
	width := mask.Bounds().Dx()
	queue := []image.Point{start}
	visited[start.Y*width+start.X] = true
	component := maskComponent{bounds: image.Rect(start.X, start.Y, start.X+1, start.Y+1)}
	directions := [...]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}}

	for len(queue) > 0 {
		point := queue[0]
		queue = queue[1:]
		component.area++
		component.bounds = component.bounds.Union(image.Rect(point.X, point.Y, point.X+1, point.Y+1))
		for _, direction := range directions {
			neighbor := point.Add(direction)
			if !neighbor.In(mask.Bounds()) {
				continue
			}
			index := neighbor.Y*width + neighbor.X
			if visited[index] || mask.GrayAt(neighbor.X, neighbor.Y).Y == 0 {
				continue
			}
			visited[index] = true
			queue = append(queue, neighbor)
		}
	}
	return component
}
