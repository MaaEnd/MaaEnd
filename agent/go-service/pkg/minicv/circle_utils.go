package minicv

import "math"

// Circle defines a circle region in image coordinates.
//
// Note that X and Y are the center coordinates.
// The circle includes all pixels whose centers are within the radius distance from (X, Y).
// If Radius is 0, it includes only the pixel at (X, Y).
type Circle struct {
	X      int
	Y      int
	Radius int
}

// MinX returns the minimum X coordinate of the circle's bounding box.
func (c Circle) MinX() int {
	return c.X - c.Radius
}

// MinY returns the minimum Y coordinate of the circle's bounding box.
func (c Circle) MinY() int {
	return c.Y - c.Radius
}

// MaxX returns the maximum X coordinate of the circle's bounding box.
func (c Circle) MaxX() int {
	return c.X + c.Radius
}

// MaxY returns the maximum Y coordinate of the circle's bounding box.
func (c Circle) MaxY() int {
	return c.Y + c.Radius
}

// circleSpan stores a continuous [X0, X1] segment on a single row Y.
type circleSpan struct {
	Y  int
	X0 int
	X1 int
}

// buildCircleSpans computes the horizontal spans of pixels covered by a circle in an image of size w*h.
func buildCircleSpans(w, h int, circle Circle) (spans []circleSpan, pixelCount int) {
	if w <= 0 || h <= 0 || circle.Radius < 0 {
		return nil, 0
	}

	yMin := max(0, circle.MinY())
	yMax := min(h-1, circle.MaxY())
	if yMin > yMax {
		return nil, 0
	}

	spans = make([]circleSpan, 0, yMax-yMin+1)
	pixelCount = 0

	r2 := circle.Radius * circle.Radius

	for y := yMin; y <= yMax; y++ {
		dy := y - circle.Y
		dx2 := r2 - dy*dy
		if dx2 < 0 {
			continue
		}
		dx := int(math.Sqrt(float64(dx2)))
		x0 := max(0, circle.X-dx)
		x1 := min(w-1, circle.X+dx)
		if x0 > x1 {
			continue
		}
		spans = append(spans, circleSpan{Y: y, X0: x0, X1: x1})
		pixelCount += x1 - x0 + 1
	}

	return spans, pixelCount
}
