// Copyright (c) 2026 Harry Huang
package maptrackerinternal

import (
	"encoding/json"
	"math"

	"github.com/rs/zerolog"
)

/* ******** Point ******** */

type Point struct {
	X float64
	Y float64
}

func (p Point) Clone() Point {
	return Point{X: p.X, Y: p.Y}
}

// MarshalZerologObject serializes the point as zerolog sub-fields X and Y.
func (p Point) MarshalZerologObject(e *zerolog.Event) {
	e.Float64("X", p.X).Float64("Y", p.Y)
}

func (p Point) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]float64{p.X, p.Y})
}

func (p *Point) UnmarshalJSON(data []byte) error {
	var arr [2]float64
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}

	p.X = arr[0]
	p.Y = arr[1]
	return nil
}

func (p Point) IntX() int {
	return int(math.Round(p.X))
}

func (p Point) IntY() int {
	return int(math.Round(p.Y))
}

func (p Point) IsNaN() bool {
	return math.IsNaN(p.X) || math.IsNaN(p.Y)
}

func (p Point) IsInf() bool {
	return math.IsInf(p.X, 0) || math.IsInf(p.Y, 0)
}

func (p Point) IsValid() bool {
	return !p.IsNaN() && !p.IsInf()
}

// InRect reports whether p lies within the axis-aligned rectangle [x, x+w) x [y, y+h).
func (p Point) InRect(x, y, w, h float64) bool {
	return p.X >= x && p.X < x+w && p.Y >= y && p.Y < y+h
}

// DistanceTo returns the Euclidean distance from this point to another point.
func (p Point) DistanceTo(other Point) float64 {
	return math.Hypot(p.X-other.X, p.Y-other.Y)
}

// ManhattanDistanceTo returns the Manhattan distance from this point to another point.
func (p Point) ManhattanDistanceTo(other Point) float64 {
	return math.Abs(p.X-other.X) + math.Abs(p.Y-other.Y)
}

// AngleTo returns the angle in degrees [0, 360) from this point to another point,
// where 0° is up (negative Y direction), and angles increase clockwise.
func (p Point) AngleTo(other Point) float64 {
	dx := other.X - p.X
	dy := other.Y - p.Y

	// 0° is up (-Y), increasing clockwise
	angle := math.Atan2(dx, -dy) * 180 / math.Pi

	// Normalize to [0, 360)
	angle = math.Mod(angle+360, 360)

	return angle
}

/* ******** Linear Transformation ******** */

type LinearTransform struct {
	ScaleX  float64
	ScaleY  float64
	OffsetX float64
	OffsetY float64
}

func (lt LinearTransform) Apply(p Point) Point {
	return Point{
		X: p.X*lt.ScaleX + lt.OffsetX,
		Y: p.Y*lt.ScaleY + lt.OffsetY,
	}
}

func (lt LinearTransform) Inverse(p Point) Point {
	return Point{
		X: (p.X - lt.OffsetX) / lt.ScaleX,
		Y: (p.Y - lt.OffsetY) / lt.ScaleY,
	}
}
