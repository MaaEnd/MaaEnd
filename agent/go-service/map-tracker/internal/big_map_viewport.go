// Copyright (c) 2026 Harry Huang
package maptracker

import "math"

// BigMapViewport represents a big-map viewport mapping between map coordinates and screen coordinates.
//
// Left/Top/Right/Bottom are viewport bounds in screen space.
//
// OriginMapX/OriginMapY are the map coordinates corresponding to the viewport's top-left corner.
//
// Scale is the screen-pixel-per-map-pixel ratio at current zoom.
type BigMapViewport struct {
	Left       float64 `json:"left"`
	Top        float64 `json:"top"`
	Right      float64 `json:"right"`
	Bottom     float64 `json:"bottom"`
	OriginMapX float64 `json:"originMapX"`
	OriginMapY float64 `json:"originMapY"`
	Scale      float64 `json:"scale"`
}

// NewBigMapViewport creates a viewport using fixed big-map screen bounds and inferred map origin/scale.
func NewBigMapViewport(originMapX, originMapY, scale float64) *BigMapViewport {
	left, top, right, bottom := bigMapViewBounds(WORK_W, WORK_H)
	return &BigMapViewport{
		Left:       float64(left),
		Top:        float64(top),
		Right:      float64(right),
		Bottom:     float64(bottom),
		OriginMapX: originMapX,
		OriginMapY: originMapY,
		Scale:      scale,
	}
}

// GetScreenCoordOf converts map coordinates to screen coordinates based on the current viewport.
func (bmv *BigMapViewport) GetScreenCoordOf(mapX, mapY float64) (float64, float64) {
	viewX := bmv.Left + (mapX-bmv.OriginMapX)*bmv.Scale
	viewY := bmv.Top + (mapY-bmv.OriginMapY)*bmv.Scale
	return viewX, viewY
}

// GetMapCoordOf converts screen coordinates to map coordinates based on the current viewport.
func (bmv *BigMapViewport) GetMapCoordOf(viewX, viewY float64) (float64, float64) {
	mapX := bmv.OriginMapX + (viewX-bmv.Left)/bmv.Scale
	mapY := bmv.OriginMapY + (viewY-bmv.Top)/bmv.Scale
	return mapX, mapY
}

// IsMapCoordInView reports whether a map coordinate is currently inside the viewport.
func (bmv *BigMapViewport) IsMapCoordInView(mapX, mapY float64) bool {
	viewX, viewY := bmv.GetScreenCoordOf(mapX, mapY)
	return bmv.IsViewCoordInView(viewX, viewY)
}

// IsViewCoordInView reports whether a screen coordinate is inside the viewport bounds.
func (bmv *BigMapViewport) IsViewCoordInView(viewX, viewY float64) bool {
	return viewX >= bmv.Left && viewX <= bmv.Right && viewY >= bmv.Top && viewY <= bmv.Bottom
}

func bigMapViewBounds(screenW, screenH int) (left int, top int, right int, bottom int) {
	padLR := int(math.Round(VIEWPORT_PADDING_LR))
	padTB := int(math.Round(VIEWPORT_PADDING_TB))
	left = max(0, min(screenW, padLR))
	right = max(0, min(screenW, screenW-padLR))
	top = max(0, min(screenH, padTB))
	bottom = max(0, min(screenH, screenH-padTB))
	return left, top, right, bottom
}
